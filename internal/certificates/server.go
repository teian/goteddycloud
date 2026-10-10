// SPDX-License-Identifier: GPL-2.0-only
package certificates

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"

	"goteddycloud/internal/config"
)

func certificatePath(snapshot config.Snapshot, prefix, key string) string {
	p := snapshot.String(prefix + ".file." + key)
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(snapshot.Base, p)
}

// Load accepts only existing inline or file-based material and never generates.
func Load(snapshot config.Snapshot, prefix string) (tls.Certificate, error) {
	certData, keyData := snapshot.String(prefix+".data.crt"), snapshot.String(prefix+".data.key")
	if certData != "" || keyData != "" {
		return tls.X509KeyPair([]byte(certData), []byte(keyData))
	}
	return loadPair(snapshot, certificatePath(snapshot, prefix, "crt"), certificatePath(snapshot, prefix, "key"))
}
func Ensure(snapshot config.Snapshot, prefix string) (tls.Certificate, error) {
	certData, keyData := snapshot.String(prefix+".data.crt"), snapshot.String(prefix+".data.key")
	if certData != "" || keyData != "" {
		return Load(snapshot, prefix)
	}
	path := func(key string) string { return certificatePath(snapshot, prefix, key) }
	crt, key := path("crt"), path("key")
	if exists(snapshot, crt) || exists(snapshot, key) {
		return loadPair(snapshot, crt, key)
	}
	if !snapshot.Bool("internal.autogen_certs") {
		return tls.Certificate{}, errors.New("server certificate missing and generation disabled")
	}
	caPath, caKeyPath := path("ca"), path("ca_key")
	var ca *x509.Certificate
	var signer crypto.Signer
	if exists(snapshot, caPath) || exists(snapshot, caKeyPath) {
		pair, err := loadPair(snapshot, caPath, caKeyPath)
		if err != nil {
			return tls.Certificate{}, fmt.Errorf("existing CA pair: %w", err)
		}
		ca, err = x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			return tls.Certificate{}, err
		}
		var ok bool
		signer, ok = pair.PrivateKey.(crypto.Signer)
		if !ok {
			return tls.Certificate{}, errors.New("unsupported CA key")
		}
	} else {
		var err error
		if prefix == "core.server_cert_tb2" {
			signer, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		} else {
			signer, err = rsa.GenerateKey(rand.Reader, 2048)
		}
		if err != nil {
			return tls.Certificate{}, err
		}
		ca, err = template("TeddyCloud")
		if err != nil {
			return tls.Certificate{}, err
		}
		ca.IsCA = true
		ca.KeyUsage = x509.KeyUsageCertSign | x509.KeyUsageCRLSign
		ca.BasicConstraintsValid = true
		der, err := x509.CreateCertificate(rand.Reader, ca, ca, signer.Public(), signer)
		if err != nil {
			return tls.Certificate{}, err
		}
		ca, err = x509.ParseCertificate(der)
		if err != nil {
			return tls.Certificate{}, err
		}
		rawKey, err := x509.MarshalPKCS8PrivateKey(signer)
		if err != nil {
			return tls.Certificate{}, err
		}
		if err := publish(snapshot, caKeyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: rawKey})); err != nil {
			return tls.Certificate{}, err
		}
		if err := publish(snapshot, caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})); err != nil {
			return tls.Certificate{}, err
		}
		if pathDER := snapshot.String(prefix + ".file.ca_der"); pathDER != "" {
			if !filepath.IsAbs(pathDER) {
				pathDER = filepath.Join(snapshot.Base, pathDER)
			}
			if err := publish(snapshot, pathDER, der); err != nil {
				return tls.Certificate{}, err
			}
		}
	}
	var serverKey crypto.Signer
	var err error
	if prefix == "core.server_cert_tb2" {
		serverKey, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	} else {
		serverKey, err = rsa.GenerateKey(rand.Reader, 2048)
	}
	if err != nil {
		return tls.Certificate{}, err
	}
	leaf, err := template("TeddyCloud")
	if err != nil {
		return tls.Certificate{}, err
	}
	leaf.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	leaf.KeyUsage = x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment
	leaf.DNSNames = []string{"localhost", "prod.de.tbs.toys", "rtnl.bxcl.de", "tbs2.tonie.cloud"}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, serverKey.Public(), signer)
	if err != nil {
		return tls.Certificate{}, err
	}
	rawKey, err := x509.MarshalPKCS8PrivateKey(serverKey)
	if err != nil {
		return tls.Certificate{}, err
	}
	private := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: rawKey})
	chain := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	chain = append(chain, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw})...)
	if err := publish(snapshot, key, private); err != nil {
		return tls.Certificate{}, err
	}
	if err := publish(snapshot, crt, chain); err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(chain, private)
}
func exists(snapshot config.Snapshot, path string) bool {
	_, err := snapshot.Storage().Stat(path)
	return !errors.Is(err, os.ErrNotExist)
}
func template(cn string) (*x509.Certificate, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	return &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: cn}, NotBefore: time.Date(2015, 11, 3, 0, 0, 0, 0, time.UTC), NotAfter: time.Date(2049, 12, 31, 0, 0, 0, 0, time.UTC)}, nil
}
func publish(snapshot config.Snapshot, path string, data []byte) error {
	if err := snapshot.Storage().MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	root, err := snapshot.Storage().OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer root.Close()
	return root.WriteAtomic(filepath.Base(path), data, 0600)
}

func loadPair(snapshot config.Snapshot, certificate, key string) (tls.Certificate, error) {
	crt, err := snapshot.Storage().ReadFile(certificate)
	if err != nil {
		return tls.Certificate{}, err
	}
	private, err := snapshot.Storage().ReadFile(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.X509KeyPair(crt, private)
}
