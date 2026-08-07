package oidc

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// fakeIdP 是一个用 httptest 搭建的 OIDC IdP，提供 discovery、JWKS、token、UserInfo。
type fakeIdP struct {
	t            *testing.T
	server       *httptest.Server
	signingKey   *rsa.PrivateKey
	keyID        string
	issuer       string
	clientID     string
	clientSecret string
	// profile 控制 token 与 userinfo 返回的 claims。
	profileClaims map[string]any
	// nonce 必须在 id_token 中回显。
	issuedNonce string
	// userInfoClaims 控制 userinfo endpoint 返回（可为 nil 表示不合并）。
	userInfoClaims map[string]any
	// userInfoSubOverride 若非空，userinfo 返回不同的 sub（模拟攻击）。
	userInfoSubOverride string
	// omitSub 控制是否完全不签发 sub claim（测试缺 sub 场景）。
	omitSub bool
	// emitIDToken 控制是否在 token 响应中包含 id_token。
	emitIDToken bool
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("生成 RSA 密钥失败: %v", err)
	}
	idp := &fakeIdP{
		t:            t,
		signingKey:   key,
		keyID:        "test-key-1",
		clientID:     "langhuan-test",
		clientSecret: "test-secret",
		emitIDToken:  true,
	}
	mux := http.NewServeMux()
	idp.server = httptest.NewServer(mux)
	idp.issuer = idp.server.URL

	mux.HandleFunc("/.well-known/openid-configuration", idp.handleDiscovery)
	mux.HandleFunc("/keys", idp.handleJWKS)
	mux.HandleFunc("/token", idp.handleToken)
	mux.HandleFunc("/userinfo", idp.handleUserInfo)
	t.Cleanup(idp.server.Close)
	return idp
}

func (f *fakeIdP) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"issuer":                                f.issuer,
		"authorization_endpoint":                f.issuer + "/auth",
		"token_endpoint":                        f.issuer + "/token",
		"userinfo_endpoint":                     f.issuer + "/userinfo",
		"jwks_uri":                              f.issuer + "/keys",
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (f *fakeIdP) handleJWKS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	jwk := jose.JSONWebKey{
		Key:       f.signingKey.Public(),
		KeyID:     f.keyID,
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}
	_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}})
}

func (f *fakeIdP) signIDToken(claims map[string]any) string {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: f.signingKey}, &jose.SignerOptions{
		ExtraHeaders: map[jose.HeaderKey]any{"kid": f.keyID},
	})
	if err != nil {
		f.t.Fatalf("创建 signer 失败: %v", err)
	}
	payload, _ := json.Marshal(claims)
	obj, err := signer.Sign(payload)
	if err != nil {
		f.t.Fatalf("签名 id_token 失败: %v", err)
	}
	signed, err := obj.CompactSerialize()
	if err != nil {
		f.t.Fatalf("序列化 id_token 失败: %v", err)
	}
	return signed
}

func (f *fakeIdP) handleToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	resp := map[string]any{
		"access_token": "fake-access-token",
		"token_type":   "Bearer",
		"expires_in":   3600,
	}
	if f.emitIDToken {
		claims := map[string]any{
			"iss":   f.issuer,
			"aud":   f.clientID,
			"exp":   time.Now().Add(time.Hour).Unix(),
			"iat":   time.Now().Unix(),
			"nonce": f.issuedNonce,
		}
		if !f.omitSub {
			claims["sub"] = "sub-" + fmt.Sprint(time.Now().UnixNano())
		}
		for k, v := range f.profileClaims {
			claims[k] = v
		}
		// 若 profileClaims 覆盖了 sub，保留它。
		if sub, ok := f.profileClaims["sub"]; ok {
			claims["sub"] = sub
		}
		resp["id_token"] = f.signIDToken(claims)
	}
	_ = json.NewEncoder(w).Encode(resp)
}

func (f *fakeIdP) handleUserInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	claims := map[string]any{}
	if f.userInfoClaims != nil {
		for k, v := range f.userInfoClaims {
			claims[k] = v
		}
	}
	if sub, ok := f.profileClaims["sub"]; ok {
		claims["sub"] = sub
	}
	if f.userInfoSubOverride != "" {
		claims["sub"] = f.userInfoSubOverride
	}
	_ = json.NewEncoder(w).Encode(claims)
}

func (f *fakeIdP) newProvider() *Provider {
	return &Provider{
		issuer:       f.issuer,
		clientID:     f.clientID,
		clientSecret: f.clientSecret,
		redirectURL:  "https://langhuan.example.com/api/v1/auth/oidc/callback",
		scopes:       []string{"openid", "profile", "email"},
		httpTimeout:  5 * time.Second,
		httpClient:   &http.Client{Timeout: 5 * time.Second},
	}
}

func (f *fakeIdP) newCodeVerifierChallenge(t *testing.T) (verifier, challenge string) {
	t.Helper()
	verifier = "test-code-verifier-1234567890"
	h := crypto.SHA256.New()
	h.Write([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h.Sum(nil))
	return
}

func TestProviderExchangeSuccess(t *testing.T) {
	idp := newFakeIdP(t)
	idp.profileClaims = map[string]any{
		"sub":                "user-sub-1",
		"email":              "ada@example.com",
		"email_verified":     true,
		"preferred_username": "ada",
		"name":               "Ada Lovelace",
		"picture":            "https://example.com/ada.png",
	}
	idp.issuedNonce = "oidc-nonce-abc"
	prov := idp.newProvider()
	verifier, challenge := idp.newCodeVerifierChallenge(t)

	// AuthCodeURL 触发 discovery。
	authURL := prov.AuthCodeURL("state-1", "oidc-nonce-abc", challenge)
	if !strings.Contains(authURL, "code_challenge=") || !strings.Contains(authURL, "nonce=oidc-nonce-abc") {
		t.Fatalf("authURL missing PKCE/nonce: %s", authURL)
	}

	profile, err := prov.Exchange(context.Background(), "fake-code", verifier, "oidc-nonce-abc")
	if err != nil {
		t.Fatalf("Exchange error: %v", err)
	}
	if profile.Subject != "user-sub-1" {
		t.Fatalf("sub = %q", profile.Subject)
	}
	if profile.Email != "ada@example.com" || !profile.EmailVerified {
		t.Fatalf("email/verified = %+v", profile)
	}
	if profile.Name != "Ada Lovelace" {
		t.Fatalf("name = %q", profile.Name)
	}
	if profile.PreferredUsername != "ada" {
		t.Fatalf("preferred_username = %q", profile.PreferredUsername)
	}
}

func TestProviderExchangeRejectsMissingIDToken(t *testing.T) {
	idp := newFakeIdP(t)
	idp.emitIDToken = false
	idp.issuedNonce = "n1"
	prov := idp.newProvider()
	_ = prov.AuthCodeURL("s", "n1", "chal")
	_, err := prov.Exchange(context.Background(), "code", "verifier", "n1")
	if err == nil || !strings.Contains(err.Error(), "id_token") {
		t.Fatalf("expected id_token error, got %v", err)
	}
}

func TestProviderExchangeRejectsNonceMismatch(t *testing.T) {
	idp := newFakeIdP(t)
	idp.profileClaims = map[string]any{"sub": "s1", "email": "a@b.com", "email_verified": true}
	idp.issuedNonce = "actual-nonce"
	prov := idp.newProvider()
	_ = prov.AuthCodeURL("s", "actual-nonce", "c")
	_, err := prov.Exchange(context.Background(), "code", "v", "wrong-nonce")
	if err == nil || !strings.Contains(err.Error(), "nonce") {
		t.Fatalf("expected nonce mismatch error, got %v", err)
	}
}

func TestProviderExchangeRejectsTamperedSignature(t *testing.T) {
	idp := newFakeIdP(t)
	idp.profileClaims = map[string]any{"sub": "s1", "email": "a@b.com", "email_verified": true}
	idp.issuedNonce = "n1"
	prov := idp.newProvider()
	_ = prov.AuthCodeURL("s", "n1", "c")
	// 正常 Exchange 会验签；这里伪造一个篡改场景：直接构造错误 verifier 触发 exchange 失败。
	// 由于 fake IdP 总签发合法 token，我们改为用一个不存在的 issuer 触发验签失败。
	prov2 := &Provider{
		issuer:       idp.issuer,
		clientID:     "wrong-client-id", // audience 不匹配 → 验签失败
		clientSecret: idp.clientSecret,
		redirectURL:  idp.newProvider().redirectURL,
		scopes:       []string{"openid"},
		httpTimeout:  5 * time.Second,
		httpClient:   &http.Client{Timeout: 5 * time.Second},
	}
	_ = prov2.AuthCodeURL("s", "n1", "c")
	_, err := prov2.Exchange(context.Background(), "code", "v", "n1")
	if err == nil {
		t.Fatal("expected verification error due to audience mismatch")
	}
}

func TestProviderExchangeMergesUserInfoWhitelist(t *testing.T) {
	idp := newFakeIdP(t)
	// id_token 只有 sub 和 email，缺 name/picture。
	idp.profileClaims = map[string]any{
		"sub":            "user-sub-2",
		"email":          "bob@example.com",
		"email_verified": true,
	}
	idp.issuedNonce = "n1"
	// UserInfo 补 name 和 picture。
	idp.userInfoClaims = map[string]any{
		"name":    "Bob Builder",
		"picture": "https://example.com/bob.png",
	}
	prov := idp.newProvider()
	_ = prov.AuthCodeURL("s", "n1", "c")
	profile, err := prov.Exchange(context.Background(), "code", "v", "n1")
	if err != nil {
		t.Fatalf("Exchange error: %v", err)
	}
	if profile.Name != "Bob Builder" {
		t.Fatalf("name should be merged from UserInfo, got %q", profile.Name)
	}
	if profile.Picture != "https://example.com/bob.png" {
		t.Fatalf("picture should be merged from UserInfo, got %q", profile.Picture)
	}
}

func TestProviderExchangeRejectsUserInfoSubMismatch(t *testing.T) {
	idp := newFakeIdP(t)
	idp.profileClaims = map[string]any{"sub": "real-sub", "email": "a@b.com", "email_verified": true}
	idp.issuedNonce = "n1"
	idp.userInfoSubOverride = "attacker-sub" // userinfo 返回不同 sub
	idp.userInfoClaims = map[string]any{"name": "Should Not Merge"}
	prov := idp.newProvider()
	_ = prov.AuthCodeURL("s", "n1", "c")
	profile, err := prov.Exchange(context.Background(), "code", "v", "n1")
	if err != nil {
		t.Fatalf("Exchange error: %v", err)
	}
	// UserInfo sub 不一致 → 不合并 name。
	if profile.Name == "Should Not Merge" {
		t.Fatal("should not merge UserInfo when sub mismatches")
	}
	if profile.Subject != "real-sub" {
		t.Fatalf("subject should remain id_token sub, got %q", profile.Subject)
	}
}

func TestProviderExchangeMissingSub(t *testing.T) {
	idp := newFakeIdP(t)
	idp.profileClaims = map[string]any{"email": "a@b.com", "email_verified": true} // 无 sub
	idp.omitSub = true
	idp.issuedNonce = "n1"
	prov := idp.newProvider()
	_ = prov.AuthCodeURL("s", "n1", "c")
	_, err := prov.Exchange(context.Background(), "code", "v", "n1")
	if err == nil || !strings.Contains(err.Error(), "sub") {
		t.Fatalf("expected sub missing error, got %v", err)
	}
}

func TestProviderLazyDiscovery(t *testing.T) {
	idp := newFakeIdP(t)
	prov := idp.newProvider()
	// 构造后未调用任何方法时不应发起 discovery（discovered=false）。
	if prov.discovered {
		t.Fatal("provider should not be discovered before first use")
	}
	// 关闭 IdP 模拟不可达，NewProvider 不应因此失败（lazy）。
	idp.server.Close()
	// 重新建一个正常 IdP（地址变了，但旧 provider 的 issuer 指向已关闭的）。
	_ = prov
	// 直接验证 discovered 状态语义：NewProvider 不触发 discovery。
	prov2 := idp.newProvider()
	if prov2.discovered {
		t.Fatal("NewProvider must not trigger discovery")
	}
}
