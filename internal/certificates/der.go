// SPDX-License-Identifier: GPL-2.0-only
package certificates

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"errors"
)

const MaxMaterial = 1 << 20

func DER(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > MaxMaterial {
		return nil, errors.New("certificate material size")
	}
	if block, _ := pem.Decode(data); block != nil {
		return block.Bytes, nil
	}
	return data, nil
}
func Certificate(data []byte) (*x509.Certificate, error) {
	der, err := DER(data)
	if err != nil {
		return nil, err
	}
	return x509.ParseCertificate(der)
}
func PrivateKey(data []byte) (crypto.Signer, error) {
	der, err := DER(data)
	if err != nil {
		return nil, err
	}
	if key, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		if signer, ok := key.(crypto.Signer); ok {
			return signer, nil
		}
	}
	if key, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return key, nil
	}
	if key, err := x509.ParseECPrivateKey(der); err == nil {
		return key, nil
	}
	return nil, errors.New("invalid private key")
}
func ValidateIdentity(ca, client, key []byte) error {
	if _, err := Certificate(ca); err != nil {
		return errors.New("invalid CA certificate")
	}
	certificate, err := Certificate(client)
	if err != nil {
		return errors.New("invalid client certificate")
	}
	signer, err := PrivateKey(key)
	if err != nil {
		return err
	}
	actual, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil {
		return err
	}
	expected, err := x509.MarshalPKIXPublicKey(certificate.PublicKey)
	if err != nil {
		return err
	}
	if !bytes.Equal(actual, expected) {
		return errors.New("client certificate and private key do not match")
	}
	return nil
}
