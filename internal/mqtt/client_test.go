// SPDX-License-Identifier: GPL-2.0-only
package mqtt

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"

	"goteddycloud/internal/config"
)

func TestWireFragmentationLimitsAndFilters(t *testing.T) {
	for _, pair := range []struct {
		filter, topic string
		match         bool
	}{{"a/+", "a/x", true}, {"a/#", "a", true}, {"a/#", "a/x/y", true}, {"a/+", "a/x/y", false}, {"#", "$SYS/x", false}, {"a/*", "a/x", false}, {"a/#/x", "a/y/x", false}} {
		if Match(pair.filter, pair.topic) != pair.match {
			t.Fatal(pair)
		}
	}
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	go func() {
		for _, value := range frame(0x30, append(str("test"), make([]byte, 1024)...)) {
			b.Write([]byte{value})
		}
	}()
	p, err := readPacket(bufio.NewReader(a))
	if err != nil || len(p.data) != 1030 {
		t.Fatal(len(p.data), err)
	}
	for _, data := range [][]byte{{0x30, 0x80, 0x80, 0x80, 0x80}, {0x30, 0xff, 0xff, 0xff, 0x7f}} {
		a, b := net.Pipe()
		go func() { b.Write(data); b.Close() }()
		if _, err := readPacket(bufio.NewReader(a)); err == nil {
			t.Fatal("oversize accepted")
		}
		a.Close()
	}
}
func TestReconnectBackoffResetAndRestoration(t *testing.T) {
	settings, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Close()
	settings.Set("", "mqtt.enabled", "true")
	client := NewClient(settings)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sleeps := []time.Duration{}
	attempt := 0
	restores := 0
	client.Sleep = func(_ context.Context, d time.Duration) bool {
		sleeps = append(sleeps, d)
		if len(sleeps) == 8 {
			cancel()
			return false
		}
		return true
	}
	var workers sync.WaitGroup
	client.Restore = func() ([]string, []Message) {
		restores++
		return []string{"test/command"}, []Message{{"test/state", "ready", 2, false}}
	}
	client.Dial = func(context.Context, config.Snapshot) (net.Conn, error) {
		attempt++
		if attempt != 7 {
			return nil, errors.New("offline")
		}
		a, b := net.Pipe()
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer b.Close()
			reader := bufio.NewReader(b)
			if _, err := readPacket(reader); err != nil {
				return
			}
			b.Write(frame(0x20, []byte{0, 0}))
			for {
				p, err := readPacket(reader)
				if err != nil {
					return
				}
				switch p.header >> 4 {
				case 8:
					b.Write(frame(0x90, append(p.data[:2], 0)))
				case 3:
					_, id, _ := parsePublish(p)
					b.Write(frame(0x50, []byte{byte(id >> 8), byte(id)}))
				case 6:
					b.Write(frame(0x70, p.data))
					return
				}
			}
		}()
		return a, nil
	}
	client.Run(ctx)
	workers.Wait()
	expected := []time.Duration{5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second, 60 * time.Second, 60 * time.Second, 5 * time.Second, 5 * time.Second}
	if !reflect.DeepEqual(sleeps, expected) || restores != 1 {
		t.Fatal(sleeps, restores)
	}
}
func TestSessionQoS2CommandExactlyOnceAndCancellation(t *testing.T) {
	settings, _ := config.Open(t.TempDir())
	defer settings.Close()
	client := NewClient(settings)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	received := make(chan Message, 2)
	client.Receive = func(_ context.Context, m Message) { received <- m }
	done := make(chan error, 1)
	go func() { _, err := client.session(ctx, a, settings.Snapshot("")); done <- err }()
	reader := bufio.NewReader(b)
	if _, err := readPacket(reader); err != nil {
		t.Fatal(err)
	}
	b.Write(frame(0x20, []byte{0, 0}))
	p, err := readPacket(reader)
	if err != nil {
		t.Fatal(err)
	}
	_, id, err := parsePublish(p)
	if err != nil {
		t.Fatal(err)
	}
	b.Write(frame(0x50, []byte{byte(id >> 8), byte(id)}))
	p, err = readPacket(reader)
	if err != nil || p.header != 0x62 {
		t.Fatal(p, err)
	}
	b.Write(frame(0x70, p.data))
	b.Write(publishPacket(Message{"commands/test", "2", 2, false}, 42))
	p, _ = readPacket(reader)
	if p.header != 0x50 || binary.BigEndian.Uint16(p.data) != 42 {
		t.Fatal(p)
	}
	b.Write(frame(0x62, p.data))
	p, _ = readPacket(reader)
	if p.header != 0x70 {
		t.Fatal(p)
	}
	b.Write(frame(0x62, p.data))
	p, _ = readPacket(reader)
	if p.header != 0x70 {
		t.Fatal(p)
	}
	select {
	case m := <-received:
		if m.Payload != "2" {
			t.Fatal(m)
		}
	case <-ctx.Done():
		t.Fatal("command missing")
	}
	select {
	case m := <-received:
		t.Fatal("duplicate", m)
	default:
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancellation leaked session")
	}
}
func FuzzPacket(f *testing.F) {
	f.Add([]byte{0xc0, 0})
	f.Add(frame(0x30, append(str("test/topic"), []byte("fixture")...)))
	f.Fuzz(func(t *testing.T, data []byte) {
		reader := bufio.NewReader(&byteReader{data: data})
		p, err := readPacket(reader)
		if err == nil && p.header>>4 == 3 {
			parsePublish(p)
		}
	})
}

type byteReader struct{ data []byte }

func (r *byteReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, errors.New("end")
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}
