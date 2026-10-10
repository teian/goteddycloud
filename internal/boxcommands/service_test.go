// SPDX-License-Identifier: GPL-2.0-only
package boxcommands

import (
	"context"
	"testing"

	"goteddycloud/internal/config"
	"goteddycloud/internal/events"
)

func TestCommandsPersistValidateAndStopOnlySelectedBox(t *testing.T) {
	base := t.TempDir()
	settings, err := config.Open(base)
	if err != nil {
		t.Fatal(err)
	}
	defer settings.Close()
	settings.Set("001122334455", "commonName", "001122334455")
	settings.Set("AABBCCDDEEFF", "commonName", "AABBCCDDEEFF")
	service := New(settings, events.New())
	for _, command := range []struct{ name, value string }{{"CmdVolLimitSpk", "2"}, {"CmdVolLimitHdp", "3"}, {"CmdLed", "1"}, {"CmdSlapEnabled", "FALSE"}, {"CmdSlapDirection", "TRUE"}} {
		if err := service.Execute("001122334455", command.name, command.value); err != nil {
			t.Fatal(err)
		}
	}
	for _, command := range []struct{ name, value string }{{"CmdVolLimitSpk", "5"}, {"CmdVolLimitHdp", "-1"}, {"CmdLed", "9"}, {"CmdSlapEnabled", "invalid"}, {"CmdVolLimitSpk", "2junk"}, {"Pause", "true"}} {
		if err := service.Execute("001122334455", command.name, command.value); err == nil {
			t.Fatal("invalid command accepted", command)
		}
	}
	reopened, err := config.Open(base)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.Snapshot("001122334455").Uint("toniebox.max_vol_spk") != 2 || reopened.Snapshot("AABBCCDDEEFF").Uint("toniebox.max_vol_spk") != 3 {
		t.Fatal("persistence or isolation")
	}
	first, releaseFirst := service.Track(context.Background(), "001122334455")
	defer releaseFirst()
	second, releaseSecond := service.Track(context.Background(), "AABBCCDDEEFF")
	defer releaseSecond()
	if err := service.Execute("001122334455", "CmdStop", ""); err != nil {
		t.Fatal(err)
	}
	if first.Err() == nil || second.Err() != nil {
		t.Fatal("stop crossed box boundary")
	}
	if err := service.Execute("001122334455", "CmdStop", ""); err == nil {
		t.Fatal("inactive stream stop reported active")
	}
}
