// SPDX-License-Identifier: GPL-2.0-only
package identity

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"goteddycloud/internal/certificates"
	"goteddycloud/internal/config"
	"goteddycloud/internal/runtime"
)

// Policy pins the complete client certificate to locally installed material.
// Certificate names and manufacturer CA membership alone never establish trust.
type Policy struct {
	Store *config.Store
	mu    sync.Mutex
}

func boxID(cn string) string {
	if len(cn) == 15 && strings.HasPrefix(cn, "b'") && strings.HasSuffix(cn, "'") {
		cn = cn[2:14]
	}
	if !config.ValidBoxID(cn) {
		return ""
	}
	if len(cn) == 12 {
		if _, err := hex.DecodeString(cn); err == nil {
			return strings.ToUpper(cn)
		}
	}
	return cn
}

func readCertificate(snap config.Snapshot, path string) ([]byte, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(snap.Base, path)
	}
	f, err := snap.Storage().Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, certificates.MaxMaterial+1))
	if err != nil || len(data) > certificates.MaxMaterial {
		return nil, errors.New("certificate unavailable")
	}
	return data, nil
}

// Explicit selections take precedence over the conventional import directory.
// No fallback after a corrupt/replaced selection can revive an old certificate.
func (p *Policy) material(id, prefix, name string) ([]byte, error) {
	snap := p.Store.Snapshot(id)
	if inline := snap.String(prefix + ".data.crt"); inline != "" && p.Store.IsOverlayed(id, prefix+".data.crt") {
		return []byte(inline), nil
	}
	if !p.Store.IsOverlayed(id, prefix+".file.crt") {
		path := filepath.Join(p.Store.String("", "internal.certdirfull"), strings.ToLower(id), name)
		data, err := readCertificate(snap, path)
		if !errors.Is(err, os.ErrNotExist) {
			return data, err
		}
		if inline := snap.String(prefix + ".data.crt"); inline != "" {
			return []byte(inline), nil
		}
	}

	return readCertificate(snap, snap.String(prefix+".file.crt"))
}

func (p *Policy) trusted(peer runtime.Peer) (string, bool) {
	if !peer.Box || p.Store == nil || len(peer.CertificateSHA256) != 64 {
		return "", false
	}
	id := boxID(peer.CommonName)
	if id == "" {
		return "", false
	}
	// MAC spelling must not bypass ownership or a disabled existing overlay.
	boxes := p.Store.BoxIDs()
	selected := ""
	for _, known := range boxes {
		if boxID(known) == id {
			if selected != "" {
				return "", false
			}
			selected = known
		}
	}
	if selected != "" {
		id = selected
	}
	if !p.Store.HasBox(id) && (!p.Store.Bool("", "core.allowNewBox") || len(boxes) >= 16) {
		return "", false
	}
	for _, candidate := range []struct{ prefix, name string }{
		{"core.client_cert", "client.der"}, {"core.client_cert_fake", "client.fake.der"},
	} {
		data, err := p.material(id, candidate.prefix, candidate.name)
		if err != nil {
			continue
		}
		cert, err := certificates.Certificate(data)
		if err != nil || boxID(cert.Subject.CommonName) != boxID(id) || len(cert.UnhandledCriticalExtensions) != 0 {
			continue
		}
		hash := sha256.Sum256(cert.Raw)
		if hex.EncodeToString(hash[:]) != peer.CertificateSHA256 {
			continue
		}
		now := time.Now()
		if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
			return "", false
		}
		if cert.KeyUsage != 0 && cert.KeyUsage&x509.KeyUsageDigitalSignature == 0 {
			return "", false
		}
		if len(cert.ExtKeyUsage) > 0 || len(cert.UnknownExtKeyUsage) > 0 {
			client := false
			for _, usage := range cert.ExtKeyUsage {
				client = client || usage == x509.ExtKeyUsageClientAuth || usage == x509.ExtKeyUsageAny
			}
			if !client {
				return "", false
			}
		}
		return id, p.Store.Bool(id, "toniebox.api_access")
	}
	return "", false
}

// VerifyCertificate is read-only: TLS calls it before proving possession of the
// private key. Registration happens only on an authenticated application request.
func (p *Policy) VerifyCertificate(raw []byte) error {
	cert, err := x509.ParseCertificate(raw)
	if err == nil {
		hash := sha256.Sum256(raw)
		_, ok := p.trusted(runtime.Peer{Box: true, CommonName: cert.Subject.CommonName, CertificateSHA256: hex.EncodeToString(hash[:])})
		if ok {
			return nil
		}
	}
	return errors.New("untrusted box certificate")
}

func (p *Policy) Resolve(peer runtime.Peer) (string, bool, error) {
	id, ok := p.trusted(peer)
	if !ok {
		return "", false, nil
	}
	if p.Store.HasBox(id) {
		return id, true, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	// Recheck policy under the registration lock, including the overlay limit.
	id, ok = p.trusted(peer)
	if !ok {
		return "", false, nil
	}
	if !p.Store.HasBox(id) {
		dir := filepath.Join(p.Store.String("", "core.certdir"), strings.ToLower(id))
		values := map[string]string{"commonName": id, "boxName": "teddyCloud Box " + id, "boxModel": "", "toniebox.api_access": p.Store.String("", "toniebox.api_access"), "core.certdir": dir}
		// Preserve configured selections; use imported per-box files when present.
		for _, candidate := range []struct{ prefix, suffix string }{{"core.client_cert", ""}, {"core.client_cert_fake", ".fake"}} {
			if _, err := readCertificate(p.Store.Snapshot(""), filepath.Join(dir, "client"+candidate.suffix+".der")); err == nil {
				for key, name := range map[string]string{"ca": "ca", "crt": "client", "key": "private"} {
					values[candidate.prefix+".file."+key] = filepath.Join(dir, name+candidate.suffix+".der")
				}
			}
		}
		updates := map[string]*string{}
		for key, value := range values {
			v := value
			updates[key] = &v
		}
		if err := p.Store.Update(id, updates); err != nil {
			return "", false, errors.New("box registration could not be persisted")
		}
	}
	return id, true, nil
}
