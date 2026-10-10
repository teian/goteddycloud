// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"slices"
	"strings"

	"goteddycloud/internal/storage"
	"goteddycloud/internal/taf"
)

func (s *Server) catalogSharesAPI(w http.ResponseWriter, r *http.Request) bool {
	if !catalogShareRoute(r.URL.Path) {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	user, logged := s.requestUser(r)
	if !logged {
		authFailure(401, "login_required", "Sign in to share catalogs").write(w, r)
		return true
	}
	switch r.URL.Path {
	case "/api/auth/catalog/shares":
		if r.Method != "GET" {
			response(w, 405, "", nil)
			return true
		}
		s.auth.mu.Lock()
		areas, users := cloneAreas(s.auth.areas), slices.Clone(s.auth.users)
		s.auth.mu.Unlock()
		rows := []map[string]any{}
		for _, area := range areas {
			manage := canShareCatalog(user, area)
			recipients := []map[string]any{}
			for _, grant := range area.Shares {
				if manage {
					if i := userByIdentity(users, grant.Recipient); i >= 0 {
						recipients = append(recipients, map[string]any{"username": users[i].Username, "all": grant.All, "items": grant.Items})
					}
				}
			}
			if canReadCatalog(user, area, nil) {
				rows = append(rows, map[string]any{"area": area.ID, "name": area.Name, "manage": manage, "recipients": recipients})
			}
		}
		jsonReply(w, "application/json", map[string]any{"catalogs": rows})
	case "/api/auth/catalog/grant", "/api/auth/catalog/revoke":
		s.catalogGrantMutation(w, r)
	case "/api/auth/catalog/items", "/api/auth/catalog/download":
		if r.Method != "GET" {
			response(w, 405, "", nil)
			return true
		}
		q := r.URL.Query()
		for _, key := range []string{"area", "root", "path"} {
			if len(q[key]) > 1 {
				response(w, 400, "", nil)
				return true
			}
		}
		area, ok := s.areaByID(q.Get("area"))
		item := catalogItem{Root: q.Get("root"), Path: q.Get("path")}
		download := r.URL.Path == "/api/auth/catalog/download"
		var selected *catalogItem
		if download {
			selected = &item
		}
		if !ok || !canReadCatalog(user, area, selected) {
			authFailure(403, "forbidden", "Catalog not shared").write(w, r)
			return true
		}
		target, err := s.areaServer(area)
		if err != nil {
			response(w, 500, "", nil)
			return true
		}
		if download {
			if !validCatalogItem(item) {
				response(w, 400, "", nil)
				return true
			}
			root, _, err := target.Content.ManagedRoot(target.Settings.Snapshot(""), item.Root)
			if err != nil {
				response(w, 500, "", nil)
				return true
			}
			f, err := openCatalogFile(root.WithContext(r.Context()), item.Path)
			if err != nil {
				response(w, 404, "", nil)
				return true
			}
			defer f.Close()
			info, err := f.Stat()
			if err != nil {
				response(w, 500, "", nil)
				return true
			}
			name := path.Base(item.Path)
			if path.Ext(name) == "" {
				name += ".taf"
			}
			w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
			w.Header().Set("Content-Type", "application/octet-stream")
			http.ServeContent(w, r, path.Base(item.Path), info.ModTime(), io.NewSectionReader(f, 0, info.Size()))
		} else {
			items, err := target.sharedCatalogItems(r, area, user)
			if err != nil {
				if errors.Is(err, errCatalogLimit) {
					response(w, 413, "", nil)
				} else {
					response(w, 500, "", nil)
				}
				return true
			}
			jsonReply(w, "application/json", map[string]any{"items": items})
		}
	}
	return true
}

func (s *Server) catalogGrantMutation(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		response(w, 405, "", nil)
		return
	}
	data, err := body(r, maxAuthBodyBytes)
	var payload struct {
		Area     string        `json:"area"`
		Username string        `json:"username"`
		All      bool          `json:"all"`
		Items    []catalogItem `json:"items"`
	}
	if err != nil || json.Unmarshal(data, &payload) != nil {
		response(w, 400, "", nil)
		return
	}
	s.auth.mutations.Lock()
	defer s.auth.mutations.Unlock()
	user, logged := s.requestUser(r)
	s.auth.mu.Lock()
	users, areas := slices.Clone(s.auth.users), cloneAreas(s.auth.areas)
	s.auth.mu.Unlock()
	index := findArea(areas, payload.Area)
	if !logged || index < 0 || !canShareCatalog(user, areas[index]) {
		authFailure(403, "forbidden", "Catalog owner or area administrator required").write(w, r)
		return
	}
	recipient := findUser(users, payload.Username)
	if recipient < 0 {
		authFailure(400, "invalid_recipient", "Existing account required").write(w, r)
		return
	}
	identity := users[recipient].AreaID
	grants := slices.DeleteFunc(areas[index].Shares, func(g catalogGrant) bool { return g.Recipient == identity })
	if r.URL.Path == "/api/auth/catalog/grant" {
		grant := catalogGrant{Recipient: identity, All: payload.All, Items: payload.Items}
		if err := validateCatalogGrants(users, []catalogGrant{grant}); err != nil {
			authFailure(400, "invalid_grant", err.Error()).write(w, r)
			return
		}
		// Validate exact selected files without fetching upstream or following sources.
		if !grant.All {
			target, err := s.areaServer(areas[index])
			if err != nil {
				response(w, 500, "", nil)
				return
			}
			for _, item := range grant.Items {
				root, _, err := target.Content.ManagedRoot(target.Settings.Snapshot(""), item.Root)
				if err != nil {
					response(w, 500, "", nil)
					return
				}
				f, err := openCatalogFile(root.WithContext(r.Context()), item.Path)
				if err != nil {
					authFailure(400, "invalid_item", "Select an existing stored audio file").write(w, r)
					return
				}
				f.Close()
			}
		}
		grants = append(grants, grant)
	}
	areas[index].Shares = grants
	if err := s.saveAccess(users, areas); err != nil {
		if errors.Is(err, errAreaAccess) {
			authFailure(409, "invalid_grant", err.Error()).write(w, r)
		} else {
			authFailure(500, "save_failed", "Could not save catalog shares").write(w, r)
		}
		return
	}
	authSuccess(map[string]any{"shared": r.URL.Path == "/api/auth/catalog/grant"}).write(w, r)
}

// Reject symlink aliases, including directory components. Roots still enforce
// confinement if a concurrent filesystem change races this additional check.
func openCatalogFile(root *storage.Root, name string) (storage.File, error) {
	parts := strings.Split(name, "/")
	for i := range parts {
		info, err := root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, os.ErrPermission
		}
	}
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, os.ErrNotExist
	}
	return f, nil
}

var errCatalogLimit = errors.New("catalog listing limit exceeded")

type sharedCatalogRow struct {
	catalogItem
	Size        int64          `json:"size"`
	Title       string         `json:"title"`
	Description map[string]any `json:"description"`
}

func (s *Server) catalogRow(root *storage.Root, item catalogItem) (sharedCatalogRow, error) {
	f, err := openCatalogFile(root, item.Path)
	if err != nil {
		return sharedCatalogRow{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return sharedCatalogRow{}, err
	}
	metadata := fileMetadata(root, item.Path)
	entry := s.Catalog.ByModel(metadata.String("tonie_model"))
	if entry == nil {
		if header, err := taf.ReadHeader(f); err == nil {
			entry = s.Catalog.ByAudio(header.GetAudioId(), header.Sha1Hash)
		}
	}
	description := tonieDescription(entry, metadata.String("tonie_model"))
	// Pictures may contain remote URLs or private paths; recipients get text only.
	delete(description, "picture")
	title := strings.TrimSpace(description["series"].(string) + " " + description["episode"].(string))
	return sharedCatalogRow{catalogItem: item, Size: info.Size(), Title: title, Description: description}, nil
}

// Listing traverses stored content and library only. Selected grants open exact
// files, avoiding a traversal of the owner's unshared collection altogether.
func (s *Server) sharedCatalogItems(r *http.Request, area webArea, user webUser) ([]sharedCatalogRow, error) {
	rows := []sharedCatalogRow{}
	budget := indexLimit
	var selected []catalogItem
	if user.Role != roleAdmin && area.role(user.Username) == "" {
		for _, grant := range area.Shares {
			if grant.Recipient == user.AreaID && !grant.All {
				selected = grant.Items
			}
		}
	}
	for _, special := range []string{"content", "library"} {
		root, _, err := s.Content.ManagedRoot(s.Settings.Snapshot(""), special)
		if err != nil {
			return nil, err
		}
		root = root.WithContext(r.Context())
		if selected != nil {
			for _, item := range selected {
				if err := r.Context().Err(); err != nil {
					return nil, err
				}
				if item.Root != special {
					continue
				}
				row, err := s.catalogRow(root, item)
				if err == nil {
					rows = append(rows, row)
				} else if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, os.ErrPermission) {
					return nil, err
				}
			}
			continue
		}
		var walk func(string, int) error
		walk = func(folder string, depth int) error {
			if err := r.Context().Err(); err != nil {
				return err
			}
			if depth > 32 {
				return errCatalogLimit
			}
			dir, err := root.Open(folder)
			if err != nil {
				return err
			}
			entries, err := dir.ReadDir(budget + 1)
			dir.Close()
			if err != nil && err != io.EOF {
				return err
			}
			budget -= len(entries)
			if budget < 0 {
				return errCatalogLimit
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".") {
					continue
				}
				name := path.Join(folder, entry.Name())
				info, err := root.Lstat(name)
				if errors.Is(err, os.ErrNotExist) {
					continue
				}
				if err != nil {
					return err
				}
				if info.IsDir() {
					if err := walk(name, depth+1); err != nil {
						return err
					}
					continue
				}
				item := catalogItem{Root: special, Path: name}
				if !info.Mode().IsRegular() || !validCatalogItem(item) {
					continue
				}
				row, err := s.catalogRow(root, item)
				if err == nil {
					rows = append(rows, row)
				} else if !errors.Is(err, os.ErrNotExist) && !errors.Is(err, os.ErrPermission) {
					return err
				}
			}
			return nil
		}
		if err := walk(".", 0); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(rows, func(a, b sharedCatalogRow) int { return strings.Compare(a.Root+"/"+a.Path, b.Root+"/"+b.Path) })
	return rows, nil
}
