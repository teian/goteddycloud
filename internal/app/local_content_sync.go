// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"goteddycloud/internal/config"
	"goteddycloud/internal/content"
	"goteddycloud/internal/protocol"
)

type localContentReceipt struct {
	Fingerprint string `json:"fingerprint"`
	Chapters    []int  `json:"chapters,omitempty"`
}

func localContentFingerprint(header *protocol.TonieboxAudioFileHeader) string {
	return fmt.Sprintf("%d:%x", header.GetAudioId(), header.Sha1Hash)
}

func localContentReceipts(m content.Metadata) map[string]localContentReceipt {
	receipts := map[string]localContentReceipt{}
	if err := json.Unmarshal(m["local_content_receipts"], &receipts); err != nil {
		return map[string]localContentReceipt{}
	}
	if receipts == nil {
		receipts = map[string]localContentReceipt{}
	}
	return receipts
}

func localContentPending(m content.Metadata, box string, header *protocol.TonieboxAudioFileHeader) bool {
	if !m.Bool("nocloud") {
		return false
	}
	receipt := localContentReceipts(m)[box]
	return receipt.Fingerprint != localContentFingerprint(header) || len(receipt.Chapters) != 0
}

// Only authenticated device delivery calls this after a complete transfer.
// V3 downloads individual chapters; keep their progress until all have arrived.
func (s *Server) recordLocalContentDownload(ctx context.Context, snap config.Snapshot, box, ruid, fingerprint string, chapter, total int) {
	if fingerprint == "" || total < 1 || total > 99 || chapter < 0 || chapter >= total {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	err := s.Content.UpdateContext(ctx, snap, ruid, func(m content.Metadata) error {
		if !m.Bool("nocloud") {
			return nil
		}
		receipts := localContentReceipts(m)
		receipt, exists := receipts[box]
		if exists && receipt.Fingerprint == fingerprint && len(receipt.Chapters) == 0 {
			return nil
		}
		if !exists || receipt.Fingerprint != fingerprint {
			receipt = localContentReceipt{Fingerprint: fingerprint}
			for i := 0; i < total; i++ {
				receipt.Chapters = append(receipt.Chapters, i)
			}
		}
		pending := receipt.Chapters[:0]
		for _, missing := range receipt.Chapters {
			if missing != chapter {
				pending = append(pending, missing)
			}
		}
		receipt.Chapters = pending
		receipts[box] = receipt
		m.Set("local_content_receipts", receipts)
		return nil
	})
	if err != nil {
		slog.Warn("Could not persist local audio download receipt", "box", box, "reason", cacheFailureReason(err))
	}
}
