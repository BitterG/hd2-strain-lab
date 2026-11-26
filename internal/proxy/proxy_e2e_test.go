package proxy

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"hd2-strain-lab/assets"
	"hd2-strain-lab/internal/certs"
	"hd2-strain-lab/internal/config"
)

func makeUpstreamTestCert(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(42),
		Subject:      pkix.Name{CommonName: certs.TargetHost},
		DNSNames:     []string{certs.TargetHost},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, pool
}

func TestProxyE2EConfigDispatchAndHotReload(t *testing.T) {
	profileDir := t.TempDir()
	cm, err := certs.NewManager(profileDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	// Upstream mock HD2 API server
	upCert, upPool := makeUpstreamTestCert(t)
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/WarSeason/GalacticWarEffects":
			_, _ = w.Write([]byte(`[{"id":1142,"effectType":32},{"id":1202,"effectType":40},{"id":1203,"effectType":71}]`))
		case "/api/WarSeason/801/Status":
			_, _ = w.Write(assets.StatusSampleJSON)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	upstream.TLS = &tls.Config{Certificates: []tls.Certificate{upCert}}
	upstream.StartTLS()
	defer upstream.Close()

	upAddr := strings.TrimPrefix(upstream.URL, "https://")

	plan := config.Plan{
		Schema: 2,
		Targets: []config.PlanetTarget{
			{
				Planet:        78,
				Enabled:       true,
				Effects:       []int{1142},
				Owner:         2,
				Campaign:      true,
				CampaignRace:  2,
				CampaignCount: 20,
			},
		},
	}

	srv := NewServer(cm, plan, config.DefaultSettings(), nil, nil)
	srv.testUpstreamAddr = upAddr
	srv.testRootCAs = upPool

	port, err := srv.Start()
	if err != nil {
		t.Fatalf("proxy Start failed: %v", err)
	}
	defer srv.Stop()

	// Trust our local CA in test HTTP client
	caBytes, err := os.ReadFile(cm.CACertPath())
	if err != nil {
		t.Fatal(err)
	}
	clientPool := x509.NewCertPool()
	clientPool.AppendCertsFromPEM(caBytes)

	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{RootCAs: clientPool},
		},
		Timeout: 10 * time.Second,
	}

	// 1. Observe GalacticWarEffects
	resp, err := client.Get("https://" + certs.TargetHost + "/api/WarSeason/GalacticWarEffects")
	if err != nil {
		t.Fatalf("GET GalacticWarEffects failed: %v", err)
	}
	_ = resp.Body.Close()
	if srv.Stats().CatalogCount != 3 {
		t.Fatalf("expected catalog count 3, got %d", srv.Stats().CatalogCount)
	}

	// 2. Request Status -> should inject 1142 on planet 78
	resp2, err := client.Get("https://" + certs.TargetHost + "/api/WarSeason/801/Status")
	if err != nil {
		t.Fatalf("GET Status failed: %v", err)
	}
	body2, _ := io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()

	var doc2 map[string]any
	if err := json.Unmarshal(body2, &doc2); err != nil {
		t.Fatal(err)
	}
	if !hasPlanetEffect(doc2, 78, 1142) {
		t.Fatalf("expected effect 1142 on planet 78")
	}

	// 3. Hot-reload plan with paired effect 1202 (auto-pairs 1203)
	plan.Targets[0].Effects = []int{1142, 1202}
	srv.UpdatePlan(plan)

	resp3, err := client.Get("https://" + certs.TargetHost + "/api/WarSeason/801/Status")
	if err != nil {
		t.Fatalf("GET Status after hot-reload failed: %v", err)
	}
	body3, _ := io.ReadAll(resp3.Body)
	_ = resp3.Body.Close()

	var doc3 map[string]any
	if err := json.Unmarshal(body3, &doc3); err != nil {
		t.Fatal(err)
	}
	if !hasPlanetEffect(doc3, 78, 1202) || !hasPlanetEffect(doc3, 78, 1203) {
		t.Fatalf("expected hot-reloaded effects 1202 & 1203 on planet 78")
	}
}

func hasPlanetEffect(doc map[string]any, planetID, effectID int) bool {
	rows, _ := doc["planetActiveEffects"].([]any)
	for _, r := range rows {
		m, ok := r.(map[string]any)
		if !ok {
			continue
		}
		if int(m["index"].(float64)) == planetID && int(m["galacticEffectId"].(float64)) == effectID {
			return true
		}
	}
	return false
}

func TestProxyE2EGalacticWarEffectsRewrite(t *testing.T) {
	profileDir := t.TempDir()
	cm, err := certs.NewManager(profileDir)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	upCert, upPool := makeUpstreamTestCert(t)
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/WarSeason/GalacticWarEffects" {
			_, _ = w.Write([]byte(`[{"id":1191,"effectType":72,"valueTypes":[13,2],"values":[769209189,0]}]`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	upstream.TLS = &tls.Config{Certificates: []tls.Certificate{upCert}}
	upstream.StartTLS()
	defer upstream.Close()

	plan := config.Plan{
		Schema:               2,
		EffectValueOverrides: map[int]int{1191: 500},
	}

	srv := NewServer(cm, plan, config.DefaultSettings(), nil, nil)
	srv.testUpstreamAddr = strings.TrimPrefix(upstream.URL, "https://")
	srv.testRootCAs = upPool

	var observedRaw []byte
	srv.SetOnLiveEffects(func(raw []byte) {
		observedRaw = append([]byte(nil), raw...)
	})

	port, err := srv.Start()
	if err != nil {
		t.Fatalf("proxy Start failed: %v", err)
	}
	defer srv.Stop()

	caBytes, err := os.ReadFile(cm.CACertPath())
	if err != nil {
		t.Fatal(err)
	}
	clientPool := x509.NewCertPool()
	clientPool.AppendCertsFromPEM(caBytes)

	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{RootCAs: clientPool},
		},
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get("https://" + certs.TargetHost + "/api/WarSeason/GalacticWarEffects")
	if err != nil {
		t.Fatalf("GET GalacticWarEffects failed: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		t.Fatalf("invalid rewritten GalacticWarEffects JSON: %v", err)
	}
	if len(rows) != 1 || int(rows[0]["values"].([]any)[1].(float64)) != 500 {
		t.Fatalf("expected rewritten values[1]=500, got %s", string(body))
	}
	if srv.Stats().EffectsRewrites != 1 {
		t.Fatalf("expected EffectsRewrites=1, got %d", srv.Stats().EffectsRewrites)
	}
	if !strings.Contains(string(observedRaw), ",0]") {
		t.Fatalf("expected onLiveEffects callback to receive original upstream values, got %s", string(observedRaw))
	}
}
