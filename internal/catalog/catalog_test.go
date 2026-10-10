// SPDX-License-Identifier: GPL-2.0-only
package catalog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPrecedenceUnknownFieldsAndAtomicReload(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("tonies.json", `[{"model":"M","title":"official","audio_id":["1342177403"],"hash":["0000000000000000000000000000000000000000"]}]`)
	write("tonies.custom.json", `[{"model":"M","title":"custom","audio_id":[123],"hash":["0000000000000000000000000000000000000000"],"extension":{"nested":42}}]`)
	c, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if e := c.ByModel("m"); e.String("title") != "custom" {
		t.Fatal(e)
	}
	e := c.ByAudio(123, nil)
	if e.String("title") != "custom" {
		t.Fatal(e)
	}
	e["title"] = json.RawMessage(`"changed"`)
	if c.ByAudio(123, nil).String("title") != "custom" {
		t.Fatal("mutable snapshot escaped")
	}
	if string(c.Entries(true)[0]["extension"]) != "{\"nested\":42}" {
		t.Fatal("unknown fields lost")
	}
	write("tonies.custom.json", `[{"model":"M","title":"new"}]`)
	write("toniesV2.json", `broken`)
	if err := c.Reload(); err == nil {
		t.Fatal("invalid reload accepted")
	}
	if c.ByModel("M").String("title") != "custom" {
		t.Fatal("partial snapshot published")
	}
	write("toniesV2.json", `[]`)
	if err := c.Reload(); err != nil {
		t.Fatal(err)
	}
	if c.ByModel("M").String("title") != "new" {
		t.Fatal("reload failed")
	}
}
