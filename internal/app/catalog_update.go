// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
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
			if len(via) >= 5 || r.URL.Scheme != "https" || r.URL.User != nil {
				return http.ErrUseLastResponse
			}
			origin := via[0].URL
			if r.URL.Host != origin.Host {
				// The fixed metadata service redirects these catalogs to its
				// public GitHub mirror. Other downloads retain same-host redirects.
				catalog := origin.Path == "/tonies.json" || origin.Path == "/tonieboxes.json"
				mirrorPath := "/toniebox-reverse-engineering/tonies-json/release" + origin.Path
				if origin.Scheme != "https" || origin.Host != "api.revvox.de" || !catalog ||
					r.URL.Host != "raw.githubusercontent.com" || r.URL.Path != mirrorPath || r.URL.RawQuery != "" {
					return http.ErrUseLastResponse
				}
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
	req.Header.Set("User-Agent", "teddyCloud/goteddycloud")
	res, err := s.MetadataHTTP.Do(req)
	if err != nil {
		return errors.New("metadata source unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("metadata source rejected request: HTTP %d", res.StatusCode)
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
	req.Header.Set("User-Agent", "teddyCloud/goteddycloud")
	res, err := s.MetadataHTTP.Do(req)
	if err != nil {
		return errors.New("box metadata source unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("box metadata source rejected request: HTTP %d", res.StatusCode)
	}
	if res.ContentLength > 64<<20 {
		return errors.New("box metadata exceeds size limit")
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
