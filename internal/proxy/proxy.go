package proxy

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"hd2-strain-lab/assets"
	"hd2-strain-lab/internal/certs"
	"hd2-strain-lab/internal/config"
)

var (
	statusPathRe  = regexp.MustCompile(`(?i)^/api/WarSeason/([0-9]+)/Status$`)
	warInfoPathRe = regexp.MustCompile(`(?i)^/api/WarSeason/([0-9]+)/WarInfo$`)
	effectsPathRe = regexp.MustCompile(`(?i)^/api/WarSeason/GalacticWarEffects$`)
)

// Stats holds real-time telemetry of the local proxy.
type Stats struct {
	Running           bool
	Port              int
	CRLURL            string
	Requests          int
	StatusHits        int
	Rewrites          int
	EffectsRewrites   int
	CatalogCount      int
	WarID             int
	LastMessage       string
	LastRewriteAt     time.Time
	AppliedPlanDigest string
	Logs              []string
}

// Server is a pure-Go HTTP/HTTPS proxy that selectively MITMs only
// api.live.prod.thehelldiversgame.com:443 while tunneling all other traffic untouched.
type Server struct {
	mu       sync.RWMutex
	rwMu     sync.Mutex
	certs    *certs.Manager
	ln       net.Listener
	running  bool
	port     int
	crlURL   string
	leafCert *tls.Certificate

	plan     config.Plan
	settings config.Settings
	rwCtx    *RewriteContext
	stats    Stats

	onUpdate      func()
	onLiveStatus  func(raw []byte)
	onLiveEffects func(raw []byte)

	lastWarInfoAttempt map[[2]int]time.Time

	// Optional overrides used by tests to point targetHost at a local TLS test server.
	testUpstreamAddr string
	testRootCAs      *x509.CertPool
}

func NewServer(cm *certs.Manager, plan config.Plan, settings config.Settings, onUpdate func(), onLiveStatus func(raw []byte)) *Server {
	rwCtx := &RewriteContext{
		AllowedEffects:  make(map[int]bool),
		EffectTypes:     make(map[int]int),
		AllowAllEffects: settings.AllowAllEffects,
		CampaignIDs:     make(map[int]int),
		DefenseStates:   make(map[int]DefenseState),
	}
	// Seed initial WarInfo region reference from bundled snapshot
	if doc, err := decodeJSONObject(assets.WarInfoJSON); err == nil {
		rwCtx.RegionReference = doc
	}
	return &Server{
		certs:              cm,
		plan:               plan.Clone(),
		settings:           settings,
		rwCtx:              rwCtx,
		onUpdate:           onUpdate,
		onLiveStatus:       onLiveStatus,
		lastWarInfoAttempt: make(map[[2]int]time.Time),
		stats: Stats{
			LastMessage: "代理未启动",
		},
	}
}

// SeedWarData updates the proxy's RegionReference (WarInfo) and AllowedEffects catalog
// from live API payloads fetched by the UI.
func (s *Server) SeedWarData(warID int, warInfoPlain, effectsPlain []byte) {
	if len(warInfoPlain) > 0 {
		s.handleObservedWarInfo(warID, warInfoPlain)
	}
	if len(effectsPlain) > 0 {
		s.handleObservedEffects(effectsPlain)
	}
}

// SetOnLiveEffects registers an optional callback invoked with raw /GalacticWarEffects payloads.
func (s *Server) SetOnLiveEffects(fn func(raw []byte)) {
	s.mu.Lock()
	s.onLiveEffects = fn
	s.mu.Unlock()
}

func (s *Server) pruneContextLocked() {
	if s.rwCtx == nil {
		return
	}
	targets, err := config.NormalizeTargets(s.plan)
	if err != nil {
		if countEnabled(s.plan) == 0 {
			clear(s.rwCtx.CampaignIDs)
			clear(s.rwCtx.DefenseStates)
		}
		return
	}
	keepCampaign := make(map[int]bool, len(targets))
	keepDefense := make(map[int]bool, len(targets))
	for _, t := range targets {
		if t.Campaign {
			keepCampaign[t.Planet] = true
			if t.CampaignType == 4 {
				keepDefense[t.Planet] = true
			}
		}
	}
	for pid := range s.rwCtx.CampaignIDs {
		if !keepCampaign[pid] {
			delete(s.rwCtx.CampaignIDs, pid)
		}
	}
	for pid := range s.rwCtx.DefenseStates {
		if !keepDefense[pid] {
			delete(s.rwCtx.DefenseStates, pid)
		}
	}
}

func (s *Server) UpdatePlan(plan config.Plan) {
	s.mu.Lock()
	s.plan = plan.Clone()
	s.pruneContextLocked()
	var extras []string
	if len(s.plan.DisabledGlobalEffects) > 0 {
		extras = append(extras, fmt.Sprintf("%d 项全局取消效果", len(s.plan.DisabledGlobalEffects)))
	}
	if len(s.plan.EffectValueOverrides) > 0 {
		extras = append(extras, fmt.Sprintf("%d 项自定义数值覆盖", len(s.plan.EffectValueOverrides)))
	}
	if len(extras) > 0 {
		s.stats.LastMessage = fmt.Sprintf("配置已更新（%d 颗启用星球，%s），等待游戏下次刷新", countEnabled(s.plan), strings.Join(extras, "，"))
	} else {
		s.stats.LastMessage = fmt.Sprintf("配置已更新（%d 颗启用星球），等待游戏下次刷新战况", countEnabled(s.plan))
	}
	s.appendLogLocked(s.stats.LastMessage)
	s.mu.Unlock()
	s.notify()
}

func (s *Server) UpdateSettings(settings config.Settings) {
	s.mu.Lock()
	s.settings = settings
	s.rwCtx.AllowAllEffects = settings.AllowAllEffects
	s.mu.Unlock()
}

func (s *Server) Stats() Stats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := s.stats
	cp.Logs = append([]string(nil), s.stats.Logs...)
	return cp
}

func (s *Server) Start() (int, error) {
	s.mu.Lock()
	if s.running {
		p := s.port
		s.mu.Unlock()
		return p, nil
	}
	bindPort := s.settings.BindPort
	s.mu.Unlock()

	leaf, crlURL, err := s.certs.StartSessionLeaf()
	if err != nil {
		return 0, fmt.Errorf("签发会话证书失败: %w", err)
	}

	addr := fmt.Sprintf("127.0.0.1:%d", bindPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		s.certs.StopSessionLeaf()
		return 0, fmt.Errorf("监听 %s 失败: %w", addr, err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	s.mu.Lock()
	s.ln = ln
	s.port = port
	s.crlURL = crlURL
	s.leafCert = leaf
	s.running = true
	s.stats.Running = true
	s.stats.Port = port
	s.stats.CRLURL = crlURL
	s.stats.LastMessage = fmt.Sprintf("代理运行中 127.0.0.1:%d，等待游戏连接", port)
	s.appendLogLocked(s.stats.LastMessage)
	s.mu.Unlock()
	s.notify()

	go s.acceptLoop(ln)
	return port, nil
}

func (s *Server) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	ln := s.ln
	s.ln = nil
	s.running = false
	s.stats.Running = false
	s.stats.LastMessage = "代理已停止"
	s.appendLogLocked("代理已停止")
	s.mu.Unlock()

	if ln != nil {
		_ = ln.Close()
	}
	s.certs.StopSessionLeaf()
	s.notify()
}

func (s *Server) appendLogLocked(msg string) {
	line := time.Now().Format("15:04:05 ") + msg
	s.stats.Logs = append(s.stats.Logs, line)
	if len(s.stats.Logs) > 120 {
		s.stats.Logs = s.stats.Logs[len(s.stats.Logs)-120:]
	}
}

func (s *Server) notify() {
	if s.onUpdate != nil {
		s.onUpdate()
	}
}

func countEnabled(p config.Plan) int {
	n := 0
	for _, t := range p.Targets {
		if t.Enabled {
			n++
		}
	}
	return n
}

func (s *Server) acceptLoop(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go s.handleConn(conn)
	}
}

func (s *Server) handleConn(clientConn net.Conn) {
	defer clientConn.Close()
	_ = clientConn.SetDeadline(time.Now().Add(30 * time.Second))

	br := bufio.NewReader(clientConn)
	req, err := http.ReadRequest(br)
	if err != nil {
		return
	}
	_ = clientConn.SetDeadline(time.Time{})

	if req.Method == http.MethodConnect {
		host, portStr, err := net.SplitHostPort(req.Host)
		if err != nil {
			host = req.Host
			portStr = "443"
		}
		isTarget := strings.EqualFold(host, certs.TargetHost) && portStr == "443"
		if isTarget {
			s.handleTargetMITM(clientConn, br)
			return
		}
		s.tunnelDirect(clientConn, br, req.Host)
		return
	}

	// Plain HTTP request (non-CONNECT): forward directly
	s.forwardPlainHTTP(clientConn, br, req)
}

func (s *Server) dialUpstream(ctx context.Context, targetAddr string) (net.Conn, error) {
	s.mu.RLock()
	settings := s.settings
	s.mu.RUnlock()

	if upstreamURL := settings.EffectiveUpstreamProxyURL(); upstreamURL != "" {
		u, err := url.Parse(upstreamURL)
		if err == nil && u.Host != "" {
			d := &net.Dialer{Timeout: 10 * time.Second}
			upConn, err := d.DialContext(ctx, "tcp", u.Host)
			if err != nil {
				return nil, err
			}
			var authHeader string
			if u.User != nil {
				pass, _ := u.User.Password()
				cred := base64.StdEncoding.EncodeToString([]byte(u.User.Username() + ":" + pass))
				authHeader = "Proxy-Authorization: Basic " + cred + "\r\n"
			}
			connectReq := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n%s\r\n", targetAddr, targetAddr, authHeader)
			if _, err := upConn.Write([]byte(connectReq)); err != nil {
				_ = upConn.Close()
				return nil, err
			}
			ubr := bufio.NewReader(upConn)
			resp, err := http.ReadResponse(ubr, &http.Request{Method: http.MethodConnect})
			if err != nil {
				_ = upConn.Close()
				return nil, err
			}
			_ = resp.Body.Close()
			if resp.StatusCode != 200 {
				_ = upConn.Close()
				return nil, fmt.Errorf("上游代理 CONNECT 返回 %d", resp.StatusCode)
			}
			return upConn, nil
		}
	}

	d := &net.Dialer{Timeout: 10 * time.Second}
	return d.DialContext(ctx, "tcp", targetAddr)
}

func (s *Server) tunnelDirect(clientConn net.Conn, br *bufio.Reader, targetAddr string) {
	if !strings.Contains(targetAddr, ":") {
		targetAddr += ":443"
	}
	upConn, err := s.dialUpstream(context.Background(), targetAddr)
	if err != nil {
		_, _ = clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\nContent-Length: 0\r\n\r\n"))
		return
	}
	defer upConn.Close()

	_, _ = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	if n := br.Buffered(); n > 0 {
		buf := make([]byte, n)
		_, _ = io.ReadFull(br, buf)
		_, _ = upConn.Write(buf)
	}
	relayBidirectional(clientConn, upConn)
}

func (s *Server) forwardPlainHTTP(clientConn net.Conn, br *bufio.Reader, req *http.Request) {
	host := req.Host
	if host == "" && req.URL != nil {
		host = req.URL.Host
	}
	if host == "" {
		return
	}
	if !strings.Contains(host, ":") {
		host += ":80"
	}
	upConn, err := s.dialUpstream(context.Background(), host)
	if err != nil {
		_, _ = clientConn.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\nContent-Length: 0\r\n\r\n"))
		return
	}
	defer upConn.Close()

	req.RequestURI = ""
	if req.URL != nil && req.URL.IsAbs() {
		req.URL.Scheme = ""
		req.URL.Host = ""
	}
	req.Header.Del("Proxy-Connection")
	req.Header.Del("Proxy-Authorization")
	if err := req.Write(upConn); err != nil {
		return
	}
	relayBidirectional(clientConn, upConn)
}

func (s *Server) handleTargetMITM(clientConn net.Conn, br *bufio.Reader) {
	_, _ = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))

	s.mu.RLock()
	leaf := s.leafCert
	s.mu.RUnlock()
	if leaf == nil {
		return
	}

	wrappedClient := &bufferedConn{Conn: clientConn, r: br}
	tlsClient := tls.Server(wrappedClient, &tls.Config{
		Certificates: []tls.Certificate{*leaf},
		MinVersion:   tls.VersionTLS12,
	})
	if err := tlsClient.Handshake(); err != nil {
		s.mu.Lock()
		s.stats.LastMessage = "客户端 TLS 握手失败（请检查是否已安装并信任本机证书）"
		s.appendLogLocked(s.stats.LastMessage + ": " + err.Error())
		s.mu.Unlock()
		s.notify()
		return
	}
	defer tlsClient.Close()

	clientReader := bufio.NewReader(tlsClient)
	transport := &http.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			s.mu.RLock()
			overrideAddr := s.testUpstreamAddr
			rootCAs := s.testRootCAs
			s.mu.RUnlock()
			if overrideAddr != "" {
				addr = overrideAddr
			}
			raw, err := s.dialUpstream(ctx, addr)
			if err != nil {
				return nil, err
			}
			tlsUp := tls.Client(raw, &tls.Config{
				ServerName: certs.TargetHost,
				RootCAs:    rootCAs,
				MinVersion: tls.VersionTLS12,
			})
			if err := tlsUp.HandshakeContext(ctx); err != nil {
				_ = raw.Close()
				return nil, err
			}
			return tlsUp, nil
		},
		DisableCompression: true,
	}
	defer transport.CloseIdleConnections()

	for {
		req, err := http.ReadRequest(clientReader)
		if err != nil {
			return
		}

		path := req.URL.Path
		isStatus := statusPathRe.MatchString(path)
		isWarInfo := warInfoPathRe.MatchString(path)
		isEffects := effectsPathRe.MatchString(path)

		s.mu.Lock()
		s.stats.Requests++
		s.mu.Unlock()

		// Prepare upstream request
		upReq := req.Clone(context.Background())
		upReq.URL.Scheme = "https"
		upReq.URL.Host = certs.TargetHost
		upReq.RequestURI = ""
		upReq.Host = certs.TargetHost
		if req.Method == http.MethodGet && (isStatus || isWarInfo || isEffects) {
			upReq.Header.Del("If-None-Match")
			upReq.Header.Del("If-Modified-Since")
			upReq.Header.Del("If-Match")
			upReq.Header.Del("If-Unmodified-Since")
			upReq.Header.Del("If-Range")
			upReq.Header.Set("Cache-Control", "no-cache")
		}

		resp, err := transport.RoundTrip(upReq)
		if err != nil {
			s.mu.Lock()
			s.stats.LastMessage = "上游游戏 API 连接失败: " + err.Error()
			s.appendLogLocked(s.stats.LastMessage)
			s.mu.Unlock()
			s.notify()
			_, _ = tlsClient.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\nContent-Length: 0\r\n\r\n"))
			return
		}

		rawBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 32*1024*1024))
		_ = resp.Body.Close()
		if readErr != nil {
			_, _ = tlsClient.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nConnection: close\r\nContent-Length: 0\r\n\r\n"))
			return
		}

		outBody := rawBody
		rebuilt := false

		if resp.StatusCode == http.StatusOK && req.Method == http.MethodGet && (isStatus || isWarInfo || isEffects) {
			if isStatus || isEffects {
				disableResponseCaching(resp)
			}
			plain, wasGz, errGz := maybeGunzip(rawBody, resp.Header.Get("Content-Encoding"))
			if errGz == nil {
				switch {
				case isEffects:
					s.handleObservedEffects(plain)
					s.mu.RLock()
					cb := s.onLiveEffects
					s.mu.RUnlock()
					if cb != nil {
						cb(plain)
					}
					patched, changed := s.handleEffectsRewrite(plain)
					if changed {
						outBody = patched
						rebuilt = true
						if wasGz {
							resp.Header.Del("Content-Encoding")
						}
						resp.Header.Del("Transfer-Encoding")
						resp.Header.Set("Content-Length", strconv.Itoa(len(outBody)))
						resp.ContentLength = int64(len(outBody))
					}
				case isWarInfo:
					if m := warInfoPathRe.FindStringSubmatch(path); len(m) == 2 {
						wid, _ := strconv.Atoi(m[1])
						s.handleObservedWarInfo(wid, plain)
					}
				case isStatus:
					if s.onLiveStatus != nil {
						s.onLiveStatus(plain)
					}
					s.ensureFreshRegionReference(transport, path, plain)
					patched, changed := s.handleStatusRewrite(plain)
					if changed {
						outBody = patched
						rebuilt = true
						if wasGz {
							resp.Header.Del("Content-Encoding")
						}
						resp.Header.Del("Transfer-Encoding")
						resp.Header.Set("Content-Length", strconv.Itoa(len(outBody)))
						resp.ContentLength = int64(len(outBody))
					}
				}
			}
		}

		if !rebuilt && resp.Header.Get("Transfer-Encoding") == "" {
			resp.Header.Set("Content-Length", strconv.Itoa(len(outBody)))
			resp.ContentLength = int64(len(outBody))
		}
		resp.Body = io.NopCloser(bytes.NewReader(outBody))
		if err := resp.Write(tlsClient); err != nil {
			return
		}
		if req.Close || resp.Close {
			return
		}
	}
}

func (s *Server) handleObservedEffects(plain []byte) {
	doc, err := decodeJSONAny(plain)
	if err != nil {
		return
	}
	var rows []any
	if m, ok := asMap(doc); ok {
		rows, _ = asSlice(m["galacticEffects"])
	} else {
		rows, _ = asSlice(doc)
	}
	if len(rows) == 0 {
		return
	}
	allowed := make(map[int]bool, len(rows))
	types := make(map[int]int, len(rows))
	for _, r := range rows {
		if rm, ok := asMap(r); ok {
			if id, okID := asInt(rm["id"]); okID && id > 0 {
				allowed[id] = true
				if et, okET := asInt(rm["effectType"]); okET {
					types[id] = et
				}
			}
		}
	}
	s.mu.Lock()
	s.rwCtx.AllowedEffects = allowed
	s.rwCtx.EffectTypes = types
	s.stats.CatalogCount = len(allowed)
	s.stats.LastMessage = fmt.Sprintf("已获取官方效果目录 (%d 条)", len(allowed))
	s.appendLogLocked(s.stats.LastMessage)
	s.mu.Unlock()
	s.notify()
}

func disableResponseCaching(resp *http.Response) {
	resp.Header.Del("ETag")
	resp.Header.Del("Last-Modified")
	resp.Header.Del("Expires")
	resp.Header.Del("Pragma")
	resp.Header.Del("Content-MD5")
	resp.Header.Del("Digest")
	resp.Header.Set("Cache-Control", "no-store, no-cache, must-revalidate")
}

func (s *Server) handleEffectsRewrite(plain []byte) ([]byte, bool) {
	s.mu.RLock()
	plan := s.plan.Clone()
	s.mu.RUnlock()

	if len(plan.EffectValueOverrides) == 0 {
		return plain, false
	}
	patched, changed, err := RewriteEffects(plain, plan)
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.stats.LastMessage = "效果数值改写跳过: " + err.Error()
		s.appendLogLocked(s.stats.LastMessage)
		go s.notify()
		return plain, false
	}
	if changed > 0 {
		s.stats.EffectsRewrites++
		s.stats.LastMessage = fmt.Sprintf("已改写 GalacticWarEffects 数值（%d 项覆盖，需启动/重启游戏生效）", changed)
		s.appendLogLocked(s.stats.LastMessage)
		go s.notify()
		return patched, true
	}
	return plain, false
}

func (s *Server) handleObservedWarInfo(warID int, plain []byte) {
	doc, err := decodeJSONObject(plain)
	if err != nil {
		return
	}
	if _, ok := asSlice(doc["planetRegions"]); !ok {
		return
	}
	s.mu.Lock()
	s.rwCtx.RegionReference = doc
	if warID > 0 {
		s.stats.WarID = warID
	}
	s.appendLogLocked(fmt.Sprintf("已采集 WarInfo (warId=%d)", warID))
	s.mu.Unlock()
	s.notify()
}

// ensureFreshRegionReference fetches /api/WarSeason/{warId}/WarInfo on-demand when
// an incoming Status has a newer layoutVersion than the cached RegionReference (matching CPV).
func (s *Server) ensureFreshRegionReference(transport *http.Transport, statusPath string, statusPlain []byte) {
	doc, err := decodeJSONObject(statusPlain)
	if err != nil {
		return
	}
	warID, okW := asInt(doc["warId"])
	if !okW || warID <= 0 {
		if m := statusPathRe.FindStringSubmatch(statusPath); len(m) == 2 {
			warID, _ = strconv.Atoi(m[1])
		}
	}
	if warID <= 0 {
		return
	}
	layout, okL := asInt(doc["layoutVersion"])
	if !okL {
		return
	}

	s.mu.Lock()
	needFetch := false
	if s.rwCtx.RegionReference == nil {
		needFetch = true
	} else {
		rw, okRW := asInt(s.rwCtx.RegionReference["warId"])
		rl, okRL := asInt(s.rwCtx.RegionReference["layoutVersion"])
		if !okRW || rw != warID || !okRL || rl != layout {
			needFetch = true
		}
	}
	if !needFetch {
		s.mu.Unlock()
		return
	}
	key := [2]int{warID, layout}
	if last, ok := s.lastWarInfoAttempt[key]; ok && time.Since(last) < 15*time.Second {
		s.mu.Unlock()
		return
	}
	s.lastWarInfoAttempt[key] = time.Now()
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("https://%s/api/WarSeason/%d/WarInfo", certs.TargetHost, warID), nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Accept-Language", "en-US")
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := transport.RoundTrip(req)
	if err != nil {
		return
	}
	rawBody, readErr := io.ReadAll(io.LimitReader(resp.Body, 16*1024*1024))
	_ = resp.Body.Close()
	if readErr != nil || resp.StatusCode != http.StatusOK {
		return
	}
	plain, _, errGz := maybeGunzip(rawBody, resp.Header.Get("Content-Encoding"))
	if errGz != nil {
		return
	}
	s.handleObservedWarInfo(warID, plain)
}

func (s *Server) handleStatusRewrite(plain []byte) ([]byte, bool) {
	s.rwMu.Lock()
	defer s.rwMu.Unlock()

	incomingWarID := 0
	if doc, err := decodeJSONObject(plain); err == nil {
		if wid, ok := asInt(doc["warId"]); ok && wid > 0 {
			incomingWarID = wid
		}
	}

	s.mu.Lock()
	plan := s.plan.Clone()
	prevWarID := s.stats.WarID
	if incomingWarID > 0 && prevWarID > 0 && incomingWarID != prevWarID {
		clear(s.rwCtx.CampaignIDs)
		clear(s.rwCtx.DefenseStates)
	}
	localCtx := &RewriteContext{
		AllowedEffects:  maps.Clone(s.rwCtx.AllowedEffects),
		EffectTypes:     maps.Clone(s.rwCtx.EffectTypes),
		AllowAllEffects: s.rwCtx.AllowAllEffects,
		RegionReference: s.rwCtx.RegionReference,
		CampaignIDs:     maps.Clone(s.rwCtx.CampaignIDs),
		DefenseStates:   maps.Clone(s.rwCtx.DefenseStates),
	}
	if localCtx.CampaignIDs == nil {
		localCtx.CampaignIDs = make(map[int]int)
	}
	if localCtx.DefenseStates == nil {
		localCtx.DefenseStates = make(map[int]DefenseState)
	}
	s.stats.StatusHits++
	s.mu.Unlock()

	planDigest := plan.Digest()
	patched, summary, err := RewriteStatus(plain, plan, localCtx)

	s.mu.Lock()
	defer s.mu.Unlock()
	if incomingWarID > 0 {
		s.stats.WarID = incomingWarID
	}
	if s.plan.Digest() == planDigest {
		s.rwCtx.CampaignIDs = localCtx.CampaignIDs
		s.rwCtx.DefenseStates = localCtx.DefenseStates
	}
	if err != nil {
		s.stats.LastMessage = "战况改写跳过: " + err.Error()
		s.appendLogLocked(s.stats.LastMessage)
		go s.notify()
		return plain, false
	}
	if summary.ChangedCount > 0 {
		s.stats.Rewrites++
		s.stats.LastRewriteAt = time.Now()
		s.stats.AppliedPlanDigest = planDigest
		if summary.PlanetsPatched == 0 {
			s.stats.LastMessage = fmt.Sprintf("已下发改写 #%d（全局效果 -%d，共 %d 处变更）",
				s.stats.Rewrites, summary.EffectsRemoved, summary.ChangedCount)
		} else {
			s.stats.LastMessage = fmt.Sprintf("已下发改写 #%d（%d 颗星球，+%d / -%d 效果，共 %d 处变更）",
				s.stats.Rewrites, summary.PlanetsPatched, summary.EffectsAdded, summary.EffectsRemoved, summary.ChangedCount)
		}
		s.appendLogLocked(s.stats.LastMessage)
		go s.notify()
		return patched, true
	}
	if countEnabled(plan) == 0 && len(plan.DisabledGlobalEffects) == 0 {
		s.stats.LastMessage = fmt.Sprintf("收到战况 Status（已启用 0 颗星球，透传原样）")
	} else {
		s.stats.LastMessage = fmt.Sprintf("收到战况 Status（配置已生效或无需新增变更）")
	}
	go s.notify()
	return plain, false
}

func maybeGunzip(raw []byte, encoding string) ([]byte, bool, error) {
	if !strings.Contains(strings.ToLower(encoding), "gzip") {
		return raw, false, nil
	}
	gr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, false, err
	}
	defer gr.Close()
	out, err := io.ReadAll(io.LimitReader(gr, 32*1024*1024))
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func relayBidirectional(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	cp := func(dst, src net.Conn) {
		defer wg.Done()
		_, _ = io.Copy(dst, src)
		if tc, ok := dst.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
	}
	go cp(a, b)
	go cp(b, a)
	wg.Wait()
}

type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufferedConn) Read(p []byte) (int, error) {
	return b.r.Read(p)
}

// Keep unused json import referenced if needed
var _ = json.Marshal
