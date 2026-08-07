// Package auth 定义认证相关端口（接口）。接口定义在使用方（application/service），
// 实现在 adapters/auth，避免接口与实现耦合导致循环依赖。
package auth

import (
	"context"

	"github.com/google/uuid"
)

// OIDCProvider 抽象内部 IdP 交互。实现封装 coreos/go-oidc + oauth2。
//
// 业务分支（建号/合并/绑定）留在 application service，本接口只负责与 IdP 的
// 协议交互：生成授权 URL、用 code 换 token、验签 id_token、归一化 profile。
type OIDCProvider interface {
	// AuthCodeURL 生成跳转 IdP 的授权 URL，同时发送 OIDC nonce 与 PKCE challenge。
	// state 由调用方（OIDCStateStore.Issue）生成并传入。
	AuthCodeURL(state, oidcNonce, codeChallenge string) string

	// Exchange 用 authorization code + PKCE verifier 换 token，验签 id_token，
	// 校验 id_token 的 nonce claim 与 expectedNonce 一致，返回归一化 profile。
	// 内部可选调用 UserInfo endpoint 合并 whitelist claims；UserInfo 的 sub
	// 必须与 id_token sub 一致，否则拒绝。
	Exchange(ctx context.Context, code, codeVerifier, expectedNonce string) (*OIDCProfile, error)
}

// OIDCProfile 是归一化后的 IdP 用户信息。
// RawProfile 只包含 whitelist claims（email/email_verified/preferred_username/
// name/picture），禁止包含完整 id_token/access_token/refresh_token/groups。
type OIDCProfile struct {
	Subject           string
	Email             string
	EmailVerified     bool
	PreferredUsername string
	Name              string
	Picture           string
	RawProfile        string // 经过 whitelist 的 claims JSON
}

// OIDCStateStore 管理 OIDC state 的下发与一次性校验。
//
// state 存 Redis、与浏览器 nonce cookie 双绑，Lua compare-and-delete 原子消费：
// 只有 browser nonce 匹配才删除，防 state 被错误 nonce 请求恶意消耗。
type OIDCStateStore interface {
	// Issue 生成一次性 state，把 payload 写入 store，返回 state。
	// payload 中包含 OIDC nonce、PKCE verifier 与浏览器绑定 nonce。
	// browser nonce 由调用方写入 oidc_nonce_<state> cookie。
	Issue(ctx context.Context, payload OIDCStatePayload) (state string, err error)

	// Consume 校验 state 有效、browser nonce 匹配、未过期、未重放；成功即删除。
	// 使用常量时间比较 nonce。返回 payload。
	Consume(ctx context.Context, state, browserNonce string) (*OIDCStatePayload, error)
}

// OIDCStatePayload 是 state 在服务端存储的载荷。
type OIDCStatePayload struct {
	Next                string    // 登录后跳转路径，sanitizeNextPath 校验过
	BrowserNonce        string    // 与浏览器 nonce cookie 比对
	OIDCNonce           string    // 写入授权请求 nonce，回调时校验 id_token
	PKCEVerifier        string    // PKCE code_verifier，回调时用于换 token
	InvitationTokenHash string    // 可选：邀请 token 的 sha256 hex，避免明文 token 进 Redis
	BindActorID         uuid.UUID // 可选：已登录绑定发起人
	BindSessionID       uuid.UUID // 绑定发起时的 session，回调时必须仍有效且属于 actor
}
