// SPDX-License-Identifier: GPL-2.0-only
// Synthetic test data only. Never use generated identities for a real deployment.
package fixtures

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"goteddycloud/internal/pluginassets"
)

const Marker = ".synthetic-fixture"

// Generate refuses to overwrite any existing deployment, including prior fixtures.
func Generate(root string) error {
	if err := os.Mkdir(root, 0700); err != nil {
		return fmt.Errorf("new fixture directory: %w", err)
	}
	write := func(name string, data []byte, mode os.FileMode) error {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		return os.WriteFile(path, data, mode)
	}
	config := `core.server.bind_ip=127.0.0.1
core.server.http_port=18080
core.server.https_web_port=18443
core.server.https_api_port=18444
core.tonies_json_auto_update=false
cloud.enabled=false
cloud.remote_hostname=127.0.0.1
cloud.remote_hostname_tb2=127.0.0.1
mqtt.enabled=false
mqtt_server.enabled=false
core.allowNewBox=true
core.boxCertAuth=true
core.contentdir=default
core.librarydir=library
core.host_url=http://127.0.0.1:18080
log.color=false
log.level=4
tonie_json.cache_images=false
tonie_json.cache_preload=false
`
	overlay := `overlay.001122334455.commonName=001122334455
overlay.001122334455.core.client_cert.file.crt=certs/client/001122334455/teddy-cert.pem
overlay.001122334455.boxName=Synthetic Box One
overlay.001122334455.toniebox.max_vol_spk=1
overlay.001122334455.toniebox.led=2
overlay.AABBCCDDEEFF.commonName=AABBCCDDEEFF
overlay.AABBCCDDEEFF.core.client_cert.file.crt=certs/client/AABBCCDDEEFF/teddy-cert.pem
overlay.AABBCCDDEEFF.boxName=Synthetic Box Two
overlay.AABBCCDDEEFF.toniebox.max_vol_spk=3
overlay.AABBCCDDEEFF.toniebox.led=0
`
	for name, data := range map[string]string{"config/config.ini": config, "config/config.overlay.ini": overlay, "config/tonies.json": "[]\n", "config/tonies.custom.json": "[]\n", "config/tonieboxes.json": "[]\n", "config/tonieboxes.custom.json": "[]\n"} {
		if err := write(name, []byte(data), 0600); err != nil {
			return err
		}
	}
	for _, dir := range []string{"data/library", "data/content/default", "data/cache", "data/firmware", "data/www/web", "certs/client"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			return err
		}
	}
	// Authored static fixtures exercise serving without redistributing frontend assets.
	for name, data := range map[string]string{
		"data/www/web/index.html":                   "<!doctype html><title>Synthetic SPA</title>\n",
		"data/www/web/app.js":                       "console.log('synthetic');\n",
		"data/www/web/app.css":                      "body { color: black; }\n",
		"data/www/plugins/synthetic/plugin.json":    "{\"pluginName\":\"Synthetic fixture\",\"author\":\"GoTeddyCloud\",\"version\":\"1\",\"description\":\"Local test fixture\",\"standalone\":true}\n",
		"data/www/plugins/synthetic/index.html":     "Synthetic plugin\n",
		"data/firmware/ota/3/aabbccdd.bin":          "synthetic hash OTA\n",
		"data/firmware/ota/3/100-synthetic.bin":     "synthetic OTA version 100\n",
		"data/firmware/ota/3/200-synthetic.bin":     "synthetic OTA version 200\n",
		"data/firmware/ota/3/999-synthetic.bin.tmp": "incomplete synthetic OTA\n",
	} {
		if err := write(name, []byte(data), 0600); err != nil {
			return err
		}
	}
	if err := write("data/www/plugins/synthetic/preview.png", pluginassets.Synthetic, 0600); err != nil {
		return err
	}
	// Stable generated tones; no recordings or upstream sandbox audio are copied.
	for i, hz := range []float64{440, 660} {
		if err := write(fmt.Sprintf("tone%d.wav", i+1), tone(hz), 0600); err != nil {
			return err
		}
	}
	// Metadata uses a test UID, with a source pointing to the generated oracle TAF.
	metadata := map[string]any{"source": "lib://synthetic.taf", "tonie_model": "SYNTHETIC_001", "nocloud": true, "live": false, "cloud_override": false, "fixture_extension": "preserve-me"}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	if err := write("data/content/default/00000001/00000001.json", append(data, '\n'), 0600); err != nil {
		return err
	}
	for i, dir := range []string{"certs/server", "certs/server_tb2"} {
		if err := certificate(write, dir, fmt.Sprintf("Synthetic TB%d", i+1), true); err != nil {
			return err
		}
	}
	for _, id := range []string{"001122334455", "AABBCCDDEEFF"} {
		if err := certificate(write, "certs/client/"+id, id, false); err != nil {
			return err
		}
	}
	return write(Marker, []byte("Generated synthetic test deployment, never production.\n"), 0600)
}

func certificate(write func(string, []byte, os.FileMode) error, dir, cn string, server bool) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	now := time.Now()
	cert := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: cn, Organization: []string{"Synthetic TeddyCloud Test"}}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign, BasicConstraintsValid: true, IsCA: true}
	if server {
		cert.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		cert.DNSNames = []string{"localhost", "box.test"}
		cert.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	} else {
		cert.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	parent := cert
	var caDER []byte
	if !server {
		root := *cert
		root.Subject = pkix.Name{CommonName: "TeddyCloud Synthetic Client CA"}
		root.ExtKeyUsage = nil
		caDER, err = x509.CreateCertificate(rand.Reader, &root, &root, &key.PublicKey, key)
		if err != nil {
			return err
		}
		parent, err = x509.ParseCertificate(caDER)
		if err != nil {
			return err
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, parent, &key.PublicKey, key)
	if err != nil {
		return err
	}
	crt := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if server {
		caDER = der
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	private := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	for name, data := range map[string][]byte{"teddy-cert.pem": crt, "teddy-key.pem": private, "ca-root.pem": caPEM, "ca-key.pem": private, "ca.der": caDER} {
		if err := write(filepath.Join(dir, name), data, 0600); err != nil {
			return err
		}
	}
	return nil
}

func tone(hz float64) []byte {
	const frames = 48000
	data := make([]byte, 44+frames*4)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 2)
	binary.LittleEndian.PutUint32(data[24:], 48000)
	binary.LittleEndian.PutUint32(data[28:], 192000)
	binary.LittleEndian.PutUint16(data[32:], 4)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], frames*4)
	for i := 0; i < frames; i++ {
		sample := uint16(int16(8192 * math.Sin(2*math.Pi*hz*float64(i)/48000)))
		binary.LittleEndian.PutUint16(data[44+i*4:], sample)
		binary.LittleEndian.PutUint16(data[46+i*4:], sample)
	}
	return data
}
