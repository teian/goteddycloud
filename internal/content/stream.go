// SPDX-License-Identifier: GPL-2.0-only
package content

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"

	"path/filepath"
	"strings"
	"time"

	"goteddycloud/internal/audio"
	"goteddycloud/internal/config"
	"goteddycloud/internal/fetchpolicy"
	"goteddycloud/internal/storage"
	"goteddycloud/internal/taf"
)

type StreamPlan struct {
	Sources       []audio.StreamSource
	TargetRoot    *storage.Root
	TargetName    string
	AudioID       uint32
	Playlist      bool
	Remote        bool
	Growing       storage.File
	GrowingLength int64
}

func streamURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" && parsed.User == nil
}

// StreamPlan selects ordinary media/HTTP radio, uncached TAP, or an explicitly
// incomplete TAF. Complete stored TAF delivery keeps using the immutable path.
func (s *Store) PrepareStream(ctx context.Context, snap, global config.Snapshot, ruid string) (*StreamPlan, error) {
	metadata, err := s.MetadataContext(ctx, snap, ruid)
	if err != nil {
		return nil, err
	}
	source := metadata.String("source")
	if source == "" {
		name, err := UIDPath(ruid)
		if err != nil {
			return nil, err
		}
		source = "content://" + name
	}
	name, err := UIDPath(ruid)
	if err != nil {
		return nil, err
	}
	root, err := s.root(snap.String("internal.contentdirfull"))
	if err != nil {
		return nil, err
	}
	plan := &StreamPlan{TargetRoot: root, TargetName: name + ".stream", AudioID: uint32(time.Now().Unix() - 0x50000000)}
	sourceNames := []string{source}
	if !streamURL(source) {
		path, relative, err := ResolveSource(snap, global, source)
		if err != nil {
			return nil, err
		}
		inputRoot, err := s.root(path)
		if err != nil {
			return nil, err
		}
		input, err := inputRoot.WithContext(ctx).Open(relative)
		if err != nil {
			return nil, err
		}
		info, err := input.Stat()
		if err != nil || !info.Mode().IsRegular() {
			input.Close()
			return nil, errors.New("stream source is not a regular file")
		}
		header, headerErr := taf.ReadHeader(input)
		if headerErr == nil {
			if header.GetNumBytes() > uint64(max(0, info.Size()-taf.BlockSize)) {
				if _, remote := s.backend.(*storage.S3); remote {
					input.Close()
					return nil, errors.New("externally growing TAF requires filesystem storage")
				}
				plan.Growing = input
				plan.GrowingLength = int64(header.GetNumBytes()) + taf.BlockSize
				return plan, nil
			}
			input.Close()
			return nil, nil
		}
		input.Close()
		if strings.EqualFold(filepath.Ext(relative), ".tap") {
			playlist, err := s.readPlaylist(snap, global, source)
			if err != nil {
				return nil, err
			}
			path, name, err := ResolveSource(snap, global, playlist.Filepath)
			if err != nil {
				return nil, err
			}
			outputRoot, err := s.root(path)
			if err != nil {
				return nil, err
			}
			if file, err := outputRoot.Open(name); err == nil {
				header, valid := taf.Validate(file)
				file.Close()
				if valid == nil && header.GetAudioId() == playlist.AudioID {
					return nil, nil
				}
			}
			plan.Playlist = true
			plan.TargetRoot = outputRoot
			plan.TargetName = name
			plan.AudioID = playlist.AudioID
			sourceNames = nil
			for _, file := range playlist.Files {
				sourceNames = append(sourceNames, file.Filepath)
			}
		}
	}
	for _, source := range sourceNames {
		if streamURL(source) {
			plan.Remote = true
			plan.Sources = append(plan.Sources, remoteStream(source, snap))
			continue
		}
		path, name, err := ResolveSource(snap, global, source)
		if err != nil {
			return nil, err
		}
		root, err := s.root(path)
		if err != nil {
			return nil, err
		}
		plan.Sources = append(plan.Sources, func(ctx context.Context) (io.ReadCloser, error) {
			file, err := root.WithContext(ctx).Open(name)
			if err != nil {
				return nil, err
			}
			info, err := file.Stat()
			if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<30 {
				file.Close()
				return nil, errors.New("stream source bounds")
			}
			if _, err := taf.Validate(playlistReader{ctx, file}); err == nil {
				_, err = file.Seek(taf.BlockSize, io.SeekStart)
				if err != nil {
					file.Close()
					return nil, err
				}
			} else {
				file.Seek(0, io.SeekStart)
			}
			return file, nil
		})
	}
	return plan, nil
}

type deadlineConn struct{ net.Conn }

func (c deadlineConn) Read(data []byte) (int, error) {
	c.Conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	return c.Conn.Read(data)
}
func remoteStream(source string, snap config.Snapshot) audio.StreamSource {
	return func(ctx context.Context) (io.ReadCloser, error) {
		dialer := &net.Dialer{Timeout: 5 * time.Second}
		dial := dialer.DialContext
		if _, scoped := snap.Storage().(*storage.Scoped); scoped {
			dial = fetchpolicy.Dial(func() bool { return snap.Bool("core.allowPrivateFetch") })
		}
		transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, MaxResponseHeaderBytes: 32 << 10, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			conn, err := dial(ctx, network, address)
			if err != nil {
				return nil, err
			}
			return deadlineConn{conn}, nil
		}}
		client := &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 || !streamURL(req.URL.String()) || req.URL.Scheme != via[0].URL.Scheme || req.URL.Host != via[0].URL.Host {
				return errors.New("radio redirect rejected")
			}
			return nil
		}}
		request, err := http.NewRequestWithContext(ctx, "GET", source, nil)
		if err != nil {
			return nil, errors.New("invalid radio URL")
		}
		response, err := client.Do(request)
		if err != nil {
			transport.CloseIdleConnections()
			return nil, errors.New("radio transport failed")
		}
		if response.StatusCode != 200 {
			response.Body.Close()
			transport.CloseIdleConnections()
			return nil, errors.New("radio HTTP failure")
		}
		return &remoteBody{reader: io.LimitReader(response.Body, 1<<30), body: response.Body, transport: transport}, nil
	}
}

type remoteBody struct {
	reader    io.Reader
	body      io.Closer
	transport *http.Transport
}

func (r *remoteBody) Read(data []byte) (int, error) { return r.reader.Read(data) }
func (r *remoteBody) Close() error {
	err := r.body.Close()
	r.transport.CloseIdleConnections()
	return err
}
