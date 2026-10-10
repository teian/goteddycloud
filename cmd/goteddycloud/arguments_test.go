// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"flag"
	"reflect"
	"testing"
)

func TestInterspersedGetopt(t *testing.T) {
	for _, args := range [][]string{
		{"one.wav", "-gDCcore.foo=bar", "two.wav", "-eout.taf"},
		{"one.wav", "--encode=out.taf", "-g", "-D", "-C", "core.foo=bar", "two.wav"},
	} {
		flags := flag.NewFlagSet("test", flag.ContinueOnError)
		var generate, docker bool
		var encode string
		var updates assignments
		flags.BoolVar(&generate, "g", false, "")
		flags.BoolVar(&docker, "D", false, "")
		flags.StringVar(&encode, "e", "", "")
		flags.StringVar(&encode, "encode", "", "")
		flags.Var(&updates, "C", "")
		if err := flags.Parse(interspersed(flags, args)); err != nil {
			t.Fatal(err)
		}
		if !generate || !docker || encode != "out.taf" || !reflect.DeepEqual([]string(updates), []string{"core.foo=bar"}) || !reflect.DeepEqual(flags.Args(), []string{"one.wav", "two.wav"}) {
			t.Fatalf("incorrect getopt behavior: %v %v %q %v %v", generate, docker, encode, updates, flags.Args())
		}
	}
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.Bool("D", false, "")
	if err := flags.Parse(interspersed(flags, []string{"--", "-D"})); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(flags.Args(), []string{"-D"}) {
		t.Fatal("end of options lost")
	}
}
func TestConfigSetCommaAndDiagnosticHeaders(t *testing.T) {
	var updates assignments
	if err := updates.Set("cloud.enabled=false,mqtt.enabled=true"); err != nil {
		t.Fatal(err)
	}
	before := len(updates)
	if err := updates.Set("cloud.enabled=true,invalid"); err == nil || len(updates) != before {
		t.Fatal("partial invalid assignment retained")
	}
	for _, token := range []string{"01234567890123456789012345678901", "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"} {
		headers, err := diagnosticHeaders(token)
		if err != nil || len(headers.Get("Authorization")) != 67 {
			t.Fatalf("hash header: %v %v", headers, err)
		}
	}
	if _, err := diagnosticHeaders("secret"); err == nil {
		t.Fatal("invalid hash accepted")
	}
}
