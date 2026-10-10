// SPDX-License-Identifier: GPL-2.0-only
package mqtt

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"goteddycloud/internal/config"
	"goteddycloud/internal/events"
)

func TestRestoreAndTelemetryRecordConcurrent(t *testing.T) {
	settings, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Close()
	const id = "001122334455"
	if err := settings.Set(id, "boxName", "Test"); err != nil {
		t.Fatal(err)
	}
	integration := NewIntegration(settings, events.New(), nil)
	integration.Record(id, "volume", "initial")
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 100 {
			integration.Record(id, "volume", fmt.Sprint(i))
		}
	}()
	go func() {
		defer wg.Done()
		for range 100 {
			_, messages := integration.Restore()
			found := false
			for _, message := range messages {
				if strings.HasSuffix(message.Topic, "/box/"+id+"/volume") {
					found = true
				}
			}
			if !found {
				t.Error("reconnect omitted registered telemetry")
			}
		}
	}()
	wg.Wait()
	_, messages := integration.Restore()
	for _, message := range messages {
		if strings.HasSuffix(message.Topic, "/box/"+id+"/volume") {
			if message.Payload != "99" {
				t.Fatal("lost latest telemetry", message.Payload)
			}
			return
		}
	}
	t.Fatal("missing latest telemetry")
}
