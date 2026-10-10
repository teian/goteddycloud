// SPDX-License-Identifier: GPL-2.0-only
package certificates

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"errors"
	"io"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"goteddycloud/internal/config"
	"goteddycloud/internal/storage"
)

// GenerateClient preserves complete existing identities and rejects partial
// ones. Client material never replaces the server or CA keys.
func GenerateClient(snapshot config.Snapshot, mac, destination string, tb2 bool) (map[string]string, error) {
	raw, err := hex.DecodeString(mac)
	if err != nil || len(raw) != 6 {
		return nil, errors.New("MAC must contain twelve hexadecimal digits")
	}
	prefix := "core.server_cert"
	subject := "b'" + mac + "'"
	suffix := ""
	if tb2 {
		prefix = "core.server_cert_tb2"
		subject = strings.ToUpper(mac)
		suffix = ".fake"
		if destination == "" {
			destination = filepath.Join(snapshot.String("internal.certdirfull"), strings.ToLower(mac))
		}
	}
	if destination == "" {
		return nil, errors.New("destination required")
	}
	if !filepath.IsAbs(destination) {
		destination = filepath.Join(snapshot.Base, destination)
	}
	if tb2 {
		if err := snapshot.Storage().MkdirAll(destination, 0700); err != nil {
			return nil, err
		}
	}
	root, err := snapshot.Storage().OpenRoot(destination)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	names := map[string]string{"crt": "client" + suffix + ".der", "key": "private" + suffix + ".der"}
	if tb2 {
		names["ca"] = "ca.fake.der"
	}
	count := 0
	for _, name := range names {
		if _, err := root.Lstat(name); err == nil {
			count++
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	paths := map[string]string{}
	for key, name := range names {
		paths[key] = filepath.Join(destination, name)
	}
	if count != 0 {
		if count != len(names) {
			return nil, errors.New("partial client identity exists")
		}
		certificateData, err := readMaterial(root, names["crt"])
		if err != nil {
			return nil, err
		}
		private, err := readMaterial(root, names["key"])
		if err != nil {
			return nil, err
		}
		certificate, err := Certificate(certificateData)
		if err != nil || certificate.Subject.CommonName != subject {
			return nil, errors.New("existing client certificate differs")
		}
		caData := certificateData
		if tb2 {
			caData, err = readMaterial(root, names["ca"])
			if err != nil {
				return nil, err
			}
		}
		if err := ValidateIdentity(caData, certificateData, private); err != nil {
			return nil, err
		}
		return paths, nil
	}
	caData, err := snapshot.Storage().ReadFile(certificatePath(snapshot, prefix, "ca"))
	if inline := snapshot.String(prefix + ".data.ca"); inline != "" {
		caData = []byte(inline)
		err = nil
	}
	if err != nil {
		return nil, errors.New("server CA unavailable")
	}
	ca, err := Certificate(caData)
	if err != nil {
		return nil, err
	}
	signerData, err := snapshot.Storage().ReadFile(certificatePath(snapshot, prefix, "ca_key"))
	if inline := snapshot.String(prefix + ".data.ca_key"); inline != "" {
		signerData = []byte(inline)
		err = nil
	}
	if err != nil {
		return nil, errors.New("server CA key unavailable")
	}
	signer, err := PrivateKey(signerData)
	if err != nil {
		return nil, err
	}
	var key crypto.Signer
	if tb2 {
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	} else {
		key, err = rsa.GenerateKey(rand.Reader, 2048)
	}
	if err != nil {
		return nil, err
	}
	serial := new(big.Int).SetBytes(raw)
	if serial.Sign() == 0 {
		serial.SetInt64(1)
	}
	province := "NW"
	if tb2 {
		province = "North Rhine-Westphalia"
	}
	template := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: subject, Organization: []string{"Team RevvoX"}, Country: []string{"DE"}, Locality: []string{"Duesseldorf"}, Province: []string{province}}, NotBefore: time.Date(2015, 11, 3, 15, 23, 19, 0, time.UTC), NotAfter: time.Date(2040, 6, 24, 15, 23, 19, 0, time.UTC)}
	if tb2 {
		template.BasicConstraintsValid = true
		template.KeyUsage = x509.KeyUsageDigitalSignature | x509.KeyUsageKeyAgreement
		template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	crt, err := x509.CreateCertificate(rand.Reader, template, ca, key.Public(), signer)
	if err != nil {
		return nil, err
	}
	var private []byte
	if tb2 {
		private, err = x509.MarshalECPrivateKey(key.(*ecdsa.PrivateKey))
	} else {
		private = x509.MarshalPKCS1PrivateKey(key.(*rsa.PrivateKey))
	}
	if err != nil {
		return nil, err
	}
	if err := ValidateIdentity(ca.Raw, crt, private); err != nil {
		return nil, err
	}
	if err := root.WriteAtomic(names["key"], private, 0600); err != nil {
		return nil, err
	}
	if err := root.WriteAtomic(names["crt"], crt, 0600); err != nil {
		return nil, err
	}
	if tb2 {
		if err := root.WriteAtomic(names["ca"], ca.Raw, 0600); err != nil {
			return nil, err
		}
	}
	return paths, nil
}

func readMaterial(root *storage.Root, name string) ([]byte, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxMaterial+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxMaterial {
		return nil, errors.New("certificate material size")
	}
	return data, nil
}
