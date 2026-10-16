// SPDX-License-Identifier: GPL-2.0-only
package boxstate

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"google.golang.org/protobuf/proto"

	"goteddycloud/internal/events"
	"goteddycloud/internal/protocol"
)

type State struct {
	UID                                                                             uint64
	Valid                                                                           bool
	AudioID                                                                         uint32
	Volume, VolumeDB                                                                int32
	Playback, Charger, Firmware, FullVersion, Detail, Region, APIDomain, RTNLDomain string
	LastEar                                                                         uint32
	LastEarPress                                                                    uint64
	DoubleEar                                                                       bool
}
type Store struct {
	mu     sync.Mutex
	boxes  map[string]*boxState
	Events *events.Hub
	// ContentInfo runs with per-box ingestion serialized. It may read state,
	// but must not recursively ingest the same box. Configure it before use.
	ContentInfo func(box string, uid uint64, audioID uint32) (title, picture string)
	// TagPresented runs after state is committed, with the same per-box
	// serialization, so discovery and last-tag notifications cannot reorder.
	TagPresented func(box string, uid uint64)
}

// Each box serializes ingestion independently. mu protects map membership and
// published state only; callbacks and event publication never hold it.
type boxState struct {
	ingest sync.Mutex
	state  State
}

func New(hub *events.Hub) *Store { return &Store{boxes: map[string]*boxState{}, Events: hub} }
func (s *Store) Snapshot(id string) State {
	s.mu.Lock()
	defer s.mu.Unlock()
	if box := s.boxes[id]; box != nil {
		return box.state
	}
	return State{}
}
func terminated(data []byte) string {
	if pos := strings.IndexByte(string(data), 0); pos >= 0 {
		return string(data[:pos])
	}
	return string(data)
}
func (s *Store) Ingest(id string, data []byte) error { return s.IngestWithMultipress(id, data, 300) }
func (s *Store) IngestWithMultipress(id string, data []byte, multipress uint64) error {
	if len(data) == 0 || len(data) > 65535 {
		return errors.New("invalid RTNL payload size")
	}
	rpc := new(protocol.TonieRtnlRPC)
	if err := proto.Unmarshal(data, rpc); err != nil {
		return err
	}
	// Validate fixed-size payloads before publishing any state or events.
	if l := rpc.Log2; l != nil {
		needed := 0
		f := l.GetFunction()
		g := l.GetFunctionGroup()
		if g == RTNL2_FUGR_TAG && (f == RTNL2_FUNC_TAG_VALID_CC3200 || f == RTNL2_FUNC_TAG_VALID_ESP32 || f == RTNL2_FUNC_TAG_INVALID_CC3200 || f == RTNL2_FUNC_TAG_INVALID_ESP32 || f == RTNL2_FUNC_TAG_VALID_REMOVED_CC3200 || f == RTNL2_FUNC_TAG_VALID_REMOVED_ESP32) {
			needed = 8
		}
		if g == RTNL2_FUGR_VOLUME && (f == RTNL2_FUNC_VOLUME_CHANGE_CC3200 || f == RTNL2_FUNC_VOLUME_CHANGE_ESP32) {
			needed = 12
		}
		if g == RTNL2_FUGR_TILT && (f == RTNL2_FUNC_TILT_A_ESP32 || f == RTNL2_FUNC_TILT_B_ESP32) {
			needed = 4
		}
		if g == RTNL2_FUGR_AUDIO_A && (f == RTNL2_FUNC_AUDIO_ID_CC3200 || f == RTNL2_FUNC_AUDIO_ID_ESP32) || g == RTNL2_FUGR_AUDIO_B && f == RTNL2_FUNC_AUDIO_ID {
			needed = 4
		}
		if g == RTNL2_FUGR_FIRMWARE && f == RTNL2_FUNC_FIRMWARE_INFOS {
			needed = 8
		}

		if len(l.Field6) < needed {
			return errors.New("truncated RTNL function payload")
		}
	}
	s.mu.Lock()
	box := s.boxes[id]
	if box == nil {
		box = &boxState{}
		s.boxes[id] = box
	}
	s.mu.Unlock()
	box.ingest.Lock()
	defer box.ingest.Unlock()
	s.mu.Lock()
	state := box.state
	s.mu.Unlock()
	var pending []events.Event
	presented := false
	emit := func(name string, data any) { pending = append(pending, events.NewEvent(id, name, data)) }
	playback := func(value string) {
		state.Playback = value
		emit("playback", value)
		if value == "stopped" {
			state.UID = 0
			state.Valid = false
			state.AudioID = 0
		}
	}
	if l := rpc.Log2; l != nil {
		emit("rtnl-raw-log2", map[string]any{"uptime": l.GetUptime(), "sequence": l.GetSequence(), "field3": l.GetField3(), "function_group": l.GetFunctionGroup(), "function": l.GetFunction(), "field6": strings.ToUpper(hex.EncodeToString(l.Field6)), "field8": l.GetField8(), "field9": strings.ToUpper(hex.EncodeToString(l.Field9))})
	}
	if l := rpc.Log3; l != nil {
		emit("rtnl-raw-log3", map[string]any{"datetime": l.GetDatetime(), "field2": l.GetField2()})
		switch l.GetField2() {
		case RTNL3_TYPE_EAR_BIG, RTNL3_TYPE_EAR_SMALL:
			value := "ear-big"
			if l.GetField2() == RTNL3_TYPE_EAR_SMALL {
				value = "ear-small"
			}
			if state.LastEar == l.GetField2() && state.DoubleEar {
				value += "-double"
				state.LastEar = 0
				state.DoubleEar = false
			} else {
				state.LastEar = l.GetField2()
			}
			emit("pressed", value)
		case RTNL3_TYPE_KNOCK_FORWARD:
			emit("knock", "forward")
		case RTNL3_TYPE_KNOCK_BACKWARD:
			emit("knock", "backward")
		case RTNL3_TYPE_TILT_FORWARD:
			emit("tilt", "forward")
		case RTNL3_TYPE_TILT_BACKWARD:
			emit("tilt", "backward")
		case RTNL3_TYPE_CHARGER_ON:
			state.Charger = "ON"
			emit("charger", "on")
		case RTNL3_TYPE_CHARGER_OFF:
			state.Charger = "OFF"
			emit("charger", "off")
		case RTNL3_TYPE_PLAYBACK_STARTING:
			playback("starting")
			playback("started") // pinned C fallthrough
		case RTNL3_TYPE_PLAYBACK_STARTED:
			playback("started")
		case RTNL3_TYPE_PLAYBACK_STOPPED:
			playback("stopped")
		}
	}
	if l := rpc.Log2; l != nil {
		f := l.GetFunction()
		d := l.Field6
		switch l.GetFunctionGroup() {
		case RTNL2_FUGR_TAG:
			switch f {
			case RTNL2_FUNC_TAG_VALID_CC3200, RTNL2_FUNC_TAG_VALID_ESP32:
				state.UID = uint64(binary.BigEndian.Uint32(d)) | uint64(binary.BigEndian.Uint32(d[4:]))<<32
				state.Valid = true
				presented = true
				emit("TagValid", fmt.Sprintf("%016X", state.UID))
			case RTNL2_FUNC_TAG_INVALID_CC3200, RTNL2_FUNC_TAG_INVALID_ESP32:
				state.UID = uint64(binary.BigEndian.Uint32(d)) | uint64(binary.BigEndian.Uint32(d[4:]))<<32
				state.Valid = false
				presented = true
				emit("TagInvalid", fmt.Sprintf("%016X", state.UID))
			case RTNL2_FUNC_TAG_VALID_REMOVED_CC3200, RTNL2_FUNC_TAG_VALID_REMOVED_ESP32:
				state.UID = uint64(binary.BigEndian.Uint32(d)) | uint64(binary.BigEndian.Uint32(d[4:]))<<32
				state.Valid = true
			}
		case RTNL2_FUGR_VOLUME:
			if f == RTNL2_FUNC_VOLUME_CHANGE_CC3200 || f == RTNL2_FUNC_VOLUME_CHANGE_ESP32 {
				state.VolumeDB = int32(binary.LittleEndian.Uint32(d[4:]))
				state.Volume = int32(binary.LittleEndian.Uint32(d[8:]))
				emit("VolumeLevel", fmt.Sprint(state.Volume))
				emit("VolumedB", fmt.Sprint(state.VolumeDB))
				state.DoubleEar = l.GetUptime()-state.LastEarPress < multipress
				state.LastEarPress = l.GetUptime()
			}
		case RTNL2_FUGR_TILT:
			if f == RTNL2_FUNC_TILT_A_ESP32 {
				emit("BoxTilt-A", fmt.Sprint(int32(binary.LittleEndian.Uint32(d))))
			}
			if f == RTNL2_FUNC_TILT_B_ESP32 {
				emit("BoxTilt-B", fmt.Sprint(int32(binary.LittleEndian.Uint32(d))))
			}
		case RTNL2_FUGR_AUDIO_A, RTNL2_FUGR_AUDIO_B:
			if l.GetFunctionGroup() == RTNL2_FUGR_AUDIO_A && (f == RTNL2_FUNC_AUDIO_ID_CC3200 || f == RTNL2_FUNC_AUDIO_ID_ESP32) || l.GetFunctionGroup() == RTNL2_FUGR_AUDIO_B && f == RTNL2_FUNC_AUDIO_ID {
				state.AudioID = binary.LittleEndian.Uint32(d)
				emit("ContentAudioId", fmt.Sprint(state.AudioID))
				title, picture := "Unknown", ""
				if s.ContentInfo != nil {
					title, picture = s.ContentInfo(id, state.UID, state.AudioID)
				}
				emit("ContentTitle", title)
				if picture != "" {
					emit("ContentPicture", picture)
				}
			}
			if l.GetFunctionGroup() == RTNL2_FUGR_AUDIO_B && f == RTNL2_FUNC_AUDIO_PLAY {
				parts := strings.FieldsFunc(terminated(d), func(r rune) bool { return r == '/' })
				if len(parts) >= 3 && len(parts[1]) == 8 && len(parts[2]) == 8 && strings.HasPrefix(parts[1], "0000000") && strings.HasPrefix(parts[2], "000000") {
					playback("stopped")
				}
			}
		case RTNL2_FUGR_FIRMWARE:
			switch f {
			case RTNL2_FUNC_FIRMWARE_VERSION:
				state.Firmware = terminated(d)
			case RTNL2_FUNC_FIRMWARE_FULL_VERSION:
				state.FullVersion = terminated(d)
			case RTNL2_FUNC_FIRMWARE_INFOS:
				state.Detail = terminated(d[8:])
			}
		case RTNL2_FUGR_NETWORK_HTTP:
			if f == RTNL2_FUNC_NETWORK_REGION {
				state.Region = terminated(d)
			}
			if f == RTNL2_FUNC_NETWORK_DOMAIN {
				state.APIDomain = terminated(d)
			}
		case RTNL2_FUGR_NETWORK_TCP:
			if f == RTNL2_FUNC_NETWORK_DOMAIN {
				state.RTNLDomain = terminated(d)
			}
		}
	}

	s.mu.Lock()
	box.state = state
	s.mu.Unlock()
	if presented && s.TagPresented != nil {
		s.TagPresented(id, state.UID)
	}
	// Publish after committing state so event consumers can immediately read it.
	// The per-box ingestion lock preserves event order through publication.
	for _, event := range pending {
		s.Events.Publish(event)
	}
	return nil
}
