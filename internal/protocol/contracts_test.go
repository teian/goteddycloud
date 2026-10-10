// SPDX-License-Identifier: GPL-2.0-only
package protocol

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/proto"
)

func TestProto2PresenceUnknownAndMalformed(t *testing.T) {
	if _, err := proto.Marshal(&TonieFreshnessCheckResponse{}); err == nil {
		t.Fatal("missing required fields accepted")
	}
	request := &TonieFreshnessCheckRequest{TonieInfos: []*TonieFCInfo{{Uid: proto.Uint64(0x0123456789abcdef), AudioId: proto.Uint32(123)}}}
	wire, err := proto.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	unknown := []byte{0xa0, 0x06, 0x01}
	wire = append(wire, unknown...)
	decoded := new(TonieFreshnessCheckRequest)
	if err := proto.Unmarshal(wire, decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.TonieInfos[0].GetUid() != request.TonieInfos[0].GetUid() {
		t.Fatal("fixed64 changed")
	}
	output, err := proto.Marshal(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(output, unknown) {
		t.Fatal("unknown field lost")
	}
	if err := proto.Unmarshal([]byte{0x0a, 10, 1}, decoded); err == nil {
		t.Fatal("truncated protobuf accepted")
	}
	if err := proto.Unmarshal([]byte{0x0a, 0}, decoded); err == nil {
		t.Fatal("nested required fields accepted")
	}
}
