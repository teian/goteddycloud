// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"goteddycloud/internal/storage"
	"io"
	"net/http"

	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"goteddycloud/internal/audio"
	"goteddycloud/internal/config"
	"goteddycloud/internal/content"
	"goteddycloud/internal/taf"
)

func (s *Server) serveLive(w http.ResponseWriter, r *http.Request, id string, snap config.Snapshot, ruid string, metadata content.Metadata) bool {
	plan, err := s.Content.PrepareStream(r.Context(), snap, s.Settings.Snapshot(""), ruid)
	if err != nil || plan == nil {
		return false
	}
	if plan.Growing != nil {
		defer plan.Growing.Close()
	}
	if !plan.Playlist && plan.Growing == nil && id != "" {
		plan.TargetName += "." + id
	}
	ctx, release := s.Commands.Begin(r.Context(), id)
	defer release()
	unlock, err := plan.TargetRoot.LockContext(ctx, plan.TargetName)
	if err != nil {
		response(w, 503, "", nil)
		return true
	}
	releaseTarget := sync.OnceFunc(unlock)
	defer releaseTarget()
	// Preparation can race another generation. Recheck after acquiring the
	// shared destination, then let ordinary stored delivery serve the completed file.
	if plan.Playlist {
		if file, err := plan.TargetRoot.Open(plan.TargetName); err == nil {
			header, err := taf.Validate(file)
			file.Close()
			if err == nil && header.GetAudioId() == plan.AudioID {
				return false
			}
		}
	}
	maximum := int64(snap.Uint("encode.stream_max_size"))
	offset := int64(0)
	if strings.HasPrefix(r.URL.Query().Get("skip_header"), "t") {
		offset = taf.BlockSize
	}
	start, end := int64(0), maximum-1
	if raw, ok := strings.CutPrefix(r.Header.Get("Range"), "bytes="); ok {
		first, last, _ := strings.Cut(raw, "-")
		n, err := strconv.ParseInt(first, 10, 64)
		if err != nil || n < 0 || n >= maximum {
			response(w, 416, "", nil)
			return true
		}
		if n > 0 {
			start = n
			if n, err := strconv.ParseInt(last, 10, 64); err == nil && n > 0 && n < end {
				end = n
			}
		}
	}
	if end < start {
		response(w, 416, "", nil)
		return true
	}
	var restartFile storage.File
	if start > 0 && !plan.Playlist && plan.Growing == nil && snap.Bool("encode.ffmpeg_stream_restart") {
		restartFile, _ = plan.TargetRoot.Open(plan.TargetName)
		if restartFile != nil {
			defer restartFile.Close()
			if info, err := restartFile.Stat(); err == nil {
				length := info.Size() - offset
				if start >= length {
					start = 0
					end = maximum - 1
				} else if end >= length {
					end = length - 1
				}
			}
		}
	}
	var stream *audio.Stream
	var read func([]byte, int64) (int, error)
	if plan.Growing != nil {
		releaseTarget()
		read = func(buffer []byte, position int64) (int, error) {
			idle := time.NewTimer(30 * time.Second)
			defer idle.Stop()
			tick := time.NewTicker(100 * time.Millisecond)
			defer tick.Stop()
			for {
				n, err := plan.Growing.ReadAt(buffer, position)
				if n > 0 {
					return n, nil
				}
				if info, statErr := plan.Growing.Stat(); statErr == nil {
					if header, headerErr := taf.ReadHeader(io.NewSectionReader(plan.Growing, 0, taf.BlockSize)); headerErr == nil && header.GetNumBytes()+taf.BlockSize <= uint64(info.Size()) {
						return 0, io.EOF
					}
				}
				if err != io.EOF {
					return n, err
				}
				select {
				case <-ctx.Done():
					return 0, ctx.Err()
				case <-idle.C:
					return 0, io.EOF
				case <-tick.C:
				}
			}
		}
	} else if restartFile != nil {
		releaseTarget()
		read = restartFile.ReadAt
	} else {
		options := audio.Options{Bitrate: snap.Uint("encode.bitrate")}
		if plan.Remote && !plan.Playlist && snap.Bool("encode.ffmpeg_sweep_startup_buffer") {
			options.SweepDuration = time.Duration(snap.Uint("encode.ffmpeg_sweep_delay_ms")) * time.Millisecond
		}
		_ = json.Unmarshal(metadata["skip_seconds"], &options.SkipSeconds)
		var original storage.File
		closeOriginal := sync.OnceFunc(func() {
			if original != nil {
				_ = original.Close()
			}
		})
		defer closeOriginal()
		if start > 0 && !plan.Playlist && !snap.Bool("encode.ffmpeg_stream_restart") {
			original, _ = plan.TargetRoot.Open(plan.TargetName)
			if original != nil {
				options.Resume = original
			}
		}
		stream, err = audio.StartStream(ctx, plan.AudioID, plan.Sources, maximum, options)
		if err != nil {
			response(w, 503, "", nil)
			return true
		}
		defer stream.Close()
		// Publication belongs to the encoder, not the response writer. A slow
		// client cannot hold the shared generation lock after encoding completes.
		published := make(chan struct{})
		go func() {
			defer close(published)
			defer releaseTarget()
			<-stream.Done()
			closeOriginal() // release the continuation before replacing it on Windows
			if plan.Playlist && stream.Result() != nil {
				return
			}
			reader, err := stream.FinalReader()
			if err != nil {
				return
			}
			if err := plan.TargetRoot.MkdirAll(filepath.Dir(plan.TargetName), 0700); err != nil {
				return
			}
			publish, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = plan.TargetRoot.WriteReaderAtomic(publish, plan.TargetName, reader, 0600, maximum)
		}()
		defer func() { stream.Stop(); <-published }()

		if err := stream.WaitReady(ctx); err != nil {
			response(w, 502, "", nil)
			return true
		}
		if delay := snap.Uint("encode.ffmpeg_stream_buffer_ms"); delay > 0 {
			timer := time.NewTimer(time.Duration(delay) * time.Millisecond)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				response(w, 502, "", nil)
				return true
			case <-timer.C:
			}
		}
		read = func(buffer []byte, position int64) (int, error) { return stream.ReadAtContext(ctx, buffer, position) }
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	status := 200
	if start > 0 {
		status = 206
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, maximum))
	}
	w.WriteHeader(status)
	controller := http.NewResponseController(w)
	_ = controller.Flush()
	buffer := make([]byte, 32768)
	for position := start; position <= end; {
		if ctx.Err() != nil {
			break
		}
		n, err := read(buffer[:min(int64(len(buffer)), end-position+1)], offset+position)
		if n > 0 {
			_ = controller.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
				break
			}
			_ = controller.Flush()
			position += int64(n)
		}
		if err != nil || n == 0 {
			break
		}
	}
	return true
}
