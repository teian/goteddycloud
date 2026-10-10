// SPDX-License-Identifier: GPL-2.0-only

// Package pluginassets contains authored previews for bundled and test plugins.
package pluginassets

import _ "embed"

//go:embed cc3200.png
var CC3200 []byte

//go:embed synthetic.png
var Synthetic []byte
