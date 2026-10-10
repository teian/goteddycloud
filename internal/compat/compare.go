// SPDX-License-Identifier: GPL-2.0-only
package compat

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protowire"
)

const MaxResponse = 8 << 20

type Case struct {
	ExpectedStatus       int               `json:"expected_status,omitempty"`
	Name                 string            `json:"name"`
	Method               string            `json:"method"`
	Path                 string            `json:"path"`
	Body                 string            `json:"body,omitempty"`
	RequestHeaders       map[string]string `json:"request_headers,omitempty"`
	Headers              []string          `json:"headers"`
	Kind                 string            `json:"kind"` // bytes, json, protobuf-wire, unix-time, sse
	TimeToleranceSeconds int64             `json:"time_tolerance_seconds,omitempty"`
	Events               int               `json:"events,omitempty"`
	// Explicit file allowlist, relative to each disposable deployment root.
	Files []string `json:"files,omitempty"`
}
type Observation struct {
	Status  int
	Headers map[string]string
	Body    []byte
	Files   map[string]string
}

func Observe(ctx context.Context, client *http.Client, base, root string, c Case) (Observation, error) {
	var out Observation
	u, err := url.Parse(base)
	if err != nil {
		return out, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return out, errors.New("base must be HTTP(S)")
	}
	path, err := url.Parse(c.Path)
	if err != nil {
		return out, err
	}
	if path.IsAbs() || path.Host != "" || !strings.HasPrefix(c.Path, "/") {
		return out, errors.New("case path must be relative to the base origin")
	}
	target := u.ResolveReference(path)
	req, err := http.NewRequestWithContext(ctx, c.Method, target.String(), strings.NewReader(c.Body))
	if err != nil {
		return out, err
	}
	for k, v := range c.RequestHeaders {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	out.Status = resp.StatusCode
	out.Headers = map[string]string{}
	for _, h := range c.Headers {
		out.Headers[http.CanonicalHeaderKey(h)] = resp.Header.Get(h)
	}
	if c.Kind == "sse" || c.Kind == "sse-json" {
		if c.Events < 1 {
			return out, errors.New("SSE case requires a positive events count")
		}
		scan := bufio.NewScanner(io.LimitReader(resp.Body, MaxResponse+1))
		scan.Buffer(make([]byte, 4096), 64<<10)
		var event []string
		var total int
		for scan.Scan() {
			line := scan.Text()
			total += len(line) + 1
			if total > MaxResponse {
				return out, errors.New("SSE response limit")
			}
			if strings.HasPrefix(line, ":") {
				continue
			}
			if line != "" {
				event = append(event, line)
				continue
			}
			if len(event) > 0 {
				out.Body = append(out.Body, []byte(strings.Join(event, "\n")+"\n\n")...)
				event = nil
				c.Events--
				if c.Events == 0 {
					break
				}
			}
		}
		if err := scan.Err(); err != nil {
			return out, err
		}
		if c.Events != 0 {
			return out, errors.New("SSE stream ended before requested events")
		}
	} else {
		out.Body, err = io.ReadAll(io.LimitReader(resp.Body, MaxResponse+1))
		if err != nil {
			return out, err
		}
		if len(out.Body) > MaxResponse {
			return out, errors.New("response exceeds limit")
		}
	}
	out.Files = map[string]string{}
	if len(c.Files) > 0 && root == "" {
		return out, errors.New("file checks require both runtime roots")
	}
	if len(c.Files) > 0 {
		confined, err := os.OpenRoot(root)
		if err != nil {
			return out, err
		}
		defer confined.Close()
		for _, name := range c.Files {
			if !filepath.IsLocal(name) {
				return out, fmt.Errorf("non-local fixture path %q", name)
			}
			f, err := confined.Open(name)
			if errors.Is(err, os.ErrNotExist) {
				out.Files[name] = "absent"
				continue
			}
			if err != nil {
				return out, err
			}
			sum := sha256.New()
			_, err = io.Copy(sum, f)
			closeErr := f.Close()
			if err != nil {
				return out, err
			}
			if closeErr != nil {
				return out, closeErr
			}
			out.Files[name] = hex.EncodeToString(sum.Sum(nil))
		}
	}
	return out, nil
}

// Compare never prints response bodies; fixture requests can contain test tokens.
func Compare(c Case, a, b Observation) error {
	if c.ExpectedStatus != 0 && (a.Status != c.ExpectedStatus || b.Status != c.ExpectedStatus) {
		return fmt.Errorf("expected status %d, reference=%d candidate=%d", c.ExpectedStatus, a.Status, b.Status)
	}
	if a.Status != b.Status {
		return fmt.Errorf("status differs: reference=%d candidate=%d", a.Status, b.Status)
	}
	if !reflect.DeepEqual(a.Headers, b.Headers) {
		return errors.New("selected response headers differ")
	}
	if !reflect.DeepEqual(a.Files, b.Files) {
		return errors.New("selected file effects differ")
	}
	switch c.Kind {
	case "sse-json":
		left, err := normalizeSSE(a.Body)
		if err != nil {
			return fmt.Errorf("reference SSE: %w", err)
		}
		right, err := normalizeSSE(b.Body)
		if err != nil {
			return fmt.Errorf("candidate SSE: %w", err)
		}
		if !bytes.Equal(left, right) {
			return errors.New("SSE names, values or sequence differ")
		}
	case "json":
		decode := func(data []byte) (any, error) {
			d := json.NewDecoder(bytes.NewReader(data))
			d.UseNumber()
			var v any
			if err := d.Decode(&v); err != nil {
				return nil, err
			}
			if err := d.Decode(new(any)); err != io.EOF {
				return nil, errors.New("trailing JSON data")
			}
			return v, nil
		}
		left, err := decode(a.Body)
		if err != nil {
			return fmt.Errorf("reference JSON: %w", err)
		}
		right, err := decode(b.Body)
		if err != nil {
			return fmt.Errorf("candidate JSON: %w", err)
		}
		if !reflect.DeepEqual(left, right) {
			return errors.New("JSON values differ")
		}
	case "protobuf-wire":
		left, err := decodeWire(a.Body)
		if err != nil {
			return fmt.Errorf("reference protobuf: %w", err)
		}
		right, err := decodeWire(b.Body)
		if err != nil {
			return fmt.Errorf("candidate protobuf: %w", err)
		}
		if !reflect.DeepEqual(left, right) {
			return errors.New("protobuf wire fields differ")
		}
	case "unix-time":
		if c.TimeToleranceSeconds < 0 {
			return errors.New("negative time tolerance")
		}
		left, err := strconv.ParseInt(string(a.Body), 10, 64)
		if err != nil {
			return err
		}
		right, err := strconv.ParseInt(string(b.Body), 10, 64)
		if err != nil {
			return err
		}
		// Compare bounded values as durations to avoid integer subtraction overflow.
		if left < 0 || right < 0 || left > 1<<40 || right > 1<<40 {
			return errors.New("invalid epoch range")
		}
		diff := left - right
		if diff < 0 {
			diff = -diff
		}
		if diff > c.TimeToleranceSeconds {
			return errors.New("epoch difference exceeds explicit tolerance")
		}
	case "bytes", "sse":
		if !bytes.Equal(a.Body, b.Body) {
			return errors.New("response bytes/event sequence differ")
		}
	default:
		return fmt.Errorf("unknown comparison kind %q", c.Kind)
	}
	return nil
}

// Decode only the protobuf wire layer, preserving unknown fields and occurrence
// order within each field. Nested/packed bytes remain exact; schema-aware domain
// comparisons are added alongside generated bindings, never guessed here.
func decodeWire(data []byte) (map[protowire.Number][]string, error) {
	fields := map[protowire.Number][]string{}
	for len(data) > 0 {
		num, kind, n := protowire.ConsumeTag(data)
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		data = data[n:]
		var value string
		switch kind {
		case protowire.VarintType:
			v, size := protowire.ConsumeVarint(data)
			n = size
			value = fmt.Sprintf("v:%d", v)
		case protowire.Fixed32Type:
			v, size := protowire.ConsumeFixed32(data)
			n = size
			value = fmt.Sprintf("32:%d", v)
		case protowire.Fixed64Type:
			v, size := protowire.ConsumeFixed64(data)
			n = size
			value = fmt.Sprintf("64:%d", v)
		case protowire.BytesType:
			v, size := protowire.ConsumeBytes(data)
			n = size
			value = "b:" + hex.EncodeToString(v)
		default:
			return nil, fmt.Errorf("unsupported protobuf wire type %d", kind)
		}
		if n < 0 {
			return nil, protowire.ParseError(n)
		}
		data = data[n:]
		fields[num] = append(fields[num], value)
	}
	return fields, nil
}

func Run(ctx context.Context, client *http.Client, reference, candidate, referenceRoot, candidateRoot string, c Case) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	left, err := Observe(ctx, client, reference, referenceRoot, c)
	if err != nil {
		return fmt.Errorf("reference observation: %w", err)
	}
	right, err := Observe(ctx, client, candidate, candidateRoot, c)
	if err != nil {
		return fmt.Errorf("candidate observation: %w", err)
	}
	return Compare(c, left, right)
}

func normalizeSSE(body []byte) ([]byte, error) {
	lines := strings.Split(string(body), "\n")
	for i, line := range lines {
		if raw, ok := strings.CutPrefix(line, "data:"); ok {
			decoder := json.NewDecoder(strings.NewReader(raw))
			decoder.UseNumber()
			var value any
			if err := decoder.Decode(&value); err != nil {
				return nil, err
			}
			if err := decoder.Decode(new(any)); err != io.EOF {
				return nil, errors.New("trailing SSE JSON")
			}
			data, err := json.Marshal(value)
			if err != nil {
				return nil, err
			}
			lines[i] = "data: " + string(data)
		}
	}
	return []byte(strings.Join(lines, "\n")), nil
}
