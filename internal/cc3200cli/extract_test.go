// SPDX-License-Identifier: GPL-2.0-only
package cc3200cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"goteddycloud/internal/cc3200"
)

func TestExtractionSavesAllEightFilesAndContinuesAfterUnavailableOptionalFile(t *testing.T) {
	// Match the real failure: pref.net precedes certificates in FAT order.
	entries := []cc3200.Entry{{ID: 17, Name: "/sys/pref.net"}}
	names := []string{
		"/cert/ca.der", "/cert/private.der", "/cert/client.der",
		"/sys/mcuimg.bin", "/sys/mcuimg1.bin", "/sys/mcuimg2.bin",
		"/sys/mcuimg3.bin", "/sys/mcubootinfo.bin",
	}
	for i, name := range names {
		entries = append(entries, cc3200.Entry{ID: i, Name: name})
	}
	entries = append(entries, cc3200.Entry{ID: 29, Name: "/settings.cfg"})
	directory := t.TempDir()
	counts := map[string]int{}
	var order []string
	var log bytes.Buffer
	err := extractFiles(context.Background(), entries, operation{args: []string{directory}}, false, func(name string, id int) ([]byte, error) {
		if id != -1 {
			t.Fatal("expected named device reads")
		}
		counts[name]++
		order = append(order, name)
		if name == "/sys/pref.net" {
			return nil, os.ErrNotExist
		}
		return []byte("payload:" + name), nil
	}, newTerminal(&log, "never", false))
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range append(names, "/settings.cfg") {
		if counts[name] != 2 {
			t.Errorf("%s: expected two independent reads, got %d", name, counts[name])
		}
		if i < len(names) && order[2*i] != name {
			t.Errorf("priority file %s was read out of order", name)
		}
		p := filepath.Join(directory, strings.TrimPrefix(name, "/"))
		data, err := os.ReadFile(p)
		if err != nil || !bytes.Equal(data, []byte("payload:"+name)) {
			t.Fatalf("%s: %q, %v", name, data, err)
		}
		info, err := os.Stat(p)
		if err != nil || info.Mode().Perm()&0077 != 0 {
			t.Fatalf("%s: output is not private: %v", name, err)
		}
	}
	if !strings.Contains(log.String(), "Unavailable device file \"/sys/pref.net\"") {
		t.Fatal("missing diagnostic for skipped file")
	}
}

func TestExtractionProbesPriorityFilesMissingFromFAT(t *testing.T) {
	directory := t.TempDir()
	var calls int
	err := extractFiles(context.Background(), nil, operation{args: []string{directory}}, false, func(name string, _ int) ([]byte, error) {
		calls++
		if name == "/cert/private.der" {
			return []byte("test key"), nil
		}
		return nil, os.ErrNotExist
	}, newTerminal(io.Discard, "never", false))
	if err != nil || calls != 9 {
		t.Fatal(calls, err)
	}
	if data, err := os.ReadFile(filepath.Join(directory, "cert/private.der")); err != nil || string(data) != "test key" {
		t.Fatal("did not recover unlisted key", err)
	}
}

func TestExtractionReportsUnavailableListedPriorityFileAfterSavingOthers(t *testing.T) {
	directory := t.TempDir()
	entries := []cc3200.Entry{{ID: 1, Name: "/cert/ca.der"}, {ID: 2, Name: "/cert/private.der"}}
	err := extractFiles(context.Background(), entries, operation{args: []string{directory}}, false, func(name string, _ int) ([]byte, error) {
		if name == "/cert/private.der" {
			return []byte("test key"), nil
		}
		return nil, os.ErrNotExist
	}, newTerminal(io.Discard, "never", false))
	if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "/cert/ca.der") {
		t.Fatal("did not report missing listed priority file", err)
	}
	if _, err := os.Stat(filepath.Join(directory, "cert/private.der")); err != nil {
		t.Fatal("stopped before saving remaining files", err)
	}
}

func TestExtractionStopsOnTransportAndVerificationFailures(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(map[bool]string{false: "transport", true: "verification"}[mismatch], func(t *testing.T) {
			directory := t.TempDir()
			var calls int
			err := extractFiles(context.Background(), nil, operation{args: []string{directory}}, false, func(string, int) ([]byte, error) {
				calls++
				if mismatch {
					return []byte{byte(calls)}, nil
				}
				return nil, context.DeadlineExceeded
			}, newTerminal(io.Discard, "never", false))
			if err == nil || calls != map[bool]int{false: 1, true: 2}[mismatch] {
				t.Fatal("continued after failure", calls, err)
			}
			if _, err := os.Stat(filepath.Join(directory, "cert/ca.der")); !os.IsNotExist(err) {
				t.Fatal("saved unverified file", err)
			}
		})
	}
}
