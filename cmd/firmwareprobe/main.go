// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"goteddycloud/internal/firmware"
)

func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: firmwareprobe IMAGE")
	}
	file, err := os.Open(os.Args[1])
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, firmware.MaxImage+1))
	if err != nil {
		return err
	}
	image, err := firmware.Parse(data)
	if err != nil {
		return err
	}
	fat, err := image.Assets()
	if err != nil {
		return err
	}
	files, err := fat.Extract("CERT")
	if err != nil {
		return err
	}
	hashes := map[string]string{}
	for name, data := range files {
		hashes[name] = fmt.Sprintf("%x", sha256.Sum256(data))
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"partitions": image.Partitions, "certificate_hashes": hashes})
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
