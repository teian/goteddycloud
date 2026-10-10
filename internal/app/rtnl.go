// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"google.golang.org/protobuf/proto"

	"goteddycloud/internal/boxstate"
	"goteddycloud/internal/config"
	"goteddycloud/internal/protocol"
)

const rtnlCSVHeader = "timestamp;log2;uptime;sequence;3;group;function;6(len);6(bytes);6(string);8;9(len);9(bytes);9(string);log3;datetime;2\r\n"

// Develop CSV escaping doubles quotes and escapes line endings; other
// non-alphanumeric bytes are replaced with dots.
func rtnlText(data []byte) string {
	var out strings.Builder
	for _, b := range data {
		switch {
		case b == '"':
			out.WriteString(`""`)
		case b == '\n':
			out.WriteString(`\n`)
		case b == '\r':
			out.WriteString(`\r`)
		case b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9':
			out.WriteByte(b)
		default:
			out.WriteByte('.')
		}
	}
	return out.String()
}
func rtnlCSV(timestamp int64, rpc *protocol.TonieRtnlRPC) []byte {
	var out strings.Builder
	fmt.Fprintf(&out, "%d;", timestamp)
	if l := rpc.Log2; l != nil {
		fmt.Fprintf(&out, "x;%d;%d;%d;%d;%d;%d;%X;\"%s\";%d;%d;", l.GetUptime(), l.GetSequence(), l.GetField3(), l.GetFunctionGroup(), l.GetFunction(), len(l.Field6), l.Field6, rtnlText(l.Field6), l.GetField8(), len(l.Field9))
		if l.Field9 != nil {
			fmt.Fprintf(&out, "%X;\"%s\";", l.Field9, rtnlText(l.Field9))
		} else {
			out.WriteString(";;")
		}
	} else {
		out.WriteString(";;;;;;;;;;;;;")
	}
	if l := rpc.Log3; l != nil {
		fmt.Fprintf(&out, "x;%d;%d\r\n", l.GetDatetime(), l.GetField2())
	} else {
		out.WriteString(";;\r\n")
	}
	return []byte(out.String())
}
func (s *Server) logRTNL(snap config.Snapshot, data []byte) {
	if !snap.Bool("rtnl.logRaw") && !snap.Bool("rtnl.logHuman") {
		return
	}
	// Serialize append/header creation across boxes. OpenRoot confines configured
	// log filenames against symlink escapes; no codec or shell process is involved.
	s.rtnlMu.Lock()
	defer s.rtnlMu.Unlock()
	appendLog := func(key string, header, payload []byte) {
		path := snap.String(key)
		if !filepath.IsAbs(path) {
			path = filepath.Join(snap.Base, path)
		}
		root, err := s.Settings.Storage().OpenRoot(filepath.Dir(path))
		if err != nil {
			return
		}
		defer root.Close()
		name := filepath.Base(path)
		file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
		if err != nil {
			return
		}
		defer file.Close()
		info, err := file.Stat()
		if err != nil {
			return
		}
		if info.Size() == 0 && len(header) > 0 {
			if _, err := file.Write(header); err != nil {
				return
			}
		}
		_, _ = file.Write(payload)
	}
	if snap.Bool("rtnl.logRaw") {
		raw := make([]byte, 4, len(data)+4)
		binary.BigEndian.PutUint32(raw, uint32(len(data)))
		appendLog("rtnl.logRawFile", nil, append(raw, data...))
	}
	if snap.Bool("rtnl.logHuman") {
		rpc := new(protocol.TonieRtnlRPC)
		if proto.Unmarshal(data, rpc) == nil && (rpc.Log2 != nil || rpc.Log3 != nil) {
			appendLog("rtnl.logHumanFile", []byte(rtnlCSVHeader), rtnlCSV(s.Now().Unix(), rpc))
		}
	}
}

func (s *Server) updateRTNLSettings(id string, data []byte) {
	rpc := new(protocol.TonieRtnlRPC)
	if proto.Unmarshal(data, rpc) != nil || rpc.Log2 == nil {
		return
	}
	l := rpc.Log2
	state := s.Boxes.Snapshot(id)
	key, value := "", ""
	switch l.GetFunctionGroup() {
	case boxstate.RTNL2_FUGR_FIRMWARE:
		switch l.GetFunction() {
		case boxstate.RTNL2_FUNC_FIRMWARE_VERSION:
			key, value = "internal.toniebox_firmware.rtnlVersion", state.Firmware
		case boxstate.RTNL2_FUNC_FIRMWARE_FULL_VERSION:
			key, value = "internal.toniebox_firmware.rtnlFullVersion", state.FullVersion
		case boxstate.RTNL2_FUNC_FIRMWARE_INFOS:
			key, value = "internal.toniebox_firmware.rtnlDetail", state.Detail
		}
	case boxstate.RTNL2_FUGR_NETWORK_HTTP:
		if l.GetFunction() == boxstate.RTNL2_FUNC_NETWORK_REGION {
			key, value = "internal.toniebox_firmware.rtnlRegion", state.Region
		}
		if l.GetFunction() == boxstate.RTNL2_FUNC_NETWORK_DOMAIN {
			key, value = "internal.rtnl.prodDomain", state.APIDomain
		}
	case boxstate.RTNL2_FUGR_NETWORK_TCP:
		if l.GetFunction() == boxstate.RTNL2_FUNC_NETWORK_DOMAIN {
			key, value = "internal.rtnl.rtnlDomain", state.RTNLDomain
		}
	}
	if key != "" {
		_ = s.Settings.SetRuntime(id, map[string]string{key: value})
		if key == "internal.rtnl.prodDomain" || key == "internal.rtnl.rtnlDomain" {
			_ = s.Settings.SetRuntime("", map[string]string{key: value})
		}
	}
}
