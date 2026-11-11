package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

// 本文件覆盖 faq_create / faq_update / faq_get 工具行为：输入构造
// （WorkspaceID/Access 注入）、乐观锁冲突错误映射、scope/KB 绑定拒绝与输出形状。

// fakeMCPFAQService 记录 FAQ 端口调用，可注入错误与返回值。
type fakeMCPFAQService struct {
	createCalls int
	createInput service.CreateFAQDocumentInput
	updateCalls int
	updateInput service.UpdateFAQDocumentInput
	getCalls    int
	getIDs      [2]uuid.UUID
	result      *dto.FAQDocument
	updateErr   error
	createErr   error
	getErr      error
}

func (f *fakeMCPFAQService) Create(_ context.Context, input service.CreateFAQDocumentInput) (*dto.FAQDocument, error) {
	f.createCalls++
	f.createInput = input
	if f.createErr != nil {
		return nil, f.createErr
	}
	return f.result, nil
}

func (f *fakeMCPFAQService) Update(_ context.Context, input service.UpdateFAQDocumentInput) (*dto.FAQDocument, error) {
	f.updateCalls++
	f.updateInput = input
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	return f.result, nil
}

func (f *fakeMCPFAQService) Get(_ context.Context, access value.ResourceAccess, knowledgeBaseID, documentID uuid.UUID) (*dto.FAQDocument, error) {
	f.getCalls++
	f.getIDs = [2]uuid.UUID{knowledgeBaseID, documentID}
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.result, nil
}

// faqFixture 组装一个 FAQ 输出 fixture。
func faqFixture() *dto.FAQDocument {
	docID, revID := uuid.New(), uuid.New()
	return &dto.FAQDocument{
		Document:  &dto.Document{ID: docID, Title: "如何重置密码", Kind: value.DocumentKindFAQ, Status: value.DocumentStatusReady},
		Revision:  &dto.FAQRevisionSummary{ID: revID, RevisionNo: 1, Status: value.DocumentRevisionReady, CreatedAt: time.Now()},
		Questions: []string{"密码忘了怎么办", "如何重置密码"},
		Answer:    "在设置中点击重置",
		Job:       &dto.Job{ID: uuid.New()},
	}
}

func callFAQTool(t *testing.T, deps Dependencies, auth value.AuthContext, tool, args string) *mcplib.CallToolResult {
	t.Helper()
	srv := NewServer(deps)
	payload := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":%q,"arguments":%s}}`, tool, args)
	ctx := value.ContextWithAuthContext(context.Background(), auth)
	resp := srv.MCP().HandleMessage(ctx, json.RawMessage(payload))
	jsonResp, ok := resp.(mcplib.JSONRPCResponse)
	require.True(t, ok, "response type = %T", resp)
	result, ok := jsonResp.Result.(*mcplib.CallToolResult)
	require.True(t, ok, "result type = %T", jsonResp.Result)
	return result
}

func faqTestDeps(faq MCPFAQService) Dependencies {
	return Dependencies{FAQDocuments: faq, InlineLimit: 8 * 1024 * 1024}
}

func faqWriteAuth(kbID uuid.UUID) value.AuthContext {
	return value.NewAPIKeyAuthContext(uuid.New(), uuid.New(),
		[]value.APIScope{value.ScopeDocumentsWrite, value.ScopeDocumentsRead}, []uuid.UUID{kbID})
}

func TestFAQCreatePassesServiceInput(t *testing.T) {
	kbID := uuid.New()
	faq := &fakeMCPFAQService{result: faqFixture()}

	result := callFAQTool(t, faqTestDeps(faq), faqWriteAuth(kbID), "faq_create", fmt.Sprintf(`{
		"knowledge_base_id":%q,"title":"如何重置密码",
		"questions":["密码忘了怎么办","如何重置密码"],"answer":"在设置中点击重置"
	}`, kbID))
	out := structuredOf(t, result)
	require.Equal(t, faq.result.Document.ID.String(), out["document_id"])
	require.Equal(t, faq.result.Revision.ID.String(), out["revision_id"])
	require.Equal(t, float64(1), out["revision_no"])
	require.Equal(t, []any{"密码忘了怎么办", "如何重置密码"}, out["questions"])
	require.Equal(t, faq.result.Answer, out["answer"])
	require.Equal(t, faq.result.Job.ID.String(), out["job_id"])

	require.Equal(t, 1, faq.createCalls)
	in := faq.createInput
	require.Equal(t, kbID, in.KnowledgeBaseID)
	require.NotEqual(t, uuid.Nil, in.WorkspaceID)
	require.Equal(t, "如何重置密码", in.Title)
	require.Equal(t, []string{"密码忘了怎么办", "如何重置密码"}, in.Questions)
	// Access 从 API Key 主体派生（绑定集合非 Unrestricted）。
	require.False(t, in.Access.Unrestricted)
	require.Equal(t, []uuid.UUID{kbID}, in.Access.AllowedKnowledgeBaseIDs)
}

func TestFAQUpdateConflictMapsToRevisionConflict(t *testing.T) {
	kbID := uuid.New()
	faq := &fakeMCPFAQService{updateErr: domainerrors.ErrRevisionConflict}

	result := callFAQTool(t, faqTestDeps(faq), faqWriteAuth(kbID), "faq_update", fmt.Sprintf(`{
		"knowledge_base_id":%q,"document_id":%q,"base_revision_id":%q,
		"questions":["q"],"answer":"a"
	}`, kbID, uuid.New(), uuid.New()))
	parsed := errorStructuredOf(t, result)
	require.Equal(t, "revision_conflict", parsed.Error.Code)
	require.True(t, parsed.Error.Retryable)

	require.Equal(t, 1, faq.updateCalls)
	require.Equal(t, "q", faq.updateInput.Questions[0])
}

func TestFAQGetPassesIDs(t *testing.T) {
	kbID, docID := uuid.New(), uuid.New()
	faq := &fakeMCPFAQService{result: faqFixture()}

	result := callFAQTool(t, faqTestDeps(faq), faqWriteAuth(kbID), "faq_get",
		fmt.Sprintf(`{"knowledge_base_id":%q,"document_id":%q}`, kbID, docID))
	out := structuredOf(t, result)
	require.Equal(t, []any{"密码忘了怎么办", "如何重置密码"}, out["questions"])
	require.Equal(t, faq.result.Answer, out["answer"])
	require.Equal(t, [2]uuid.UUID{kbID, docID}, faq.getIDs)
}

func TestFAQToolsScopeDenied(t *testing.T) {
	kbID := uuid.New()
	faq := &fakeMCPFAQService{result: faqFixture()}
	// 只有读 scope：create/update 拒绝，get 放行。
	readOnly := value.NewAPIKeyAuthContext(uuid.New(), uuid.New(),
		[]value.APIScope{value.ScopeDocumentsRead}, []uuid.UUID{kbID})

	// create/update 需要 documents:write：ToolFilter 在 tools/call 层直接拒绝。
	assertToolFilterDenial(t, callMCPToolRaw(faqTestDeps(faq), readOnly, "faq_create",
		fmt.Sprintf(`{"knowledge_base_id":%q,"title":"t","questions":["q"],"answer":"a"}`, kbID)))
	assertToolFilterDenial(t, callMCPToolRaw(faqTestDeps(faq), readOnly, "faq_update",
		fmt.Sprintf(`{"knowledge_base_id":%q,"document_id":%q,"base_revision_id":%q,"questions":["q"],"answer":"a"}`, kbID, uuid.New(), uuid.New())))

	structuredOf(t, callFAQTool(t, faqTestDeps(faq), readOnly, "faq_get",
		fmt.Sprintf(`{"knowledge_base_id":%q,"document_id":%q}`, kbID, uuid.New())))
	require.Equal(t, 1, faq.getCalls)
}

// 编译期锁定：生产 service 满足 MCP 窄接口（防止签名漂移）。
var (
	_ MCPFAQService      = (*service.FAQDocumentService)(nil)
	_ MCPFileTreeService = (*service.FileTreeService)(nil)
	_                    = model.Job{}
)
