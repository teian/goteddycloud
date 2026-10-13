// SPDX-License-Identifier: GPL-2.0-only
package cloud

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"strings"
)

const legacyBoxineHost = "prod.de.tbs.toys"

// verifyBoxineServer normally uses x509's complete hostname/chain verifier.
// Only the official legacy endpoint may use an exact CN when SAN is absent.
// CA trust, signatures, validity, name constraints and server EKU still apply.
func verifyBoxineServer(roots *x509.CertPool) func(tls.ConnectionState) error {
	return func(state tls.ConnectionState) error {
		if state.ServerName != legacyBoxineHost || len(state.PeerCertificates) == 0 || roots == nil {
			return errors.New("invalid Boxine server identity")
		}
		leaf := state.PeerCertificates[0]
		intermediates := x509.NewCertPool()
		for _, cert := range state.PeerCertificates[1:] {
			intermediates.AddCert(cert)
		}
		options := x509.VerifyOptions{Roots: roots, Intermediates: intermediates, DNSName: legacyBoxineHost, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
		_, err := leaf.Verify(options)
		var hostname x509.HostnameError
		if !errors.As(err, &hostname) || !strings.EqualFold(leaf.Subject.CommonName, legacyBoxineHost) {
			return err
		}
		for _, extension := range leaf.Extensions {
			if extension.Id.Equal([]int{2, 5, 29, 17}) {
				return err
			}
		}
		// The exact CN establishes the hostname for this single legacy endpoint.
		// Verify the complete original certificate chain without a DNS SAN check.
		options.DNSName = ""
		chains, err := leaf.Verify(options)
		if err != nil {
			return err
		}
		// DNS name constraints are defined for SANs, not legacy CNs. Refuse
		// constrained chains rather than allowing CN fallback to bypass them.
		for _, chain := range chains {
			constrained := false
			for _, cert := range chain {
				for _, extension := range cert.Extensions {
					constrained = constrained || extension.Id.Equal([]int{2, 5, 29, 30})
				}
			}
			if !constrained {
				return nil
			}
		}
		return errors.New("legacy Boxine hostname requires an unconstrained CA chain")
	}
}
