// SPDX-License-Identifier: GPL-2.0-only
package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

type Root struct {
	directory
	identity        string
	caseInsensitive bool
}

// Resource coordination is shared by roots opened on the same canonical
// directory. Entries disappear after the last holder/waiter; Root lifetime does
// not affect a held token. This is in-process coordination, not a file lock.
var resources = struct {
	sync.Mutex
	locks map[string]*entry
}{locks: make(map[string]*entry)}

type entry struct {
	token chan struct{}
	refs  int
}

func Open(path string) (*Root, error) {
	r, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(path)
	if err == nil {
		absolute, err = filepath.EvalSymlinks(absolute)
	}
	if err != nil {
		r.Close()
		return nil, err
	}
	return &Root{directory: filesystemRoot{r}, identity: absolute, caseInsensitive: runtime.GOOS == "windows"}, nil
}
func (r *Root) Lock(names ...string) func() {
	unlock, _ := r.LockContext(context.Background(), names...)
	return unlock
}

// LockContext releases acquired resources if cancellation interrupts a wait.
func (r *Root) LockContext(ctx context.Context, names ...string) (func(), error) {
	names = append([]string(nil), names...)
	for i, n := range names {
		names[i] = filepath.Join(r.identity, filepath.Clean(n))
		// Coalesce case aliases on Windows, even for separately opened handles.
		if r.caseInsensitive {
			names[i] = strings.ToLower(names[i])
		}
	}
	sort.Strings(names)
	names = unique(names)
	entries := make([]*entry, len(names))
	resources.Lock()
	for i, n := range names {
		e := resources.locks[n]
		if e == nil {
			e = &entry{token: make(chan struct{}, 1)}
			resources.locks[n] = e
		}
		e.refs++
		entries[i] = e
	}
	resources.Unlock()
	acquired := 0
	release := func() {
		for i := acquired - 1; i >= 0; i-- {
			<-entries[i].token
		}
		resources.Lock()
		defer resources.Unlock()
		for i, e := range entries {
			e.refs--
			if e.refs == 0 {
				delete(resources.locks, names[i])
			}
		}
	}
	for _, e := range entries {
		select {
		case e.token <- struct{}{}:
			acquired++
		case <-ctx.Done():
			release()
			return nil, ctx.Err()
		}
	}
	if err := ctx.Err(); err != nil {
		release()
		return nil, err
	}
	return release, nil
}

func unique(names []string) []string {
	out := names[:0]
	for _, n := range names {
		if len(out) == 0 || n != out[len(out)-1] {
			out = append(out, n)
		}
	}
	return out
}
func (r *Root) WriteAtomic(name string, data []byte, mode os.FileMode) error {
	return r.WriteReaderAtomic(context.Background(), name, bytes.NewReader(data), mode, int64(len(data)))
}

// WriteReaderAtomic publishes a bounded, completed stream without retaining it
// in memory. Cancellation and overflow remove the private file and retain the
// existing destination. Paths remain confined by the selected backend.
func (r *Root) WriteReaderAtomic(ctx context.Context, name string, source io.Reader, mode os.FileMode, limit int64) error {
	if !filepath.IsLocal(name) || limit < 0 || limit == int64(^uint64(0)>>1) {
		return errors.New("invalid atomic stream destination or limit")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if remote, ok := r.directory.(*s3Root); ok {
		return remote.writeAtomic(ctx, name, source, mode, limit)
	}
	var suffix [16]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return err
	}
	temporary := filepath.Join(filepath.Dir(name), ".tmp-"+hex.EncodeToString(suffix[:]))
	f, err := r.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer r.Remove(temporary)
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(contextReader{ctx, source}, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return errors.New("atomic stream exceeds limit")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.Rename(temporary, name)
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(data)
}

func (r *Root) ReadFile(name string) ([]byte, error) {
	f, err := r.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}
