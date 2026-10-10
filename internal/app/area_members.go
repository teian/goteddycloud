// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"

	"goteddycloud/internal/config"
)

const maxAreas = 64

var errAreaAccess = errors.New("invalid area access")

type areaMember struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}
type webArea struct {
	ID      string         `json:"id"`
	Name    string         `json:"name"`
	Boxes   []string       `json:"boxes"`
	Members []areaMember   `json:"members"`
	Shares  []catalogGrant `json:"shares,omitempty"`
}

func cloneArea(area webArea) webArea {
	area.Boxes = append([]string{}, area.Boxes...)
	area.Members = append([]areaMember{}, area.Members...)
	area.Shares = cloneCatalogGrants(area.Shares)
	return area
}
func cloneAreas(areas []webArea) []webArea {
	result := make([]webArea, len(areas))
	for i, area := range areas {
		result[i] = cloneArea(area)
	}
	return result
}
func (area webArea) role(username string) string {
	for _, member := range area.Members {
		if strings.EqualFold(member.Username, username) {
			return member.Role
		}
	}
	return ""
}
func validAreaName(name string) bool {
	return strings.TrimSpace(name) == name && len(name) > 0 && len(name) <= 64 && !strings.ContainsFunc(name, unicode.IsControl)
}
func findArea(areas []webArea, id string) int {
	for i, area := range areas {
		if area.ID == id {
			return i
		}
	}
	return -1
}
func findUser(users []webUser, username string) int {
	for i, user := range users {
		if strings.EqualFold(user.Username, username) {
			return i
		}
	}
	return -1
}

// Areas are independent of accounts: deleting a member never deletes shared data.
// Only legacy user-role/box edits update that user's default area.
func reconcileUserAreas(oldUsers, users []webUser, areas []webArea) []webArea {
	result := cloneAreas(areas)
	for i := range result {
		result[i].Shares = slices.DeleteFunc(result[i].Shares, func(grant catalogGrant) bool { return userByIdentity(users, grant.Recipient) < 0 })
		result[i].Members = slices.DeleteFunc(result[i].Members, func(member areaMember) bool { return findUser(users, member.Username) < 0 })
	}
	for _, user := range users {
		index := findArea(result, user.AreaID)
		if index < 0 {
			result = append(result, webArea{ID: user.AreaID, Name: user.Username, Boxes: slices.Clone(user.Boxes), Members: []areaMember{{Username: user.Username, Role: user.Role}}})
			continue
		}
		old := findUser(oldUsers, user.Username)
		if old >= 0 && oldUsers[old].Role != user.Role {
			for j := range result[index].Members {
				if strings.EqualFold(result[index].Members[j].Username, user.Username) {
					result[index].Members[j].Role = user.Role
				}
			}
		}
		if old >= 0 && !slices.Equal(oldUsers[old].Boxes, user.Boxes) {
			result[index].Boxes = slices.Clone(user.Boxes)
		}
	}
	return result
}
func validateAreas(users []webUser, areas []webArea) error {
	if len(areas) > maxAreas {
		return fmt.Errorf("%w: maximum %d areas", errAreaAccess, maxAreas)
	}
	ids := map[string]bool{}
	boxes := map[string]bool{}
	for _, area := range areas {
		id, err := hex.DecodeString(area.ID)
		if err != nil || len(id) != passwordSaltBytes || ids[area.ID] || !validAreaName(area.Name) {
			return fmt.Errorf("%w: invalid area identity or name", errAreaAccess)
		}
		if err := validateCatalogGrants(users, area.Shares); err != nil {
			return err
		}
		ids[area.ID] = true
		members := map[string]bool{}
		for _, member := range area.Members {
			key := strings.ToLower(member.Username)
			if findUser(users, member.Username) < 0 || members[key] || !validRole(member.Role) {
				return fmt.Errorf("%w: invalid or duplicate member", errAreaAccess)
			}
			members[key] = true
		}
		for _, box := range area.Boxes {
			key := strings.ToLower(box)
			if !config.ValidBoxID(box) || boxes[key] {
				return fmt.Errorf("%w: invalid or multiply assigned box", errAreaAccess)
			}
			boxes[key] = true
		}
	}
	for _, user := range users {
		if !ids[user.AreaID] {
			return fmt.Errorf("%w: missing default area", errAreaAccess)
		}
	}
	return nil
}
func newAreaID() (string, error) {
	var id [passwordSaltBytes]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}
func (s *Server) areaByID(id string) (webArea, bool) {
	s.auth.mu.Lock()
	defer s.auth.mu.Unlock()
	index := findArea(s.auth.areas, id)
	if index < 0 {
		return webArea{}, false
	}
	return cloneArea(s.auth.areas[index]), true
}

// IDs are canonical selectors. Usernames remain aliases for existing private areas.
func (s *Server) resolveArea(selector string) (webArea, bool) {
	s.auth.mu.Lock()
	defer s.auth.mu.Unlock()
	index := findArea(s.auth.areas, selector)
	if index < 0 {
		if user := s.auth.findUserLocked(selector); user >= 0 {
			index = findArea(s.auth.areas, s.auth.users[user].AreaID)
		}
	}
	if index < 0 {
		for i, area := range s.auth.areas {
			if strings.EqualFold(area.Name, selector) {
				if index >= 0 {
					return webArea{}, false
				}
				index = i
			}
		}
	}
	if index < 0 {
		return webArea{}, false
	}
	return cloneArea(s.auth.areas[index]), true
}
func (s *Server) accessibleAreas(user webUser) []webArea {
	s.auth.mu.Lock()
	defer s.auth.mu.Unlock()
	result := []webArea{}
	for _, area := range s.auth.areas {
		if user.Role == roleAdmin || area.role(user.Username) != "" {
			result = append(result, cloneArea(area))
		}
	}
	return result
}
func (s *Server) profile(user webUser) map[string]any {
	profile := publicProfile(user)
	profile["areas"] = s.accessibleAreas(user)
	if area, ok := s.areaByID(user.AreaID); user.Role != roleAdmin && (!ok || area.role(user.Username) == "") {
		profile["boxes"] = []string{}
	}
	return profile
}
