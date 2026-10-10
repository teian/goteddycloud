// SPDX-License-Identifier: GPL-2.0-only
package mqtt

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"time"
)

// Server is the device MQTT endpoint, not a retained-message general broker.
// It accepts clean MQTT 3.1.1 sessions, logs/settings requests and fresh-tonies.
type Server struct {
	TLS         *tls.Config
	Fresh       func(string) []string
	Known       func(string) bool
	mu          sync.Mutex
	connections map[net.Conn]struct{}
}

func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	ctx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.connections = map[net.Conn]struct{}{}
	s.mu.Unlock()
	stopped := make(chan struct{})
	var workers sync.WaitGroup
	go func() {
		defer close(stopped)
		<-ctx.Done()
		listener.Close()
		s.mu.Lock()
		connections := make([]net.Conn, 0, len(s.connections))
		for conn := range s.connections {
			connections = append(connections, conn)
		}
		s.mu.Unlock()
		for _, conn := range connections {
			conn.Close()
		}
	}()
	defer func() {
		cancel()
		<-stopped
		workers.Wait()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		s.mu.Lock()
		if ctx.Err() != nil || len(s.connections) >= 32 {
			s.mu.Unlock()
			conn.Close()
			continue
		}
		s.connections[conn] = struct{}{}
		s.mu.Unlock()
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer conn.Close()
			defer func() { s.mu.Lock(); delete(s.connections, conn); s.mu.Unlock() }()
			_ = s.connection(ctx, conn)
		}()
	}
}
func (s *Server) connection(ctx context.Context, raw net.Conn) error {
	conn := raw
	if s.TLS != nil {
		secured := tls.Server(raw, s.TLS)
		handshake, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := secured.HandshakeContext(handshake)
		cancel()
		if err != nil {
			return err
		}
		conn = secured
	}
	reader := bufio.NewReader(conn)
	writer := &frameWriter{conn: conn}
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	connect, err := readPacket(reader)
	if err != nil {
		return err
	}
	if len(connect.data) > 4091 {
		return ErrProtocol
	}
	if connect.header != 0x10 {
		return ErrProtocol
	}
	data := connect.data
	protocol, err := takeString(&data)
	if err != nil || protocol != "MQTT" || len(data) < 4 || data[0] != 4 || data[1]&1 != 0 {
		return ErrProtocol
	}
	flags := data[1]
	keepalive := binary.BigEndian.Uint16(data[2:])
	data = data[4:]
	if _, err := takeString(&data); err != nil {
		return err
	}
	if flags&4 != 0 {
		if _, err := takeString(&data); err != nil {
			return err
		}
		if _, err := takeString(&data); err != nil {
			return err
		}
	}
	if flags&128 != 0 {
		if _, err := takeString(&data); err != nil {
			return err
		}
	}
	if flags&64 != 0 {
		if _, err := takeString(&data); err != nil {
			return err
		}
	}
	if len(data) != 0 {
		return ErrProtocol
	}
	if err := writer.write(frame(0x20, []byte{0, 0})); err != nil {
		return err
	}
	subscriptions := map[string]bool{}
	incoming := map[uint16]Message{}
	freshSent := map[string]string{}
	packets := make(chan packet, 8)
	readErrors := make(chan error, 1)
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			timeout := 45 * time.Second
			if keepalive > 0 {
				timeout = time.Duration(keepalive) * 1500 * time.Millisecond
			}
			conn.SetReadDeadline(time.Now().Add(timeout))
			p, err := readPacket(reader)
			if err != nil {
				select {
				case readErrors <- err:
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
	}()
	fresh := func() error {
		if s.Fresh == nil {
			return nil
		}
		for topic := range subscriptions {
			parts := strings.Split(topic, "/")
			if len(parts) != 3 || parts[0] != "toniebox" || parts[2] != "fresh-tonies" || s.Known == nil || !s.Known(parts[1]) {
				continue
			}
			values := s.Fresh(parts[1])
			serialized, _ := json.Marshal(values)
			if freshSent[topic] == string(serialized) {
				continue
			}
			freshSent[topic] = string(serialized)
			for _, ruid := range values {
				payload, _ := json.Marshal(map[string]string{"tonie": strings.ToUpper(ruid)})
				if err := writer.write(publishPacket(Message{topic, string(payload), 0, false}, 0)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	deliver := func(message Message) error {
		parts := strings.Split(message.Topic, "/")
		if len(parts) == 4 && parts[0] == "toniebox" && parts[2] == "settings" && parts[3] == "request" {
			return writer.write(publishPacket(Message{"toniebox/" + parts[1] + "/settings/desired", DesiredSettings, 0, false}, 0))
		}
		return nil
	}
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-readErrors:
			return err
		case <-tick.C:
			if err := fresh(); err != nil {
				return err
			}
		case p := <-packets:
			if len(p.data) > 4091 {
				return ErrProtocol
			}
			switch p.header >> 4 {
			case 3:
				message, id, err := parsePublish(p)
				if err != nil {
					return err
				}
				if message.QoS == 2 {
					if len(incoming) >= 64 {
						return ErrProtocol
					}
					incoming[id] = message
				} else if err := deliver(message); err != nil {
					return err
				}
				if message.QoS > 0 {
					h := byte(0x40)
					if message.QoS == 2 {
						h = 0x50
					}
					if err := writer.write(frame(h, []byte{byte(id >> 8), byte(id)})); err != nil {
						return err
					}
				}
			case 6:
				if p.header != 0x62 || len(p.data) != 2 {
					return ErrProtocol
				}
				id := binary.BigEndian.Uint16(p.data)
				if message, ok := incoming[id]; ok {
					delete(incoming, id)
					if err := deliver(message); err != nil {
						return err
					}
				}
				if err := writer.write(frame(0x70, p.data)); err != nil {
					return err
				}
			case 8:
				if p.header != 0x82 || len(p.data) < 5 {
					return ErrProtocol
				}
				data := p.data[2:]
				codes := []byte{}
				for len(data) > 0 {
					topic, err := takeString(&data)
					if err != nil || len(data) < 1 || data[0] > 2 || !topicValid(topic, true) {
						return ErrProtocol
					}
					data = data[1:]
					if len(subscriptions) >= 32 && !subscriptions[topic] {
						codes = append(codes, 128)
						continue
					}
					subscriptions[topic] = true
					codes = append(codes, 0)
				}
				if err := writer.write(frame(0x90, append(p.data[:2], codes...))); err != nil {
					return err
				}
				if err := fresh(); err != nil {
					return err
				}
			case 10:
				if p.header != 0xa2 || len(p.data) < 4 {
					return ErrProtocol
				}
				data := p.data[2:]
				for len(data) > 0 {
					topic, err := takeString(&data)
					if err != nil {
						return err
					}
					delete(subscriptions, topic)
				}
				if err := writer.write(frame(0xb0, p.data[:2])); err != nil {
					return err
				}
			case 12:
				if p.header != 0xc0 || len(p.data) != 0 {
					return ErrProtocol
				}
				if err := writer.write(frame(0xd0, nil)); err != nil {
					return err
				}
			case 14:
				if p.header != 0xe0 || len(p.data) != 0 {
					return ErrProtocol
				}
				return nil
			default:
				return errors.New("unsupported device MQTT packet")
			}
		}
	}
}
