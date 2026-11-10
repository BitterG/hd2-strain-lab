package certs

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const (
	TargetHost = "api.live.prod.thehelldiversgame.com"

	certStoreProvSystemW       = 10
	certSystemStoreCurrentUser = 0x00010000
	certStoreOpenExistingFlag  = 0x00004000
	certStoreReadonlyFlag      = 0x00008000
	x509ASNEncoding            = 0x00000001
	pkcs7ASNEncoding           = 0x00010000
	certStoreAddReplace        = 3
	certFindSHA1Hash           = 0x00010000
)

var (
	crypt32                     = syscall.NewLazyDLL("crypt32.dll")
	procCertOpenStore           = crypt32.NewProc("CertOpenStore")
	procCertCloseStore          = crypt32.NewProc("CertCloseStore")
	procCertFindCertificate     = crypt32.NewProc("CertFindCertificateInStore")
	procCertAddEncoded          = crypt32.NewProc("CertAddEncodedCertificateToStore")
	procCertDeleteFromStore     = crypt32.NewProc("CertDeleteCertificateFromStore")
	procCertFreeContext         = crypt32.NewProc("CertFreeCertificateContext")
)

type cryptHashBlob struct {
	cbData uint32
	pbData *byte
}

// Manager owns the local CA, CurrentUser trust status, and session leaf cert + CRL server.
type Manager struct {
	mu      sync.Mutex
	certDir string
	caCert  *x509.Certificate
	caKey   *rsa.PrivateKey
	caDER   []byte
	thumb   string

	crlServer *http.Server
	crlLn     net.Listener
	crlBytes  []byte
	crlURL    string
	leafCert  *tls.Certificate
}

func NewManager(profileDir string) (*Manager, error) {
	dir := filepath.Join(profileDir, "certs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	m := &Manager{certDir: dir}
	if err := m.loadOrCreateCA(); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Manager) loadOrCreateCA() error {
	certPath := filepath.Join(m.certDir, "ca-cert.pem")
	keyPath := filepath.Join(m.certDir, "ca-key.pem")
	certPEM, err1 := os.ReadFile(certPath)
	keyPEM, err2 := os.ReadFile(keyPath)
	if err1 == nil && err2 == nil {
		cBlock, _ := pem.Decode(certPEM)
		kBlock, _ := pem.Decode(keyPEM)
		if cBlock != nil && kBlock != nil {
			cert, errC := x509.ParseCertificate(cBlock.Bytes)
			key, errK := x509.ParsePKCS1PrivateKey(kBlock.Bytes)
			if errC == nil && errK == nil && time.Now().Add(24*time.Hour).Before(cert.NotAfter) {
				m.caCert = cert
				m.caKey = key
				m.caDER = cert.Raw
				sum := sha1.Sum(cert.Raw)
				m.thumb = hex.EncodeToString(sum[:])
				return nil
			}
		}
	}
	return m.GenerateCA()
}

// GenerateCA creates a fresh local CA certificate and private key.
func (m *Manager) GenerateCA() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return err
	}
	now := time.Now().Add(-1 * time.Hour)
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "HD2 Strain Lab Local CA",
			Organization: []string{"HD2 Strain Lab"},
		},
		NotBefore:             now,
		NotAfter:              now.AddDate(5, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
		SubjectKeyId:          randomSKI(),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	if err := os.WriteFile(filepath.Join(m.certDir, "ca-cert.pem"), certPEM, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(m.certDir, "ca-key.pem"), keyPEM, 0o600); err != nil {
		return err
	}
	m.caCert = cert
	m.caKey = key
	m.caDER = der
	sum := sha1.Sum(der)
	m.thumb = hex.EncodeToString(sum[:])
	m.leafCert = nil
	return nil
}

func randomSKI() []byte {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	return b
}

func (m *Manager) Thumbprint() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.thumb
}

func (m *Manager) CACertPath() string {
	return filepath.Join(m.certDir, "ca-cert.pem")
}

// IsTrusted checks if the CA certificate is installed in CurrentUser\ROOT.
func (m *Manager) IsTrusted() bool {
	m.mu.Lock()
	thumb := m.thumb
	m.mu.Unlock()
	if len(thumb) != 40 {
		return false
	}
	store, err := openCurrentUserRoot(true)
	if err != nil {
		return false
	}
	defer closeStore(store)
	ctx, err := findCertByThumbprint(store, thumb)
	if err != nil || ctx == 0 {
		return false
	}
	procCertFreeContext.Call(ctx)
	return true
}

// InstallTrust adds the CA certificate to Windows CurrentUser\ROOT store (no elevation needed).
func (m *Manager) InstallTrust() error {
	m.mu.Lock()
	der := append([]byte(nil), m.caDER...)
	thumb := m.thumb
	m.mu.Unlock()
	if len(der) == 0 {
		return errors.New("未生成本机 CA 证书")
	}
	store, err := openCurrentUserRoot(false)
	if err != nil {
		return err
	}
	defer closeStore(store)

	r1, _, callErr := procCertAddEncoded.Call(
		store,
		x509ASNEncoding,
		uintptr(unsafe.Pointer(&der[0])),
		uintptr(len(der)),
		certStoreAddReplace,
		0,
	)
	if r1 == 0 {
		return fmt.Errorf("Windows 证书信任写入失败: %v", callErr)
	}
	ctx, _ := findCertByThumbprint(store, thumb)
	if ctx == 0 {
		return errors.New("证书写入后验证失败，可能受到系统策略限制")
	}
	procCertFreeContext.Call(ctx)
	return nil
}

// RemoveTrust deletes our CA certificate from CurrentUser\ROOT.
func (m *Manager) RemoveTrust() error {
	m.mu.Lock()
	thumb := m.thumb
	m.mu.Unlock()
	if len(thumb) != 40 {
		return nil
	}
	store, err := openCurrentUserRoot(false)
	if err != nil {
		return err
	}
	defer closeStore(store)
	ctx, err := findCertByThumbprint(store, thumb)
	if err != nil || ctx == 0 {
		return nil
	}
	r1, _, callErr := procCertDeleteFromStore.Call(ctx)
	if r1 == 0 {
		return fmt.Errorf("移除证书失败: %v", callErr)
	}
	return nil
}

// StartSessionLeaf starts a loopback CRL HTTP responder and issues a short-lived
// leaf certificate for TargetHost signed by the local CA.
func (m *Manager) StartSessionLeaf() (*tls.Certificate, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.caCert == nil || m.caKey == nil {
		return nil, "", errors.New("CA 未初始化")
	}

	if m.crlServer == nil {
		crlNum := big.NewInt(1)
		now := time.Now().Add(-10 * time.Minute)
		crlDER, err := x509.CreateRevocationList(rand.Reader, &x509.RevocationList{
			Number:     crlNum,
			ThisUpdate: now,
			NextUpdate: now.Add(7 * 24 * time.Hour),
		}, m.caCert, m.caKey)
		if err != nil {
			return nil, "", fmt.Errorf("创建 CRL 失败: %w", err)
		}
		m.crlBytes = crlDER

		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, "", fmt.Errorf("启动 CRL 服务失败: %w", err)
		}
		m.crlLn = ln
		m.crlURL = fmt.Sprintf("http://127.0.0.1:%d/ca.crl", ln.Addr().(*net.TCPAddr).Port)

		mux := http.NewServeMux()
		mux.HandleFunc("/ca.crl", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/pkix-crl")
			_, _ = w.Write(m.crlBytes)
		})
		m.crlServer = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		go func(srv *http.Server, l net.Listener) {
			_ = srv.Serve(l)
		}(m.crlServer, ln)
	}

	leafKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return nil, "", err
	}
	now := time.Now().Add(-15 * time.Minute)
	leafTmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: TargetHost,
		},
		DNSNames:              []string{TargetHost},
		NotBefore:             now,
		NotAfter:              now.Add(48 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		CRLDistributionPoints: []string{m.crlURL},
		SubjectKeyId:          randomSKI(),
		AuthorityKeyId:        m.caCert.SubjectKeyId,
	}
	der, err := x509.CreateCertificate(rand.Reader, leafTmpl, m.caCert, &leafKey.PublicKey, m.caKey)
	if err != nil {
		return nil, "", err
	}
	tlsCert := &tls.Certificate{
		Certificate: [][]byte{der, m.caDER},
		PrivateKey:  leafKey,
	}
	m.leafCert = tlsCert
	return tlsCert, m.crlURL, nil
}

// StopSessionLeaf stops the loopback CRL HTTP server.
func (m *Manager) StopSessionLeaf() {
	m.mu.Lock()
	srv := m.crlServer
	m.crlServer = nil
	m.crlLn = nil
	m.leafCert = nil
	m.mu.Unlock()
	if srv != nil {
		_ = srv.Close()
	}
}

func openCurrentUserRoot(readOnly bool) (uintptr, error) {
	rootPtr, err := syscall.UTF16PtrFromString("ROOT")
	if err != nil {
		return 0, err
	}
	flags := uint32(certSystemStoreCurrentUser | certStoreOpenExistingFlag)
	if readOnly {
		flags |= certStoreReadonlyFlag
	}
	store, _, callErr := procCertOpenStore.Call(
		certStoreProvSystemW,
		0,
		0,
		uintptr(flags),
		uintptr(unsafe.Pointer(rootPtr)),
	)
	if store == 0 {
		return 0, fmt.Errorf("无法打开当前用户根证书库: %v", callErr)
	}
	return store, nil
}

func closeStore(store uintptr) {
	if store != 0 {
		procCertCloseStore.Call(store, 0)
	}
}

func findCertByThumbprint(store uintptr, thumb string) (uintptr, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(thumb))
	if err != nil || len(raw) != 20 {
		return 0, errors.New("无效证书指纹")
	}
	blob := cryptHashBlob{
		cbData: uint32(len(raw)),
		pbData: &raw[0],
	}
	ctx, _, _ := procCertFindCertificate.Call(
		store,
		x509ASNEncoding|pkcs7ASNEncoding,
		0,
		certFindSHA1Hash,
		uintptr(unsafe.Pointer(&blob)),
		0,
	)
	return ctx, nil
}
