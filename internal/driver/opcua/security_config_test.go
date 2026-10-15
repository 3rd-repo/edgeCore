package opcua

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gopcua/opcua"
	"github.com/gopcua/opcua/ua"
)

// TestBuildClientOptions_DefaultUnchanged 验证未配置安全项时行为与历史版本一致：
// 仅 RequestTimeout 一个 option，连接方式由 gopcua 自动协商。
func TestBuildClientOptions_DefaultUnchanged(t *testing.T) {
	d := NewOpcUaDriver().(*OpcUaDriver)
	opts, err := d.buildClientOptions(map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opts) != 1 {
		t.Fatalf("expected 1 default option (RequestTimeout), got %d", len(opts))
	}
}

// TestApplySecurityOptions_Empty 验证未配置 security_policy/security_mode 时不追加选项。
func TestApplySecurityOptions_Empty(t *testing.T) {
	d := NewOpcUaDriver().(*OpcUaDriver)
	opts, err := d.applySecurityOptions(context.Background(), "opc.tcp://127.0.0.1:1", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opts) != 0 {
		t.Fatalf("expected 0 options, got %d", len(opts))
	}
}

// TestApplySecurityOptions_InvalidMode 验证非法 security_mode 被拒绝（不发起网络请求）。
func TestApplySecurityOptions_InvalidMode(t *testing.T) {
	d := NewOpcUaDriver().(*OpcUaDriver)
	if _, err := d.applySecurityOptions(context.Background(), "opc.tcp://127.0.0.1:1",
		map[string]any{"security_mode": "Bogus"}); err == nil {
		t.Fatal("expected error for invalid security_mode")
	}
}

// TestAuthTokenType 验证 auth_method 到 UserTokenType 的映射。
func TestAuthTokenType(t *testing.T) {
	cases := map[string]ua.UserTokenType{
		"":            ua.UserTokenTypeAnonymous,
		"Anonymous":   ua.UserTokenTypeAnonymous,
		"UserName":    ua.UserTokenTypeUserName,
		"Certificate": ua.UserTokenTypeCertificate,
	}
	for method, want := range cases {
		if got := authTokenType(map[string]any{"auth_method": method}); got != want {
			t.Fatalf("auth_method %q: got %v, want %v", method, got, want)
		}
	}
}

// TestBuildClientOptions_UserName 验证用户名认证配置可构建。
func TestBuildClientOptions_UserName(t *testing.T) {
	d := NewOpcUaDriver().(*OpcUaDriver)
	opts, err := d.buildClientOptions(map[string]any{
		"auth_method": "UserName",
		"username":    "svc",
		"password":    "secret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(opts) != 2 {
		t.Fatalf("expected 2 options (RequestTimeout + AuthUsername), got %d", len(opts))
	}
}

// TestBuildClientOptions_Certificate 验证证书认证：PEM 与 DER 两种格式均可，
// 且 PEM 中不含 CERTIFICATE 块或文件缺失时返回明确错误。
func TestBuildClientOptions_Certificate(t *testing.T) {
	d := NewOpcUaDriver().(*OpcUaDriver)
	dir := t.TempDir()

	certPathPEM := filepath.Join(dir, "client.pem")
	keyPath := filepath.Join(dir, "client.key")
	writeTestCertAndKey(t, certPathPEM, keyPath)

	// PEM 格式
	opts, err := d.buildClientOptions(map[string]any{
		"auth_method":      "Certificate",
		"certificate_file": certPathPEM,
		"private_key_file": keyPath,
	})
	if err != nil {
		t.Fatalf("PEM cert: unexpected error: %v", err)
	}
	// RequestTimeout + Certificate + PrivateKey + AuthCertificate + AuthPrivateKey
	if len(opts) != 5 {
		t.Fatalf("PEM cert: expected 5 options, got %d", len(opts))
	}

	// DER 格式
	pemBytes, err := os.ReadFile(certPathPEM)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		t.Fatal("failed to decode test PEM")
	}
	certPathDER := filepath.Join(dir, "client.der")
	if err := os.WriteFile(certPathDER, block.Bytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := d.buildClientOptions(map[string]any{
		"auth_method":      "Certificate",
		"certificate_file": certPathDER,
		"private_key_file": keyPath,
	}); err != nil {
		t.Fatalf("DER cert: unexpected error: %v", err)
	}

	// .key 扩展名的 PEM 私钥（回归：gopcua PrivateKeyFile 仅识别 .pem 扩展名）
	keyPEMBytes, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	keyPathNoPem := filepath.Join(dir, "client_secret.key")
	if err := os.WriteFile(keyPathNoPem, keyPEMBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := d.buildClientOptions(map[string]any{
		"auth_method":      "Certificate",
		"certificate_file": certPathPEM,
		"private_key_file": keyPathNoPem,
	}); err != nil {
		t.Fatalf("PEM key with non-.pem extension: unexpected error: %v", err)
	}

	// 缺文件
	if _, err := d.buildClientOptions(map[string]any{
		"auth_method":      "Certificate",
		"certificate_file": filepath.Join(dir, "missing.pem"),
		"private_key_file": keyPath,
	}); err == nil {
		t.Fatal("expected error for missing certificate file")
	}

	// 缺 key 路径
	if _, err := d.buildClientOptions(map[string]any{
		"auth_method":      "Certificate",
		"certificate_file": certPathPEM,
	}); err == nil {
		t.Fatal("expected error for missing private_key_file")
	}
}

// TestParseCertificateBytes 单独验证证书解析辅助函数。
func TestParseCertificateBytes(t *testing.T) {
	dir := t.TempDir()
	certPath, _ := filepath.Abs(filepath.Join(dir, "c.pem"))
	keyPath := filepath.Join(dir, "c.key")
	writeTestCertAndKey(t, certPath, keyPath)

	pemBytes, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	der, err := parseCertificateBytes(pemBytes)
	if err != nil {
		t.Fatalf("PEM: %v", err)
	}
	if _, err := x509.ParseCertificate(der); err != nil {
		t.Fatalf("parsed bytes are not a certificate: %v", err)
	}

	if _, err := parseCertificateBytes([]byte("not a certificate")); err == nil {
		t.Fatal("expected error for garbage input")
	}
}

// writeTestCertAndKey 生成自签证书（PEM 证书 + PKCS1 PEM 私钥）写入指定路径。
func writeTestCertAndKey(t *testing.T, certPath, keyPath string) {
	t.Helper()

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "edgecore-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)}), 0o600); err != nil {
		t.Fatal(err)
	}
}

// 编译期守卫：确认 gopcua 相关 API 仍存在（防止升级后静默失效）。
var (
	_ = opcua.GetEndpoints
	_ = opcua.SelectEndpoint
	_ = opcua.SecurityFromEndpoint
	_ = opcua.Certificate
	_ = opcua.PrivateKey
)
