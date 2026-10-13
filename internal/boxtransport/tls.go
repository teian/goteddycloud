// SPDX-License-Identifier: GPL-2.0-only
package boxtransport

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
)

// TLSConfig requires a pinned client certificate and TLS proof of its private
// key, retaining legacy AES suites only on the isolated box listener.
func TLSConfig(tb1, tb2 tls.Certificate, verify func([]byte) error) (*tls.Config, error) {
	if len(tb1.Certificate) == 0 || len(tb2.Certificate) == 0 {
		return nil, errors.New("both box certificate identities are required")
	}
	if verify == nil {
		return nil, errors.New("box certificate verifier is required")
	}
	return &tls.Config{
		SessionTicketsDisabled: true,
		MinVersion:             tls.VersionTLS12, MaxVersion: tls.VersionTLS12,
		ClientAuth: tls.RequireAnyClientCert,
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 || len(raw[0]) > 64<<10 {
				return errors.New("invalid box certificate")
			}
			return verify(raw[0])
		},
		// Legacy RSA/CBC suites are explicit and limited to this box listener. No
		// verification settings are changed for web HTTPS or upstream connections.
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
			tls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
			tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,
			tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_128_CBC_SHA256,
			tls.TLS_RSA_WITH_AES_128_CBC_SHA,
			tls.TLS_RSA_WITH_AES_256_CBC_SHA,
		},
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			if hello.ServerName != "" {
				return &tb2, nil
			}
			return &tb1, nil
		},
	}, nil
}
