package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// fakeSourceConnectionProber 记录探测入参并返回预设结果。
type fakeSourceConnectionProber struct {
	gotProvider    value.SourceProvider
	gotSecret      string
	gotEndpointURL string
	err            error
}

func (f *fakeSourceConnectionProber) Probe(_ context.Context, provider value.SourceProvider, secret, endpointURL string) error {
	f.gotProvider = provider
	f.gotSecret = secret
	f.gotEndpointURL = endpointURL
	return f.err
}

func newProbeFixture(t *testing.T, provider, appID, appSecret string, config map[string]any) (*SourceConnectionService, *fakeSourceConnectionProber, *model.SourceConnection) {
	t.Helper()
	repo := newFakeSourceConnectionRepo()
	cipher := newFakeCipher()
	svc := NewSourceConnectionService(SourceConnectionServiceDeps{
		Repository: repo,
		Cipher:     cipher,
		Selector:   NewSourceConnectionSelector(repo, cipher),
		Prober:     &fakeSourceConnectionProber{},
	})
	created, err := svc.Create(context.Background(), CreateSourceConnectionInput{
		WorkspaceID: uuid.New(), Provider: provider, Name: "连接",
		AppID: appID, AppSecret: appSecret, Config: config,
	})
	if err != nil {
		t.Fatal(err)
	}
	prober := svc.prober.(*fakeSourceConnectionProber)
	return svc, prober, repo.data[created.ID]
}

func TestSourceConnectionTestSuccessCarriesDuration(t *testing.T) {
	svc, prober, conn := newProbeFixture(t, "firecrawl", "", "fc-key", map[string]any{"endpoint_url": "https://fc.internal"})
	result, err := svc.TestConnection(context.Background(), conn.WorkspaceID, conn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("ok = false, message = %q", result.Message)
	}
	if result.Provider != "firecrawl" {
		t.Fatalf("provider = %q", result.Provider)
	}
	if result.DurationMS < 0 {
		t.Fatalf("duration_ms = %d, want >= 0", result.DurationMS)
	}
	if !strings.Contains(result.Message, "耗时") {
		t.Fatalf("message = %q, want 含耗时", result.Message)
	}
	if prober.gotSecret != "fc-key" {
		t.Fatalf("probe secret = %q", prober.gotSecret)
	}
	if prober.gotEndpointURL != "https://fc.internal" {
		t.Fatalf("probe endpoint = %q", prober.gotEndpointURL)
	}
}

func TestSourceConnectionTestFeishuUnsupported(t *testing.T) {
	svc, prober, conn := newProbeFixture(t, "feishu", "cli_a1", "secret", nil)
	result, err := svc.TestConnection(context.Background(), conn.WorkspaceID, conn.ID)
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	if result != nil {
		t.Fatalf("result = %+v, want nil", result)
	}
	if prober.gotProvider != "" {
		t.Fatal("prober must not be called for feishu")
	}
}

// TestSourceConnectionTestBrowserPassesOptionalToken 覆盖 browser 无 token 连接：
// 空密文解密为空凭证后仍可探测（endpoint_url 必传）。
func TestSourceConnectionTestBrowserPassesOptionalToken(t *testing.T) {
	svc, prober, conn := newProbeFixture(t, "browser", "", "", map[string]any{"endpoint_url": "ws://cdp:9222"})
	result, err := svc.TestConnection(context.Background(), conn.WorkspaceID, conn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !result.OK {
		t.Fatalf("ok = false, message = %q", result.Message)
	}
	if prober.gotEndpointURL != "ws://cdp:9222" {
		t.Fatalf("probe endpoint = %q", prober.gotEndpointURL)
	}
	if prober.gotSecret != "" {
		t.Fatalf("probe secret = %q, want empty", prober.gotSecret)
	}
}

func TestSourceConnectionTestProbeFailureReturnsResultNotError(t *testing.T) {
	svc, _, conn := newProbeFixture(t, "jina", "", "jina-key", nil)
	svc.prober.(*fakeSourceConnectionProber).err = errors.New("jina 返回 HTTP 401")
	result, err := svc.TestConnection(context.Background(), conn.WorkspaceID, conn.ID)
	if err != nil {
		t.Fatalf("probe failure must not surface as error, got %v", err)
	}
	if result.OK {
		t.Fatal("ok = true, want false")
	}
	if !strings.Contains(result.Message, "HTTP 401") {
		t.Fatalf("message = %q, want 含失败原因", result.Message)
	}
}

func TestSourceConnectionTestUnknownConnection(t *testing.T) {
	repo := newFakeSourceConnectionRepo()
	cipher := newFakeCipher()
	svc := NewSourceConnectionService(SourceConnectionServiceDeps{
		Repository: repo, Cipher: cipher,
		Selector: NewSourceConnectionSelector(repo, cipher),
		Prober:   &fakeSourceConnectionProber{},
	})
	if _, err := svc.TestConnection(context.Background(), uuid.New(), uuid.New()); err == nil {
		t.Fatal("expected error for unknown connection")
	}
}

func TestSourceConnectionTestRequiresDeps(t *testing.T) {
	repo := newFakeSourceConnectionRepo()
	cipher := newFakeCipher()
	if _, err := NewSourceConnectionService(SourceConnectionServiceDeps{Repository: repo, Cipher: cipher}).
		TestConnection(context.Background(), uuid.New(), uuid.New()); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("missing deps: err = %v, want ErrValidation", err)
	}
	full := NewSourceConnectionService(SourceConnectionServiceDeps{
		Repository: repo, Cipher: cipher,
		Selector: NewSourceConnectionSelector(repo, cipher),
	})
	if _, err := full.TestConnection(context.Background(), uuid.New(), uuid.New()); !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("missing prober: err = %v, want ErrValidation", err)
	}
}

// TestSourceConnectionTestStoppedConnection 复用 selector 的 active 校验：
// 已停用连接不可测试，且错误为 ErrValidation（用户可自助解决，REST 层映射 400
// 而非 500——T12 评审移交 a）。
func TestSourceConnectionTestStoppedConnection(t *testing.T) {
	svc, _, conn := newProbeFixture(t, "jina", "", "jina-key", nil)
	stopped := "disabled"
	if _, err := svc.Update(context.Background(), UpdateSourceConnectionInput{
		WorkspaceID: conn.WorkspaceID, ID: conn.ID, Status: &stopped,
	}); err != nil {
		t.Fatal(err)
	}
	_, err := svc.TestConnection(context.Background(), conn.WorkspaceID, conn.ID)
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("err = %v, want ErrValidation", err)
	}
	// 停用是可自助解决的状态，错误语义不得是「来源连接已停用」之外的用户可见文案。
	if !strings.Contains(err.Error(), "已停用") {
		t.Fatalf("err = %v, want 含「已停用」", err)
	}
}
