// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"crypto/sha1"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"goteddycloud/internal/catalog"
	"goteddycloud/internal/certificates"
	"goteddycloud/internal/config"
)

func validateDeployment(settings *config.Store) error {
	if err := settings.Validate(); err != nil {
		return err
	}
	snap := settings.Snapshot("")
	if err := certificates.ValidateManagementTLS(snap); err != nil {
		return err
	}
	if _, err := settings.Storage().Stat(filepath.Join(snap.String("internal.configdirfull"), catalog.RenameJournal)); err == nil {
		return fmt.Errorf("pending custom model rename requires startup recovery")
	} else if !os.IsNotExist(err) {
		return err
	}
	for _, key := range []string{"internal.contentdirfull", "internal.librarydirfull", "internal.wwwdirfull", "internal.firmwaredirfull"} {
		info, err := settings.Storage().Stat(snap.String(key))
		if err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", key)
		}
	}
	for _, prefix := range []string{"core.server_cert", "core.server_cert_tb2"} {
		if _, err := certificates.Load(snap, prefix); err != nil {
			return fmt.Errorf("%s: %w", prefix, err)
		}
	}
	fmt.Printf("deployment valid: %s (%d box overlays); read-only check\n", snap.Base, len(settings.BoxIDs()))
	return nil
}

func printFileHash(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha1.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	fmt.Printf("%X\n", h.Sum(nil))
	return nil
}
