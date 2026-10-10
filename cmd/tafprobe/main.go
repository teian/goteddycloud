// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"goteddycloud/internal/audio"
	"goteddycloud/internal/taf"
)

func main() {
	destination := flag.String("output", "", "new generated TAF (empty validates positional TAF)")
	id := flag.Uint("audio-id", 123456, "synthetic audio ID")
	flag.Parse()
	if *destination != "" {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := audio.Convert(ctx, *destination, uint32(*id), flag.Args()); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		show(*destination)
		return
	}
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: tafprobe [-output file.taf] source...")
		os.Exit(2)
	}
	show(flag.Arg(0))
}
func show(path string) {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer f.Close()
	header, err := taf.Validate(f)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"audio_id": header.GetAudioId(), "num_bytes": header.GetNumBytes(), "tracks": header.TrackPageNums, "sha1": fmt.Sprintf("%x", header.Sha1Hash)})
}
