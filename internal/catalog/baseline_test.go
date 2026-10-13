// SPDX-License-Identifier: GPL-2.0-only
package catalog

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"goteddycloud/internal/storage"
	"goteddycloud/internal/teststorage"
)

func TestBaselineSeedsEmptyCatalogsAndPreservesDeploymentData(t *testing.T) {
	for _, kind := range []string{"filesystem", "s3"} {
		t.Run(kind, func(t *testing.T) {
			base := t.TempDir()
			var backend storage.Backend = storage.Filesystem{}
			if kind == "s3" {
				backend = teststorage.New(t, base, nil)
			}
			root, err := backend.OpenRoot(base)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			const custom = `[{"model":"CUSTOM","series":"Custom"}]`
			for name, data := range map[string]string{"tonies.json": "[]", "tonies.custom.json": custom} {
				if err := root.WriteAtomic(name, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			bundle := t.TempDir()
			const official = `[{"model":"BASE","series":"Baseline"}]`
			const boxes = `[{"id":"box","name":"Baseline box"}]`
			write := func(name, value string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(bundle, name), []byte(value), 0644); err != nil {
					t.Fatal(err)
				}
			}
			write("tonies.json", official)
			write("tonieboxes.json", "invalid")
			c, err := OpenWithStorage(base, backend)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if err := c.SeedBaseline(bundle); err == nil {
				t.Fatal("invalid baseline accepted")
			}
			if data, err := c.ReadOfficial(); err != nil || string(data) != "[]" || c.ByModel("BASE") != nil {
				t.Fatal("partially published invalid baseline", err)
			}
			write("tonieboxes.json", boxes)
			if err := c.SeedBaseline(bundle); err != nil {
				t.Fatal(err)
			}
			if c.ByModel("BASE").String("series") != "Baseline" || c.ByModel("CUSTOM").String("series") != "Custom" {
				t.Fatal("baseline/custom lookup missing")
			}
			if data, err := c.ReadOfficial(); err != nil || string(data) != official {
				t.Fatal("baseline bytes changed", err)
			}
			if err := c.ReplaceOfficial([]byte(`[{"model":"DOWNLOADED"}]`)); err != nil {
				t.Fatal(err)
			}
			if err := c.ReplaceBoxes([]byte(`[{"id":"downloaded"}]`)); err != nil {
				t.Fatal(err)
			}
			// A later image, even one with unavailable baseline files, must not
			// replace populated deployment databases on restart.
			if err := c.SeedBaseline(filepath.Join(bundle, "absent")); err != nil {
				t.Fatal(err)
			}
			if err := c.Reload(); err != nil {
				t.Fatal(err)
			}
			if c.ByModel("DOWNLOADED") == nil || c.ByModel("BASE") != nil || c.ByModel("CUSTOM") == nil {
				t.Fatal("deployment catalogs replaced")
			}
			file, err := root.Open("tonieboxes.json")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			var data bytes.Buffer
			if _, err := data.ReadFrom(file); err != nil || data.String() != `[{"id":"downloaded"}]` {
				t.Fatal("deployment box catalog replaced", err)
			}
		})
	}
}
