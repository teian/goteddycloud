// SPDX-License-Identifier: GPL-2.0-only
package cloud

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"goteddycloud/internal/config"
)

type diagnosticLoggerKey struct{}

// WithDiagnosticLogger enables bounded, credential-free transport diagnostics
// for one request. Routine availability probes do not enable these messages.
func WithDiagnosticLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, diagnosticLoggerKey{}, logger)
}

func diagnosticLogger(ctx context.Context) *slog.Logger {
	logger, _ := ctx.Value(diagnosticLoggerKey{}).(*slog.Logger)
	return logger
}

func logIdentity(logger *slog.Logger, policy *tls.Config, selected config.Snapshot, fallback bool) {
	if logger == nil || !logger.Enabled(context.Background(), slog.LevelDebug) {
		return
	}
	leaf, err := x509.ParseCertificate(policy.Certificates[0].Certificate[0])
	if err != nil {
		return
	}
	hash := sha256.Sum256(leaf.Raw)
	source := "selected"
	if fallback {
		source = "global"
	}
	now := time.Now()
	logger.Debug("Tonies cloud TLS identity",
		"identity_source", source, "global_fallback", fallback,
		"certificate_inline", selected.String("core.client_cert.data.crt") != "",
		"certificate_cn", boundedCertificateName(leaf.Subject.CommonName),
		"certificate_issuer", boundedCertificateName(leaf.Issuer.CommonName),
		"certificate_signature", leaf.SignatureAlgorithm.String(),
		"certificate_key_type", leaf.PublicKeyAlgorithm.String(),
		"certificate_sha256", hex.EncodeToString(hash[:]),
		"certificate_not_before", leaf.NotBefore.UTC().Format(time.RFC3339),
		"certificate_not_after", leaf.NotAfter.UTC().Format(time.RFC3339),
		"certificate_currently_valid", !now.Before(leaf.NotBefore) && !now.After(leaf.NotAfter))
}

func boundedCertificateName(value string) string {
	value = strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, value)
	if len(value) > 128 {
		return value[:128]
	}
	return value
}

// Use the same SupportsCertificate selection as crypto/tls's default path.
// Observing the callback must not force a certificate the server cannot accept.
func traceClientCertificate(policy *tls.Config, logger *slog.Logger) *atomic.Int32 {
	selection := new(atomic.Int32)
	if logger == nil {
		return selection
	}
	policy.GetClientCertificate = func(request *tls.CertificateRequestInfo) (*tls.Certificate, error) {
		reason := "no_compatible_certificate"
		for i := range policy.Certificates {
			certificate := &policy.Certificates[i]
			if err := request.SupportsCertificate(certificate); err != nil {
				if err.Error() == "chain is not signed by an acceptable CA" {
					reason = "issuer_not_accepted"
				}
				continue
			}
			selection.Store(1)
			logger.Debug("Tonies cloud TLS certificate selection", "client_certificate_sent", true,
				"acceptable_ca_count", len(request.AcceptableCAs), "signature_scheme_count", len(request.SignatureSchemes))
			return certificate, nil
		}
		selection.Store(2)
		logger.Debug("Tonies cloud TLS certificate selection", "client_certificate_sent", false,
			"reason", reason, "acceptable_ca_count", len(request.AcceptableCAs), "signature_scheme_count", len(request.SignatureSchemes))
		return new(tls.Certificate), nil
	}
	return selection
}

func certificateSelectionState(selection *atomic.Int32) string {
	switch selection.Load() {
	case 1:
		return "sent"
	case 2:
		return "not_sent"
	default:
		return "not_requested"
	}
}
