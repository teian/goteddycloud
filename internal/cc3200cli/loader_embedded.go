//go:build cc3200_embedded_loader

// SPDX-License-Identifier: GPL-2.0-only
package cc3200cli

import _ "embed"

// The user-supplied blob is excluded from Git and standard release builds.
//
//go:embed loader/rbtl3100s.dll
var embeddedLoader []byte
