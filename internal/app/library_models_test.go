// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"google.golang.org/protobuf/proto"
	"goteddycloud/internal/content"
	"goteddycloud/internal/taf"
)

const libraryModelCatalog = `[{"model":"11001763","series":"Schlummerbande","episodes":"Nachtlicht Tonie - Schlummerhase","audio_id":["1727440095"],"hash":["AF2199266AFFDE39ED16D64126026B7491564B47"]},{"model":"OTHER","series":"Other"}]`

func libraryModelFile(t *testing.T, s *Server) string {
	t.Helper()
	if err := s.Catalog.ReplaceOfficial([]byte(libraryModelCatalog)); err != nil {
		t.Fatal(err)
	}
	data := syntheticTAF(t)
	header, err := taf.ReadHeader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	header.AudioId = proto.Uint32(1758910061)
	encoded, err := taf.MarshalHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	copy(data, encoded)
	name := "by/audioID/1758910061.taf"
	path := filepath.Join(s.Settings.String("", "internal.librarydirfull"), name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return name
}

func listedLibraryModel(t *testing.T, s *Server, version, overlay string) string {
	t.Helper()
	reply := perform(s, "GET", "/api/fileIndex"+version+"?special=library&path=by/audioID&overlay="+overlay, nil, false)
	var result struct {
		Files []struct {
			Name string `json:"name"`
			Info struct {
				Model string `json:"model"`
			} `json:"tonieInfo"`
		} `json:"files"`
	}
	if reply.Code != 200 || json.Unmarshal(reply.Body.Bytes(), &result) != nil {
		t.Fatal(reply.Code, reply.Body.String())
	}
	for _, file := range result.Files {
		if file.Name == "1758910061.taf" {
			return file.Info.Model
		}
	}
	t.Fatal("library file missing", reply.Body.String())
	return ""
}

func TestLibraryNewAudioIDUsesLinkedTagModel(t *testing.T) {
	for _, version := range []string{"", "V2"} {
		t.Run(version, func(t *testing.T) {
			s := fixtureApp(t)
			name := libraryModelFile(t, s)
			if got := listedLibraryModel(t, s, version, ""); got != "" {
				t.Fatal("unknown audio ID guessed a model", got)
			}
			if err := s.Content.Update(s.Settings.Snapshot(""), "11000000500304e0", func(m content.Metadata) error {
				m.Set("source", "lib://"+name)
				m.Set("tonie_model", "11001763")
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if got := listedLibraryModel(t, s, version, ""); got != "11001763" {
				t.Fatal("library ignored the explicitly linked model", got)
			}
		})
	}
}

func TestLibraryLinkedModelsRespectOverlayAndConflicts(t *testing.T) {
	s := fixtureApp(t)
	name := libraryModelFile(t, s)
	const id = "001122334455"
	if err := s.Settings.Set(id, "core.contentdir", "private-library-models"); err != nil {
		t.Fatal(err)
	}
	snap := s.Settings.Snapshot(id)
	if err := os.MkdirAll(snap.String("internal.contentdirfull"), 0700); err != nil {
		t.Fatal(err)
	}
	link := func(ruid, model, source string) {
		t.Helper()
		if err := s.Content.Update(snap, ruid, func(m content.Metadata) error {
			m.Set("source", source)
			m.Set("tonie_model", model)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	link("11000000500304e0", "11001763", "lib://"+name)
	if got := listedLibraryModel(t, s, "V2", id); got != "11001763" {
		t.Fatal("selected overlay model missing", got)
	}
	if got := listedLibraryModel(t, s, "V2", ""); got != "" {
		t.Fatal("model leaked from another overlay", got)
	}
	link("22000000500304e0", "11001763", filepath.Join(snap.String("internal.librarydirfull"), name))
	if got := listedLibraryModel(t, s, "V2", id); got != "11001763" {
		t.Fatal("same-model links were treated as ambiguous", got)
	}
	link("33000000500304e0", "OTHER", "lib://"+name)
	if got := listedLibraryModel(t, s, "V2", id); got != "" {
		t.Fatal("conflicting tag models selected an arbitrary model", got)
	}
	link("33000000500304e0", "OTHER", "lib://../"+name)
	if got := listedLibraryModel(t, s, "V2", id); got != "11001763" {
		t.Fatal("unsafe source affected library recognition", got)
	}
}

func TestLibraryCatalogAndFileMetadataTakePrecedenceOverTagLinks(t *testing.T) {
	s := fixtureApp(t)
	name := libraryModelFile(t, s)
	snap := s.Settings.Snapshot("")
	if err := s.Content.Update(snap, "11000000500304e0", func(m content.Metadata) error {
		m.Set("source", "lib://"+name)
		m.Set("tonie_model", "11001763")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(snap.String("internal.librarydirfull"), name)
	if err := os.WriteFile(path+".json", []byte(`{"tonie_model":"OTHER","listened":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := listedLibraryModel(t, s, "V2", ""); got != "OTHER" {
		t.Fatal("linked model replaced file metadata", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	header, err := taf.ReadHeader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var entries []map[string]any
	if err := json.Unmarshal([]byte(libraryModelCatalog), &entries); err != nil {
		t.Fatal(err)
	}
	entries = append(entries, map[string]any{"model": "EXACT", "audio_id": []uint32{1758910061}, "hash": []string{hex.EncodeToString(header.Sha1Hash)}})
	catalog, err := json.Marshal(entries)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Catalog.ReplaceOfficial(catalog); err != nil {
		t.Fatal(err)
	}
	if got := listedLibraryModel(t, s, "V2", ""); got != "EXACT" {
		t.Fatal("linked model replaced the exact catalog match", got)
	}
}

func TestTagAlternativeContentRequiresRecognizedDifferentModel(t *testing.T) {
	for _, test := range []struct {
		name, tagModel, audioModel string
		alternative                bool
	}{
		{"unknown audio edition", "11001763", "", false},
		{"same model", "11001763", "11001763", false},
		{"different model", "11001763", "OTHER", true},
		{"same model ignoring case", "other", "OTHER", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := fixtureApp(t)
			name := libraryModelFile(t, s)
			snap := s.Settings.Snapshot("")
			const ruid = "11000000500304e0"
			if err := s.Content.Update(snap, ruid, func(m content.Metadata) error {
				m.Set("source", "lib://"+name)
				m.Set("tonie_model", test.tagModel)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if test.audioModel != "" {
				data, err := os.ReadFile(filepath.Join(snap.String("internal.librarydirfull"), name))
				if err != nil {
					t.Fatal(err)
				}
				header, err := taf.ReadHeader(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}
				var entries []map[string]any
				if err := json.Unmarshal([]byte(libraryModelCatalog), &entries); err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if entry["model"] == test.audioModel {
						entry["audio_id"] = []uint32{header.GetAudioId()}
						entry["hash"] = []string{hex.EncodeToString(header.Sha1Hash)}
					}
				}
				catalog, err := json.Marshal(entries)
				if err != nil {
					t.Fatal(err)
				}
				if err := s.Catalog.ReplaceOfficial(catalog); err != nil {
					t.Fatal(err)
				}
			}
			info, err := s.tagInfo(context.Background(), snap, "", ruid)
			if err != nil {
				t.Fatal(err)
			}
			if _, alternative := info["sourceInfo"]; alternative != test.alternative {
				t.Fatal("wrong alternative-content indication", info)
			}
			if test.alternative && info["sourceInfo"].(map[string]any)["model"] != test.audioModel {
				t.Fatal("alternative content lost its recognized model", info)
			}
			metadata, err := s.Content.Metadata(snap, ruid)
			if err != nil || metadata.String("tonie_model") != test.tagModel {
				t.Fatal("content recognition changed the assigned figure", metadata, err)
			}
		})
	}
}
