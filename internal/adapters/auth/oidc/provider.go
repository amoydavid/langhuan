package oidc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	authport "github.com/dajee/langhuan/internal/ports/auth"
)

// 编译期断言：Provider 实现端口 auth.OIDCProvider。
var _ authport.OIDCProvider = (*Provider)(nil)

// profileClaimsWhitelist 定义允许从 id_token/UserInfo 提取并写入 raw_profile 的 claim 名。
var profileClaimsWhitelist = map[string]struct{}{
	"email":              {},
	"email_verified":     {},
	"preferred_username": {},
	"name":               {},
	"picture":            {},
}

// Provider 实现 OIDCProvider，封装 coreos/go-oidc + oauth2。
// discovery 采用 lazy：NewProvider 不发网络请求，首次 AuthCodeURL/Exchange 才
// 拉取 .well-known/openid-configuration（IdP 不可达时不阻止琅嬛启动）。
type Provider struct {
	issuer       string
	clientID     string
	clientSecret string
	redirectURL  string
	scopes       []string
	httpTimeout  time.Duration
	httpClient   *http.Client
	// lazy 字段：首次需要时填充。
	discovered   bool
	oauthConfig  *oauth2.Config
	verifier     *gooidc.IDTokenVerifier
	discoveryErr error
	provider     *gooidc.Provider
}

// NewProvider 根据 config 构造 OIDCProvider。
// cfg.Enabled=false 时返回 (nil, nil)，调用方据此跳过装配。
//
// 入参用本地 oidcProviderConfig 而非 config.OIDCConfig，避免 adapter 反向依赖
// infrastructure/config；由 main.go/DI 层做 config.OIDCConfig → oidcProviderConfig 转换。
//
// 返回 error 的唯一情形：配置非法（字段缺失等，validateAuth 已拦，这里防御性复核）。
// 不发起 discovery（lazy）：IdP 宕机时返回 (provider, nil)，琅嬛照常启动。
func NewProvider(cfg oidcProviderConfig) (*Provider, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{gooidc.ScopeOpenID, "profile", "email"}
	}
	timeout := time.Duration(cfg.HTTPTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &Provider{
		issuer:       strings.TrimSpace(cfg.Issuer),
		clientID:     strings.TrimSpace(cfg.ClientID),
		clientSecret: cfg.ClientSecret,
		redirectURL:  strings.TrimSpace(cfg.RedirectURL),
		scopes:       scopes,
		httpTimeout:  timeout,
		httpClient:   &http.Client{Timeout: timeout},
	}, nil
}

// oidcProviderConfig 是 NewProvider 的入参，避免 adapter 反向依赖 config 包。
type oidcProviderConfig struct {
	Enabled            bool
	Issuer             string
	ClientID           string
	ClientSecret       string
	RedirectURL        string
	Scopes             []string
	HTTPTimeoutSeconds int
}

// ensureDiscovered 懒加载 IdP discovery（.well-known/openid-configuration）。
// 首次调用发起 HTTP；后续复用。discovery 失败时缓存 error，下次请求重试。
func (p *Provider) ensureDiscovered(ctx context.Context) error {
	if p.discovered {
		if p.discoveryErr != nil {
			return p.discoveryErr
		}
		return nil
	}
	p.discovered = true
	discoveryCtx, cancel := context.WithTimeout(ctx, p.httpTimeout)
	defer cancel()
	gooidcCtx := gooidc.ClientContext(discoveryCtx, p.httpClient)
	prov, err := gooidc.NewProvider(gooidcCtx, p.issuer)
	if err != nil {
		p.discoveryErr = fmt.Errorf("oidc discovery 失败: %w", err)
		return p.discoveryErr
	}
	p.provider = prov
	p.oauthConfig = &oauth2.Config{
		ClientID:     p.clientID,
		ClientSecret: p.clientSecret,
		Endpoint:     prov.Endpoint(),
		RedirectURL:  p.redirectURL,
		Scopes:       p.scopes,
	}
	p.verifier = prov.Verifier(&gooidc.Config{ClientID: p.clientID})
	return nil
}

// resetDiscovery 清除缓存的 discovery 结果，便于失败后重试。
func (p *Provider) resetDiscovery() {
	p.discovered = false
	p.provider = nil
	p.oauthConfig = nil
	p.verifier = nil
	p.discoveryErr = nil
}

// AuthCodeURL 生成跳转 IdP 的授权 URL，发送 OIDC nonce 与 PKCE S256 challenge。
func (p *Provider) AuthCodeURL(state, oidcNonce, codeChallenge string) string {
	// discovery 在 AuthCodeURL 时也尝试（让 begin 阶段就能暴露 IdP 不可达）。
	// 忽略 error：begin 失败由 handler 层映射为 oidc_provider_unavailable。
	_ = p.ensureDiscovered(context.Background())
	opts := []oauth2.AuthCodeOption{
		gooidc.Nonce(oidcNonce),
		oauth2.SetAuthURLParam("code_challenge", codeChallenge),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	}
	if p.oauthConfig != nil {
		return p.oauthConfig.AuthCodeURL(state, opts...)
	}
	// discovery 未就绪时返回 issuer 上的占位授权 URL（handler 会因 IdP 不可达失败）。
	return strings.TrimRight(p.issuer, "/") + "/protocol/openid-connect/auth?client_id=" + p.clientID
}

// Exchange 用 authorization code + PKCE verifier 换 token，验签 id_token，
// 校验 id_token 的 nonce claim，返回归一化 profile。
func (p *Provider) Exchange(ctx context.Context, code, codeVerifier, expectedNonce string) (*authport.OIDCProfile, error) {
	if err := p.ensureDiscovered(ctx); err != nil {
		p.resetDiscovery()
		return nil, err
	}
	exchangeCtx := context.WithValue(ctx, oauth2.HTTPClient, p.httpClient)
	token, err := p.oauthConfig.Exchange(exchangeCtx, code,
		oauth2.SetAuthURLParam("code_verifier", codeVerifier),
	)
	if err != nil {
		return nil, fmt.Errorf("oidc code exchange 失败: %w", err)
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || strings.TrimSpace(rawIDToken) == "" {
		return nil, fmt.Errorf("oidc 响应缺少 id_token")
	}
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("oidc id_token 验签失败: %w", err)
	}
	if idToken.Nonce != expectedNonce {
		return nil, fmt.Errorf("oidc id_token nonce 不匹配")
	}
	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("解析 oidc id_token claims 失败: %w", err)
	}
	profile, err := profileFromClaims(claims)
	if err != nil {
		return nil, err
	}
	// 可选 UserInfo 合并：sub 必须一致，只合并 whitelist 字段。
	userInfo, err := p.provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
	if err == nil && userInfo != nil && userInfo.Subject == profile.Subject {
		var uiClaims map[string]any
		if uerr := userInfo.Claims(&uiClaims); uerr == nil {
			profile = mergeUserInfo(profile, uiClaims)
		}
	}
	return profile, nil
}

// profileFromClaims 从 id_token claims 构造归一化 profile。
// rawProfile 只保留 whitelist claims。
func profileFromClaims(claims map[string]any) (*authport.OIDCProfile, error) {
	sub := claimString(claims, "sub")
	if strings.TrimSpace(sub) == "" {
		return nil, fmt.Errorf("oidc id_token 缺少 sub")
	}
	profile := &authport.OIDCProfile{
		Subject:           sub,
		Email:             claimString(claims, "email"),
		EmailVerified:     claimBool(claims, "email_verified"),
		PreferredUsername: claimString(claims, "preferred_username"),
		Name:              claimString(claims, "name"),
		Picture:           claimString(claims, "picture"),
	}
	profile.RawProfile = whitelistJSON(claims)
	return profile, nil
}

// mergeUserInfo 用 UserInfo claims 补齐 id_token 缺失的 whitelist 字段。
// 不覆盖已验证的 subject。
func mergeUserInfo(base *authport.OIDCProfile, uiClaims map[string]any) *authport.OIDCProfile {
	merged := *base
	if merged.Email == "" {
		merged.Email = claimString(uiClaims, "email")
	}
	if merged.PreferredUsername == "" {
		merged.PreferredUsername = claimString(uiClaims, "preferred_username")
	}
	if merged.Name == "" {
		merged.Name = claimString(uiClaims, "name")
	}
	if merged.Picture == "" {
		merged.Picture = claimString(uiClaims, "picture")
	}
	// email_verified 优先用 UserInfo（若 id_token 没有则取 UserInfo）。
	if uiEv := claimBool(uiClaims, "email_verified"); uiEv && !merged.EmailVerified {
		merged.EmailVerified = uiEv
	}
	return &merged
}

// whitelistJSON 把 claims 中 whitelist 字段序列化为紧凑 JSON。
func whitelistJSON(claims map[string]any) string {
	filtered := make(map[string]any, len(profileClaimsWhitelist))
	for k := range profileClaimsWhitelist {
		if v, ok := claims[k]; ok {
			filtered[k] = v
		}
	}
	data, err := json.Marshal(filtered)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func claimString(claims map[string]any, key string) string {
	if v, ok := claims[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func claimBool(claims map[string]any, key string) bool {
	if v, ok := claims[key].(bool); ok {
		return v
	}
	return false
}

// randomBase64URL 生成 n 字节随机数的 base64url 编码字符串（用于 nonce/verifier）。
func randomBase64URL(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
