// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"time"

	"goteddycloud/internal/fetchpolicy"
)

// Metadata has a fixed public source, system trust, and no box/tag credentials.
func metadataClient(allowPrivate func() bool) *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
			DialContext:         fetchpolicy.Dial(allowPrivate),
			TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second,
			MaxResponseHeaderBytes: 32 << 10, DisableKeepAlives: true,
		},
		CheckRedirect: func(r *http.Request, via []*http.Request) error {
			if len(via) >= 5 || r.URL.Scheme != "https" || r.URL.Host != via[0].URL.Host {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}
func (s *Server) updateCatalog(ctx context.Context) error {
	select {
	case s.metadataUpdate <- struct{}{}:
		defer func() { <-s.metadataUpdate }()
	default:
		return errors.New("metadata update already in progress")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.revvox.de/tonies.json?source=teddyCloud&version=goteddycloud", nil)
	if err != nil {
		return err
	}
	res, err := s.MetadataHTTP.Do(req)
	if err != nil {
		return errors.New("metadata source unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return errors.New("metadata source rejected request")
	}
	const limit = 64 << 20
	if res.ContentLength > limit {
		return errors.New("metadata exceeds size limit")
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return errors.New("metadata transfer failed")
	}
	if len(data) > limit || res.ContentLength >= 0 && res.ContentLength != int64(len(data)) {
		return errors.New("metadata length invalid")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.Catalog.ReplaceOfficial(data); err != nil {
		return err
	}
	s.registerCatalogImages(ctx)
	return nil
}

func (s *Server) updateBoxCatalog(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.revvox.de/tonieboxes.json?source=teddyCloud&version=goteddycloud", nil)
	if err != nil {
		return err
	}
	res, err := s.MetadataHTTP.Do(req)
	if err != nil {
		return errors.New("box metadata source unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 || res.ContentLength > 64<<20 {
		return errors.New("box metadata response rejected")
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, (64<<20)+1))
	if err != nil || len(data) > 64<<20 || res.ContentLength >= 0 && int64(len(data)) != res.ContentLength {
		return errors.New("box metadata transfer invalid")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.Catalog.ReplaceBoxes(data)
}
