// SPDX-License-Identifier: GPL-2.0-only
package cc3200cli

import (
	"strings"
	"testing"
)

func TestPlatformPortSelection(t *testing.T) {
	for _, tc := range []struct {
		os            string
		ports         []string
		want          string
		errorContains string
	}{
		{"linux", nil, "/dev/ttyUSB0", ""},
		{"linux", []string{"/dev/ttyS0", "/dev/ttyACM2"}, "/dev/ttyACM2", ""},
		{"linux", []string{"/dev/ttyUSB1", "/dev/ttyACM0"}, "", "multiple serial ports"},
		{"windows", []string{"COM11"}, "COM11", ""},
		{"windows", nil, "", "COM<x>"},
		{"windows", []string{"COM3", "COM8"}, "", "--port"},
		{"darwin", []string{"/dev/tty.Bluetooth-Incoming-Port", "/dev/tty.usbserial-123", "/dev/cu.usbserial-123"}, "/dev/cu.usbserial-123", ""},
		{"darwin", []string{"/dev/cu.usbmodem1101"}, "/dev/cu.usbmodem1101", ""},
		{"darwin", []string{"/dev/tty.SLAB_USBtoUART"}, "/dev/cu.SLAB_USBtoUART", ""},
		{"darwin", nil, "", "/dev/cu.usbserial"},
	} {
		t.Run(tc.os+"/"+tc.want+tc.errorContains, func(t *testing.T) {
			got, e := selectPort(tc.os, tc.ports)
			if got != tc.want || (tc.errorContains == "" && e != nil) || (tc.errorContains != "" && (e == nil || !strings.Contains(e.Error(), tc.errorContains))) {
				t.Fatal(got, e)
			}
		})
	}
}
