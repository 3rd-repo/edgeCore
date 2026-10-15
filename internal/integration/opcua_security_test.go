package integration_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/awcullen/opcua/server"
	awua "github.com/awcullen/opcua/ua"

	driveropcua "github.com/anviod/edgeCore/internal/driver/opcua"
	"github.com/anviod/edgeCore/internal/model"
)

// 本文件提供「真实 OPC UA server 场景」的集成测试：
// 用依赖中已有的 awcullen/opcua server 起一个本机 opc.tcp 服务端，
// 再驱动真实的 OpcUaDriver（gopcua 客户端栈）完成连接与读取。
//
// 覆盖两条安全链路：
//  1. SecurityPolicy=None + 匿名认证（网关接入 PLC 最常见的明文链路）；
//  2. Basic256Sha256 / SignAndEncrypt + 客户端证书认证（证书/安全策略验证）。
//
// 资源开销小（单进程、随机端口），CI nightly 可稳定重复执行。

const testTagValue int32 = 42

// TestOpcuaSecurity_AnonymousNone 验证默认链路：真实 TCP 连接 + 匿名会话 + Read 取值。
func TestOpcuaSecurity_AnonymousNone(t *testing.T) {
	if testing.Short() {
		t.Skip("OPC UA real-server integration test skipped in short mode")
	}

	fx := startOpcuaTestServer(t, opcuaServerOptions{})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	d := newOpcuaTestDriver(t, fx.endpointURL, nil)
	points := []model.Point{opcuaTestPoint("dev1", fx.tagAddress)}

	var got model.Value
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		var vals map[string]model.Value
		vals, err = d.ReadPoints(ctx, points)
		t.Logf("anon attempt %d: err=%v vals=%+v", attempt, err, vals)
		if err == nil {
			if v, ok := vals["p1"]; ok && v.Quality == "Good" {
				got = v
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("ReadPoints failed against real server: %v", err)
	}
	t.Logf("raw read result: %+v", got)
	if got.Value != testTagValue {
		t.Fatalf("value = %v (%T), want %d", got.Value, got.Value, testTagValue)
	}
	if got.Quality != "Good" {
		t.Fatalf("quality = %q, want Good", got.Quality)
	}
}

// TestOpcuaSecurity_CertificateSignAndEncrypt 验证证书链路：
// 客户端以 Basic256Sha256 / SignAndEncrypt + 应用证书接入，
// 服务端在受信 PKI 目录校验客户端证书后放行，读取同样成功。
func TestOpcuaSecurity_CertificateSignAndEncrypt(t *testing.T) {
	if testing.Short() {
		t.Skip("OPC UA real-server integration test skipped in short mode")
	}

	fx := startOpcuaTestServer(t, opcuaServerOptions{clientCertPEM: true})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	secCfg := map[string]any{
		"security_policy":  "Basic256Sha256",
		"security_mode":    "SignAndEncrypt",
		"auth_method":      "Certificate",
		"certificate_file": fx.clientCertPath,
		"private_key_file": fx.clientKeyPath,
	}
	d := newOpcuaTestDriver(t, fx.endpointURL, secCfg)
	points := []model.Point{opcuaTestPoint("dev1", fx.tagAddress)}

	var got model.Value
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		var vals map[string]model.Value
		vals, err = d.ReadPoints(ctx, points)
		if err == nil {
			if v, ok := vals["p1"]; ok && v.Quality == "Good" {
				got = v
				break
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("secure ReadPoints failed against real server: %v", err)
	}
	if got.Value != testTagValue {
		t.Fatalf("value = %v (%T), want %d", got.Value, got.Value, testTagValue)
	}
}

// TestOpcuaSecurity_UntrustedClientRejected 验证安全策略的否定路径：
// 未被服务端信任的客户端证书应被拒绝（连接失败），防止误配后静默降级为明文。
func TestOpcuaSecurity_UntrustedClientRejected(t *testing.T) {
	if testing.Short() {
		t.Skip("OPC UA real-server integration test skipped in short mode")
	}

	// clientCertPEM=false：server 不信任任何客户端证书
	fx := startOpcuaTestServer(t, opcuaServerOptions{})

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	secCfg := map[string]any{
		"security_policy":  "Basic256Sha256",
		"security_mode":    "SignAndEncrypt",
		"auth_method":      "Certificate",
		"certificate_file": fx.clientCertPath,
		"private_key_file": fx.clientKeyPath,
	}
	d := newOpcuaTestDriver(t, fx.endpointURL, secCfg)
	points := []model.Point{opcuaTestPoint("dev1", fx.tagAddress)}

	_, err := d.ReadPoints(ctx, points)
	if err == nil {
		t.Fatal("expected ReadPoints to fail with untrusted client certificate, got success")
	}
}

// ---------- 测试夹具 ----------

type opcuaServerOptions struct {
	// clientCertPEM 为 true 时把客户端证书写入服务端受信目录，允许证书认证。
	clientCertPEM bool
}

type opcuaFixture struct {
	endpointURL    string
	tagAddress     string
	pkiBase        string
	serverCertPath string
	clientCertPath string
	clientKeyPath  string
}

func opcuaTestPoint(deviceID, address string) model.Point {
	return model.Point{
		ID:        "p1",
		Name:      "demo tag",
		DeviceID:  deviceID,
		Address:   address,
		DataType:  "int32",
		ReadWrite: "R",
	}
}

func newOpcuaTestDriver(t *testing.T, endpointURL string, security map[string]any) *driveropcua.OpcUaDriver {
	t.Helper()

	cfg := map[string]any{"endpoint": endpointURL}
	for k, v := range security {
		cfg[k] = v
	}

	d, ok := driveropcua.NewOpcUaDriver().(*driveropcua.OpcUaDriver)
	if !ok {
		t.Fatal("unexpected driver type")
	}
	if err := d.Init(model.DriverConfig{ChannelID: "ch-itest", Protocol: "opc-ua", Config: map[string]any{}}); err != nil {
		t.Fatalf("driver init: %v", err)
	}
	if err := d.Connect(context.Background()); err != nil {
		t.Fatalf("driver connect: %v", err)
	}
	if err := d.SetDeviceConfig(cfg); err != nil {
		t.Fatalf("device config: %v", err)
	}
	t.Cleanup(func() { _ = d.Disconnect() })
	return d
}

// startOpcuaTestServer 起一个本机 awcullen OPC UA server：
//   - 自签服务器证书（SAN 含 127.0.0.1/localhost）；
//   - SecurityPolicy=None 与 Basic256Sha256 端点并存；
//   - ns=2;s=Demo.Tag 静态 int32 变量（值为 testTagValue）；
//   - 可选将客户端证书放入受信 PKI 目录。
func startOpcuaTestServer(t *testing.T, opts opcuaServerOptions) opcuaFixture {
	t.Helper()

	// 不用 t.TempDir：Windows 上 awcullen server 关闭后可能仍短暂持有证书文件
	// 句柄，导致清理阶段 os.RemoveAll 阻塞直至测试超时。这里用带超时的
	// 尽力清理，残留文件留给 OS 临时目录。
	baseDir, err := os.MkdirTemp("", "edgecore-opcua-itest-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		done := make(chan struct{})
		go func() {
			_ = os.RemoveAll(baseDir)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Logf("opcua test PKI dir %s cleanup timed out (file still locked); leaving residual files", baseDir)
		}
	})

	pkiDir := filepath.Join(baseDir, "pki")
	trustedDir := filepath.Join(pkiDir, "trusted")
	crlDir := filepath.Join(pkiDir, "crl")
	rejectedDir := filepath.Join(pkiDir, "rejected")
	for _, dir := range []string{trustedDir, crlDir, rejectedDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	serverCertPath := filepath.Join(pkiDir, "server.crt")
	serverKeyPath := filepath.Join(pkiDir, "server.key")
	writeOpcuaTestCert(t, serverCertPath, serverKeyPath, "edgecore-opcua-server", "urn:edgecore:opcua:itest", true)

	// 客户端证书（无论是否受信都生成，供拒绝路径使用）
	clientCertPath := filepath.Join(pkiDir, "client.crt")
	clientKeyPath := filepath.Join(pkiDir, "client.key")
	writeOpcuaTestCert(t, clientCertPath, clientKeyPath, "edgecore-opcua-client", "urn:edgecore:opcua:client", false)
	if opts.clientCertPEM {
		// 以 PEM 写入受信目录（与 northbound MaterializeTrustedCerts 同格式）
		certPEM, err := os.ReadFile(clientCertPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(trustedDir, "trusted_client.pem"), certPEM, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	port := freePort(t)
	endpointURL := fmt.Sprintf("opc.tcp://127.0.0.1:%d", port)

	srv, err := server.New(
		awua.ApplicationDescription{
			ApplicationURI:  "urn:edgecore:opcua:itest",
			ProductURI:      "http://github.com/awcullen/opcua",
			ApplicationName: awua.LocalizedText{Text: "edgecore-itest", Locale: "en"},
			ApplicationType: awua.ApplicationTypeServer,
			DiscoveryURLs:   []string{endpointURL},
		},
		serverCertPath,
		serverKeyPath,
		endpointURL,
		server.WithSecurityPolicyNone(true),
		server.WithAuthenticateAnonymousIdentityFunc(func(awua.AnonymousIdentity, string, string) error {
			return nil
		}),
		server.WithAuthenticateX509IdentityFunc(func(awua.X509Identity, string, string) error {
			return nil
		}),
		server.WithTrustedCertificatesPaths(trustedDir, crlDir),
		server.WithRejectedCertificatesPath(rejectedDir),
		server.WithMaxSessionCount(16),
		server.WithMaxSubscriptionCount(64),
		server.WithMaxWorkerThreads(2),
		server.WithTrace(),
	)
	if err != nil {
		t.Fatalf("create opcua test server: %v", err)
	}

	// ns=2;s=Demo.Tag 静态变量
	nm := srv.NamespaceManager()
	nsIdx := nm.Add("urn:edgecore:opcua:itest")
	t.Logf("opcua test server: nsIdx=%d endpoint=%s", nsIdx, endpointURL)
	varTag := server.NewVariableNode(
		srv,
		awua.NodeIDString{NamespaceIndex: nsIdx, ID: "Demo.Tag"},
		awua.QualifiedName{NamespaceIndex: nsIdx, Name: "Demo.Tag"},
		awua.LocalizedText{Text: "Demo.Tag"},
		awua.LocalizedText{Text: "edgeCore integration test tag"},
		nil,
		nil,
		awua.NewDataValue(testTagValue, awua.Good, time.Now().UTC(), 0, time.Now().UTC(), 0),
		awua.DataTypeIDInt32,
		awua.ValueRankScalar,
		[]uint32{},
		awua.AccessLevelsCurrentRead|awua.AccessLevelsCurrentWrite,
		250.0,
		false,
		nil,
	)
	if err := nm.AddNode(varTag); err != nil {
		t.Fatalf("add test variable: %v", err)
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			t.Logf("opcua test server exited: %v", err)
		}
	}()

	// 等待监听端口就绪（最多 5s）
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 500*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("opcua test server not listening on %s: %v", endpointURL, err)
		}
		time.Sleep(100 * time.Millisecond)
	}

	fx := opcuaFixture{
		endpointURL:    endpointURL,
		tagAddress:     fmt.Sprintf("ns=%d;s=Demo.Tag", nsIdx),
		pkiBase:        pkiDir,
		serverCertPath: serverCertPath,
		clientCertPath: clientCertPath,
		clientKeyPath:  clientKeyPath,
	}
	t.Cleanup(func() { _ = srv.Close() })
	return fx
}

// writeOpcuaTestCert 生成自签证书并写 PEM 文件。
// asServer 控制 ExtKeyUsage 与 SAN（服务端证书需要 IP/DNS SAN 供客户端校验）。
func writeOpcuaTestCert(t *testing.T, certPath, keyPath, cn, appURI string, asServer bool) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageDataEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	if appURI != "" {
		parsed, err := url.Parse(appURI)
		if err != nil {
			t.Fatal(err)
		}
		tmpl.URIs = []*url.URL{parsed}
	}
	if asServer {
		tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
		tmpl.DNSNames = []string{"localhost"}
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	if err := os.WriteFile(certPath, pemEncode("CERTIFICATE", der), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pemEncode("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(priv)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func pemEncode(blockType string, der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der})
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}
