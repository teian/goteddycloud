// SPDX-License-Identifier: GPL-2.0-only
package app

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/proto"

	"goteddycloud/internal/boxcommands"
	"goteddycloud/internal/boxstate"
	"goteddycloud/internal/catalog"
	"goteddycloud/internal/cloud"
	"goteddycloud/internal/config"
	"goteddycloud/internal/content"
	"goteddycloud/internal/events"
	"goteddycloud/internal/identity"
	"goteddycloud/internal/mqtt"
	"goteddycloud/internal/protocol"
	"goteddycloud/internal/runtime"
	"goteddycloud/internal/storage"
	"goteddycloud/internal/syncutil"
	"goteddycloud/internal/taf"
)

type Server struct {
	parent      *Server
	areaID      string
	areasMu     sync.Mutex
	areas       map[string]*userArea
	areasClosed bool

	auth              webAuth
	trackExports      chan struct{}
	MQTT              *mqtt.Integration
	integrations      sync.WaitGroup
	integrationsStop  context.CancelFunc
	Commands          *boxcommands.Service
	connections       atomic.Uint64
	cloudRequests     atomic.Uint64
	cloudBlocked      atomic.Uint64
	cloudFailed       atomic.Uint64
	imagesMu          sync.Mutex
	images            map[string]*imageEntry
	imageSlots        chan struct{}
	assignmentMu      sync.Mutex // short pending-assignment reservation state
	assignmentWork    syncutil.RWGate
	assignmentVersion uint64
	Shutdown          func(restart bool)
	cacheRoot         *storage.Root
	reverseFlushed    map[string]bool
	reverseSlots      chan struct{}
	reverseRequests   atomic.Uint64
	www               *storage.Root
	bundledWWW        *storage.Root
	Cloud             upstream
	MetadataHTTP      *http.Client
	metadataUpdate    chan struct{}
	Catalog           *catalog.Catalog
	Events            *events.Hub
	Boxes             *boxstate.Store
	Settings          *config.Store
	Content           *content.Store
	Identity          *identity.Policy
	Now               func() time.Time
	mu                sync.Mutex // freshness only
	rtnlMu            sync.Mutex
	reverseMu         sync.Mutex
	freshness         map[string][]uint64
}

func New(settings *config.Store) (*Server, error) {
	cat, err := catalog.OpenWithStorage(settings.String("", "internal.configdirfull"), settings.Storage())
	if err != nil {
		return nil, err
	}
	www, err := settings.Storage().OpenRoot(settings.String("", "internal.wwwdirfull"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		cat.Close()
		return nil, err
	}
	hub := events.New()
	server := &Server{
		www:          www,
		Cloud:        cloud.New(),
		Catalog:      cat,
		Settings:     settings,
		Content:      content.NewWithStorage(settings.Storage()),
		Identity:     &identity.Policy{Store: settings},
		Now:          time.Now,
		freshness:    make(map[string][]uint64),
		Events:       hub,
		Boxes:        boxstate.New(hub),
		trackExports: make(chan struct{}, maxConcurrentTrackExports),
	}
	// Container assets live outside the writable data mount. Keep the local
	// bundle independent of the configured storage backend (including S3).
	if path := os.Getenv("GOTEDDYCLOUD_BUNDLED_WWW"); path != "" {
		server.bundledWWW, err = storage.Open(path)
		if err != nil {
			server.Close()
			return nil, fmt.Errorf("bundled web assets: %w", err)
		}
	}
	if err := server.loadWebUsers(); err != nil {
		server.Close()
		return nil, fmt.Errorf("web users: %w", err)
	}
	server.Commands = boxcommands.New(settings, hub)
	server.MQTT = mqtt.NewIntegration(settings, hub, server.executeBoxCommand)
	server.MQTT.PlaybackStop = func(id string) {
		if target, err := server.boxServer(id); err == nil {
			target.Commands.Cancel(id)
		}
	}
	server.cacheRoot, err = settings.Storage().OpenRoot(settings.String("", "internal.cachedirfull"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		server.Close()
		return nil, err
	}
	server.images = map[string]*imageEntry{}
	server.imageSlots = make(chan struct{}, 4)
	server.reverseFlushed = map[string]bool{}
	server.reverseSlots = make(chan struct{}, 8)
	server.MetadataHTTP = metadataClient(func() bool { return settings.Bool("", "core.allowPrivateFetch") })
	server.metadataUpdate = make(chan struct{}, 1)
	server.Boxes.ContentInfo = func(id string, uid uint64, audioID uint32) (string, string) {
		entry := cat.ByAudio(audioID, nil)
		if entry == nil || audioID == 1 {
			metadata, err := server.Content.Metadata(settings.Snapshot(id), content.RUID(uid))
			if err == nil {
				entry = cat.ByModel(metadata.String("tonie_model"))
			}
		}
		if entry == nil {
			return "Unknown", ""
		}
		return entry.String("title"), server.registerImage(context.Background(), entry.String("pic"), false, false)
	}
	snap := settings.Snapshot("")
	if err := cat.RecoverRename(snap.String("internal.contentdirfull"), snap.Uint("tonie_json.custom_backup_keep"), time.Now(), func(from, to string) error { return server.Content.RenameModels(context.Background(), snap, from, to) }); err != nil {
		server.Close()
		return nil, fmt.Errorf("custom model rename recovery: %w", err)
	}
	server.registerCatalogImages(context.Background())
	return server, nil
}
func (s *Server) Close() error {
	if s.integrationsStop != nil {
		s.integrationsStop()
		s.integrations.Wait()
	}
	var err error
	s.areasMu.Lock()
	s.areasClosed = true
	areas := s.areas
	s.areas = nil
	s.areasMu.Unlock()
	for _, area := range areas {
		err = errors.Join(err, area.close())
	}
	if s.www != nil {
		err = errors.Join(err, s.www.Close())
	}
	if s.bundledWWW != nil {
		err = errors.Join(err, s.bundledWWW.Close())
	}
	if s.cacheRoot != nil {
		err = errors.Join(err, s.cacheRoot.Close())
	}
	return errors.Join(err, s.Catalog.Close(), s.Content.Close())
}
func response(w http.ResponseWriter, status int, kind string, data []byte) {
	if kind != "" {
		w.Header().Set("Content-Type", kind)
	} else if status == http.StatusNotFound {
		w.Header()["Content-Type"] = nil
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
func (s *Server) Device(w http.ResponseWriter, r *http.Request) {
	// Deployment security policy is enforced once, before ownership dispatch.
	if s.parent == nil {
		s.allowOrigin(w)
		if s.securityCheck(w, r) {
			return
		}
	}
	s.connections.Add(1)
	peer := runtime.RequestPeer(r.Context())
	id, authenticated, err := s.Identity.Resolve(peer)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	if s.parent == nil && id != "" {
		if owner, ok := s.boxOwner(id); ok {
			area, err := s.areaServer(owner)
			if err != nil {
				http.Error(w, "User area unavailable", http.StatusServiceUnavailable)
				return
			}
			area.Device(w, r)
			return
		}
	}
	if peer.Box && id != "" {
		s.boxUserAgent(id, r.UserAgent())
	}
	snap := s.Settings.Snapshot(id)
	if peer.Box && snap.Bool("core.boxCertAuth") && !authenticated {
		response(w, 401, "", nil)
		return
	}
	get := strings.EqualFold(r.Method, "GET")
	post := strings.EqualFold(r.Method, "POST")
	path := r.URL.Path
	if peer.Binary {
		data, err := body(r, 65535)
		if err != nil {
			response(w, 413, "", nil)
			return
		}
		if err := s.Boxes.IngestWithMultipress(id, data, snap.Uint("internal.rtnl.multipressTime")); err != nil {
			response(w, 400, "", nil)
			return
		}
		s.MQTT.Record(id, "LastSeen", s.Now().UTC().Format("2006-01-02T15:04:05Z"))
		s.updateRTNLSettings(id, data)
		s.logRTNL(snap, data)
		response(w, 200, "", nil)
		return
	}
	switch {
	case get && strings.HasPrefix(path, "/v3/ota/"):
		s.otaV3(w, r, snap)
	case get && strings.HasPrefix(path, "/v3/content-meta/"):
		s.v3ContentMeta(w, r, snap)
	case get && strings.HasPrefix(path, "/v3/chapter/"):
		s.v3Chapter(w, r, snap)
	case post && strings.HasPrefix(path, "/v3/freshness-check"):
		s.v3Freshness(w, r, id, snap)
	case post && strings.HasPrefix(path, "/v3/setup-status"):
		s.v3Simple(w, r, snap, "enableV3SetupStatus", "")
	case post && strings.HasPrefix(path, "/v3/check-ota"):
		s.v3Simple(w, r, snap, "enableV3Ota", "{}")
	case post && strings.HasPrefix(path, "/v1/log"):
		data, err := body(r, 4095)
		if err != nil {
			response(w, 413, "", nil)
			return
		}
		if enabled(snap, "enableV1Log") {
			if s.cloudResponse(w, r, snap, path, cloudHeaders(r.Header), data) {
				return
			}
			response(w, 502, "", nil)
			return
		}
		response(w, 200, "", nil)
	case get && strings.HasPrefix(path, "/v1/ota/"):
		s.otaV1(w, r, id, snap)
	case get && strings.HasPrefix(path, "/v1/time"):
		if enabled(snap, "enableV1Time") && s.cloudResponse(w, r, snap, path, cloudHeaders(r.Header), nil) {
			return
		}
		response(w, 200, "text/plain; charset=utf-8", []byte(strconv.FormatInt(s.Now().Unix(), 10)))
	case get && strings.HasPrefix(path, "/v1/claim"):
		ruid, ok := ruidAt(path, 10)
		if !ok {
			http.NotFound(w, r)
			return
		}
		err := s.Content.UpdateContext(r.Context(), snap, ruid, func(m content.Metadata) error {
			m.Set("hide", false)
			m.Set("claimed", true)
			updateTagAuth(m, r, snap, ruid, true)
			return nil
		})
		if err != nil {
			http.Error(w, "claim persistence failed", 500)
			return
		}
		m, err := s.Content.Metadata(snap, ruid)
		if err != nil {
			response(w, 500, "", nil)
			return
		}
		_, _, hasOverride := override(m)
		if enabled(snap, "enableV1Claim") && (!m.Bool("nocloud") || hasOverride) {
			forwardPath, headers := cloudTagRequest(r, m, ruid)
			if s.cloudResponse(w, r, snap, forwardPath, headers, nil) {
				return
			}
			response(w, 502, "", nil)
			return
		}
		w.Header()["Content-Type"] = nil
		w.Header().Set("Content-Length", "0")
		w.WriteHeader(http.StatusOK)
	case get && (strings.HasPrefix(path, "/v1/content") || strings.HasPrefix(path, "/v2/content")):
		_, hasToken := boxToken(r)
		if strings.HasPrefix(path, "/v2/content") && !hasToken {
			response(w, 200, "", nil)
			return
		}
		ruid, ok := ruidAt(path, 12)
		if !ok {
			http.NotFound(w, r)
			return
		}
		s.serveStored(w, r, id, snap, ruid)
	case post && strings.HasPrefix(path, "/v1/cloud-reset"):
		if enabled(snap, "enableV1CloudReset") {
			if s.cloudResponse(w, r, snap, path, cloudHeaders(r.Header), nil) {
				return
			}
			response(w, 502, "", nil)
			return
		}
		response(w, 200, "application/json; charset=utf-8", []byte("{}"))
	case post && strings.HasPrefix(path, "/v1/freshness-check"):
		s.freshnessCheck(w, r, id, snap)
	default:
		http.NotFound(w, r)
	}
}
func ruidAt(path string, start int) (string, bool) {
	if len(path) < start+16 {
		return "", false
	}
	v := path[start : start+16]
	_, err := content.UIDPath(v)
	return v, err == nil
}
func (s *Server) serveStored(w http.ResponseWriter, r *http.Request, id string, snap config.Snapshot, ruid string) {
	s.assignUnknown(r, snap, ruid)
	if err := s.Content.UpdateContext(r.Context(), snap, ruid, func(m content.Metadata) error {
		m.Set("hide", false)
		updateTagAuth(m, r, snap, ruid, strings.HasPrefix(r.URL.Path, "/v2/content"))
		return nil
	}); err != nil {
		response(w, 500, "", nil)
		return
	}
	metadata, err := s.Content.Metadata(snap, ruid)
	if err != nil {
		response(w, 500, "", nil)
		return
	}
	if s.serveLive(w, r, id, snap, ruid, metadata) {
		return
	}
	_ = s.Content.EnsurePlaylist(r.Context(), snap, s.Settings.Snapshot(""), ruid)
	file, _, err := s.Content.OpenContext(r.Context(), snap, s.Settings.Snapshot(""), ruid)
	canForward := snap.Bool("cloud.enabled") && snap.Bool("cloud.enableV2Content")
	marked := false
	if canForward && !metadata.Bool("nocloud") {
		uid, _ := content.UIDFromRUID(ruid)
		s.mu.Lock()
		for _, value := range s.freshness[id] {
			if value == uid {
				marked = true
				break
			}
		}
		s.mu.Unlock()
	}
	canCloud := canForward && (!metadata.Bool("nocloud") || metadata.Bool("cloud_override"))
	if canCloud && (err != nil || marked) && !metadata.Bool("live") {
		if file != nil {
			file.Close()
		}
		s.serveCloudContent(w, r, snap, ruid, metadata, marked)
		return
	}
	if err != nil {
		response(w, 404, "", nil)
		return
	}
	file.Close()
	s.serveStoredLocal(w, r, snap, ruid)
}
func (s *Server) serveStoredLocal(w http.ResponseWriter, r *http.Request, snap config.Snapshot, ruid string) {
	_ = s.Content.EnsurePlaylist(r.Context(), snap, s.Settings.Snapshot(""), ruid)
	file, _, err := s.Content.OpenContext(r.Context(), snap, s.Settings.Snapshot(""), ruid)
	if err != nil {
		response(w, 404, "", nil)
		return
	}
	defer file.Close()
	s.markLibraryListened(r.Context(), snap, ruid)
	info, err := file.Stat()
	if err != nil {
		http.Error(w, "content unavailable", 500)
		return
	}
	offset := int64(0)
	size := info.Size()
	if strings.HasPrefix(r.URL.Query().Get("skip_header"), "t") {
		offset = taf.BlockSize
		size -= offset
	}
	serveRangeFile(w, r, file, offset, size, "application/octet-stream")
}
func serveRangeFile(w http.ResponseWriter, r *http.Request, file io.ReaderAt, offset, size int64, kind string) {
	start, end := int64(0), size-1
	if raw, ok := strings.CutPrefix(r.Header.Get("Range"), "bytes="); ok {
		first, last, _ := strings.Cut(raw, "-")
		v, e := strconv.ParseUint(first, 10, 32)
		if e == nil && v > 0 {
			start = int64(v)
			if n, e := strconv.ParseUint(last, 10, 32); e == nil && n > 0 && int64(n) < end {
				end = int64(n)
			}
		}
	}
	// The C implementation underflows invalid ranges; reject those explicitly.
	if start >= size || end < start {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		response(w, 416, "", nil)
		return
	}
	status := 200
	if start > 0 {
		status = 206
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.WriteHeader(status)
	_, _ = io.Copy(w, io.NewSectionReader(file, offset+start, end-start+1))
}
func body(r *http.Request, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("request body exceeds %d bytes", limit)
	}
	return data, nil
}
func (s *Server) freshnessCheck(w http.ResponseWriter, r *http.Request, id string, snap config.Snapshot) {
	data, err := body(r, 4095)
	if err != nil {
		response(w, 413, "", nil)
		return
	}
	request := new(protocol.TonieFreshnessCheckRequest)
	if err := proto.Unmarshal(data, request); err != nil {
		response(w, 400, "", nil)
		return
	}
	reply := s.localFreshnessContext(r.Context(), id, snap, request)
	cloudReply := s.cloudFreshness(r, snap, request, reply)
	data, err = proto.Marshal(reply)
	if err != nil {
		http.Error(w, "freshness encoding failed", 500)
		return
	}
	s.mu.Lock()
	s.freshness[id] = append([]uint64(nil), reply.TonieMarked...)
	s.mu.Unlock()
	kind := "application/octet-stream; charset=utf-8"
	if cloudReply {
		kind = "application/octet-stream"
	}
	response(w, 200, kind, data)
}
func (s *Server) localFreshness(id string, snap config.Snapshot, request *protocol.TonieFreshnessCheckRequest) *protocol.TonieFreshnessCheckResponse {
	return s.localFreshnessContext(context.Background(), id, snap, request)
}
func (s *Server) localFreshnessContext(ctx context.Context, id string, snap config.Snapshot, request *protocol.TonieFreshnessCheckRequest) *protocol.TonieFreshnessCheckResponse {
	reply := &protocol.TonieFreshnessCheckResponse{Field2: proto.Int32(0), Field6: proto.Int32(0), MaxVolSpk: proto.Int32(int32(snap.Uint("toniebox.max_vol_spk"))), MaxVolHdp: proto.Int32(int32(snap.Uint("toniebox.max_vol_hdp"))), Led: proto.Int32(int32(snap.Uint("toniebox.led"))), SlapEn: proto.Int32(0), SlapDir: proto.Int32(0)}
	if snap.Bool("toniebox.slap_enabled") {
		reply.SlapEn = proto.Int32(1)
	}
	if snap.Bool("toniebox.slap_back_left") {
		reply.SlapDir = proto.Int32(1)
	}
	if snap.Bool("toniebox.overrideFields") {
		reply.Field2 = proto.Int32(int32(snap.Uint("toniebox.field2")))
		reply.Field6 = proto.Int32(int32(snap.Uint("toniebox.field6")))
	}
	requested := map[uint64]bool{}
	for _, info := range request.TonieInfos {
		requested[info.GetUid()] = true
	}
	s.mu.Lock()
	previous := append([]uint64(nil), s.freshness[id]...)
	s.mu.Unlock()
	for _, uid := range previous {
		if !requested[uid] {
			reply.TonieMarked = append(reply.TonieMarked, uid)
		}
	}
	for _, info := range request.TonieInfos {
		ruid := content.RUID(info.GetUid())
		metadata, err := s.Content.Metadata(snap, ruid)
		if err != nil {
			continue
		}
		marked := metadata.Bool("live") || strings.HasPrefix(metadata.String("source"), "http://") || strings.HasPrefix(metadata.String("source"), "https://")
		if plan, planErr := s.Content.PrepareStream(ctx, snap, s.Settings.Snapshot(""), ruid); planErr == nil && plan != nil {
			marked = true
			if plan.Growing != nil {
				plan.Growing.Close()
			}
		}
		if snap.Bool("core.flex_enabled") && strings.EqualFold(snap.String("core.flex_uid"), fmt.Sprintf("%016X", info.GetUid())) {
			marked = true
		}
		file, header, err := s.Content.OpenContext(ctx, snap, s.Settings.Snapshot(""), ruid)
		if err == nil {
			file.Close()
			boxID, boxCustom := normalizedAudioID(info.GetAudioId())
			serverID, serverCustom := normalizedAudioID(header.GetAudioId())
			updated := boxID < serverID || snap.Bool("cloud.updateOnLowerAudioId") && boxID > serverID
			if snap.Bool("cloud.prioCustomContent") && !snap.Bool("cloud.updateOnLowerAudioId") {
				if boxCustom && !serverCustom {
					updated = false
				}
				if !boxCustom && serverCustom {
					updated = true
				}
			}
			marked = marked || updated
		}
		if marked {
			reply.TonieMarked = append(reply.TonieMarked, info.GetUid())
		}
	}
	return reply
}
func normalizedAudioID(id uint32) (uint32, bool) {
	if id >= 0x0e000000 && id < 0x50000000 {
		return id + 0x50000000, true
	}
	return id, false
}
func (s *Server) Management(w http.ResponseWriter, r *http.Request) {
	var err error
	r, err = s.managementProxyRequest(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.allowOrigin(w)
	if s.securityCheck(w, r) {
		return
	}
	if strings.EqualFold(r.Method, "OPTIONS") {
		if w.Header().Get("Access-Control-Allow-Origin") != "" {
			w.Header().Add("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400")
		response(w, 204, "", nil)
		return
	}
	if s.webAuthGate(w, r) {
		return
	}
	if s.managementAccess(w, r) {
		return
	}
	s.serveManagement(w, r)
}

func (s *Server) serveManagement(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/api/cloud/status" {
		s.cloudStatus(w, r)
		return
	}
	// The UI needs the local clock without exposing device routes or contacting the cloud.
	if r.Method == http.MethodGet && r.URL.Path == "/api/time" {
		response(w, http.StatusOK, "text/plain; charset=utf-8", []byte(strconv.FormatInt(s.Now().Unix(), 10)))
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/") || strings.HasPrefix(r.URL.Path, "/v2/") || strings.HasPrefix(r.URL.Path, "/v3/") {
		referenceNotFound(w)
		return
	}
	if strings.EqualFold(r.Method, "GET") && strings.HasPrefix(r.URL.Path, "/robots.txt") {
		_ = s.Settings.SetRuntime("", map[string]string{"internal.security_mit.robots_txt_access": strconv.FormatInt(s.Now().Unix(), 10)})
		s.static(w, r)
		return
	}
	s.connections.Add(1)
	if s.statsAPI(w, r) {
		return
	}
	if strings.EqualFold(r.Method, "GET") && strings.HasPrefix(r.URL.Path, "/api/sse") {
		s.accessStream(w, r, s)
		return
	}
	if s.cc3200API(w, r) {
		return
	}
	if s.firmwareAPI(w, r) {
		return
	}
	if s.cacheAPI(w, r) {
		return
	}
	if s.tagsAPI(w, r) {
		return
	}
	if s.listenedAPI(w, r) {
		return
	}
	if s.fileIndexAPI(w, r) {
		return
	}
	if s.contentAPI(w, r) {
		return
	}
	if s.fileMutationAPI(w, r) {
		return
	}
	if s.audioAPI(w, r) {
		return
	}
	if s.reverseAPI(w, r) {
		return
	}
	if s.catalogAPI(w, r) {
		return
	}
	if s.usersUI(w, r) {
		return
	}
	if s.accessAPI(w, r) {
		return
	}
	if s.authAPI(w, r) {
		return
	}
	if s.settingsAPI(w, r) {
		return
	}
	s.static(w, r)
}

func updateTagAuth(m content.Metadata, r *http.Request, snap config.Snapshot, ruid string, detectCustom bool) {
	token, present := boxToken(r)
	if !present {
		return
	}
	if snap.Bool("cloud.dumpRuidAuthContentJson") && !m.Bool("cloud_override") && m.String("cloud_ruid") == "" {
		m.Set("cloud_ruid", ruid)
		m.Set("cloud_auth", strings.ToUpper(hex.EncodeToString(token)))
	}
	if !detectCustom || m.Bool("nocloud") || m.Bool("cloud_override") {
		return
	}
	custom := false
	if snap.Bool("cloud.markCustomTagByPass") {
		custom = true
		for _, b := range token {
			if b != 0 {
				custom = false
				break
			}
		}
	}
	if snap.Bool("cloud.markCustomTagByUid") && !strings.HasPrefix(ruid, "00000af0") && !strings.HasSuffix(ruid, "0304e0") {
		custom = true
	}
	if custom {
		m.Set("nocloud", true)
	}
}

func (s *Server) allowOrigin(w http.ResponseWriter) {
	if origin := s.Settings.String("", "core.allowOrigin"); origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
	}
}
