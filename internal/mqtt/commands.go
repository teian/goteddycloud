// SPDX-License-Identifier: GPL-2.0-only
package mqtt

import (
	"context"
	"sync"
)

const maxQueuedCommands = 32

type inboundCommand struct {
	message Message
	ack     byte
	id      uint16
}

// commandWorker owns one ordered callback queue per session. Overflow ends the
// session instead of spawning workers or dropping acknowledged commands. QoS
// acknowledgements return to the session's single writer after the callback;
// cancelled queued commands are not acknowledged as completed.
func commandWorker(ctx context.Context, workers *sync.WaitGroup, receive func(context.Context, Message)) (chan<- inboundCommand, <-chan packet) {
	commands := make(chan inboundCommand, maxQueuedCommands)
	completed := make(chan packet, maxQueuedCommands)
	workers.Go(func() {
		for {
			select {
			case <-ctx.Done():
				return
			case command := <-commands:
				if ctx.Err() != nil {
					return
				}
				if receive != nil {
					receive(ctx, command.message)
				}
				if ctx.Err() != nil {
					return
				}
				if command.ack != 0 {
					ack := packet{header: command.ack, data: []byte{byte(command.id >> 8), byte(command.id)}}
					select {
					case <-ctx.Done():
						return
					case completed <- ack:
					}
				}
			}
		}
	})
	return commands, completed
}
