// SPDX-License-Identifier: GPL-2.0-only
package mqtt

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"
	"sync"

	"goteddycloud/internal/boxcommands"
	"goteddycloud/internal/config"
	"goteddycloud/internal/events"
)

// Entity declarations and settings payload are derived from the pinned GPL-2.0
// teddyCloud mqtt.c/mqtt_server.c; see NOTICE and docs/reference.json.
//
//go:embed box_entities.json
var boxEntitiesJSON []byte

//go:embed desired_settings.json
var DesiredSettings string

//go:embed server_settings.json
var serverSettingsJSON []byte

type Integration struct {
	Client            *Client
	Settings          *config.Store
	Hub               *events.Hub
	Execute           func(id, command, payload string) error
	Version, Hardware string
	PlaybackStop      func(string)
	mu                sync.Mutex
	states            map[string]map[string]string
	registered        map[string]bool
}

func NewIntegration(settings *config.Store, hub *events.Hub, execute func(string, string, string) error) *Integration {
	m := &Integration{Settings: settings, Hub: hub, Execute: execute, Version: "Go rewrite", states: map[string]map[string]string{}, registered: map[string]bool{}}
	m.Client = NewClient(settings)
	m.Client.Restore = m.Restore
	m.Client.Receive = m.receive
	return m
}
func (m *Integration) Run(ctx context.Context) {
	clientDone := make(chan struct{})
	go func() { defer close(clientDone); m.Client.Run(ctx) }()
	defer func() { <-clientDone }()
	for ctx.Err() == nil {
		channel, cancel := m.Hub.Observe()
		for {
			select {
			case <-ctx.Done():
				cancel()
				return
			case event, ok := <-channel:
				if !ok {
					cancel()
					goto reconnect
				}
				m.Event(event)
			}
		}
	reconnect:
	}
}
func (m *Integration) Record(id, name, value string) {
	if id == "" || !config.ValidBoxID(id) || len(name) > 64 || len(value) > MaxPacket-1024 {
		return
	}
	found := false
	for _, box := range m.Settings.BoxIDs() {
		if box == id {
			found = true
			break
		}
	}
	if !found {
		return
	}
	m.mu.Lock()
	if !m.registered[id] && len(m.registered) >= 32 {
		m.mu.Unlock()
		return
	}
	fresh := !m.registered[id]
	m.registered[id] = true
	if m.states[id] == nil {
		m.states[id] = map[string]string{}
	}
	// Keep bounded latest telemetry for replay; event-only messages need no replay.
	if name != "VolUp" && name != "VolDown" && !strings.HasPrefix(name, "Knock") && !strings.HasPrefix(name, "Tilt") {
		if _, exists := m.states[id][name]; len(m.states[id]) < 64 || exists {
			m.states[id][name] = value
		}
	}
	m.mu.Unlock()
	if fresh {
		_, messages := m.BoxDiscovery(id)
		for _, message := range messages {
			m.Client.Publish(message)
		}
	}
	snap := m.Settings.Snapshot("")
	m.Client.Publish(Message{snap.String("mqtt.topic") + "/box/" + id + "/" + name, value, byte(snap.Uint("mqtt.qosLevel")), false})
}
func (m *Integration) Event(event events.Event) {
	var value string
	if json.Unmarshal(event.Data, &value) != nil {
		return
	}
	if event.Box == "" {
		snap := m.Settings.Snapshot("")
		m.Client.Publish(Message{snap.String("mqtt.topic") + "/event/" + event.Name, value, byte(snap.Uint("mqtt.qosLevel")), false})
		return
	}
	name := event.Name
	switch name {
	case "playback":
		if value == "stopped" {
			if m.PlaybackStop != nil {
				m.PlaybackStop(event.Box)
			}
			m.Record(event.Box, "Playback", "OFF")
			m.Record(event.Box, "TagValid", "")
			m.Record(event.Box, "ContentAudioId", "")
			m.Record(event.Box, "ContentTitle", "")
			m.Record(event.Box, "ContentPicture", m.Settings.Snapshot("").String("core.host_url")+"/img_empty.png")
		} else if value == "starting" {
			m.Record(event.Box, "Playback", "OFF")
		} else {
			m.Record(event.Box, "Playback", "ON")
		}
		return
	case "TagValid", "TagInvalid":
		other := "TagValid"
		if name == other {
			other = "TagInvalid"
		}
		m.Record(event.Box, other, "")
	case "charger":
		name = "Charger"
		value = strings.ToUpper(value)
	case "pressed":
		name = "VolUp"
		if strings.HasPrefix(value, "ear-small") {
			name = "VolDown"
		}
		value = `{"event_type": "pressed"}`
		var original string
		json.Unmarshal(event.Data, &original)
		if strings.HasSuffix(original, "-double") {
			value = `{"event_type": "double-pressed"}`
		}
	case "knock", "tilt":
		prefix := "Knock"
		if name == "tilt" {
			prefix = "Tilt"
		}
		name = prefix + "Backward"
		if value == "forward" {
			name = prefix + "Forward"
		}
		value = `{"event_type": "triggered"}`
	case "BoxTilt-A", "BoxTilt-B":
		name = "BoxTilt"
	case "ContentPicture":
		if strings.HasPrefix(value, "/") {
			value = m.Settings.Snapshot("").String("core.host_url") + value
		}
	case "VolumeLevel", "VolumedB", "ContentAudioId", "ContentTitle":
	default:
		return
	}
	m.Record(event.Box, name, value)
}
func (m *Integration) receive(ctx context.Context, message Message) {
	if ctx.Err() != nil {
		return
	}
	snap := m.Settings.Snapshot("")
	prefix := snap.String("mqtt.topic") + "/"
	relative, ok := strings.CutPrefix(message.Topic, prefix)
	if !ok {
		return
	}
	if strings.HasPrefix(relative, "box/") {
		parts := strings.Split(relative, "/")
		if len(parts) != 4 || parts[3] != "set" {
			return
		}
		if m.Execute == nil || m.Execute(parts[1], parts[2], message.Payload) != nil {
			return
		}
		if key, _, _, boolean := boxcommands.Setting(parts[2]); key != "" {
			value := m.Settings.Snapshot(parts[1]).String(key)
			if boolean {
				value = strings.ToUpper(value)
			}
			m.Record(parts[1], parts[2], value)
		}
		return
	}
	if !strings.HasSuffix(relative, "/command") {
		return
	}
	name := strings.TrimSuffix(relative, "/command")
	for _, definition := range serverDefinitions() {
		if strings.ReplaceAll(definition.ID, ".", "-") != name {
			continue
		}
		value := message.Payload
		if definition.Type == "bool" {
			if !strings.EqualFold(value, "TRUE") && !strings.EqualFold(value, "FALSE") {
				return
			}
			value = strings.ToLower(value)
		}
		if definition.Type == "hex" {
			number, err := strconv.ParseUint(value, 16, 32)
			if err != nil {
				return
			}
			value = strconv.FormatUint(number, 10)
		}
		if m.Settings.Set("", definition.ID, value) != nil {
			return
		}
		m.Client.Publish(Message{prefix + name + "/status", settingPayload(definition, m.Settings.Snapshot("")), byte(snap.Uint("mqtt.qosLevel")), false})
		return
	}
}
func serverDefinitions() []config.Definition {
	var ids []string
	json.Unmarshal(serverSettingsJSON, &ids)
	all := map[string]config.Definition{}
	for _, d := range config.Definitions() {
		all[d.ID] = d
	}
	selected := make([]config.Definition, 0, len(ids))
	for _, id := range ids {
		if d, ok := all[id]; ok {
			selected = append(selected, d)
		}
	}
	return selected
}

func settingPayload(d config.Definition, s config.Snapshot) string {
	value := s.String(d.ID)
	switch d.Type {
	case "bool":
		return strings.ToUpper(value)
	case "float":
		number, _ := strconv.ParseFloat(value, 64)
		return strconv.FormatFloat(number, 'f', 4, 64)
	case "int":
		number, _ := strconv.ParseInt(value, 10, 32)
		return strconv.FormatUint(uint64(uint32(number)), 10)
	}
	return value
}
func (m *Integration) device(id string, box bool) map[string]any {
	snap := m.Settings.Snapshot("")
	device := map[string]any{"name": snap.String("hass.name"), "ids": snap.String("hass.id"), "cu": snap.String("core.host_url"), "mf": "Team RevvoX", "mdl": "teddyCloud", "sw": m.Version, "hw": m.Hardware}
	if box {
		s := m.Settings.Snapshot(id)
		device["name"] = s.String("boxName")
		device["ids"] = snap.String("mqtt.topic") + "_Box_" + id
		device["mf"] = "tonies"
		device["mdl"] = "Toniebox"
		device["via_device"] = snap.String("hass.id")
		device["sw"] = s.String("internal.toniebox_firmware.rtnlFullVersion")
		device["hw"] = s.String("internal.toniebox_firmware.boxIC")
	}
	return device
}
func (m *Integration) discovery(id, base string, entity map[string]any, device map[string]any) Message {
	snap := m.Settings.Snapshot("")
	kind := entity["type"].(string)
	eid := entity["id"].(string)
	object := map[string]any{"name": entity["name"], "uniq_id": id + "_" + eid, "avty_t": snap.String("mqtt.topic") + "/status", "dev": device}
	for key, value := range entity {
		switch key {
		case "type", "id", "name":
			continue
		case "dev_class":
			key = "dev_cla"
		case "state_class":
			key = "stat_cla"
		case "event_types":
			key = "evt_typ"
		case "options":
			key = "ops"
		}
		if text, ok := value.(string); ok {
			switch key {
			case "cmd_t", "stat_t", "url_t", "val_tpl":
				value = strings.ReplaceAll(text, "%s", base)
			case "evt_typ", "ops":
				value = strings.Split(text, ";")
			}
		}
		object[key] = value
	}
	if kind == "switch" {
		object["pl_on"] = "TRUE"
		object["pl_off"] = "FALSE"
	}
	data, _ := json.Marshal(object)
	return Message{"homeassistant/" + kind + "/" + id + "/" + eid + "/config", string(data), byte(snap.Uint("mqtt.qosLevel")), false}
}
func (m *Integration) BoxDiscovery(id string) ([]string, []Message) {
	snap := m.Settings.Snapshot("")
	box := m.Settings.Snapshot(id)
	base := snap.String("mqtt.topic") + "/box/" + id
	identifier := snap.String("mqtt.topic") + "_Box_" + id
	var entities []map[string]any
	json.Unmarshal(boxEntitiesJSON, &entities)
	subscriptions := []string{}
	messages := []Message{}
	device := m.device(id, true)
	for _, entity := range entities {
		messages = append(messages, m.discovery(identifier, base, entity, device))
		if topic, ok := entity["cmd_t"].(string); ok {
			subscriptions = append(subscriptions, strings.ReplaceAll(topic, "%s", base))
		}
		key, _, _, boolean := boxcommands.Setting(entity["id"].(string))
		if key != "" {
			value := box.String(key)
			if boolean {
				value = strings.ToUpper(value)
			}
			messages = append(messages, Message{base + "/" + entity["id"].(string), value, byte(snap.Uint("mqtt.qosLevel")), false})
		}
	}
	return subscriptions, messages
}
func (m *Integration) Restore() ([]string, []Message) {
	snap := m.Settings.Snapshot("")
	base := snap.String("mqtt.topic")
	subscriptions := []string{base + "/*", base + "/box/+/+/set"}
	messages := []Message{}
	device := m.device("", false)
	for _, definition := range serverDefinitions() {
		name := strings.ReplaceAll(definition.ID, ".", "-")
		kind := "text"
		if definition.Type == "bool" {
			kind = "switch"
		} else if definition.Type != "string" {
			kind = "number"
		}
		entity := map[string]any{"id": name, "type": kind, "name": fmt.Sprintf("%s - %s", definition.ID, definition.Description), "stat_t": "%s/" + name + "/status", "cmd_t": "%s/" + name + "/command"}
		if kind == "number" {
			entity["min"] = int32(max(-2147483648, min(definition.Min, 2147483647)))
			entity["max"] = int32(max(-2147483648, min(definition.Max, 2147483647)))
		}
		messages = append(messages, m.discovery(snap.String("hass.id"), base, entity, device), Message{base + "/" + name + "/status", settingPayload(definition, snap), byte(snap.Uint("mqtt.qosLevel")), false})
		subscriptions = append(subscriptions, base+"/"+name+"/command")
	}
	m.mu.Lock()
	states := make(map[string]map[string]string, len(m.registered))
	for id := range m.registered {
		states[id] = maps.Clone(m.states[id])
	}
	m.mu.Unlock()
	for id, state := range states {
		subs, boxMessages := m.BoxDiscovery(id)
		subscriptions = append(subscriptions, subs...)
		messages = append(messages, boxMessages...)
		for name, value := range state {
			messages = append(messages, Message{base + "/box/" + id + "/" + name, value, byte(snap.Uint("mqtt.qosLevel")), false})
		}
	}
	return subscriptions, messages
}
