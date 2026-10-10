// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"testing"

	"google.golang.org/protobuf/proto"

	"goteddycloud/internal/protocol"
)

func TestRESTAndMQTTCommandsShareNextFreshnessSettings(t *testing.T) {
	s := fixtureApp(t)
	id := "001122334455"
	if err := s.Commands.Execute(id, "CmdVolLimitSpk", "2"); err != nil {
		t.Fatal(err)
	}
	perform(s, "POST", "/api/settings/set/toniebox.max_vol_hdp?overlay="+id, []byte("1"), false)
	perform(s, "POST", "/api/settings/set/toniebox.led?overlay="+id, []byte("9"), false)
	reply := s.localFreshness(id, s.Settings.Snapshot(id), new(protocol.TonieFreshnessCheckRequest))
	if reply.GetMaxVolSpk() != 2 || reply.GetMaxVolHdp() != 1 || reply.GetLed() != 2 {
		t.Fatal(reply)
	}
	if _, err := proto.Marshal(reply); err != nil {
		t.Fatal(err)
	}
}
