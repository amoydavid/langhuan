// Package feishu 实现飞书云文档/知识库的 SourceConnector 适配器，
// 基于飞书官方 SDK github.com/larksuite/oapi-sdk-go/v3。
//
// 适配器不直接持有 *lark.Client 做编排，而是通过薄接口 feishuAPI 抽象
// SDK 的具体调用（GetNode/List/DriveList/DocxRawContent/DocxGet），
// 使递归遍历、节点组装、错误映射等编排逻辑可脱离真实 SDK 单测；
// SDK 调用正确性由 sdkClient 包装（生产装配注入），其契约由飞书 OpenAPI 保证。
package feishu

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkdocx "github.com/larksuite/oapi-sdk-go/v3/service/docx/v1"
	larkdrive "github.com/larksuite/oapi-sdk-go/v3/service/drive/v1"
	larkwiki "github.com/larksuite/oapi-sdk-go/v3/service/wiki/v2"

	"github.com/dajee/langhuan/internal/domain/model"
	sourceport "github.com/dajee/langhuan/internal/ports/source"
)

// ProviderName 是飞书 provider 标识。
const ProviderName = "feishu"

// CredentialDecryptor 解密 SourceConnection 的凭证密文，返回明文 app_secret。
type CredentialDecryptor interface {
	Decrypt(connectionID uuid.UUID, ciphertext []byte) ([]byte, error)
}

// Connector 实现飞书 SourceConnector。
type Connector struct {
	clientFactory LarkClientFactory
	decryptor     CredentialDecryptor

	mu      sync.Mutex
	clients map[clientKey]*sdkClient

	// apiForTest 仅测试注入：非空时 apiFor 直接返回它，绕过 SDK/凭证装配。
	// 生产代码不应设置该字段。
	apiForTest feishuAPI
}

type clientKey struct {
	appID     string
	appSecret string
}

// Option 是 NewConnector 的 functional option。
type Option func(*Connector)

// WithClientFactory 注入自定义 lark.Client 工厂（测试用）。
func WithClientFactory(f LarkClientFactory) Option {
	return func(c *Connector) {
		if f != nil {
			c.clientFactory = f
		}
	}
}

// WithCredentialDecryptor 注入凭证解密器（生产装配用）。
func WithCredentialDecryptor(d CredentialDecryptor) Option {
	return func(c *Connector) {
		if d != nil {
			c.decryptor = d
		}
	}
}

// NewConnector 构造飞书 SourceConnector。
func NewConnector(opts ...Option) *Connector {
	c := &Connector{
		clientFactory: defaultClientFactory{},
		decryptor:     identityDecryptor{},
		clients:       map[clientKey]*sdkClient{},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Provider 返回 "feishu"。
func (c *Connector) Provider() string { return ProviderName }

// ListTree 按 SyncRoot.Kind 分派，递归遍历飞书节点树。
//
// 错误分类（spec 3.1）：
//   - 致命错误（ErrSourceUnavailable 鉴权失败 / ErrSourceNotFound 根不存在或无权限）：
//     返回 error，且不返回可应用 snapshot（Nodes 为空）。
//   - 可恢复错误（限流、瞬态分页失败、子树列举失败等非致命错误）：
//     停止受影响分页/子树的进一步列举，保留已收集节点，标记 Complete=false 并附 warning。
//   - 全量列举成功：Complete=true。
//
// MaxEditTime 为快照内所有非零 EditTime 的最大值；adapter 只负责报告，是否推进
// application cursor 由调用方依据 Complete 决定。
func (c *Connector) ListTree(ctx context.Context, conn model.SourceConnection, root model.SyncRoot) (sourceport.TreeSnapshot, error) {
	if root.Token == "" {
		return sourceport.TreeSnapshot{}, fmt.Errorf("%w: sync root token 为空", sourceport.ErrSourceNotFound)
	}
	api, err := c.apiFor(ctx, conn)
	if err != nil {
		return sourceport.TreeSnapshot{}, err
	}
	state := newWalkState()
	switch root.Kind {
	case sourceport.SyncRootWikiNode:
		err = c.walkWiki(ctx, api, root.Token, state)
	case sourceport.SyncRootDriveFolder:
		err = c.walkDrive(ctx, api, root.Token, state)
	default:
		return sourceport.TreeSnapshot{}, fmt.Errorf("不支持的 sync root kind: %s", root.Kind)
	}
	if err != nil {
		// 致命错误：不返回可应用 snapshot。
		return sourceport.TreeSnapshot{}, err
	}
	return sourceport.TreeSnapshot{
		Nodes:       state.nodes,
		Complete:    !state.partial,
		Warnings:    state.warnings,
		MaxEditTime: maxEditTime(state.nodes),
	}, nil
}

// Fetch 拉取单个外部文档（首版仅 docx）。
//
// 内容大小限制：当 options.MaxContentBytes > 0 且返回的 markdown 字节数超过该上限时，
// 返回包装了 ErrSourceContentTooLarge 的错误；MaxContentBytes <= 0 表示不限。
//
// 实现说明：飞书 SDK 的 DocxRawContent 一次性返回完整内容字符串，无法在传输层流式截断。
// 因此本实现采用「先拉取后校验长度」的策略——这是在 SDK 不提供流式接口前提下的现实边界。
// application 层（spec 7.2）仍会对最终 markdown 做第二次校验，防止 adapter 实现错误。
func (c *Connector) Fetch(ctx context.Context, conn model.SourceConnection, externalID string, options sourceport.FetchOptions) (model.FetchedDocument, error) {
	if externalID == "" {
		return model.FetchedDocument{}, fmt.Errorf("%w: external_id 为空", sourceport.ErrSourceNotFound)
	}
	api, err := c.apiFor(ctx, conn)
	if err != nil {
		return model.FetchedDocument{}, err
	}
	content, err := api.DocxRawContent(ctx, externalID)
	if err != nil {
		return model.FetchedDocument{}, err
	}
	markdown := []byte(content)
	if options.MaxContentBytes > 0 && int64(len(markdown)) > options.MaxContentBytes {
		return model.FetchedDocument{}, fmt.Errorf("%w: docx %s 内容 %d 字节超过上限 %d",
			sourceport.ErrSourceContentTooLarge, externalID, len(markdown), options.MaxContentBytes)
	}
	title, _ := api.DocxGetTitle(ctx, externalID) // 标题获取失败降级为空，非硬错误
	return model.FetchedDocument{
		Markdown: markdown,
		Title:    title,
		ObjType:  "docx",
	}, nil
}

// apiFor 按 connection 凭证解析 app_id/app_secret，构造（或复用）sdkClient，
// 返回其满足的 feishuAPI 接口供编排逻辑使用。
func (c *Connector) apiFor(ctx context.Context, conn model.SourceConnection) (feishuAPI, error) {
	// 测试钩子：注入 fake feishuAPI 时直接返回，绕过 SDK 与凭证解析。
	if c.apiForTest != nil {
		return c.apiForTest, nil
	}
	appID := appIDFromConfig(conn)
	appSecret, err := c.appSecret(conn)
	if err != nil {
		return nil, err
	}
	if appID == "" || appSecret == "" {
		return nil, fmt.Errorf("%w: 缺少 app_id 或 app_secret", sourceport.ErrSourceUnavailable)
	}
	key := clientKey{appID: appID, appSecret: appSecret}
	c.mu.Lock()
	client, ok := c.clients[key]
	c.mu.Unlock()
	if !ok {
		lc, err := c.clientFactory.NewClient(appID, appSecret)
		if err != nil {
			return nil, fmt.Errorf("%w: 构造飞书 client 失败: %v", sourceport.ErrSourceUnavailable, err)
		}
		client = newSDKClient(lc)
		c.mu.Lock()
		c.clients[key] = client
		c.mu.Unlock()
	}
	return client, nil
}

func (c *Connector) appSecret(conn model.SourceConnection) (string, error) {
	if len(conn.CredentialsCiphertext) == 0 {
		return "", nil
	}
	plaintext, err := c.decryptor.Decrypt(conn.ID, conn.CredentialsCiphertext)
	if err != nil {
		return "", fmt.Errorf("解密来源连接凭证失败: %w", err)
	}
	return string(plaintext), nil
}

func appIDFromConfig(conn model.SourceConnection) string {
	if v, ok := conn.Config["app_id"].(string); ok {
		return v
	}
	return ""
}

// identityDecryptor 假定密文即为明文（测试/本地 fixture 用）。
type identityDecryptor struct{}

func (identityDecryptor) Decrypt(_ uuid.UUID, ciphertext []byte) ([]byte, error) {
	return ciphertext, nil
}

// compile-time interface conformance.
var _ sourceport.SourceConnector = (*Connector)(nil)

// ---- 飞书 wiki/drive/docx 对象类型常量 ----

const (
	objTypeFolder = "folder"
	objTypeDocx   = "docx"
)

// hasDocument 判断节点是否对应可同步文档（folder/wiki 目录节点为 false，其它为 true）。
func hasDocument(objType string) bool {
	return objType != "" && objType != objTypeFolder
}

// parseEditTime 把飞书的字符串时间戳（毫秒 Unix）解析为 time.Time，失败返回零值。
func parseEditTime(raw *string) time.Time {
	if raw == nil || *raw == "" {
		return sourceport.EditTimeZero
	}
	ms, err := strconv.ParseInt(*raw, 10, 64)
	if err != nil {
		return sourceport.EditTimeZero
	}
	return time.UnixMilli(ms).UTC()
}

// ptrString 安全取 *string 的值。
func ptrString(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// ---- 编排逻辑：wiki 递归遍历 ----

// walkState 累积一次 ListTree 遍历过程中的节点、告警与完整性标记。
//
// 设计：所有递归遍历函数共享同一个 state，从而把「子树可恢复失败」
// 透传到顶层，最终让 ListTree 决定 Complete=false 并附 warning。
type walkState struct {
	nodes    []model.ExternalNode
	warnings []string
	partial  bool
}

func newWalkState() *walkState {
	return &walkState{nodes: make([]model.ExternalNode, 0, 64)}
}

// append 追加一个外部节点，保持首项出现顺序（token 去重由 application 负责）。
func (s *walkState) append(n model.ExternalNode) {
	s.nodes = append(s.nodes, n)
}

// markPartial 记录一条 warning 并把快照标记为 partial（Complete=false）。
// warning 仅含非敏感的定位信息（标识、错误文本），不含文档内容。
func (s *walkState) markPartial(warning string) {
	s.partial = true
	s.warnings = append(s.warnings, warning)
}

// isFatalListError 列举过程中判定错误是否致命。
//
// 致命（返回 error，不返回可应用 snapshot）：
//   - ErrSourceUnavailable：鉴权失败 / 源不可达；
//   - ErrSourceNotFound：根或对象不存在 / 无权限。
//
// 其它错误（限流、瞬态分页失败、未分类的 API error）视为可恢复，
// 由调用方记录 warning 并标记 partial。
func isFatalListError(err error) bool {
	return errors.Is(err, sourceport.ErrSourceUnavailable) || errors.Is(err, sourceport.ErrSourceNotFound)
}

// maxEditTime 返回所有节点中非零 EditTime 的最大值；全为零则返回零值。
func maxEditTime(nodes []model.ExternalNode) time.Time {
	var max time.Time
	for i := range nodes {
		et := nodes[i].EditTime
		if et.IsZero() {
			continue
		}
		if max.IsZero() || et.After(max) {
			max = et
		}
	}
	return max
}

func (c *Connector) walkWiki(ctx context.Context, api feishuAPI, rootToken string, state *walkState) error {
	// 先用 get_node 解析根节点的 space_id 与自身信息。
	node, err := api.WikiGetNode(ctx, rootToken)
	if err != nil {
		return err
	}
	spaceID := ptrString(node.SpaceId)
	if spaceID == "" {
		return fmt.Errorf("%w: wiki 节点缺少 space_id", sourceport.ErrSourceNotFound)
	}
	state.append(wikiNodeToExternal(node, ""))
	// 递归收集子节点。
	return c.collectWikiChildren(ctx, api, spaceID, ptrString(node.NodeToken), state)
}

func (c *Connector) collectWikiChildren(ctx context.Context, api feishuAPI, spaceID, parentToken string, state *walkState) error {
	pageToken := ""
	for {
		items, next, hasMore, err := api.WikiListChildren(ctx, spaceID, parentToken, pageToken)
		if err != nil {
			if isFatalListError(err) {
				return err
			}
			// 可恢复错误：停止本父节点的分页，记录 partial + warning，保留已收集节点。
			state.markPartial(fmt.Sprintf("wiki 子节点列举失败 (space=%s parent=%s page=%s): %v",
				spaceID, parentToken, pageToken, err))
			return nil
		}
		for i := range items {
			item := items[i]
			state.append(wikiNodeToExternal(item, parentToken))
			// has_child 为 true 时继续递归。
			if item.HasChild != nil && *item.HasChild {
				if err := c.collectWikiChildren(ctx, api, spaceID, ptrString(item.NodeToken), state); err != nil {
					return err
				}
			}
		}
		if !hasMore || next == "" {
			return nil
		}
		pageToken = next
	}
}

func wikiNodeToExternal(node *larkwiki.Node, fallbackParent string) model.ExternalNode {
	parent := ptrString(node.ParentNodeToken)
	if parent == "" {
		parent = fallbackParent
	}
	objType := ptrString(node.ObjType)
	return model.ExternalNode{
		Token:       ptrString(node.ObjToken), // 文档真实 token（docx 的 document_id）
		ParentToken: parent,
		Title:       ptrString(node.Title),
		ObjType:     objType,
		HasDocument: hasDocument(objType),
		EditTime:    parseEditTime(node.ObjEditTime),
	}
}

// ---- 编排逻辑：drive 文件夹递归遍历 ----

func (c *Connector) walkDrive(ctx context.Context, api feishuAPI, folderToken string, state *walkState) error {
	return c.collectDriveChildren(ctx, api, folderToken, state)
}

func (c *Connector) collectDriveChildren(ctx context.Context, api feishuAPI, folderToken string, state *walkState) error {
	pageToken := ""
	for {
		files, next, hasMore, err := api.DriveList(ctx, folderToken, pageToken)
		if err != nil {
			if isFatalListError(err) {
				return err
			}
			// 可恢复错误：停止本文件夹的分页，记录 partial + warning。
			state.markPartial(fmt.Sprintf("drive 文件夹列举失败 (folder=%s page=%s): %v",
				folderToken, pageToken, err))
			return nil
		}
		for i := range files {
			f := files[i]
			objType := ptrString(f.Type)
			state.append(model.ExternalNode{
				Token:       ptrString(f.Token),
				ParentToken: folderToken,
				Title:       ptrString(f.Name),
				ObjType:     objType,
				HasDocument: hasDocument(objType),
				// drive 列举不返回 edit_time，统一零值。
			})
			if objType == objTypeFolder {
				if err := c.collectDriveChildren(ctx, api, ptrString(f.Token), state); err != nil {
					return err
				}
			}
		}
		if !hasMore || next == "" {
			return nil
		}
		pageToken = next
	}
}

// ---- 薄接口：抽象 SDK 调用 ----

// feishuAPI 抽象飞书 SDK 的列举/拉取能力，使编排逻辑可单测。
type feishuAPI interface {
	WikiGetNode(ctx context.Context, token string) (*larkwiki.Node, error)
	WikiListChildren(ctx context.Context, spaceID, parentToken, pageToken string) (items []*larkwiki.Node, nextPageToken string, hasMore bool, err error)
	DriveList(ctx context.Context, folderToken, pageToken string) (files []*larkdrive.File, nextPageToken string, hasMore bool, err error)
	DocxRawContent(ctx context.Context, documentID string) (string, error)
	DocxGetTitle(ctx context.Context, documentID string) (string, error)
}

// ---- SDK 调用实现 ----

// LarkClientFactory 按 app 凭证构造 *lark.Client（生产用默认实现，测试可注入）。
type LarkClientFactory interface {
	NewClient(appID, appSecret string) (*lark.Client, error)
}

type defaultClientFactory struct{}

func (defaultClientFactory) NewClient(appID, appSecret string) (*lark.Client, error) {
	return lark.NewClient(appID, appSecret), nil
}

// sdkClient 包装 *lark.Client，实现 feishuAPI。
// 因 SDK 子 service 类型未导出（*spaceNode/*space/*file/*document），
// 这里以闭包持有具体方法引用，既满足 feishuAPI 又避免暴露未导出类型。
type sdkClient struct {
	driveFileList func(ctx context.Context, req *larkdrive.ListFileReq, opts ...larkcore.RequestOptionFunc) (*larkdrive.ListFileResp, error)
	docxGet       func(ctx context.Context, req *larkdocx.GetDocumentReq, opts ...larkcore.RequestOptionFunc) (*larkdocx.GetDocumentResp, error)
	docxRaw       func(ctx context.Context, req *larkdocx.RawContentDocumentReq, opts ...larkcore.RequestOptionFunc) (*larkdocx.RawContentDocumentResp, error)
	spaceGetNode  func(ctx context.Context, req *larkwiki.GetNodeSpaceReq, opts ...larkcore.RequestOptionFunc) (*larkwiki.GetNodeSpaceResp, error)
	nodeList      func(ctx context.Context, req *larkwiki.ListSpaceNodeReq, opts ...larkcore.RequestOptionFunc) (*larkwiki.ListSpaceNodeResp, error)
}

func newSDKClient(c *lark.Client) *sdkClient {
	return &sdkClient{
		driveFileList: c.Drive.File.List,
		docxGet:       c.Docx.Document.Get,
		docxRaw:       c.Docx.Document.RawContent,
		spaceGetNode:  c.Wiki.Space.GetNode,
		nodeList:      c.Wiki.SpaceNode.List,
	}
}

func (s *sdkClient) WikiGetNode(ctx context.Context, token string) (*larkwiki.Node, error) {
	resp, err := s.spaceGetNode(ctx, larkwiki.NewGetNodeSpaceReqBuilder().Token(token).Build())
	if err != nil {
		return nil, mapNetOrSDKError(err)
	}
	if resp.Code != 0 {
		return nil, mapCodeError(resp.Code, resp.Msg)
	}
	if resp.Data == nil || resp.Data.Node == nil {
		return nil, fmt.Errorf("%w: get_node 返回空节点", sourceport.ErrSourceNotFound)
	}
	return resp.Data.Node, nil
}

func (s *sdkClient) WikiListChildren(ctx context.Context, spaceID, parentToken, pageToken string) ([]*larkwiki.Node, string, bool, error) {
	req := larkwiki.NewListSpaceNodeReqBuilder().
		SpaceId(spaceID).
		ParentNodeToken(parentToken).
		PageSize(50)
	if pageToken != "" {
		req = req.PageToken(pageToken)
	}
	resp, err := s.nodeList(ctx, req.Build())
	if err != nil {
		return nil, "", false, mapNetOrSDKError(err)
	}
	if resp.Code != 0 {
		return nil, "", false, mapCodeError(resp.Code, resp.Msg)
	}
	if resp.Data == nil {
		return nil, "", false, nil
	}
	next := ptrString(resp.Data.PageToken)
	more := resp.Data.HasMore != nil && *resp.Data.HasMore
	return resp.Data.Items, next, more, nil
}

func (s *sdkClient) DriveList(ctx context.Context, folderToken, pageToken string) ([]*larkdrive.File, string, bool, error) {
	req := larkdrive.NewListFileReqBuilder().
		FolderToken(folderToken).
		PageSize(200)
	if pageToken != "" {
		req = req.PageToken(pageToken)
	}
	resp, err := s.driveFileList(ctx, req.Build())
	if err != nil {
		return nil, "", false, mapNetOrSDKError(err)
	}
	if resp.Code != 0 {
		return nil, "", false, mapCodeError(resp.Code, resp.Msg)
	}
	if resp.Data == nil {
		return nil, "", false, nil
	}
	next := ptrString(resp.Data.NextPageToken)
	more := resp.Data.HasMore != nil && *resp.Data.HasMore
	return resp.Data.Files, next, more, nil
}

func (s *sdkClient) DocxRawContent(ctx context.Context, documentID string) (string, error) {
	resp, err := s.docxRaw(ctx, larkdocx.NewRawContentDocumentReqBuilder().DocumentId(documentID).Build())
	if err != nil {
		return "", mapNetOrSDKError(err)
	}
	if resp.Code != 0 {
		return "", mapCodeError(resp.Code, resp.Msg)
	}
	if resp.Data == nil {
		return "", nil
	}
	return ptrString(resp.Data.Content), nil
}

func (s *sdkClient) DocxGetTitle(ctx context.Context, documentID string) (string, error) {
	resp, err := s.docxGet(ctx, larkdocx.NewGetDocumentReqBuilder().DocumentId(documentID).Build())
	if err != nil {
		return "", mapNetOrSDKError(err)
	}
	if resp.Code != 0 {
		return "", mapCodeError(resp.Code, resp.Msg)
	}
	if resp.Data == nil || resp.Data.Document == nil {
		return "", nil
	}
	return ptrString(resp.Data.Document.Title), nil
}

// mapCodeError 把飞书业务错误码映射为领域错误（1254003/1254040 等.NotFound → ErrSourceNotFound）。
func mapCodeError(code int, msg string) error {
	if code == 0 {
		return nil
	}
	switch code {
	case 1254040, 1061045: // 常见的「不存在 / 无权限」错误码
		return fmt.Errorf("%w: 飞书对象不存在或无权限 (code=%d)", sourceport.ErrSourceNotFound, code)
	case 99991663, 99991664: // 鉴权失败
		return fmt.Errorf("%w: 飞书鉴权失败 (code=%d)", sourceport.ErrSourceUnavailable, code)
	}
	return fmt.Errorf("飞书 API 错误 (code=%d): %s", code, strings.TrimSpace(msg))
}

// mapNetOrSDKError 处理 SDK 返回的网络/传输层错误。
func mapNetOrSDKError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	// SDK 网络错误通常包含 "http request failed" 或具体状态码。
	if strings.Contains(msg, "401") || strings.Contains(msg, "403") {
		return fmt.Errorf("%w: %v", sourceport.ErrSourceUnavailable, err)
	}
	if strings.Contains(msg, "404") {
		return fmt.Errorf("%w: %v", sourceport.ErrSourceNotFound, err)
	}
	return fmt.Errorf("飞书调用失败: %w", err)
}

// newConnectorForTest 构造一个注入 fake feishuAPI 的 Connector，仅用于测试：
// apiFor 会直接返回该 fake，绕过 SDK client 构造与凭证解密。
// decryptor 使用 identityDecryptor（密文即明文），clients map 为空。
func newConnectorForTest(api feishuAPI) *Connector {
	return &Connector{
		decryptor:  identityDecryptor{},
		clients:    map[clientKey]*sdkClient{},
		apiForTest: api,
	}
}
