// SPDX-License-Identifier: GPL-2.0-only
package boxstate

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestRTNLMappingRegistryContract(t *testing.T) {
	data, etag := RTNLMappingJSON()
	var document struct {
		SchemaVersion int                            `json:"schemaVersion"`
		Revision      string                         `json:"revision"`
		Log2          map[string]rtnlDefinition      `json:"log2"`
		Log3          map[string]rtnlLabel           `json:"log3"`
		Events        map[string]rtnlEventDefinition `json:"events"`
	}
	if err := json.Unmarshal([]byte(data), &document); err != nil {
		t.Fatal(err)
	}
	if document.SchemaVersion != 1 || len(document.Log2) < 20 || document.Log3["12"].DE != "Wiedergabe gestartet" || document.Events["audio-sync"].Variants["claim"].DE != "Server: Tag-Anmeldung" {
		t.Fatal(document)
	}
	if etag != `"`+document.Revision+`"` {
		t.Fatal(etag)
	}
	var unsigned map[string]json.RawMessage
	json.Unmarshal([]byte(data), &unsigned)
	delete(unsigned, "revision")
	canonical, _ := json.Marshal(unsigned)
	if want := fmt.Sprintf("sha256:%x", sha256.Sum256(canonical)); want != document.Revision {
		t.Fatal(want, document.Revision)
	}
	for key, definition := range document.Log2 {
		if definition.Label.EN == "" || definition.Label.DE == "" || definition.Fields == nil {
			t.Fatal(key, definition)
		}
		for _, field := range definition.Fields {
			size := 0
			switch field.Type {
			case "uid", "ruid":
				size = 8
			case "u32le", "i32le", "hex32le":
				size = 4
			case "cstring", "next-cstring", "content-ruid":
			default:
				t.Fatal("unsupported public field type", key, field)
			}
			if field.Offset < 0 || field.Offset+size > definition.MinBytes {
				t.Fatal("field exceeds declared minimum", key, field)
			}
		}
	}
	if document.Log2["6-8628"].Interpretation != "inferred" || document.Log2["6-8498"].Interpretation != "partial" || !document.Log2["6-8498"].RawValues || !strings.Contains(document.Log2["6-8498"].Label.DE, "Downloadfortschritt") {
		t.Fatal("lost decoding qualifications")
	}
	if _, exists := document.Log2["24-6139"]; exists {
		t.Fatal("unverified event was assigned a meaning")
	}
}
