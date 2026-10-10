// SPDX-License-Identifier: GPL-2.0-only
package mqtt

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"io"
	"net"

	"path/filepath"
	"strconv"
	"sync"
	"time"

	"goteddycloud/internal/config"
)

type Client struct {
	Settings *config.Store
	// Restore returns subscriptions, discovery and current states after every connection.
	Restore func() ([]string, []Message)
	// Receive runs in one bounded, ordered command worker. It must honor cancellation.
	Receive func(context.Context, Message)
	Sleep   func(context.Context, time.Duration) bool
	Dial    func(context.Context, config.Snapshot) (net.Conn, error)
	queue   chan Message
}

func NewClient(settings *config.Store) *Client {
	return &Client{Settings: settings, queue: make(chan Message, 512), Sleep: sleep, Dial: dial}
}
func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
func (c *Client) Publish(m Message) bool {
	if !m.valid() {
		return false
	}
	select {
	case c.queue <- m:
		return true
	default:
		return false
	}
}
func (c *Client) Run(ctx context.Context) {
	delay := 5 * time.Second
	for ctx.Err() == nil {
		snap := c.Settings.Snapshot("")
		if !snap.Bool("mqtt.enabled") {
			if !c.Sleep(ctx, time.Second) {
				return
			}
			continue
		}
		conn, err := c.Dial(ctx, snap)
		connected := false
		if err == nil {
			connected, _ = c.session(ctx, conn, snap)
			conn.Close()
		}
		if ctx.Err() != nil {
			return
		}
		if connected {
			delay = 5 * time.Second
		}
		if !c.Sleep(ctx, delay) {
			return
		}
		if !connected {
			delay = min(delay*2, 60*time.Second)
		}
	}
}
func dial(ctx context.Context, s config.Snapshot) (net.Conn, error) {
	address := net.JoinHostPort(s.String("mqtt.hostname"), strconv.FormatUint(s.Uint("mqtt.port"), 10))
	d := net.Dialer{Timeout: 5 * time.Second}
	if !s.Bool("mqtt.tls_enabled") {
		return d.DialContext(ctx, "tcp", address)
	}
	policy := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: s.String("mqtt.hostname"), InsecureSkipVerify: s.Bool("mqtt.tls_insecure")}
	if path := s.String("mqtt.tls_ca_file"); path != "" {
		if !filepath.IsAbs(path) {
			path = filepath.Join(s.Base, path)
		}
		file, err := s.Storage().Open(path)
		if err != nil {
			return nil, errors.New("MQTT CA unavailable")
		}
		data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
		file.Close()
		if err != nil || len(data) > 1<<20 {
			return nil, errors.New("MQTT CA bounds")
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(data) {
			return nil, errors.New("MQTT CA invalid")
		}
		policy.RootCAs = pool
	}
	return (&tls.Dialer{NetDialer: &d, Config: policy}).DialContext(ctx, "tcp", address)
}
func (c *Client) session(ctx context.Context, conn net.Conn, s config.Snapshot) (bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	workers.Go(func() {
		<-ctx.Done()
		conn.Close()
	})
	defer func() {
		cancel()
		conn.Close()
		workers.Wait()
	}()
	conn.SetDeadline(time.Now().Add(20 * time.Second))
	flags := byte(2 | 4 | 2<<3)
	if s.Bool("mqtt.retain_will") {
		flags |= 32
	}
	payload := append(str(s.String("mqtt.identification")), str(s.String("mqtt.topic")+"/status")...)
	payload = append(payload, str("offline")...)
	if s.String("mqtt.username") != "" {
		flags |= 128
		payload = append(payload, str(s.String("mqtt.username"))...)
	}
	if s.String("mqtt.password") != "" {
		flags |= 64
		payload = append(payload, str(s.String("mqtt.password"))...)
	}
	data := append(str("MQTT"), 4, flags, 0, 30)
	data = append(data, payload...)
	if len(data) > MaxPacket {
		return false, ErrProtocol
	}
	if _, err := conn.Write(frame(0x10, data)); err != nil {
		return false, err
	}
	reader := bufio.NewReader(conn)
	ack, err := readPacket(reader)
	if err != nil {
		return false, err
	}
	if ack.header != 0x20 || len(ack.data) != 2 || ack.data[1] != 0 {
		return false, ErrProtocol
	}
	conn.SetDeadline(time.Time{})
	done := ctx.Done()
	packets := make(chan packet, 32)
	readErr := make(chan error, 1)
	workers.Go(func() {
		for {
			conn.SetReadDeadline(time.Now().Add(45 * time.Second))
			p, err := readPacket(reader)
			if err != nil {
				select {
				case readErr <- err:
				case <-done:
				}
				return
			}
			select {
			case packets <- p:
			case <-done:
				return
			}
		}
	})
	commands, completed := commandWorker(ctx, &workers, c.Receive)
	enqueue := func(message Message, ack byte, id uint16) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case commands <- inboundCommand{message: message, ack: ack, id: id}:
			return nil
		default:
			return errors.New("MQTT command queue is full")
		}
	}
	executing := make(map[uint16]bool)
	// The snapshot supersedes stale telemetry queued during the outage.
	for len(c.queue) > 0 {
		<-c.queue
	}
	subscriptions, messages := []string{}, []Message{}
	if c.Restore != nil {
		subscriptions, messages = c.Restore()
	}
	if len(messages) > 4096 || len(subscriptions) > 1024 {
		return true, ErrProtocol
	}
	messages = append([]Message{{s.String("mqtt.topic") + "/status", "online", 2, s.Bool("mqtt.retain_will")}}, messages...)
	// One goroutine owns writes, packet IDs and QoS state; callbacks cannot block it.
	pending := map[uint16]byte{}
	sentAt := map[uint16]time.Time{}
	incoming := map[uint16]Message{}
	next := uint16(0)
	identifier := func() uint16 {
		for {
			next++
			if next == 0 {
				next++
			}
			if _, ok := pending[next]; !ok {
				return next
			}
		}
	}
	write := func(data []byte) error {
		conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		_, err := conn.Write(data)
		return err
	}
	for _, topic := range subscriptions {
		if !topicValid(topic, true) {
			return true, ErrProtocol
		}
		id := identifier()
		data := []byte{byte(id >> 8), byte(id)}
		data = append(data, str(topic)...)
		data = append(data, 2)
		if err := write(frame(0x82, data)); err != nil {
			return true, err
		}
	}
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	enabledPoll := time.NewTicker(time.Second)
	defer enabledPoll.Stop()
	refresh := time.NewTicker(60 * time.Second)
	defer refresh.Stop()
	for {
		for len(messages) > 0 && len(pending) < 64 {
			m := messages[0]
			messages = messages[1:]
			if !m.valid() {
				return true, ErrProtocol
			}
			id := uint16(0)
			if m.QoS > 0 {
				id = identifier()
				pending[id] = m.QoS
				sentAt[id] = time.Now()
			}
			if err := write(publishPacket(m, id)); err != nil {
				return true, err
			}
		}
		var outbound <-chan Message
		if len(messages) < 512 {
			outbound = c.queue
		}
		select {
		case <-ctx.Done():
			_ = write(frame(0xe0, nil))
			return true, ctx.Err()
		case err := <-readErr:
			return true, err
		case ack := <-completed:
			if ack.header == 0x70 {
				delete(executing, binary.BigEndian.Uint16(ack.data))
			}
			if err := write(frame(ack.header, ack.data)); err != nil {
				return true, err
			}
		case m := <-outbound:
			messages = append(messages, m)
		case <-enabledPoll.C:
			if !c.Settings.Snapshot("").Bool("mqtt.enabled") {
				_ = write(frame(0xe0, nil))
				return true, nil
			}
		case <-refresh.C:
			if !c.Settings.Snapshot("").Bool("mqtt.enabled") {
				_ = write(frame(0xe0, nil))
				return true, nil
			}
			if c.Restore != nil {
				_, restored := c.Restore()
				if len(messages)+len(restored) > 4096 {
					return true, ErrProtocol
				}
				messages = append(messages, restored...)
			}
		case <-tick.C:
			for _, sent := range sentAt {
				if time.Since(sent) > 30*time.Second {
					return true, errors.New("MQTT acknowledgment timeout")
				}
			}
			if err := write(frame(0xc0, nil)); err != nil {
				return true, err
			}
		case p := <-packets:
			switch p.header >> 4 {
			case 3:
				m, id, err := parsePublish(p)
				if err != nil {
					return true, err
				}
				if m.QoS == 2 {
					if len(incoming) >= 64 {
						return true, ErrProtocol
					}
					incoming[id] = m
					if err := write(frame(0x50, []byte{byte(id >> 8), byte(id)})); err != nil {
						return true, err
					}
				} else {
					ack := byte(0)
					if m.QoS == 1 {
						ack = 0x40
					}
					if err := enqueue(m, ack, id); err != nil {
						return true, err
					}
				}
			case 4, 5, 6, 7:
				if len(p.data) != 2 {
					return true, ErrProtocol
				}
				id := binary.BigEndian.Uint16(p.data)
				switch p.header >> 4 {
				case 4, 7:
					delete(pending, id)
					delete(sentAt, id)
				case 5:
					if _, ok := pending[id]; !ok {
						return true, ErrProtocol
					}
					if err := write(frame(0x62, p.data)); err != nil {
						return true, err
					}
				case 6:
					if executing[id] {
						continue
					}
					if m, ok := incoming[id]; ok {
						delete(incoming, id)
						executing[id] = true
						if err := enqueue(m, 0x70, id); err != nil {
							return true, err
						}
					} else if err := write(frame(0x70, p.data)); err != nil {
						return true, err
					}
				}
			case 9:
				if len(p.data) < 3 {
					return true, ErrProtocol
				}
				for _, code := range p.data[2:] {
					if code > 2 {
						return true, ErrProtocol
					}
				}
			case 13:
				if len(p.data) != 0 {
					return true, ErrProtocol
				}
			default:
				return true, ErrProtocol
			}
		}
	}
}

// frameWriter belongs to the embedded connection loop; no other goroutine writes frames.
type frameWriter struct {
	conn net.Conn
}

func (w *frameWriter) write(data []byte) error {
	w.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err := w.conn.Write(data)
	return err
}
