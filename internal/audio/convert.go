// SPDX-License-Identifier: GPL-2.0-only
package audio

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"goteddycloud/internal/taf"
)

// A bounded admission queue prevents conversion requests from spawning an
// unbounded number of subprocesses. Existing TAF delivery needs no codec.
var admission = make(chan struct{}, 10)
var encoders = make(chan struct{}, 2)

// Convert builds a TAF privately and publishes only after structural validation.
func Convert(ctx context.Context, destination string, id uint32, sources []string) error {
	return ConvertWithOptions(ctx, destination, id, sources, Options{})
}

// ConvertPCM consumes reference stereo 48kHz signed little-endian 16-bit samples.
func ConvertPCM(ctx context.Context, destination string, id uint32, source string) error {
	return convert(ctx, destination, id, []string{source}, true, Options{})
}

// Options applies the reference skip duration to the start of every source.
type Options struct {
	SkipSeconds uint64
	Append      bool
	Input       io.Reader
	// ConfineInput feeds local media through stdin and forbids secondary file/network references.
	ConfineInput  bool
	Bitrate       uint64
	Resume        io.ReadSeeker
	SweepDuration time.Duration
}

func ConvertWithOptions(ctx context.Context, destination string, id uint32, sources []string, options Options) error {
	return convert(ctx, destination, id, sources, false, options)
}
func convert(ctx context.Context, destination string, id uint32, sources []string, pcm bool, options Options) error {
	if len(sources) == 0 || len(sources) > 99 {
		return errors.New("expected 1–99 audio sources")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case admission <- struct{}{}:
	default:
		return errors.New("audio conversion queue is full")
	}
	defer func() { <-admission }()
	unlock, err := lockDestination(ctx, destination)
	if err != nil {
		return err
	}
	defer unlock()
	select {
	case encoders <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-encoders }()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		return errors.New("FFmpeg is required for conversion")
	}
	file, err := os.CreateTemp(filepath.Dir(destination), ".taf-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	var writer *taf.Writer
	if options.Append {
		original, openErr := os.Open(destination)
		if openErr == nil {
			writer, err = taf.AppendWriter(file, original)
			original.Close()
		} else if errors.Is(openErr, os.ErrNotExist) {
			writer, err = taf.NewWriter(file, id)
		} else {
			return openErr
		}
	} else {
		writer, err = taf.NewWriter(file, id)
	}
	if err != nil {
		return err
	}
	for _, source := range sources {
		if err := convertSource(ctx, ffmpeg, source, writer, pcm, options); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := taf.Validate(file); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return os.Rename(file.Name(), destination)
}
func convertSource(ctx context.Context, ffmpeg, source string, writer *taf.Writer, pcm bool, options Options) error {
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error"}
	if pcm {
		args = append(args, "-f", "s16le", "-ar", "48000", "-ac", "2")
	}
	// A valid local TAF contains a 4096-byte protobuf header before its Ogg
	// stream. Feed the validated payload directly, without a shell or tail process.
	var input *os.File
	if options.Input == nil && (!pcm || options.ConfineInput) {
		file, err := os.Open(source)
		if err != nil && options.ConfineInput {
			return err
		}
		if err == nil {
			_, tafErr := taf.Validate(file)
			if tafErr == nil || options.ConfineInput {
				offset := int64(0)
				if tafErr == nil {
					offset = taf.BlockSize
				}
				if _, err := file.Seek(offset, io.SeekStart); err != nil {
					file.Close()
					return err
				}
				input = file
				defer input.Close()
				source = "pipe:0"
			} else {
				file.Close()
			}
		}
	}
	if options.Input != nil {
		source = "pipe:0"
	}
	if options.Input != nil || options.ConfineInput {
		args = append(args, "-protocol_whitelist", "pipe")
	}
	bitrate := options.Bitrate
	if bitrate == 0 {
		bitrate = 96
	}
	args = append(args, "-i", source, "-ss", strconv.FormatUint(options.SkipSeconds, 10), "-vn", "-ar", "48000", "-ac", "2", "-c:a", "libopus", "-frame_duration", "60", "-b:a", strconv.FormatUint(bitrate, 10)+"k", "-f", "ogg", "pipe:1")
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	if options.Input != nil {
		cmd.Stdin = options.Input
	}
	if input != nil {
		cmd.Stdin = input
	}

	cmd.WaitDelay = 2 * time.Second
	stderr := &boundedBuffer{limit: 4096}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	sweepUntil := time.Now().Add(options.SweepDuration)
	parseErr := func() error {
		reader := taf.NewOggReader(stdout)
		head, err := reader.Next()
		if err != nil {
			return err
		}
		if err := writer.StartTrack(head); err != nil {
			return err
		}
		tags, err := reader.Next()
		if err != nil {
			return err
		}
		if !bytes.HasPrefix(tags, []byte("OpusTags")) {
			return errors.New("missing OpusTags")
		}
		for {
			packet, err := reader.Next()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
			if options.SweepDuration > 0 && time.Now().Before(sweepUntil) {
				continue
			}
			if err := writer.Packet(packet); err != nil {
				return err
			}
		}
	}()
	if parseErr != nil {
		_ = stdout.Close()
		_ = cmd.Process.Kill()
	}
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if parseErr != nil {
		return fmt.Errorf("invalid Opus stream: %w", parseErr)
	}
	if waitErr != nil {
		return fmt.Errorf("FFmpeg failed: %w (%s)", waitErr, stderr.String())
	}
	return nil
}

type boundedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	remain := b.limit - b.Len()
	if remain > 0 {
		if remain > len(p) {
			remain = len(p)
		}
		_, _ = b.Buffer.Write(p[:remain])
	}
	return n, nil
}
