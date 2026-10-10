// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestAssignmentConsumedByOnlyOneUnknownTag(t *testing.T) {
	s := fixtureApp(t)
	snap := s.Settings.Snapshot("")
	source := filepath.Join(snap.String("internal.librarydirfull"), "synthetic.taf")
	if err := os.WriteFile(source, syntheticTAF(t), 0600); err != nil {
		t.Fatal(err)
	}
	// The synthetic fixture is already a valid, bounded TAF.
	if err := s.Settings.SetRuntime("", map[string]string{"internal.assign_unknown": source}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	for _, uid := range []string{"1100000011000000", "1200000012000000"} {
		wg.Add(1)
		go func(uid string) { defer wg.Done(); codes <- perform(s, "GET", "/v1/content/"+uid, nil, true).Code }(uid)
	}
	wg.Wait()
	close(codes)
	success := 0
	for code := range codes {
		if code == 200 {
			success++
		} else if code != 404 {
			t.Fatal(code)
		}
	}
	if success != 1 {
		t.Fatal("assignment reused", success)
	}
	if s.Settings.Snapshot("").String("internal.assign_unknown") != "" {
		t.Fatal("pending assignment retained")
	}
	before, _ := os.ReadFile(source)
	if len(before) == 0 {
		t.Fatal("source damaged")
	}
}
func TestTagInfoListingsAndTriggerHooks(t *testing.T) {
	s := fixtureApp(t)
	if err := os.WriteFile(filepath.Join(s.Settings.Snapshot("").String("internal.librarydirfull"), "synthetic.taf"), syntheticTAF(t), 0600); err != nil {
		t.Fatal(err)
	}
	result := perform(s, "GET", "/api/getTagInfo?ruid=0000000100000001", nil, false)
	var object struct {
		Tag map[string]any `json:"tagInfo"`
	}
	if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &object) != nil {
		t.Fatal(result.Code, result.Body.String())
	}
	if object.Tag["uid"] != "01:00:00:00:01:00:00:00" || object.Tag["type"] != "system" || object.Tag["exists"] != true {
		t.Fatal(object.Tag)
	}
	calls := make(chan bool, 2)
	s.Shutdown = func(restart bool) { calls <- restart }
	for _, tc := range []struct{ path, body string }{{"triggerUnknown", "FAILED"}, {"triggerWriteConfig", "OK"}, {"triggerReloadConfig", "OK"}, {"triggerRestart", "OK"}, {"triggerExit", "OK"}} {
		result = perform(s, "GET", "/api/"+tc.path, nil, false)
		if result.Code != 200 || result.Header().Get("Content-Type") != "text/plain" || result.Body.String() != tc.body {
			t.Fatal(tc.path, result.Code, result.Body.String())
		}
	}
	if !<-calls || <-calls {
		t.Fatal("wrong lifecycle request")
	}
}
