// SPDX-License-Identifier: GPL-2.0-only
package cc3200cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"goteddycloud/internal/cc3200"
)

// Save identity and boot files before optional files that may be unavailable
// through the device API even though they are still listed in the FAT.
func extractionPriority() []string {
	return []string{
		"/cert/ca.der", "/cert/private.der", "/cert/client.der",
		"/sys/mcuimg.bin", "/sys/mcuimg1.bin", "/sys/mcuimg2.bin",
		"/sys/mcuimg3.bin", "/sys/mcubootinfo.bin",
	}
}

func extractFiles(ctx context.Context, entries []cc3200.Entry, op operation, offline bool, readFile func(string, int) ([]byte, error), t *terminal) error {
	root, err := openExtractionDirectory(op.args[0])
	if err != nil {
		return err
	}
	defer root.Close()
	priority := map[string]bool{}
	var ordered []cc3200.Entry
	if !op.allByID {
		for _, name := range extractionPriority() {
			priority[name] = true
			found := false
			for _, entry := range entries {
				if entry.Name == name {
					ordered = append(ordered, entry)
					found = true
					break
				}
			}
			// Live named reads can recover files whose FAT names are unknown.
			if !found && !offline && !op.inactive {
				ordered = append(ordered, cc3200.Entry{ID: -1, Name: name})
			}
		}
	}
	for _, entry := range entries {
		if !priority[entry.Name] {
			ordered = append(ordered, entry)
		}
	}
	var missing []error
	for _, entry := range ordered {
		if err = ctx.Err(); err != nil {
			return err
		}
		if entry.Name == "/sys/servicepack.ucf" {
			t.status("Skipping /sys/servicepack.ucf (upstream extraction blacklist)")
			continue
		}
		name, id := strings.TrimPrefix(entry.Name, "/"), -1
		if op.allByID || entry.Name == "" && op.byID {
			name, id = fmt.Sprint(entry.ID), entry.ID
		} else if entry.Name == "" {
			return fmt.Errorf("file ID %d has no name; use --by-file-id", entry.ID)
		}
		t.status(fmt.Sprintf("Extracting file %d: %q", entry.ID, name))
		data, err := readFile(entry.Name, id)
		if err != nil {
			// A clean 'not found' response leaves the live session usable.
			// Never continue after transport, protocol or verification failures.
			if !offline && id < 0 && errors.Is(err, os.ErrNotExist) {
				t.notice(fmt.Sprintf("Unavailable device file %q; continuing extraction", entry.Name))
				if priority[entry.Name] && entry.ID >= 0 {
					missing = append(missing, fmt.Errorf("required backup file %q: %w", entry.Name, err))
				}
				continue
			}
			return fmt.Errorf("reading %q: %w", entry.Name, err)
		}
		if !offline && !op.noVerify {
			t.status("Verifying extracted file with a second read")
			again, err := readFile(entry.Name, id)
			if err != nil {
				return fmt.Errorf("verifying %q: %w", entry.Name, err)
			}
			if !bytes.Equal(data, again) {
				return fmt.Errorf("file backup verification failed for %q", entry.Name)
			}
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = writeRootFile(root, name, data); err != nil {
			return err
		}
	}
	return errors.Join(missing...)
}
