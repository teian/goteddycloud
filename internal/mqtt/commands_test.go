// SPDX-License-Identifier: GPL-2.0-only
package mqtt

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestCommandsRemainOrderedAndBounded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var workers sync.WaitGroup
	started := make(chan string, 2)
	release := make(chan struct{})
	queue, completed := commandWorker(ctx, &workers, func(ctx context.Context, message Message) {
		started <- message.Payload
		select {
		case <-release:
		case <-ctx.Done():
		}
	})
	defer func() { cancel(); workers.Wait() }()
	queue <- inboundCommand{message: Message{Payload: "first"}, ack: 0x40, id: 1}
	if got := <-started; got != "first" {
		t.Fatal(got)
	}
	for range maxQueuedCommands {
		queue <- inboundCommand{message: Message{Payload: "queued"}, ack: 0x40, id: 2}
	}
	select {
	case queue <- inboundCommand{}:
		t.Fatal("command queue is unbounded")
	default:
	}
	select {
	case <-completed:
		t.Fatal("acknowledged before command completed")
	default:
	}
	release <- struct{}{}
	select {
	case ack := <-completed:
		if ack.header != 0x40 || len(ack.data) != 2 || ack.data[1] != 1 {
			t.Fatal(ack)
		}
	case <-time.After(time.Second):
		t.Fatal("command completion missing")
	}
	if got := <-started; got != "queued" {
		t.Fatal("execution reordered", got)
	}
	cancel()
	workers.Wait()
	// Cancellation must not execute the remaining queued commands.
	select {
	case value := <-started:
		t.Fatal("executed after cancellation", value)
	default:
	}
}
