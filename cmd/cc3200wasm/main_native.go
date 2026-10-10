// SPDX-License-Identifier: GPL-2.0-only
//go:build !js || !wasm

package main

import "fmt"

func main() { fmt.Println("Build the browser module using scripts/build-cc3200-wasm.sh") }
