// SPDX-License-Identifier: GPL-2.0-only
package audio

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"goteddycloud/internal/taf"
)

type StreamSource func(context.Context) (io.ReadCloser, error)

// Stream owns a bounded private spool. Readers see only completely written data;
// no provisional file is published as complete content.
type Stream struct {
	mu            sync.Mutex   // committed state and notifications only
	ioMu          sync.RWMutex // file lifetime and header rewrites
	position      int64        // encoder write cursor
	closeOnce     sync.Once
	closeErr      error
	file          *os.File
	size, maximum int64
	changed       chan struct{}
	done          chan struct{}
	finished      bool
	initialized   bool
	err           error
	cancel        context.CancelFunc
}

func StartStream(parent context.Context, id uint32, sources []StreamSource, maximum int64, options Options) (*Stream, error) {
	if len(sources) == 0 || len(sources) > 99 || maximum < 8192 || maximum > 1<<31-1 {
		return nil, errors.New("live source/size bounds")
	}
	select {
	case admission <- struct{}{}:
	default:
		return nil, errors.New("audio conversion queue is full")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		<-admission
		return nil, errors.New("FFmpeg is required for live conversion")
	}
	file, err := os.CreateTemp("", "goteddycloud-live-*.taf")
	if err != nil {
		<-admission
		return nil, err
	}
	ctx, cancel := context.WithCancel(parent)
	s := &Stream{file: file, maximum: maximum, changed: make(chan struct{}), done: make(chan struct{}), cancel: cancel}
	go func() {
		defer close(s.done)
		defer func() { <-admission }()
		var result error
		select {
		case encoders <- struct{}{}:
			defer func() { <-encoders }()
		case <-ctx.Done():
			s.finish(ctx.Err())
			return
		}
		var writer *taf.Writer
		var err error
		if options.Resume != nil {
			writer, err = taf.AppendWriter(s, options.Resume)
		} else {
			writer, err = taf.NewWriter(s, id)
		}
		if err != nil {
			s.finish(err)
			return
		}
		if err := writer.StreamHeader(uint64(maximum - taf.BlockSize)); err != nil {
			s.finish(err)
			return
		}
		s.mu.Lock()
		s.initialized = true
		close(s.changed)
		s.changed = make(chan struct{})
		s.mu.Unlock()
		for _, source := range sources {
			input, err := source(ctx)
			if err != nil {
				result = err
				break
			}
			options.Input = input
			err = convertSource(ctx, ffmpeg, "pipe:0", writer, false, options)
			input.Close()
			if err != nil {
				result = err
				break
			}
		}
		// Finalize even cancelled streams, so a retained finite prefix can be
		// validated independently. Never use a failed Close as a complete artifact.
		closeErr := writer.Close()
		if closeErr != nil {
			result = closeErr
		}
		s.finish(result)
	}()
	return s, nil
}
func (s *Stream) finish(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
	s.finished = true
	close(s.changed)
	s.changed = make(chan struct{})
}

// The encoder owns writes/seeks. ioMu prevents readers from observing a
// partially rewritten header, without holding the committed-state mutex for I/O.
func (s *Stream) Write(data []byte) (int, error) {
	s.ioMu.Lock()
	defer s.ioMu.Unlock()
	if int64(len(data)) > s.maximum-s.position {
		return 0, errors.New("stream size limit")
	}
	n, err := s.file.WriteAt(data, s.position)
	s.position += int64(n)
	s.mu.Lock()
	s.size = max(s.size, s.position)
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
	return n, err
}
func (s *Stream) Seek(offset int64, whence int) (int64, error) {
	s.ioMu.Lock()
	defer s.ioMu.Unlock()
	position := offset
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		position += s.position
	case io.SeekEnd:
		info, err := s.file.Stat()
		if err != nil {
			return 0, err
		}
		position += info.Size()
	default:
		return 0, errors.New("invalid seek whence")
	}
	if position < 0 {
		return 0, errors.New("negative stream offset")
	}
	s.position = position
	return position, nil
}
func (s *Stream) WaitReady(ctx context.Context) error {
	for {
		s.mu.Lock()
		size, done, err, changed := s.size, s.finished, s.err, s.changed
		initialized := s.initialized
		s.mu.Unlock()
		if initialized && size >= 8192 {
			return nil
		}
		if done {
			if err != nil {
				return err
			}
			return errors.New("live source contains no complete audio block")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-changed:
		}
	}
}

// ReadAtContext waits for growing data, with an idle deadline and request stop.
func (s *Stream) ReadAtContext(ctx context.Context, data []byte, offset int64) (int, error) {
	if offset < 0 {
		return 0, errors.New("negative live offset")
	}
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		s.mu.Lock()
		size, done, err, changed := s.size, s.finished, s.err, s.changed
		if offset < size {
			s.mu.Unlock()
			s.ioMu.RLock()
			n, readErr := s.file.ReadAt(data[:min(int64(len(data)), size-offset)], offset)
			s.ioMu.RUnlock()
			return n, readErr
		}
		s.mu.Unlock()
		if done {
			if err != nil {
				return 0, err
			}
			return 0, io.EOF
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-timer.C:
			return 0, errors.New("live source idle timeout")
		case <-changed:
		}
	}
}

// Done closes after final header publication and encoder cleanup.
func (s *Stream) Done() <-chan struct{} { return s.done }

func (s *Stream) Close() error {
	s.closeOnce.Do(func() {
		s.cancel()
		<-s.done
		s.ioMu.Lock()
		defer s.ioMu.Unlock()
		name := s.file.Name()
		s.closeErr = errors.Join(s.file.Close(), os.Remove(name))
	})
	return s.closeErr
}

// FinalCopy validates the completed spool and uses an independent read cursor.
// A slow destination never holds the state or file lock between reads.
func (s *Stream) FinalCopy(destination io.Writer) error {
	reader, err := s.FinalReader()
	if err != nil {
		return err
	}
	_, err = io.Copy(destination, reader)
	return err
}

func (s *Stream) Stop() { s.cancel(); <-s.done }

// spoolReader keeps file closure coordinated with each individual read. Readers
// remain independent; after Close, further reads return the file's closed error.
type spoolReader struct {
	stream *Stream
	reader *io.SectionReader
}

func (r *spoolReader) Read(data []byte) (int, error) {
	r.stream.ioMu.RLock()
	defer r.stream.ioMu.RUnlock()
	return r.reader.Read(data)
}
func (s *Stream) FinalReader() (io.Reader, error) {
	<-s.done
	s.ioMu.RLock()
	defer s.ioMu.RUnlock()
	// done publishes size/error and guarantees the encoder no longer changes them.
	if _, err := taf.Validate(io.NewSectionReader(s.file, 0, s.size)); err != nil {
		return nil, err
	}
	return &spoolReader{stream: s, reader: io.NewSectionReader(s.file, 0, s.size)}, nil
}
func (s *Stream) Result() error { <-s.done; return s.err }
