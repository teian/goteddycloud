// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"strconv"
	"strings"
)

func (s *Server) boxUserAgent(id, ua string) {
	if len(ua) <= 3 {
		return
	}
	number := func(prefix string) uint64 {
		_, tail, found := strings.Cut(ua, prefix)
		if !found {
			return 0
		}
		return decimalPrefix(tail)
	}
	firmware, service, hardware := number("TB/"), number("SP/"), number("HW/")
	chip, generation := uint64(0), uint64(0)
	values := map[string]string{}
	if firmware > 0 {
		if strings.HasPrefix(ua, "TB/") {
			chip, generation = 1, 1
			if hardware > 1100000 {
				chip, generation = 2, 1
			}
		} else {
			chip, generation = 3, 1
		}
	} else if _, tail, ok := strings.Cut(ua, "toniebox-esp32-"); ok {
		chip, generation = 3, 1
		values["internal.toniebox_firmware.uaEsp32Firmware"] = tail
	} else if strings.Contains(ua, "TB2/") {
		chip, generation = 4, 2
	}
	for key, value := range map[string]uint64{
		"internal.toniebox_firmware.boxIC":                chip,
		"internal.toniebox_firmware.uaVersionFirmware":    firmware,
		"internal.toniebox_firmware.uaVersionServicePack": service,
		"internal.toniebox_firmware.uaVersionHardware":    hardware,
		"toniebox.boxGeneration":                          generation,
	} {
		values[key] = strconv.FormatUint(value, 10)
	}
	_ = s.Settings.SetRuntime(id, values)
}
