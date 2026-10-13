// SPDX-License-Identifier: GPL-2.0-only
//go:build !windows

package cc3200cli

import "os"

func enableANSI(*os.File) bool { return true }
