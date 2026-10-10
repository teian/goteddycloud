// SPDX-License-Identifier: GPL-2.0-only
package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os/exec"
	"testing"
	"time"

	"goteddycloud/internal/taf"
)

func waveform(seconds int) []byte {
	frames := seconds * 48000
	data := make([]byte, 44+frames*4)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 2)
	binary.LittleEndian.PutUint32(data[24:], 48000)
	binary.LittleEndian.PutUint32(data[28:], 192000)
	binary.LittleEndian.PutUint16(data[32:], 4)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], uint32(frames*4))
	return data
}
func TestFiniteStreamAndResumeFinalizeValidTAF(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg unavailable")
	}
	source := func(context.Context) (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(waveform(20))), nil }
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	stream, err := StartStream(ctx, 1234, []StreamSource{source}, 1<<20, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if err := stream.WaitReady(ctx); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 4096)
	if n, err := stream.ReadAtContext(ctx, buffer, 0); err != nil || n != 4096 {
		t.Fatal(n, err)
	}
	if _, err := taf.ReadHeader(bytes.NewReader(buffer)); err != nil {
		t.Fatal(err)
	}
	<-stream.done
	var completed bytes.Buffer
	if err := stream.FinalCopy(&completed); err != nil {
		t.Fatal(err)
	}
	header, err := taf.Validate(bytes.NewReader(completed.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	original := append([]byte(nil), completed.Bytes()...)
	resumed, err := StartStream(ctx, 5678, []StreamSource{source}, 1<<20, Options{Resume: bytes.NewReader(original)})
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	<-resumed.done
	completed.Reset()
	if err := resumed.FinalCopy(&completed); err != nil {
		t.Fatal(err)
	}
	next, err := taf.Validate(bytes.NewReader(completed.Bytes()))
	if err != nil || next.GetAudioId() != 1234 || next.GetNumBytes() <= header.GetNumBytes() {
		t.Fatal(next, err)
	}
	if !bytes.Equal(original[4096:], completed.Bytes()[4096:len(original)]) {
		t.Fatal("resume changed old audio")
	}
}
func TestStreamCancellationReapsBlockedSource(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg unavailable")
	}
	reader, writer := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	source := func(ctx context.Context) (io.ReadCloser, error) {
		go func() { <-ctx.Done(); writer.CloseWithError(ctx.Err()) }()
		return reader, nil
	}
	stream, err := StartStream(ctx, 1234, []StreamSource{source}, 1<<20, Options{})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	done := make(chan struct{})
	go func() { stream.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("live codec/source leaked")
	}
}
