// SPDX-License-Identifier: GPL-2.0-only
package audio

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"goteddycloud/internal/fixtures"
	"goteddycloud/internal/taf"
)

func TestConversionsPreserveValidFilesOnFailure(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg unavailable")
	}
	base := filepath.Join(t.TempDir(), "fixture")
	if err := fixtures.Generate(base); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(base, "output.taf")
	sources := []string{filepath.Join(base, "tone1.wav"), filepath.Join(base, "tone2.wav")}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := Convert(ctx, destination, 123456, sources); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	h, err := taf.Validate(file)
	file.Close()
	if err != nil || len(h.TrackPageNums) != 2 {
		t.Fatal(h, err)
	}
	wav, err := os.ReadFile(sources[0])
	if err != nil {
		t.Fatal(err)
	}
	pcm := filepath.Join(base, "samples.pcm")
	if err := os.WriteFile(pcm, wav[44:], 0600); err != nil {
		t.Fatal(err)
	}
	pcmOutput := filepath.Join(base, "pcm.taf")
	if err := ConvertPCM(ctx, pcmOutput, 123, pcm); err != nil {
		t.Fatal(err)
	}
	pcmFile, err := os.Open(pcmOutput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := taf.Validate(pcmFile); err != nil {
		t.Fatal(err)
	}
	pcmFile.Close()
	before, _ := os.ReadFile(destination)
	if err := Convert(ctx, destination, 123, []string{filepath.Join(base, "missing audio;$(false).wav")}); err == nil {
		t.Fatal("missing source accepted")
	}
	after, _ := os.ReadFile(destination)
	if !bytes.Equal(before, after) {
		t.Fatal("valid output replaced after error")
	}
	leftovers, _ := filepath.Glob(filepath.Join(base, ".taf-*.tmp"))
	if len(leftovers) > 0 {
		t.Fatal(leftovers)
	}
	var wg sync.WaitGroup
	for _, name := range []string{"one.taf", "two.taf"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := Convert(ctx, filepath.Join(base, name), 123, sources[:1]); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}
func TestMissingCodecAndCancellation(t *testing.T) {
	directory := t.TempDir()
	destination := filepath.Join(directory, "output.taf")
	t.Setenv("PATH", directory)
	if err := Convert(context.Background(), destination, 123, []string{"source"}); err == nil {
		t.Fatal("missing codec accepted")
	}
	if runtime.GOOS == "windows" {
		return
	}
	// exec replaces the test shell, leaving a single owned sleeping process.
	if err := os.WriteFile(filepath.Join(directory, "ffmpeg"), []byte("#!/bin/sh\nexec /bin/sleep 60\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := Convert(ctx, destination, 123, []string{"source"}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("process cancellation exceeded bound")
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled result published")
	}
	leftovers, _ := filepath.Glob(filepath.Join(directory, ".taf-*.tmp"))
	if len(leftovers) > 0 {
		t.Fatal(leftovers)
	}
}

func TestSkipAndTAFInput(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg unavailable")
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "three seconds.wav")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if out, err := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-loglevel", "error", "-f", "lavfi", "-i", "sine=frequency=440:duration=3", source).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	full := filepath.Join(dir, "full.taf")
	short := filepath.Join(dir, "short.taf")
	if err := Convert(ctx, full, 123, []string{source}); err != nil {
		t.Fatal(err)
	}
	if err := ConvertWithOptions(ctx, short, 124, []string{full, source}, Options{SkipSeconds: 1}); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(short)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	h, err := taf.Validate(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.TrackPageNums) != 2 {
		t.Fatalf("chapters: %v", h.TrackPageNums)
	}
	// Both three-second sources must lose one second, within Opus frame rounding.
	seconds := float64(h.GetOggGranulePosition()) / 48000
	if seconds < 4 || seconds > 4.3 {
		t.Fatalf("seek duration: %f", seconds)
	}
}

func TestAppendPreservesPayloadAndIdentity(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg unavailable")
	}
	base := filepath.Join(t.TempDir(), "fixture")
	if err := fixtures.Generate(base); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(base, "append.taf")
	source := filepath.Join(base, "tone1.wav")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := Convert(ctx, destination, 123, []string{source}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if err := ConvertWithOptions(ctx, destination, 999, []string{source}, Options{Append: true}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(destination)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) <= len(before) || !bytes.Equal(before[taf.BlockSize:], after[taf.BlockSize:len(before)]) {
		t.Fatal("append changed existing payload")
	}
	h, err := taf.Validate(bytes.NewReader(after))
	if err != nil {
		t.Fatal(err)
	}
	if h.GetAudioId() != 123 || len(h.TrackPageNums) != 1 || h.TrackPageNums[0] != uint32(len(before)/taf.BlockSize-1) {
		t.Fatalf("reference append identity/chapters: %v", h)
	}
	if err := taf.CheckPages(bytes.NewReader(after[taf.BlockSize:])); err != nil {
		t.Fatal(err)
	}
	if err := ConvertWithOptions(ctx, destination, 999, []string{filepath.Join(base, "missing")}, Options{Append: true}); err == nil {
		t.Fatal("missing append source accepted")
	}
	preserved, _ := os.ReadFile(destination)
	if !bytes.Equal(after, preserved) {
		t.Fatal("failed append replaced valid original")
	}
}

func TestConcurrentAppendsRetainEveryPayload(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg unavailable")
	}
	base := filepath.Join(t.TempDir(), "fixture")
	if err := fixtures.Generate(base); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "append.taf")
	source := filepath.Join(base, "tone1.wav")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := Convert(ctx, path, 123, []string{source}); err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	initial, err := taf.Validate(bytes.NewReader(original))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := ConvertWithOptions(ctx, path, 999, []string{source}, Options{Append: true}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	header, err := taf.Validate(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if header.GetAudioId() != 123 || header.GetOggGranulePosition() != 3*initial.GetOggGranulePosition() {
		t.Fatal("append lost a concurrent writer", header)
	}
	if !bytes.Equal(original[taf.BlockSize:], data[taf.BlockSize:len(original)]) {
		t.Fatal("original payload changed")
	}
	destinations.Lock()
	defer destinations.Unlock()
	if len(destinations.entries) != 0 {
		t.Fatal("destination locks leaked")
	}
}

func TestDestinationWaitCancellationReleasesReferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out")
	release, err := lockDestination(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if unlock, err := lockDestination(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		if unlock != nil {
			unlock()
		}
		t.Fatal(err)
	}
	release()
	destinations.Lock()
	defer destinations.Unlock()
	if len(destinations.entries) != 0 {
		t.Fatal("canceled destination lock leaked")
	}
}

func TestConfinedConversionRejectsSecondaryResources(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("FFmpeg unavailable")
	}
	base := filepath.Join(t.TempDir(), "fixture")
	if err := fixtures.Generate(base); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	destination := filepath.Join(base, "confined.taf")
	if err := ConvertWithOptions(ctx, destination, 123, []string{filepath.Join(base, "tone1.wav"), filepath.Join(base, "tone2.wav")}, Options{ConfineInput: true}); err != nil {
		t.Fatal("ordinary media failed", err)
	}
	// Both ordinary and TAF media must still work without secondary protocols.
	if err := ConvertWithOptions(ctx, filepath.Join(base, "again.taf"), 123, []string{destination}, Options{ConfineInput: true}); err != nil {
		t.Fatal("TAF media failed", err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(404) }))
	defer server.Close()
	playlist := filepath.Join(base, "external.m3u8")
	data := "#EXTM3U\n#EXT-X-TARGETDURATION:10\n#EXT-X-MEDIA-SEQUENCE:0\n#EXTINF:10,\n" + server.URL + "/secret.ts\n#EXT-X-ENDLIST\n"
	if err := os.WriteFile(playlist, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := ConvertWithOptions(ctx, filepath.Join(base, "unsafe.taf"), 123, []string{playlist}, Options{ConfineInput: true}); err == nil {
		t.Fatal("external playlist accepted")
	}
	if calls.Load() != 0 {
		t.Fatal("FFmpeg contacted secondary resource")
	}
}
