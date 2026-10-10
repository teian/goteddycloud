// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"goteddycloud/internal/catalog"
	"goteddycloud/internal/config"
	"goteddycloud/internal/content"
)

func TestRenameCrashHelper(t *testing.T) {
	if os.Getenv("GOTEDDY_RENAME_HELPER") != "1" {
		return
	}
	settings, err := config.Open(os.Getenv("GOTEDDY_RENAME_ROOT"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := New(settings)
	if err != nil {
		t.Fatal(err)
	}
	snap := settings.Snapshot("")
	err = server.Catalog.Rename("SYNTHETIC_001", "RENAMED", snap.String("internal.contentdirfull"), true, 10, time.Now(), func(from, to string) error {
		if err := server.Content.Update(snap, "0000000100000001", func(m content.Metadata) error { m.Set("tonie_model", to); return nil }); err != nil {
			return err
		}
		fmt.Println("linked write ready")
		select {} // parent terminates this owned process after one linked publication
	})
	t.Fatal("expected parent termination", err)
}
func TestKilledRenameRecoversAllLinksBeforeStartup(t *testing.T) {
	server := fixtureApp(t)
	snap := server.Settings.Snapshot("")
	entry := catalog.Entry{}
	entry["model"] = []byte(`"SYNTHETIC_001"`)
	entry["series"] = []byte(`"Synthetic series"`)
	if err := server.Catalog.Upsert([]catalog.Entry{entry}, 10, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := server.Content.Update(snap, "0200000002000000", func(m content.Metadata) error {
		m.Set("tonie_model", "SYNTHETIC_001")
		m.Set("preserve", 42)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	server.Close() // only the child writes the copied deployment during the crash
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRenameCrashHelper$")
	command.Env = append(os.Environ(), "GOTEDDY_RENAME_HELPER=1", "GOTEDDY_RENAME_ROOT="+snap.Base)
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	line, readErr := bufio.NewReader(output).ReadString('\n')
	_ = command.Process.Kill()
	_ = command.Wait()
	if readErr != nil || strings.TrimSpace(line) != "linked write ready" {
		t.Fatal("child did not reach interruption point", line, readErr)
	}
	if _, err := os.Stat(filepath.Join(snap.String("internal.configdirfull"), catalog.RenameJournal)); err != nil {
		t.Fatal("intent missing", err)
	}
	recovered, err := New(server.Settings)
	if err != nil {
		t.Fatal(err)
	}
	defer recovered.Close()
	if recovered.Catalog.ByModel("SYNTHETIC_001") != nil || recovered.Catalog.ByModel("RENAMED") == nil {
		t.Fatal("catalog recovery failed")
	}
	for _, ruid := range []string{"0000000100000001", "0200000002000000"} {
		metadata, err := recovered.Content.Metadata(snap, ruid)
		if err != nil || metadata.String("tonie_model") != "RENAMED" {
			t.Fatal(ruid, metadata, err)
		}
	}
	metadata, err := recovered.Content.Metadata(snap, "0200000002000000")
	if err != nil || string(metadata["preserve"]) != "42" {
		t.Fatal("unknown metadata lost", err)
	}
	if _, err := os.Stat(filepath.Join(snap.String("internal.configdirfull"), catalog.RenameJournal)); !os.IsNotExist(err) {
		t.Fatal("intent retained", err)
	}
	recovered.Close()
	reopened, err := New(server.Settings)
	if err != nil {
		t.Fatal(err)
	}
	reopened.Close()
}
func TestRenameCollisionOptOutAndResponseContracts(t *testing.T) {
	server := fixtureApp(t)
	for _, model := range []string{"SYNTHETIC_001", "EXISTING"} {
		reply := perform(server, "POST", "/api/toniesCustomJsonUpsert", []byte(`{"model":"`+model+`","series":"Synthetic"}`), false)
		if reply.Code != 200 {
			t.Fatal(reply.Code, reply.Body.String())
		}
	}
	collision := perform(server, "POST", "/api/toniesCustomJsonRename", []byte(`{"fromModel":"SYNTHETIC_001","toModel":"EXISTING"}`), false)
	if collision.Code != 400 || collision.Body.String() != "Target model already exists" {
		t.Fatal(collision)
	}
	optOut := perform(server, "POST", "/api/toniesCustomJsonRename", []byte(`{"fromModel":"SYNTHETIC_001","toModel":"OPT_OUT","updateContentJson":false}`), false)
	if optOut.Code != 200 {
		t.Fatal(optOut)
	}
	metadata, err := server.Content.Metadata(server.Settings.Snapshot(""), "0000000100000001")
	if err != nil || metadata.String("tonie_model") != "SYNTHETIC_001" {
		t.Fatal("opt-out changed linked content", err)
	}
	missing := perform(server, "POST", "/api/toniesCustomJsonRename", []byte(`{"fromModel":"missing","toModel":"NEW"}`), false)
	if missing.Code != 404 || missing.Body.String() != "Source model not found" {
		t.Fatal(missing)
	}
}
