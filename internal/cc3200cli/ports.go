// SPDX-License-Identifier: GPL-2.0-only
package cc3200cli

import (
	"fmt"
	"sort"
	"strings"
)

func selectPort(goos string, ports []string) (string, error) {
	candidates := map[string]bool{}
	for _, port := range ports {
		switch goos {
		case "windows":
			if strings.HasPrefix(strings.ToUpper(port), "COM") {
				candidates[port] = true
			}
		case "darwin":
			if strings.HasPrefix(port, "/dev/cu.usb") || strings.HasPrefix(port, "/dev/tty.usb") || strings.HasPrefix(port, "/dev/cu.SLAB") || strings.HasPrefix(port, "/dev/tty.SLAB") || strings.HasPrefix(port, "/dev/cu.wch") || strings.HasPrefix(port, "/dev/tty.wch") {
				candidates[strings.Replace(port, "/dev/tty.", "/dev/cu.", 1)] = true
			}
		default:
			if strings.HasPrefix(port, "/dev/ttyUSB") || strings.HasPrefix(port, "/dev/ttyACM") {
				candidates[port] = true
			}
		}
	}
	names := make([]string, 0, len(candidates))
	for name := range candidates {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 1 {
		return names[0], nil
	}
	if len(names) > 1 {
		return "", fmt.Errorf("multiple serial ports detected: %s; select one with --port", strings.Join(names, ", "))
	}
	switch goos {
	case "linux":
		return "/dev/ttyUSB0", nil
	case "windows":
		return "", fmt.Errorf("no COM ports detected; connect the adapter and use --ports or --port COM<x>")
	case "darwin":
		return "", fmt.Errorf("no USB serial ports detected; connect the adapter and use --ports or --port /dev/cu.usbserial-<id>")
	default:
		return "", fmt.Errorf("no USB serial ports detected; use --ports or --port <device>")
	}
}
