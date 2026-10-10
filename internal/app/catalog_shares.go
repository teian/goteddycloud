// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"fmt"
	"path"
	"slices"
	"strings"
)

// Shares belong to an area, not a membership. Recipient identities survive
// password changes but never account deletion and recreation with the same name.
type catalogItem struct {
	Root string `json:"root"` // content or library
	Path string `json:"path"`
}
type catalogGrant struct {
	Recipient string        `json:"recipient"` // stable account identity (default area ID)
	All       bool          `json:"all"`
	Items     []catalogItem `json:"items"`
}

const maxCatalogShareItems = 256

func cloneCatalogGrants(grants []catalogGrant) []catalogGrant {
	result := slices.Clone(grants)
	for i := range result {
		result[i].Items = slices.Clone(result[i].Items)
	}
	return result
}
func userByIdentity(users []webUser, id string) int {
	for i, user := range users {
		if user.AreaID == id {
			return i
		}
	}
	return -1
}
func validCatalogItem(item catalogItem) bool {
	if item.Root != "content" && item.Root != "library" {
		return false
	}
	if item.Path == "" || len(item.Path) > 1024 || strings.ContainsAny(item.Path, "\\\x00\r\n") || path.IsAbs(item.Path) || path.Clean(item.Path) != item.Path {
		return false
	}
	for _, segment := range strings.Split(item.Path, "/") {
		if segment == "." || segment == ".." || strings.HasPrefix(segment, ".") {
			return false
		}
	}
	// JSON sidecars, certificates and arbitrary configuration files are never shared.
	switch strings.ToLower(path.Ext(item.Path)) {
	case ".taf", ".ogg", ".opus", ".mp3", ".wav", ".flac", ".m4a", ".m4b", ".aac":
		return true
	}
	if item.Root == "content" {
		parts := strings.Split(item.Path, "/")
		if len(parts) == 2 && len(parts[0]) == 8 && len(parts[1]) == 8 {
			for _, c := range parts[0] + parts[1] {
				if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
					return false
				}
			}
			return true
		}
	}
	return false
}
func validateCatalogGrants(users []webUser, grants []catalogGrant) error {
	seen := map[string]bool{}
	if len(grants) > maxWebUsers {
		return fmt.Errorf("%w: too many catalog recipients", errAreaAccess)
	}
	for _, grant := range grants {
		if userByIdentity(users, grant.Recipient) < 0 || seen[grant.Recipient] || len(grant.Items) > maxCatalogShareItems || (grant.All && len(grant.Items) != 0) || (!grant.All && len(grant.Items) == 0) {
			return fmt.Errorf("%w: invalid catalog grant", errAreaAccess)
		}
		seen[grant.Recipient] = true
		items := map[catalogItem]bool{}
		for _, item := range grant.Items {
			if !validCatalogItem(item) || items[item] {
				return fmt.Errorf("%w: invalid or duplicate catalog item", errAreaAccess)
			}
			items[item] = true
		}
	}
	return nil
}
func canShareCatalog(user webUser, area webArea) bool {
	return user.Role == roleAdmin || area.role(user.Username) == roleAdmin || (area.ID == user.AreaID && area.role(user.Username) == roleEditor)
}
func canReadCatalog(user webUser, area webArea, item *catalogItem) bool {
	if user.Role == roleAdmin || area.role(user.Username) != "" {
		return true
	}
	for _, grant := range area.Shares {
		if grant.Recipient == user.AreaID {
			return item == nil || grant.All || slices.Contains(grant.Items, *item)
		}
	}
	return false
}
func catalogShareRoute(p string) bool {
	switch p {
	case "/api/auth/catalog/shares", "/api/auth/catalog/grant", "/api/auth/catalog/revoke", "/api/auth/catalog/items", "/api/auth/catalog/download":
		return true
	}
	return false
}
