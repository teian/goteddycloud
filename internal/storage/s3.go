// SPDX-License-Identifier: GPL-2.0-only
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"goteddycloud/internal/s3api"
)

// S3Options contains no credentials. Environment or shared profile credentials
// are loaded by the S3 REST client.
type S3Options struct {
	Bucket, Prefix, Region, Endpoint, TempDir string
	PathStyle                                 bool
}
type S3 struct {
	client  *s3api.Client
	options S3Options
	base    string
}

func NewS3(ctx context.Context, base string, options S3Options) (*S3, error) {
	if options.Bucket == "" {
		return nil, errors.New("S3 bucket is required")
	}

	if options.Endpoint != "" {
		u, err := url.Parse(options.Endpoint)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, errors.New("invalid S3 endpoint")
		}
	}
	absolute, err := filepath.Abs(base)
	if err != nil {
		return nil, err
	}
	options.Prefix = strings.Trim(options.Prefix, "/")
	if options.Prefix != "" && (path.Clean(options.Prefix) != options.Prefix || strings.Contains(options.Prefix, "\\") || !filepath.IsLocal(filepath.FromSlash(options.Prefix))) {
		return nil, errors.New("invalid S3 prefix")
	}
	if options.Region == "" {
		options.Region = os.Getenv("AWS_REGION")
	}
	if options.Region == "" {
		options.Region = os.Getenv("AWS_DEFAULT_REGION")
	}
	client, err := s3api.New(s3api.Options{Endpoint: options.Endpoint, Region: options.Region, Bucket: options.Bucket, PathStyle: options.PathStyle})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &S3{client: client, options: options, base: absolute}, nil
}
func (b *S3) key(name string) (string, error) {
	absolute, err := filepath.Abs(name)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(b.base, absolute)
	if err != nil || !filepath.IsLocal(rel) {
		return "", &os.PathError{Op: "resolve", Path: name, Err: fs.ErrPermission}
	}
	if rel == "." {
		return b.options.Prefix, nil
	}
	return path.Join(b.options.Prefix, filepath.ToSlash(rel)), nil
}
func (b *S3) root(name string) (*s3Root, error) {
	key, err := b.key(name)
	if err != nil {
		return nil, err
	}
	return &s3Root{backend: b, prefix: key, name: name, ctx: context.Background()}, nil
}
func (b *S3) OpenRoot(name string) (*Root, error) {
	r, err := b.root(name)
	if err != nil {
		return nil, err
	}
	info, err := r.Stat(".")
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, &os.PathError{Op: "open", Path: name, Err: errors.New("not a directory")}
	}
	return &Root{directory: r, identity: "s3://" + b.options.Endpoint + "/" + b.options.Bucket + "/" + r.prefix}, nil
}
func (b *S3) MkdirAll(name string, mode os.FileMode) error {
	r, err := b.root(name)
	if err != nil {
		return err
	}
	return r.MkdirAll(".", mode)
}
func (b *S3) Open(name string) (File, error) {
	r, n, err := openParent(b, name)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.Open(n)
}
func (b *S3) Stat(name string) (os.FileInfo, error) {
	r, err := b.root(filepath.Dir(name))
	if err != nil {
		return nil, err
	}
	return r.Stat(filepath.Base(name))
}
func (b *S3) ReadFile(name string) ([]byte, error) { return readAll(b, name) }

type s3Root struct {
	backend      *S3
	prefix, name string
	ctx          context.Context
}

func (r *s3Root) Name() string { return r.name }
func (*s3Root) Close() error   { return nil }
func (r *s3Root) key(name string) (string, error) {
	if !filepath.IsLocal(name) {
		return "", &os.PathError{Op: "resolve", Path: name, Err: fs.ErrPermission}
	}
	if name == "." {
		return r.prefix, nil
	}
	return path.Join(r.prefix, filepath.ToSlash(name)), nil
}
func s3Error(op, name string, err error) error {
	if err == nil {
		return nil
	}
	var api *s3api.Error
	if errors.As(err, &api) {
		switch api.Status {
		case 404:
			err = fs.ErrNotExist
		case 409, 412:
			err = fs.ErrExist
		case 401, 403:
			err = fs.ErrPermission
		}
	}

	return &os.PathError{Op: op, Path: name, Err: err}
}
func directoryKey(key string) string {
	if key == "" {
		return ""
	}
	return strings.TrimSuffix(key, "/") + "/"
}

type objectInfo struct {
	name     string
	size     int64
	mode     os.FileMode
	modified time.Time
}

func (i objectInfo) Name() string       { return i.name }
func (i objectInfo) Size() int64        { return i.size }
func (i objectInfo) Mode() os.FileMode  { return i.mode }
func (i objectInfo) ModTime() time.Time { return i.modified }
func (i objectInfo) IsDir() bool        { return i.mode.IsDir() }
func (i objectInfo) Sys() any           { return nil }
func objectMode(metadata map[string]string) os.FileMode {
	n, err := strconv.ParseUint(metadata["mode"], 8, 32)
	if err != nil {
		return 0600
	}
	return os.FileMode(n) & 0777
}
func (r *s3Root) head(name string) (*s3api.Object, error) {
	key, err := r.key(name)
	if err != nil {
		return nil, err
	}
	out, err := r.backend.client.Head(r.ctx, key)
	if err != nil {
		return nil, s3Error("stat", name, err)
	}
	return &out, nil
}
func (r *s3Root) Stat(name string) (os.FileInfo, error) {
	key, err := r.key(name)
	if err != nil {
		return nil, err
	}
	if key != "" {
		out, e := r.head(name)
		if e == nil {
			return objectInfo{filepath.Base(name), out.Size, objectMode(out.Metadata), out.Modified}, nil
		}
		if !errors.Is(e, fs.ErrNotExist) {
			return nil, e
		}
	}
	result, err := r.backend.client.List(r.ctx, directoryKey(key), "", "", 1)
	if err != nil {
		return nil, s3Error("stat", name, err)
	}
	if key != "" && len(result.Objects) == 0 && len(result.Prefixes) == 0 {
		return nil, s3Error("stat", name, fs.ErrNotExist)
	}
	return objectInfo{name: filepath.Base(name), mode: fs.ModeDir | 0700}, nil
}
func (r *s3Root) Lstat(name string) (os.FileInfo, error) { return r.Stat(name) }
func (r *s3Root) Mkdir(name string, mode os.FileMode) error {
	if _, err := r.Stat(name); err == nil {
		return s3Error("mkdir", name, fs.ErrExist)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	parent, err := r.Stat(filepath.Dir(name))
	if err != nil {
		return err
	}
	if !parent.IsDir() {
		return errors.New("parent is not a directory")
	}
	return r.MkdirAll(name, mode)
}
func (r *s3Root) MkdirAll(name string, mode os.FileMode) error {
	key, err := r.key(name)
	if err != nil {
		return err
	}
	namespace := r.backend.options.Prefix
	relative := strings.TrimPrefix(key, directoryKey(namespace))
	if key == namespace {
		relative = ""
	}
	keys := []string{}
	if namespace != "" {
		keys = append(keys, namespace)
	}
	current := namespace
	if relative != "" {
		for _, part := range strings.Split(relative, "/") {
			current = path.Join(current, part)
			keys = append(keys, current)
		}
	}
	for _, directory := range keys {
		if _, err = r.backend.client.Head(r.ctx, directory); err == nil {
			return errors.New("path is not a directory")
		} else if !errors.Is(s3Error("mkdir", name, err), fs.ErrNotExist) {
			return s3Error("mkdir", name, err)
		}
		if _, err = r.backend.client.Head(r.ctx, directoryKey(directory)); err == nil {
			continue
		} else if !errors.Is(s3Error("mkdir", name, err), fs.ErrNotExist) {
			return s3Error("mkdir", name, err)
		}
		if err = r.backend.client.Put(r.ctx, directoryKey(directory), strings.NewReader(""), 0, map[string]string{"mode": fmt.Sprintf("%o", mode.Perm())}, false); err != nil {
			return s3Error("mkdir", name, err)
		}
	}
	if len(keys) == 0 {
		_, err = r.Stat(".")
	}
	return err
}
func (r *s3Root) Open(name string) (File, error) { return r.OpenFile(name, os.O_RDONLY, 0) }
func (r *s3Root) OpenFile(name string, flag int, mode os.FileMode) (File, error) {
	key, err := r.key(name)
	if err != nil {
		return nil, err
	}
	var head *s3api.Object
	var headErr error
	if key == "" {
		headErr = fs.ErrNotExist
	} else {
		head, headErr = r.head(name)
	}
	if headErr != nil && !errors.Is(headErr, fs.ErrNotExist) {
		return nil, headErr
	}
	if headErr != nil {
		info, statErr := r.Stat(name)
		if statErr == nil && info.IsDir() {
			if flag != os.O_RDONLY {
				return nil, errors.New("is a directory")
			}
			return &s3File{root: r, key: key, name: name, info: info.(objectInfo), dir: true}, nil
		}
		if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
			return nil, statErr
		}
	}
	if headErr != nil && flag&os.O_CREATE == 0 {
		return nil, headErr
	}
	if headErr == nil && flag&(os.O_CREATE|os.O_EXCL) == (os.O_CREATE|os.O_EXCL) {
		return nil, s3Error("open", name, fs.ErrExist)
	}
	writable := flag&(os.O_WRONLY|os.O_RDWR) != 0
	f := &s3File{root: r, key: key, name: name, info: objectInfo{name: filepath.Base(name), mode: mode.Perm()}, writable: writable, readable: flag&os.O_WRONLY == 0, exclusive: flag&os.O_EXCL != 0}
	if head != nil {
		f.info.size = head.Size
		f.info.mode = objectMode(head.Metadata)
		f.info.modified = head.Modified
		f.etag = head.ETag
		f.version = head.Version
	}
	if head != nil && head.Size > 5_000_000_000 && writable {
		return nil, errors.New("S3 staged file exceeds upload limit")
	}
	if !writable && flag&os.O_CREATE == 0 {
		return f, nil
	}
	parent, err := r.Stat(filepath.Dir(name))
	if err != nil {
		return nil, err
	}
	if !parent.IsDir() {
		return nil, errors.New("parent is not a directory")
	}
	f.local, err = os.CreateTemp(r.backend.options.TempDir, "teddycloud-s3-*")
	if err != nil {
		return nil, err
	}
	if head != nil && flag&os.O_TRUNC == 0 {
		out, e := r.backend.client.Get(r.ctx, key, f.etag, f.version, "")
		if e == nil {
			_, e = io.Copy(f.local, out)
			e = errors.Join(e, out.Close())
		}
		if e != nil {
			f.discard()
			return nil, s3Error("open", name, e)
		}
	}
	f.dirty = head == nil || flag&os.O_TRUNC != 0
	f.appendMode = flag&os.O_APPEND != 0
	_, err = f.local.Seek(0, io.SeekStart)
	if err != nil {
		f.discard()
		return nil, err
	}
	return f, nil
}
func (r *s3Root) Remove(name string) error {
	info, err := r.Stat(name)
	if err != nil {
		return err
	}
	key, err := r.key(name)
	if err != nil {
		return err
	}
	if info.IsDir() {
		entries, e := r.list(name)
		if e != nil {
			return e
		}
		if len(entries) > 0 {
			return errors.New("directory not empty")
		}
		key = directoryKey(key)
	}
	err = r.backend.client.Delete(r.ctx, key)
	return s3Error("remove", name, err)
}
func (r *s3Root) Rename(old, new string) error {
	source, err := r.key(old)
	if err != nil {
		return err
	}
	target, err := r.key(new)
	if err != nil {
		return err
	}
	if source == target {
		return nil
	}
	info, err := r.Stat(old)
	if err != nil {
		return err
	}
	parent, e := r.Stat(filepath.Dir(new))
	if e != nil {
		return e
	}
	if !parent.IsDir() {
		return errors.New("destination parent is not a directory")
	}
	if info.IsDir() {
		return r.renameDirectory(source, target)
	}
	if destination, e := r.Stat(new); e == nil && destination.IsDir() {
		return errors.New("destination is a directory")
	} else if e != nil && !errors.Is(e, fs.ErrNotExist) {
		return e
	}
	head, err := r.head(old)
	if err != nil {
		return err
	}
	err = r.backend.client.Copy(r.ctx, source, target, head.ETag)
	if err != nil {
		return s3Error("rename", old, err)
	}
	err = r.backend.client.Delete(r.ctx, source)
	return s3Error("rename", old, err)
}
func (r *s3Root) list(name string) ([]os.DirEntry, error) {
	key, err := r.key(name)
	if err != nil {
		return nil, err
	}
	prefix := directoryKey(key)
	entries := map[string]os.DirEntry{}
	pager := s3api.NewPaginator(r.backend.client, prefix, "/")
	for pager.HasMorePages() {
		page, err := pager.NextPage(r.ctx)
		if err != nil {
			return nil, s3Error("readdir", name, err)
		}
		if len(entries)+len(page.Prefixes)+len(page.Objects) > 100000 {
			return nil, errors.New("S3 directory entry limit exceeded")
		}
		for _, p := range page.Prefixes {
			n := strings.TrimSuffix(strings.TrimPrefix(p, prefix), "/")
			if n != "" {
				entries[n] = fs.FileInfoToDirEntry(objectInfo{name: n, mode: fs.ModeDir | 0700})
			}
		}
		for _, o := range page.Objects {
			n := strings.TrimPrefix(o.Key, prefix)
			if n != "" {
				entries[n] = fs.FileInfoToDirEntry(objectInfo{name: n, size: o.Size, mode: 0600, modified: o.Modified})
			}
		}
	}
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	sort.Strings(names)
	result := make([]os.DirEntry, 0, len(names))
	for _, n := range names {
		result = append(result, entries[n])
	}
	return result, nil
}
func (r *s3Root) writeAtomic(ctx context.Context, name string, source io.Reader, mode os.FileMode, limit int64) error {
	clone := *r
	clone.ctx = ctx
	r = &clone
	key, err := r.key(name)
	if err != nil {
		return err
	}
	if destination, e := r.Stat(name); e == nil && destination.IsDir() {
		return errors.New("destination is a directory")
	} else if e != nil && !errors.Is(e, fs.ErrNotExist) {
		return e
	}
	parent, err := r.Stat(filepath.Dir(name))
	if err != nil {
		return err
	}
	if !parent.IsDir() {
		return errors.New("destination parent is not a directory")
	}
	f, err := os.CreateTemp(r.backend.options.TempDir, "teddycloud-publish-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(contextReader{ctx, source}, limit+1))
	if err != nil {
		return err
	}
	if n > limit || n > 5_000_000_000 {
		return errors.New("atomic stream exceeds limit")
	}
	_, err = f.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}
	err = r.backend.client.Put(ctx, key, f, n, map[string]string{"mode": fmt.Sprintf("%o", mode.Perm())}, false)
	return s3Error("publish", name, err)
}

// Directory moves copy a bounded snapshot before deleting sources. A failed
// copy retains every source; a failed delete can leave duplicates. Readers may
// see both directories during the operation. Deployments require one writer.
func (r *s3Root) renameDirectory(source, target string) error {
	if strings.HasPrefix(directoryKey(target), directoryKey(source)) {
		return errors.New("cannot move a directory into itself")
	}
	if _, err := (&s3Root{backend: r.backend, prefix: target, name: target, ctx: r.ctx}).Stat("."); err == nil {
		return fs.ErrExist
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	prefix := directoryKey(source)
	type item struct {
		key  string
		etag string
	}
	var objects []item
	pager := s3api.NewPaginator(r.backend.client, prefix, "")
	for pager.HasMorePages() {
		result, err := pager.NextPage(r.ctx)
		if err != nil {
			return s3Error("rename", source, err)
		}
		for _, o := range result.Objects {
			objects = append(objects, item{o.Key, o.ETag})
			if len(objects) > 100000 {
				return errors.New("S3 directory move entry limit exceeded")
			}
		}
	}
	for _, o := range objects {
		destination := directoryKey(target) + strings.TrimPrefix(o.key, prefix)
		err := r.backend.client.Copy(r.ctx, o.key, destination, o.etag)
		if err != nil {
			return s3Error("rename-copy", source, err)
		}
	}
	for _, o := range objects {
		err := r.backend.client.Delete(r.ctx, o.key)
		if err != nil {
			return s3Error("rename-delete", source, err)
		}
	}
	return nil
}
