// SPDX-License-Identifier: GPL-2.0-only
package content

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"goteddycloud/internal/config"
	"goteddycloud/internal/fixtures"
)

func TestContentWaitsRespectCancellation(t *testing.T) {
	base := filepath.Join(t.TempDir(), "fixture")
	if err := fixtures.Generate(base); err != nil {
		t.Fatal(err)
	}
	settings, err := config.Open(base)
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Close()
	snap := settings.Snapshot("")
	store := New()
	defer store.Close()
	root, err := store.root(snap.String("internal.contentdirfull"))
	if err != nil {
		t.Fatal(err)
	}
	const uid = "0000000100000001"
	name, _ := UIDPath(uid)
	release := root.Lock(name)
	defer release()
	for _, operation := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"open", func(ctx context.Context) error {
			f, _, err := store.OpenContext(ctx, snap, snap, uid)
			if f != nil {
				f.Close()
			}
			return err
		}},
		{"inspect", func(ctx context.Context) error {
			f, _, err := store.InspectContext(ctx, snap, snap, uid)
			if f != nil {
				f.Close()
			}
			return err
		}},
		{"update", func(ctx context.Context) error {
			return store.UpdateContext(ctx, snap, uid, func(Metadata) error { t.Error("cancelled update ran"); return nil })
		}},
	} {
		t.Run(operation.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			if err := operation.run(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
		})
	}
	writer, err := store.mutations.Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer writer()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := store.Assign(ctx, snap, snap, uid, "lib://synthetic.taf", false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	if err := store.RenameModels(ctx, snap, "old", "new"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
