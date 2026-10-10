// SPDX-License-Identifier: GPL-2.0-only
package cc3200cli

import (
	"errors"
	"goteddycloud/internal/cc3200"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

func readBounded(name string, limit int) ([]byte, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > int64(limit) {
		return nil, errors.New("input is not a bounded regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, errors.New("input exceeds size limit")
	}
	return data, nil
}
func writeOutput(name string, data []byte, stdout io.Writer) error {
	if name == "-" {
		_, err := stdout.Write(data)
		return err
	}
	root, err := os.OpenRoot(filepath.Dir(name))
	if err != nil {
		return err
	}
	defer root.Close()
	return writeRootFile(root, filepath.Base(name), data)
}
func writeRootFile(root *os.Root, name string, data []byte) (err error) {
	if !filepath.IsLocal(name) {
		return errors.New("output path escapes directory")
	}
	if err = root.MkdirAll(filepath.Dir(name), 0700); err != nil {
		return err
	}
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = root.Remove(name)
		}
	}()
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
func createDirectory(name string) (*os.Root, error) {
	if err := os.Mkdir(name, 0700); err != nil {
		return nil, err
	}
	return os.OpenRoot(name)
}

type inputFile struct {
	name string
	data []byte
}

func readDirectory(name string) ([]inputFile, error) {
	root, err := os.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	var result []inputFile
	total := 0
	var walk func(string) error
	walk = func(dir string) error {
		file, err := root.Open(dir)
		if err != nil {
			return err
		}
		entries, err := file.ReadDir(-1)
		file.Close()
		if err != nil {
			return err
		}
		for _, entry := range entries {
			rel := path.Join(dir, entry.Name())
			if entry.Type()&os.ModeSymlink != 0 {
				return errors.New("symlinks are not accepted in bulk uploads")
			}
			if entry.IsDir() {
				if strings.Count(rel, "/") > 16 {
					return errors.New("input tree too deep")
				}
				if err = walk(rel); err != nil {
					return err
				}
				continue
			}
			if !cc3200.ValidName("/"+rel) || len(result) >= 128 {
				return errors.New("invalid filename or too many input files")
			}
			input, err := root.Open(rel)
			if err != nil {
				return err
			}
			info, err := input.Stat()
			if err != nil {
				input.Close()
				return err
			}
			if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > int64(cc3200.MaxFlash-total) {
				input.Close()
				return errors.New("invalid input file size")
			}
			data, err := io.ReadAll(io.LimitReader(input, int64(cc3200.MaxFlash-total)+1))
			input.Close()
			if err != nil {
				return err
			}
			total += len(data)
			if total > cc3200.MaxFlash {
				return errors.New("bulk input exceeds limit")
			}
			result = append(result, inputFile{"/" + rel, data})
		}
		return nil
	}
	err = walk(".")
	sort.Slice(result, func(i, j int) bool { return result[i].name < result[j].name })
	return result, err
}
