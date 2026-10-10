// SPDX-License-Identifier: GPL-2.0-only
package catalog

import (
	"fmt"
	"strings"
)

// MutationErrorKind identifies a custom-catalog failure without matching prose.
type MutationErrorKind uint8

const (
	InvalidMutation MutationErrorKind = iota
	MissingModel
	CatalogReadFailure
	CatalogBackupFailure
	RenamePending
	RenameRecoveryFailure
	CatalogWriteFailure
)

// MutationError separates idiomatic internal errors from the pinned reference's
// user-facing messages. These messages contain no filesystem error details.
type MutationError struct {
	Kind    MutationErrorKind
	message string
}

func (e *MutationError) Error() string {
	if e.message == "" {
		return "custom catalog mutation failed"
	}
	return strings.ToLower(e.message[:1]) + e.message[1:]
}

// ReferenceMessage returns the exact message expected by the official frontend.
func (e *MutationError) ReferenceMessage() string { return e.message }

func mutationError(kind MutationErrorKind, format string, args ...any) *MutationError {
	return &MutationError{Kind: kind, message: fmt.Sprintf(format, args...)}
}
