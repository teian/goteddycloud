// SPDX-License-Identifier: GPL-2.0-only
package diagnostics

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Capture emits link-type 101 raw IPv4/TCP packets containing application HTTP.
// It records explicit opt-in plaintext traffic, including bodies and credentials.
// IPv6 endpoints are represented as loopback IPv4; no packet is sent on a network.
type Capture struct {
	file *File
	now  func() time.Time
}

func NewCapture(f *File) (*Capture, error) {
	f.mu.Lock()
	info, err := f.file.Stat()
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if info.Size() != 0 {
		header := make([]byte, 24)
		if _, err := f.file.ReadAt(header, 0); err != nil || !bytes.Equal(header[:8], []byte{0xd4, 0xc3, 0xb2, 0xa1, 2, 0, 4, 0}) || binary.LittleEndian.Uint32(header[20:]) != 101 {
			return nil, fmt.Errorf("existing diagnostic file is not raw IPv4 PCAP")
		}
		return &Capture{file: f, now: time.Now}, nil
	}
	header := make([]byte, 24)
	binary.LittleEndian.PutUint32(header, 0xa1b2c3d4)
	binary.LittleEndian.PutUint16(header[4:], 2)
	binary.LittleEndian.PutUint16(header[6:], 4)
	binary.LittleEndian.PutUint32(header[16:], 65535)
	binary.LittleEndian.PutUint32(header[20:], 101)
	_, _ = f.Write(header)
	return &Capture{file: f, now: time.Now}, nil
}

type flow struct {
	capture               *Capture
	mu                    sync.Mutex
	remote, local         [4]byte
	remotePort, localPort uint16
	rx, tx                uint32
}

func endpoint(address string) ([4]byte, uint16) {
	ip := [4]byte{127, 0, 0, 1}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return ip, 0
	}
	if parsed := net.ParseIP(host).To4(); parsed != nil {
		copy(ip[:], parsed)
	}
	n, _ := strconv.ParseUint(port, 10, 16)
	return ip, uint16(n)
}
func checksum(p []byte) uint16 {
	var n uint32
	for len(p) > 1 {
		n += uint32(binary.BigEndian.Uint16(p))
		p = p[2:]
	}
	if len(p) > 0 {
		n += uint32(p[0]) << 8
	}
	for n>>16 != 0 {
		n = (n & 65535) + (n >> 16)
	}
	return ^uint16(n)
}
func (f *flow) packet(tx bool, data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for len(data) > 0 {
		n := len(data)
		if n > 65495 {
			n = 65495
		}
		packet := make([]byte, 40+n)
		packet[0] = 0x45
		binary.BigEndian.PutUint16(packet[2:], uint16(len(packet)))
		packet[8] = 64
		packet[9] = 6
		source, dest, sp, dp, seq, ack := f.remote, f.local, f.remotePort, f.localPort, f.rx, f.tx
		if tx {
			source, dest, sp, dp, seq, ack = f.local, f.remote, f.localPort, f.remotePort, f.tx, f.rx
			f.tx += uint32(n)
		} else {
			f.rx += uint32(n)
		}
		copy(packet[12:16], source[:])
		copy(packet[16:20], dest[:])
		binary.BigEndian.PutUint16(packet[10:], checksum(packet[:20]))
		binary.BigEndian.PutUint16(packet[20:], sp)
		binary.BigEndian.PutUint16(packet[22:], dp)
		binary.BigEndian.PutUint32(packet[24:], seq)
		binary.BigEndian.PutUint32(packet[28:], ack)
		packet[32] = 0x50
		packet[33] = 0x18
		binary.BigEndian.PutUint16(packet[34:], 65535)
		copy(packet[40:], data[:n])
		pseudo := make([]byte, 12+len(packet)-20)
		copy(pseudo, packet[12:20])
		pseudo[9] = 6
		binary.BigEndian.PutUint16(pseudo[10:], uint16(len(packet)-20))
		copy(pseudo[12:], packet[20:])
		binary.BigEndian.PutUint16(packet[36:], checksum(pseudo))
		now := f.capture.now()
		record := make([]byte, 16+len(packet))
		binary.LittleEndian.PutUint32(record, uint32(now.Unix()))
		binary.LittleEndian.PutUint32(record[4:], uint32(now.Nanosecond()/1000))
		binary.LittleEndian.PutUint32(record[8:], uint32(len(packet)))
		binary.LittleEndian.PutUint32(record[12:], uint32(len(packet)))
		copy(record[16:], packet)
		_, _ = f.capture.file.Write(record)
		data = data[n:]
	}
}

type readCapture struct {
	io.ReadCloser
	flow *flow
}

func (r *readCapture) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		r.flow.packet(false, p[:n])
	}
	return n, err
}

type responseCapture struct {
	http.ResponseWriter
	flow   *flow
	status bool
}

func (w *responseCapture) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *responseCapture) WriteHeader(status int) {
	if w.status {
		return
	}
	w.status = true
	w.flow.packet(true, []byte(fmt.Sprintf("HTTP/1.1 %d %s\r\n", status, http.StatusText(status))))
	var headers []byte
	for key, values := range w.Header() {
		for _, value := range values {
			headers = fmt.Appendf(headers, "%s: %s\r\n", key, value)
		}
	}
	headers = append(headers, '\r', '\n')
	w.flow.packet(true, headers)
	w.ResponseWriter.WriteHeader(status)
}
func (w *responseCapture) Write(p []byte) (int, error) {
	if !w.status {
		w.WriteHeader(200)
	}
	n, err := w.ResponseWriter.Write(p)
	if n > 0 {
		w.flow.packet(true, p[:n])
	}
	return n, err
}
func (w *responseCapture) Flush() {
	if !w.status {
		w.WriteHeader(200)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}
func (c *Capture) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f := &flow{capture: c}
		f.remote, f.remotePort = endpoint(r.RemoteAddr)
		if address, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
			f.local, f.localPort = endpoint(address.String())
		} else {
			f.local = [4]byte{127, 0, 0, 1}
		}
		f.packet(false, []byte(fmt.Sprintf("%s %s HTTP/1.1\r\nHost: %s\r\n", r.Method, r.URL.RequestURI(), r.Host)))
		var headers []byte
		for key, values := range r.Header {
			for _, value := range values {
				headers = fmt.Appendf(headers, "%s: %s\r\n", key, value)
			}
		}
		headers = append(headers, '\r', '\n')
		f.packet(false, headers)
		if r.Body != nil {
			r.Body = &readCapture{r.Body, f}
		}
		next.ServeHTTP(&responseCapture{ResponseWriter: w, flow: f}, r)
	})
}
