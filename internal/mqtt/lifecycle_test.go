// SPDX-License-Identifier: GPL-2.0-only
package mqtt

import (
	"bufio"
	"context"
	"net"
	"testing"
	"time"

	"goteddycloud/internal/config"
)

func TestServeCleansUpOnAcceptFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &Server{}
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listener) }()
	client, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	deadline := time.Now().Add(time.Second)
	for {
		server.mu.Lock()
		accepted := len(server.connections) > 0
		server.mu.Unlock()
		if accepted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("connection was not accepted")
		}
		time.Sleep(time.Millisecond)
	}
	listener.Close()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Error("Serve did not terminate after listener failure while a client was connected")
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("cleanup failed")
		}
	}
}

func TestConnectHonorsCancellation(t *testing.T) {
	settings, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Close()
	client := NewClient(settings)
	local, broker := net.Pipe()
	defer local.Close()
	defer broker.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := client.session(ctx, local, settings.Snapshot("")); done <- err }()
	if _, err := readPacket(bufio.NewReader(broker)); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Error("cancelled session remained blocked waiting for CONNACK")
		broker.Close()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("cleanup failed")
		}
	}
}

func TestCommandCallbackAllowsProtocolProgressAndCancellation(t *testing.T) {
	settings, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Close()
	client := NewClient(settings)
	started, finished := make(chan struct{}), make(chan struct{})
	client.Receive = func(ctx context.Context, _ Message) { close(started); <-ctx.Done(); close(finished) }
	local, broker := net.Pipe()
	defer local.Close()
	defer broker.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := client.session(ctx, local, settings.Snapshot("")); done <- err }()
	reader := bufio.NewReader(broker)
	if _, err := readPacket(reader); err != nil {
		t.Fatal(err)
	}
	if _, err := broker.Write(frame(0x20, []byte{0, 0})); err != nil {
		t.Fatal(err)
	}
	online, err := readPacket(reader)
	if err != nil {
		t.Fatal(err)
	}
	_, id, err := parsePublish(online)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := broker.Write(frame(0x50, []byte{byte(id >> 8), byte(id)})); err != nil {
		t.Fatal(err)
	}
	pubrel, err := readPacket(reader)
	if err != nil || pubrel.header != 0x62 {
		t.Fatal(pubrel, err)
	}
	if _, err := broker.Write(publishPacket(Message{"test/command", "test", 1, false}, 42)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("command did not start")
	}
	// A repeated PUBREC for the outstanding online state requires a PUBREL
	// response while the inbound command callback is still blocked.
	if _, err := broker.Write(frame(0x50, pubrel.data)); err != nil {
		t.Fatal(err)
	}
	broker.SetReadDeadline(time.Now().Add(time.Second))
	repeated, err := readPacket(reader)
	if err != nil || repeated.header != 0x62 {
		t.Fatal("command callback stalled protocol acknowledgements", repeated, err)
	}
	if _, err := broker.Write(frame(0x70, pubrel.data)); err != nil {
		t.Fatal(err)
	}
	// No PUBACK is sent before command completion.
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("callback blocked session cancellation")
	}
	select {
	case <-finished:
	default:
		t.Fatal("session returned without joining its command worker")
	}
}
