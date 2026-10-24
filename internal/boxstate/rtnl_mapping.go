// SPDX-License-Identifier: GPL-2.0-only
package boxstate

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"
)

// This registry is the source of RTNL names and payload layouts for clients.
// Field types describe byte operations, never executable client code. Unknown
// payload words retain their raw values rather than acquiring guessed units.
type rtnlLabel struct {
	EN string `json:"en"`
	DE string `json:"de"`
}

type rtnlField struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	Offset int    `json:"offset,omitempty"`
}

type rtnlDefinition struct {
	Label          rtnlLabel   `json:"label"`
	MinBytes       int         `json:"minBytes"`
	Fields         []rtnlField `json:"fields"`
	Interpretation string      `json:"interpretation,omitempty"`
	RawValues      bool        `json:"rawValues,omitempty"`
}

type rtnlEventDefinition struct {
	Label    rtnlLabel            `json:"label"`
	Selector string               `json:"selector,omitempty"`
	Variants map[string]rtnlLabel `json:"variants,omitempty"`
}

func rtnlMappingDocument() map[string]any {
	label := func(en, de string) rtnlLabel { return rtnlLabel{en, de} }
	field := func(name, kind string, offset int) rtnlField { return rtnlField{name, kind, offset} }
	tag := []rtnlField{field("uid", "uid", 0), field("ruid", "ruid", 0)}
	path := []rtnlField{field("path", "cstring", 0), field("ruid", "content-ruid", 0)}
	log2 := map[string]rtnlDefinition{}
	add := func(group int, functions []int, en, de string, minimum int, fields []rtnlField) {
		for _, fn := range functions {
			log2[fmt.Sprintf("%d-%d", group, fn)] = rtnlDefinition{Label: label(en, de), MinBytes: minimum, Fields: fields}
		}
	}
	add(RTNL2_FUGR_TAG, []int{RTNL2_FUNC_TAG_VALID_CC3200, RTNL2_FUNC_TAG_VALID_ESP32}, "Valid tag detected", "Gültiger Tag erkannt", 8, tag)
	add(RTNL2_FUGR_TAG, []int{RTNL2_FUNC_TAG_INVALID_CC3200, RTNL2_FUNC_TAG_INVALID_ESP32}, "Invalid tag detected", "Ungültiger Tag erkannt", 8, tag)
	add(RTNL2_FUGR_TAG, []int{RTNL2_FUNC_TAG_VALID_REMOVED_CC3200, RTNL2_FUNC_TAG_VALID_REMOVED_ESP32}, "Tag removed", "Tag entfernt", 8, tag)
	audio := []rtnlField{field("audioId", "u32le", 0), field("audioIdHex", "hex32le", 0)}
	add(RTNL2_FUGR_AUDIO_A, []int{RTNL2_FUNC_AUDIO_ID_CC3200, RTNL2_FUNC_AUDIO_ID_ESP32}, "Audio ID reported", "Audio-ID gemeldet", 4, audio)
	add(RTNL2_FUGR_AUDIO_B, []int{RTNL2_FUNC_AUDIO_ID, RTNL2_FUNC_AUDIO_ID2_ESP32}, "Audio ID reported", "Audio-ID gemeldet", 4, audio)
	add(RTNL2_FUGR_AUDIO_B, []int{RTNL2_FUNC_AUDIO_PLAY}, "Playing local file", "Lokale Datei wird abgespielt", 0, path)
	add(RTNL2_FUGR_NETWORK_HTTP, []int{RTNL2_FUNC_NETWORK_HTTP_PATH}, "HTTP request", "HTTP-Anfrage", 0,
		[]rtnlField{field("path", "cstring", 0), field("host", "next-cstring", 0)})
	for _, group := range []int{RTNL2_FUGR_NETWORK_HTTP, RTNL2_FUGR_NETWORK_TCP} {
		add(group, []int{RTNL2_FUNC_NETWORK_DOMAIN}, "Network domain", "Netzwerk-Domain", 0, []rtnlField{field("domain", "cstring", 0)})
	}
	add(RTNL2_FUGR_FIRMWARE, []int{RTNL2_FUNC_FIRMWARE_FULL_VERSION, RTNL2_FUNC_FIRMWARE_VERSION}, "Firmware information", "Firmware-Information", 0, []rtnlField{field("text", "cstring", 0)})
	add(RTNL2_FUGR_FIRMWARE, []int{RTNL2_FUNC_FIRMWARE_INFOS}, "Firmware information", "Firmware-Information", 8, []rtnlField{field("text", "cstring", 8)})
	add(RTNL2_FUGR_VOLUME, []int{RTNL2_FUNC_VOLUME_CHANGE_CC3200, RTNL2_FUNC_VOLUME_CHANGE_ESP32}, "Volume changed", "Lautstärke geändert", 12,
		[]rtnlField{field("volumeDb", "i32le", 4), field("volume", "i32le", 8)})
	add(RTNL2_FUGR_TILT, []int{RTNL2_FUNC_TILT_A_ESP32, RTNL2_FUNC_TILT_B_ESP32}, "Orientation reported", "Ausrichtung gemeldet", 4, []rtnlField{field("value", "i32le", 0)})
	// Observed CC3200 format; its field interpretation is not established by the
	// reference. Keep this qualification in both the name and exported fields.
	log2["6-8628"] = rtnlDefinition{Label: label("HTTP result (inferred format)", "HTTP-Ergebnis (Format abgeleitet)"), MinBytes: 16,
		Fields: append(append([]rtnlField{}, tag...), field("resultRaw", "u32le", 8), field("statusCandidate", "u32le", 12)), Interpretation: "inferred"}
	add(RTNL2_FUGR_NETWORK_HTTP, []int{RTNL2_FUNC_NETWORK_DOWNLOAD_PROGRESS}, "Download progress (raw counters)", "Downloadfortschritt (Rohzähler)", 0, []rtnlField{})
	progress := log2[fmt.Sprintf("%d-%d", RTNL2_FUGR_NETWORK_HTTP, RTNL2_FUNC_NETWORK_DOWNLOAD_PROGRESS)]
	progress.RawValues = true
	progress.Interpretation = "partial"
	log2[fmt.Sprintf("%d-%d", RTNL2_FUGR_NETWORK_HTTP, RTNL2_FUNC_NETWORK_DOWNLOAD_PROGRESS)] = progress
	add(RTNL2_FUGR_AUDIO_B, []int{RTNL2_FUNC_AUDIO_1}, "Audio file event (partially decoded)", "Audiodatei-Ereignis (teilweise decodiert)", 0, path)
	partial := log2[fmt.Sprintf("%d-%d", RTNL2_FUGR_AUDIO_B, RTNL2_FUNC_AUDIO_1)]
	partial.RawValues = true
	partial.Interpretation = "partial"
	log2[fmt.Sprintf("%d-%d", RTNL2_FUGR_AUDIO_B, RTNL2_FUNC_AUDIO_1)] = partial
	return map[string]any{
		"schemaVersion": 1,
		"reference":     "https://github.com/toniebox-reverse-engineering/teddycloud/blob/78c3b38e36f665d3a5fffe907f73a02725176377/include/handler_rtnl.h",
		"log2":          log2,
		"log3": map[int]rtnlLabel{
			RTNL3_TYPE_EAR_BIG: label("Large ear pressed", "Großes Ohr gedrückt"), RTNL3_TYPE_EAR_SMALL: label("Small ear pressed", "Kleines Ohr gedrückt"),
			RTNL3_TYPE_KNOCK_FORWARD: label("Knock forward", "Klopfen vorwärts"), RTNL3_TYPE_KNOCK_BACKWARD: label("Knock backward", "Klopfen rückwärts"),
			RTNL3_TYPE_TILT_FORWARD: label("Tilt forward", "Nach vorne geneigt"), RTNL3_TYPE_TILT_BACKWARD: label("Tilt backward", "Nach hinten geneigt"),
			RTNL3_TYPE_CHARGER_ON: label("Charger connected", "Ladestation verbunden"), RTNL3_TYPE_CHARGER_OFF: label("Charger disconnected", "Ladestation getrennt"),
			RTNL3_TYPE_PLAYBACK_STARTING: label("Playback starting", "Wiedergabe startet"), RTNL3_TYPE_PLAYBACK_STARTED: label("Playback started", "Wiedergabe gestartet"), RTNL3_TYPE_PLAYBACK_STOPPED: label("Playback stopped", "Wiedergabe gestoppt"),
		},
		"events": map[string]rtnlEventDefinition{
			"tag-present": {Label: label("Tag presented", "Tag aufgelegt")}, "tag-updated": {Label: label("Tag information updated", "Tag-Informationen aktualisiert")},
			"audio-sync": {Label: label("Server: audio synchronization", "Server: Audiosynchronisierung"), Selector: "phase", Variants: map[string]rtnlLabel{
				"freshness-response": label("Server: synchronization response", "Server: Synchronisierungsantwort"), "claim": label("Server: tag claim", "Server: Tag-Anmeldung"),
				"local-content-transfer": label("Server: audio transfer", "Server: Audioübertragung"), "local-chapter-transfer": label("Server: chapter transfer", "Server: Kapitelübertragung"),
				"content-unavailable": label("Server: audio unavailable", "Server: Audio nicht verfügbar"),
			}},
		},
	}
}

var cachedRTNLMapping = sync.OnceValue(func() [2]string {
	document := rtnlMappingDocument()
	canonical, _ := json.Marshal(document)
	revision := fmt.Sprintf("sha256:%x", sha256.Sum256(canonical))
	document["revision"] = revision
	data, _ := json.Marshal(document)
	return [2]string{string(data), `"` + revision + `"`}
})

// RTNLMappingJSON returns immutable JSON and an ETag derived from the complete
// registry, including labels, layouts and confidence annotations.
func RTNLMappingJSON() (string, string) {
	value := cachedRTNLMapping()
	return value[0], value[1]
}
