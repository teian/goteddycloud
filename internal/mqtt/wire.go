// SPDX-License-Identifier: GPL-2.0-only
// Package mqtt implements the bounded MQTT 3.1.1 transport used by teddyCloud.
package mqtt

import (
	"bufio"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

const MaxPacket = 65536

var ErrProtocol = errors.New("invalid or unsupported MQTT packet")

type packet struct {
	header byte
	data   []byte
}

func readPacket(r *bufio.Reader) (packet, error) {
	h, err := r.ReadByte()
	if err != nil {
		return packet{}, err
	}
	size, mult := 0, 1
	for i := 0; i < 4; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return packet{}, err
		}
		size += int(b&127) * mult
		if size > MaxPacket {
			return packet{}, ErrProtocol
		}
		if b&128 == 0 {
			data := make([]byte, size)
			_, err = io.ReadFull(r, data)
			return packet{h, data}, err
		}
		mult *= 128
	}
	return packet{}, ErrProtocol
}
func frame(h byte, data []byte) []byte {
	out := []byte{h}
	size := len(data)
	for {
		b := byte(size % 128)
		size /= 128
		if size > 0 {
			b |= 128
		}
		out = append(out, b)
		if size == 0 {
			break
		}
	}
	return append(out, data...)
}
func str(value string) []byte {
	return append([]byte{byte(len(value) >> 8), byte(len(value))}, []byte(value)...)
}
func takeString(data *[]byte) (string, error) {
	if len(*data) < 2 {
		return "", ErrProtocol
	}
	size := int(binary.BigEndian.Uint16(*data))
	*data = (*data)[2:]
	if size > len(*data) {
		return "", ErrProtocol
	}
	value := string((*data)[:size])
	*data = (*data)[size:]
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return "", ErrProtocol
	}
	return value, nil
}
func topicValid(topic string, filter bool) bool {
	if topic == "" || len(topic) > 256 || !utf8.ValidString(topic) || strings.ContainsRune(topic, 0) {
		return false
	}
	if !filter {
		return !strings.ContainsAny(topic, "+#")
	}
	levels := strings.Split(topic, "/")
	for i, level := range levels {
		if strings.ContainsAny(level, "+#") && level != "+" && !(level == "#" && i == len(levels)-1) {
			return false
		}
	}
	return true
}
func Match(filter, topic string) bool {
	if !topicValid(filter, true) || !topicValid(topic, false) {
		return false
	}
	if strings.HasPrefix(topic, "$") && (strings.HasPrefix(filter, "+") || strings.HasPrefix(filter, "#")) {
		return false
	}
	f, t := strings.Split(filter, "/"), strings.Split(topic, "/")
	for i, level := range f {
		if level == "#" {
			return true
		}
		if i >= len(t) || level != "+" && level != t[i] {
			return false
		}
	}
	return len(f) == len(t)
}

type Message struct {
	Topic, Payload string
	QoS            byte
	Retain         bool
}

func (m Message) valid() bool {
	return topicValid(m.Topic, false) && len(m.Payload)+len(m.Topic)+4 <= MaxPacket && m.QoS <= 2
}
func publishPacket(m Message, id uint16) []byte {
	data := str(m.Topic)
	if m.QoS > 0 {
		data = append(data, byte(id>>8), byte(id))
	}
	data = append(data, []byte(m.Payload)...)
	h := byte(0x30) | m.QoS<<1
	if m.Retain {
		h |= 1
	}
	return frame(h, data)
}
func parsePublish(p packet) (Message, uint16, error) {
	data := p.data
	topic, err := takeString(&data)
	qos := (p.header >> 1) & 3
	id := uint16(0)
	if err != nil || !topicValid(topic, false) || qos == 3 {
		return Message{}, 0, ErrProtocol
	}
	if qos > 0 {
		if len(data) < 2 {
			return Message{}, 0, ErrProtocol
		}
		id = binary.BigEndian.Uint16(data)
		data = data[2:]
		if id == 0 {
			return Message{}, 0, ErrProtocol
		}
	}
	return Message{topic, string(data), qos, p.header&1 != 0}, id, nil
}
