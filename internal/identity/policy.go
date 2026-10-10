// SPDX-License-Identifier: GPL-2.0-only
package identity

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"

	"goteddycloud/internal/config"
	"goteddycloud/internal/runtime"
)

// Policy reproduces the reference's name-based legacy identity acceptance. It
// deliberately makes no cryptographic trust claim about an issuer string.
type Policy struct {
	Store *config.Store
	mu    sync.Mutex
}

func (p *Policy) Resolve(peer runtime.Peer) (string, bool, error) {
	if !peer.Box {
		return "", false, nil
	}
	allowed := strings.Contains(peer.Subject, "TeddyCloud")
	for _, issuer := range []string{"Boxine Factory SubCA", "Toniebox SubCA", "TeddyCloud", "Toniebox Root CA"} {
		allowed = allowed || strings.Contains(peer.Issuer, issuer)
	}
	if !allowed {
		return "", false, nil
	}
	id := peer.CommonName
	if len(id) == 15 && strings.HasPrefix(id, "b'") && strings.HasSuffix(id, "'") {
		id = id[2:14]
	}
	if !config.ValidBoxID(id) {
		return "", false, nil
	}
	if p.Store.HasBox(id) {
		return id, p.Store.Bool(id, "toniebox.api_access"), nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	known := false
	for _, box := range p.Store.BoxIDs() {
		if box == id {
			known = true
			break
		}
	}
	if !known {
		if len(p.Store.BoxIDs()) >= 16 {
			return "", false, nil
		}
		if !p.Store.Snapshot("").Bool("core.allowNewBox") {
			return "", false, nil
		}
		dir := filepath.Join(p.Store.Snapshot("").String("core.certdir"), strings.ToLower(id))
		values := map[string]string{"commonName": id, "boxName": "teddyCloud Box " + id, "boxModel": "", "toniebox.api_access": p.Store.Snapshot("").String("toniebox.api_access"), "core.certdir": dir, "core.client_cert.file.ca": filepath.Join(dir, "ca.der"), "core.client_cert.file.crt": filepath.Join(dir, "client.der"), "core.client_cert.file.key": filepath.Join(dir, "private.der")}
		updates := map[string]*string{}
		for key, value := range values {
			v := value
			updates[key] = &v
		}
		if err := p.Store.Update(id, updates); err != nil {
			return "", false, errors.New("box registration could not be persisted")
		}

	}
	return id, p.Store.Snapshot(id).Bool("toniebox.api_access"), nil
}
