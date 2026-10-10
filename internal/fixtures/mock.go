// SPDX-License-Identifier: GPL-2.0-only
package fixtures

import (
	"io"
	"net/http"
	"sync"
)

type Request struct {
	Method, Path string
	Body         []byte
}

// MockCloud records bounded requests in memory. It never contacts the internet
// or logs authorization values; callers inspect Records for policy assertions.
type MockCloud struct {
	mu      sync.Mutex
	records []Request
}

func (m *MockCloud) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(io.LimitReader(r.Body, 65537))
	if err != nil || len(data) > 65536 {
		http.Error(w, "request limit", http.StatusRequestEntityTooLarge)
		return
	}
	m.mu.Lock()
	if len(m.records) >= 1024 {
		m.mu.Unlock()
		http.Error(w, "recording limit", http.StatusServiceUnavailable)
		return
	}
	m.records = append(m.records, Request{r.Method, r.URL.RequestURI(), data})
	m.mu.Unlock()
	switch r.URL.Path {
	case "/v1/time":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("1700000000"))
	case "/v1/freshness-check":
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte{0x10, 0, 0x18, 3, 0x20, 1, 0x28, 0, 0x30, 0, 0x38, 3, 0x40, 0})
	default:
		http.NotFound(w, r)
	}
}
func (m *MockCloud) Records() []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Request, len(m.records))
	for i, r := range m.records {
		out[i] = r
		out[i].Body = append([]byte(nil), r.Body...)
	}
	return out
}
