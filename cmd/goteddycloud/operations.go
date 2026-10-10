// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"path/filepath"
	"strings"
	"time"

	"goteddycloud/internal/certificates"
	"goteddycloud/internal/cloud"
	"goteddycloud/internal/config"
	"goteddycloud/internal/fetchpolicy"
	"goteddycloud/internal/firmware"
)

type operations struct {
	source      string
	destination string
	client      string
	tb2Client   string
	hostpatch   string
	portpatch   string
	fixup       string
	inject      string
	extract     string
	hostname    string
	oldRTNL     string
	oldAPI      string
	urlTest     string
	cloudTest   string
	encodeTest  string
	port        uint
	addSettings bool
	dockerTest  bool
	help        bool
}

func (o *operations) register(flags *flag.FlagSet) {
	for _, option := range []struct {
		names       []string
		value       *string
		description string
	}{
		{[]string{"source", "s"}, &o.source, "certificate source directory"},
		{[]string{"destination", "d"}, &o.destination, "output directory for certificates"},
		{[]string{"generate-client-cert", "c"}, &o.client, "generate a MAC client identity"},
		{[]string{"generate-client-cert-tb2"}, &o.tb2Client, "generate a fake TB2 MAC identity"},
		{[]string{"esp32-portpatch"}, &o.portpatch, "patch ESP32 connection port (1..32767; 443 restores original)"},
		{[]string{"esp32-hostpatch", "P"}, &o.hostpatch, "patch an ESP32 image using a private copy"},
		{[]string{"esp32-fixup", "F"}, &o.fixup, "repair an ESP32 image using a private copy"},
		{[]string{"esp32-inject", "I"}, &o.inject, "inject CERT files into an ESP32 image"},
		{[]string{"esp32-extract", "X"}, &o.extract, "extract CERT files from an ESP32 image"},
		{[]string{"hostname", "h"}, &o.hostname, "replacement firmware hostname"},
		{[]string{"oldrtnlhost"}, &o.oldRTNL, "old RTNL hostname"},
		{[]string{"oldapihost"}, &o.oldAPI, "old API hostname"},
		{[]string{"url-test", "U"}, &o.urlTest, "bounded HTTP(S) diagnostic"},
		{[]string{"cloud-test", "T"}, &o.cloudTest, "bounded configured cloud diagnostic"},
		{[]string{"encode_test", "encode-test", "E"}, &o.encodeTest, "generate the reference synthetic encoding test"},
	} {
		for _, name := range option.names {
			flags.StringVar(option.value, name, "", option.description)
		}
	}
	flags.UintVar(&o.port, "port", 0, "replacement firmware connection port")
	flags.BoolVar(&o.addSettings, "add-to-settings", false, "persist generated TB2 client paths in its overlay")
	flags.BoolVar(&o.dockerTest, "docker-test", false, "start listeners briefly, then shut down")
	flags.BoolVar(&o.dockerTest, "D", false, "start listeners briefly, then shut down")
	flags.BoolVar(&o.help, "help", false, "show usage")
	flags.BoolVar(&o.help, "?", false, "show usage")
}
func (o operations) active() bool {
	return o.client != "" || o.tb2Client != "" || o.hostpatch != "" || o.portpatch != "" || o.fixup != "" || o.inject != "" || o.extract != "" || o.urlTest != "" || o.cloudTest != "" || o.encodeTest != "" || o.dockerTest
}
func (o operations) validate() error {
	count := 0
	for _, value := range []string{o.client, o.tb2Client, o.hostpatch, o.portpatch, o.fixup, o.inject, o.extract, o.urlTest, o.cloudTest, o.encodeTest} {
		if value != "" {
			count++
		}
	}
	if o.dockerTest {
		count++
	}
	if count > 1 {
		return errors.New("choose one operation")
	}
	if o.portpatch != "" && (o.port < 1 || o.port > 32767) {
		return errors.New("esp32-portpatch requires --port in 1..32767")
	}
	if o.port != 0 && o.portpatch == "" {
		return errors.New("port requires esp32-portpatch")
	}
	if o.addSettings && o.tb2Client == "" {
		return errors.New("add-to-settings requires generate-client-cert-tb2")
	}
	return nil
}
func (o operations) execute(ctx context.Context, settings *config.Store, token string) error {
	snap := settings.Snapshot("")
	if o.client != "" || o.tb2Client != "" {
		tb2 := o.tb2Client != ""
		mac := o.client
		if tb2 {
			mac = o.tb2Client
		}
		prefix := "core.server_cert"
		if tb2 {
			prefix = "core.server_cert_tb2"
		}
		snap.Values["internal.autogen_certs"] = "true"
		if _, err := certificates.Ensure(snap, prefix); err != nil {
			return err
		}
		paths, err := certificates.GenerateClient(snap, mac, o.destination, tb2)
		if err != nil {
			return err
		}
		if o.addSettings {
			updates := map[string]*string{}
			for key, path := range paths {
				value := path
				updates["core.client_cert_fake.file."+key] = &value
			}
			generation := "2"
			updates["toniebox.boxGeneration"] = &generation
			if err := settings.Update(strings.ToUpper(mac), updates); err != nil {
				return err
			}
		}
		fmt.Println("client identity ready")
		return nil
	}
	if o.urlTest != "" || o.cloudTest != "" {
		return o.diagnostic(ctx, snap, token)
	}
	path := o.portpatch
	if path == "" {
		path = o.hostpatch
	}
	if path == "" {
		path = o.fixup
	}
	if path == "" {
		path = o.inject
	}
	if path == "" {
		path = o.extract
	}
	if path == "" {
		return errors.New("missing operation")
	}
	file, err := settings.Storage().Open(path)
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(file, firmware.MaxImage+1))
	file.Close()
	if err != nil {
		return err
	}
	var image *firmware.Image
	if o.fixup != "" {
		image, err = firmware.Repair(data)
	} else {
		image, err = firmware.Parse(data)
	}
	if err != nil {
		return err
	}
	if o.portpatch != "" {
		if _, err := image.PatchPort(uint32(o.port)); err != nil {
			return err
		}
	}
	if o.hostpatch != "" {
		oldRTNL, oldAPI := o.oldRTNL, o.oldAPI
		if oldRTNL == "" {
			oldRTNL = "rtnl.bxcl.de"
		}
		if oldAPI == "" {
			oldAPI = "prod.de.tbs.toys"
		}
		if _, err := image.PatchHost(o.hostname, oldRTNL, oldAPI); err != nil {
			return err
		}
	}
	if o.inject != "" || o.extract != "" {
		fat, err := image.Assets()
		if err != nil {
			return err
		}
		if o.extract != "" {
			if o.destination == "" {
				return errors.New("destination required")
			}
			files, err := fat.Extract("CERT")
			if err != nil {
				return err
			}
			root, err := settings.Storage().OpenRoot(o.destination)
			if err != nil {
				return err
			}
			defer root.Close()
			for name, data := range files {
				if !filepath.IsLocal(name) || strings.ContainsAny(name, "/\\:") {
					return errors.New("unsafe extracted name")
				}
				if err := root.WriteAtomic(name, data, 0600); err != nil {
					return err
				}
			}
			fmt.Println("firmware certificates extracted")
			return nil
		}
		if o.source == "" {
			return errors.New("source required")
		}
		root, err := settings.Storage().OpenRoot(o.source)
		if err != nil {
			return err
		}
		defer root.Close()
		dir, err := root.Open(".")
		if err != nil {
			return err
		}
		entries, err := dir.ReadDir(100)
		dir.Close()
		if err != nil && err != io.EOF {
			return err
		}
		if len(entries) >= 100 {
			return errors.New("certificate source file limit")
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			file, err := root.Open(entry.Name())
			if err != nil {
				return err
			}
			data, err := io.ReadAll(io.LimitReader(file, certificates.MaxMaterial+1))
			file.Close()
			if err != nil || len(data) > certificates.MaxMaterial {
				return errors.New("certificate input bounds")
			}
			if strings.Contains(strings.ToUpper(entry.Name()), "PRIVATE") {
				_, err = certificates.PrivateKey(data)
			} else {
				_, err = certificates.Certificate(data)
			}
			if err != nil {
				return err
			}
			if err := fat.Put("CERT", entry.Name(), data); err != nil {
				return err
			}
		}
		fat.Commit()
	}
	if err := image.Fixup(); err != nil {
		return err
	}
	if _, err := firmware.Parse(image.Data); err != nil {
		return err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	root, err := settings.Storage().OpenRoot(filepath.Dir(absolute))
	if err != nil {
		return err
	}
	defer root.Close()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := root.WriteAtomic(filepath.Base(absolute), image.Data, 0600); err != nil {
		return err
	}
	fmt.Println("firmware operation complete")
	return nil
}
func diagnosticHeaders(token string) (http.Header, error) {
	headers := http.Header{}
	if token == "" {
		return headers, nil
	}
	raw, err := hex.DecodeString(token)
	if err != nil || len(raw) != 32 {
		if len(token) != 32 {
			return nil, errors.New("diagnostic hash requires 32 raw bytes or 64 hexadecimal digits")
		}
		raw = []byte(token)
	}
	headers.Set("Authorization", "BD "+strings.ToUpper(hex.EncodeToString(raw)))
	return headers, nil
}
func (o operations) diagnostic(ctx context.Context, snap config.Snapshot, token string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	headers, err := diagnosticHeaders(token)
	if err != nil {
		return err
	}
	var res *http.Response
	if o.cloudTest != "" {
		parsed, parseErr := url.ParseRequestURI(o.cloudTest)
		if parseErr != nil || parsed.IsAbs() || !strings.HasPrefix(o.cloudTest, "/") {
			return errors.New("cloud-test requires a request path")
		}
		if !snap.Bool("cloud.enabled") {
			return errors.New("configured cloud disabled")
		}
		res, err = cloud.New().Do(ctx, snap, snap, "GET", parsed.Path, parsed.RawQuery, headers, nil)
	} else {
		parsed, parseErr := url.Parse(o.urlTest)
		if parseErr != nil || parsed.User != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return errors.New("url-test requires HTTP(S) URL without userinfo")
		}
		transport := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, DialContext: fetchpolicy.Dial(func() bool { return snap.Bool("core.allowPrivateFetch") }), TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, MaxResponseHeaderBytes: 32 << 10}
		defer transport.CloseIdleConnections()
		client := &http.Client{Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
		req, requestErr := http.NewRequestWithContext(ctx, "GET", parsed.String(), nil)
		if requestErr != nil {
			return errors.New("invalid diagnostic request")
		}
		req.Header = headers
		res, err = client.Do(req)
	}
	if err != nil {
		return errors.New("diagnostic transport failed")
	}
	defer res.Body.Close()
	n, err := io.Copy(io.Discard, io.LimitReader(res.Body, (64<<20)+1))
	if err != nil || n > 64<<20 {
		return errors.New("diagnostic response failed or exceeds bounds")
	}
	fmt.Printf("HTTP %d; %d bytes\n", res.StatusCode, n)
	if res.StatusCode >= 400 {
		return errors.New("diagnostic HTTP failure")
	}
	return nil
}
