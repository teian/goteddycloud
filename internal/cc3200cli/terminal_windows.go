// SPDX-License-Identifier: GPL-2.0-only
package cc3200cli

import (
	"golang.org/x/sys/windows"
	"os"
)

func enableANSI(file *os.File) bool {
	handle := windows.Handle(file.Fd())
	var mode uint32
	if windows.GetConsoleMode(handle, &mode) != nil {
		return false
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return true
	}
	return windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}
