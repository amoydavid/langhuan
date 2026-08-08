package feishu

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	larkdrive "github.com/larksuite/oapi-sdk-go/v3/service/drive/v1"
	larkwiki "github.com/larksuite/oapi-sdk-go/v3/service/wiki/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dajee/langhuan/internal/domain/model"
	sourceport "github.com/dajee/langhuan/internal/ports/source"
)

// ---- 指针 helper：larkwiki.Node / larkdrive.File 字段均为指针类型 ----

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

// ---- fakeAPI：实现 feishuAPI，按预设 map 返回数据，支持注入错误与分页 ----

// wikiChildKey 标识一个 wiki 父节点下的子节点集合（按 spaceID + parentToken）。
type wikiChildKey struct {
	spaceID, parentToken string
}

// drivePageKey 标识一个 drive 文件夹下某页的文件集合。
// 同一 folderToken 可有多页，按 pageToken 区分。
type drivePageKey struct {
	folderToken, pageToken string
}

// fakeAPI 是 feishuAPI 的内存实现，供 Connector 编排逻辑单测。
type fakeAPI struct {
	// wikiNodes: token → node（用于 WikiGetNode）。
	wikiNodes map[string]*larkwiki.Node
	// wikiChildren: (spaceID, parentToken) → 子节点列表（单页场景）。
	wikiChildren map[wikiChildKey][]*larkwiki.Node
	// wikiChildrenPaged: (spaceID, parentToken, pageToken) → 子节点列表，用于分页测试。
	// pageToken == "" 表示第一页。同时配合 wikiNextPage / wikiHasMore 控制翻页。
	wikiChildrenPaged map[wikiChildKey]map[string][]*larkwiki.Node
	wikiNextPage      map[wikiChildKey]map[string]string // pageToken(当前页) → nextPageToken
	wikiHasMore       map[wikiChildKey]map[string]bool   // pageToken(当前页) → hasMore

	// driveFiles: folderToken → 文件列表（单页场景）。
	driveFiles map[string][]*larkdrive.File
	// drivePages: (folderToken, pageToken) → 文件列表，分页场景。
	drivePages   map[drivePageKey][]*larkdrive.File
	driveNext    map[drivePageKey]string // 当前页 → nextPageToken
	driveHasMore map[drivePageKey]bool   // 当前页 → hasMore

	// docx 内容与标题。
	docxContent map[string]string
	docxTitle   map[string]string

	// 注入错误：按 token / documentID 返回特定 error，命中即短路返回。
	getNodeErr      map[string]error // by node token
	listChildrenErr map[string]error // by parent token (任意页命中)
	driveListErr    map[string]error // by folder token (任意页命中)
	docxContentErr  map[string]error // by document id
	docxTitleErr    map[string]error // by document id

	// 分页级别的错误注入：用于测试「第一页成功、第二页可恢复失败」之类的场景。
	// wikiChildrenPagedErr[wikiChildKey][pageToken] 命中时返回该 error，否则按分页表返回数据。
	wikiChildrenPagedErr map[wikiChildKey]map[string]error
	// driveListPagedErr[drivePageKey] 命中时返回该 error，否则按分页表/单页表返回数据。
	driveListPagedErr map[drivePageKey]error

	// 计数器：校验调用次数。
	getNodeCalls      int
	listChildrenCalls int
	driveListCalls    int
	rawContentCalls   int
	getTitleCalls     int
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{
		wikiNodes:            map[string]*larkwiki.Node{},
		wikiChildren:         map[wikiChildKey][]*larkwiki.Node{},
		wikiChildrenPaged:    map[wikiChildKey]map[string][]*larkwiki.Node{},
		wikiNextPage:         map[wikiChildKey]map[string]string{},
		wikiHasMore:          map[wikiChildKey]map[string]bool{},
		driveFiles:           map[string][]*larkdrive.File{},
		drivePages:           map[drivePageKey][]*larkdrive.File{},
		driveNext:            map[drivePageKey]string{},
		driveHasMore:         map[drivePageKey]bool{},
		docxContent:          map[string]string{},
		docxTitle:            map[string]string{},
		getNodeErr:           map[string]error{},
		listChildrenErr:      map[string]error{},
		driveListErr:         map[string]error{},
		docxContentErr:       map[string]error{},
		docxTitleErr:         map[string]error{},
		wikiChildrenPagedErr: map[wikiChildKey]map[string]error{},
		driveListPagedErr:    map[drivePageKey]error{},
	}
}

func (f *fakeAPI) WikiGetNode(_ context.Context, token string) (*larkwiki.Node, error) {
	f.getNodeCalls++
	if err, ok := f.getNodeErr[token]; ok {
		return nil, err
	}
	node, ok := f.wikiNodes[token]
	if !ok {
		return nil, sourceport.ErrSourceNotFound
	}
	return node, nil
}

func (f *fakeAPI) WikiListChildren(_ context.Context, spaceID, parentToken, pageToken string) ([]*larkwiki.Node, string, bool, error) {
	f.listChildrenCalls++
	if err, ok := f.listChildrenErr[parentToken]; ok {
		return nil, "", false, err
	}
	key := wikiChildKey{spaceID: spaceID, parentToken: parentToken}
	// 分页级别的错误注入（按 pageToken）。
	if pageErrs, ok := f.wikiChildrenPagedErr[key]; ok {
		if err, hit := pageErrs[pageToken]; hit {
			return nil, "", false, err
		}
	}
	// 优先走分页表。
	if pages, ok := f.wikiChildrenPaged[key]; ok {
		items := pages[pageToken]
		next := f.wikiNextPage[key][pageToken]
		more := f.wikiHasMore[key][pageToken]
		return items, next, more, nil
	}
	// 单页表。
	items := f.wikiChildren[key]
	return items, "", false, nil
}

func (f *fakeAPI) DriveList(_ context.Context, folderToken, pageToken string) ([]*larkdrive.File, string, bool, error) {
	f.driveListCalls++
	if err, ok := f.driveListErr[folderToken]; ok {
		return nil, "", false, err
	}
	key := drivePageKey{folderToken: folderToken, pageToken: pageToken}
	// 分页级别的错误注入。
	if err, hit := f.driveListPagedErr[key]; hit {
		return nil, "", false, err
	}
	if items, ok := f.drivePages[key]; ok {
		next := f.driveNext[key]
		more := f.driveHasMore[key]
		return items, next, more, nil
	}
	// 单页表（pageToken 为空时命中）。
	if pageToken == "" {
		if items, ok := f.driveFiles[folderToken]; ok {
			return items, "", false, nil
		}
	}
	return nil, "", false, nil
}

func (f *fakeAPI) DocxRawContent(_ context.Context, documentID string) (string, error) {
	f.rawContentCalls++
	if err, ok := f.docxContentErr[documentID]; ok {
		return "", err
	}
	return f.docxContent[documentID], nil
}

func (f *fakeAPI) DocxGetTitle(_ context.Context, documentID string) (string, error) {
	f.getTitleCalls++
	if err, ok := f.docxTitleErr[documentID]; ok {
		return "", err
	}
	return f.docxTitle[documentID], nil
}

// ---- 测试用 SourceConnection / Connector 装配 helper ----

func testConn() model.SourceConnection {
	// CredentialsCiphertext 使用 identityDecryptor 时即明文，但测试不校验解密
	// （apiForTest 钩子已绕过凭证解析），故填占位值即可，不记录真实 secret。
	return model.SourceConnection{
		ID:                    uuid.New(),
		Provider:              ProviderName,
		Config:                map[string]any{"app_id": "cli_test"},
		CredentialsCiphertext: []byte("encrypted-placeholder"),
	}
}

func testConnector(api *fakeAPI) *Connector {
	return newConnectorForTest(api)
}

// ---- wikiNode 构造 helper：减少重复 ----

func wikiNode(token, objToken, objType, parent, title string, hasChild bool) *larkwiki.Node {
	return &larkwiki.Node{
		NodeToken:       strPtr(token),
		ObjToken:        strPtr(objToken),
		ObjType:         strPtr(objType),
		ParentNodeToken: strPtr(parent),
		Title:           strPtr(title),
		HasChild:        boolPtr(hasChild),
	}
}

// wikiNodeWithEdit 是 wikiNode 的扩展版，额外设置 ObjEditTime（毫秒 unix 字符串）。
func wikiNodeWithEdit(token, objToken, objType, parent, title, editTime string, hasChild bool) *larkwiki.Node {
	n := wikiNode(token, objToken, objType, parent, title, hasChild)
	n.ObjEditTime = strPtr(editTime)
	return n
}

// ---- 测试用例 ----

// TestConnectorProvider 断言 Provider() 返回 "feishu"。
func TestConnectorProvider(t *testing.T) {
	c := testConnector(newFakeAPI())
	assert.Equal(t, ProviderName, c.Provider())
	assert.Equal(t, "feishu", c.Provider())
}

// TestListTreeWalksWikiNodesRecursively 验证 wiki 节点树递归遍历：
//
//	root(docx) ─┬─ childA(docx)
//	             └─ childB(folder, has_child=true)
//	                          └─ grandChild(docx)
//
// 期望返回 4 个节点（root + childA + childB + grandChild）；
// folder 节点 HasDocument=false，docx 节点 HasDocument=true。
func TestListTreeWalksWikiNodesRecursively(t *testing.T) {
	const (
		spaceID     = "spcAAA"
		rootToken   = "wikcnRoot"
		childAToken = "wikcnA"
		childBToken = "wikcnB"
		grandToken  = "wikcnG"
	)
	api := newFakeAPI()
	api.wikiNodes[rootToken] = &larkwiki.Node{
		NodeToken: strPtr(rootToken), ObjToken: strPtr("doxcnRoot"),
		ObjType: strPtr("docx"), SpaceId: strPtr(spaceID),
		Title: strPtr("根文档"),
	}
	// 根节点没有 ParentNodeToken，walkWiki 用 fallbackParent("") 处理。
	api.wikiChildren[wikiChildKey{spaceID, rootToken}] = []*larkwiki.Node{
		wikiNode(childAToken, "doxcnA", "docx", "", "文档A", false),
		wikiNode(childBToken, "fldcnB", "folder", "", "文件夹B", true),
	}
	api.wikiChildren[wikiChildKey{spaceID, childBToken}] = []*larkwiki.Node{
		wikiNode(grandToken, "doxcnG", "docx", childBToken, "孙文档", false),
	}

	snapshot, err := testConnector(api).ListTree(context.Background(), testConn(),
		model.SyncRoot{Kind: sourceport.SyncRootWikiNode, Token: rootToken})
	require.NoError(t, err)
	nodes := snapshot.Nodes
	require.True(t, snapshot.Complete, "首版假设 Complete=true")
	require.Len(t, nodes, 4, "应返回 root+2子+1孙 共4个节点")

	// 根节点。
	assert.Equal(t, "doxcnRoot", nodes[0].Token)
	assert.Equal(t, "docx", nodes[0].ObjType)
	assert.True(t, nodes[0].HasDocument, "docx 节点 HasDocument 应为 true")

	// 按 token 建索引便于断言，避免依赖遍历顺序。
	byToken := map[string]model.ExternalNode{}
	for _, n := range nodes {
		byToken[n.Token] = n
	}

	// 文档A。
	if a, ok := byToken["doxcnA"]; assert.True(t, ok, "文档A 应在结果中") {
		assert.True(t, a.HasDocument, "docx HasDocument=true")
		assert.Equal(t, "docx", a.ObjType)
	}
	// 文件夹B：HasDocument=false。
	if b, ok := byToken["fldcnB"]; assert.True(t, ok, "文件夹B 应在结果中") {
		assert.False(t, b.HasDocument, "folder HasDocument 应为 false")
		assert.Equal(t, "folder", b.ObjType)
	}
	// 孙文档。
	if g, ok := byToken["doxcnG"]; assert.True(t, ok, "孙文档 应在结果中") {
		assert.True(t, g.HasDocument)
		assert.Equal(t, "docx", g.ObjType)
	}
}

// TestListTreeWalksDriveFolderRecursively 验证 drive 文件夹递归遍历：
//
//	folder/ ─┬─ docx 文件
//	         └─ 子folder/ ── docx 文件
//
// drive 遍历不包含根 folder 自身（仅列举子项），故返回 3 个节点。
func TestListTreeWalksDriveFolderRecursively(t *testing.T) {
	const (
		rootFolder = "fldcnRoot"
		subFolder  = "fldcnSub"
	)
	api := newFakeAPI()
	api.driveFiles[rootFolder] = []*larkdrive.File{
		{Token: strPtr("doxcn1"), Name: strPtr("文档1"), Type: strPtr("docx")},
		{Token: strPtr(subFolder), Name: strPtr("子文件夹"), Type: strPtr("folder")},
	}
	api.driveFiles[subFolder] = []*larkdrive.File{
		{Token: strPtr("doxcn2"), Name: strPtr("文档2"), Type: strPtr("docx")},
	}

	snapshot, err := testConnector(api).ListTree(context.Background(), testConn(),
		model.SyncRoot{Kind: sourceport.SyncRootDriveFolder, Token: rootFolder})
	require.NoError(t, err)
	nodes := snapshot.Nodes
	require.Len(t, nodes, 3, "应返回 root 下 2 文件 + subfolder 下 1 文件 共3个节点")

	byToken := map[string]model.ExternalNode{}
	for _, n := range nodes {
		byToken[n.Token] = n
	}
	if d1, ok := byToken["doxcn1"]; assert.True(t, ok) {
		assert.True(t, d1.HasDocument)
		assert.Equal(t, "docx", d1.ObjType)
		assert.Equal(t, rootFolder, d1.ParentToken)
	}
	if sf, ok := byToken[subFolder]; assert.True(t, ok) {
		assert.False(t, sf.HasDocument, "folder HasDocument 应为 false")
		assert.Equal(t, "folder", sf.ObjType)
		assert.Equal(t, rootFolder, sf.ParentToken)
	}
	if d2, ok := byToken["doxcn2"]; assert.True(t, ok) {
		assert.True(t, d2.HasDocument)
		assert.Equal(t, subFolder, d2.ParentToken)
	}
}

// TestFetchReturnsDocxMarkdown 验证 Fetch 返回 docx 的 markdown 内容与标题。
func TestFetchReturnsDocxMarkdown(t *testing.T) {
	const docID = "doxcnFetch"
	api := newFakeAPI()
	api.docxContent[docID] = "# 标题\n正文"
	api.docxTitle[docID] = "文档A"

	doc, err := testConnector(api).Fetch(context.Background(), testConn(), docID, sourceport.FetchOptions{})
	require.NoError(t, err)
	assert.Equal(t, "文档A", doc.Title)
	assert.Equal(t, "docx", doc.ObjType)
	assert.Contains(t, string(doc.Markdown), "# 标题")
	assert.Contains(t, string(doc.Markdown), "正文")
}

// TestFetchToleratesMissingTitle 验证 DocxGetTitle 返回 error 时，
// Fetch 仍成功（标题降级为空），不报错。
func TestFetchToleratesMissingTitle(t *testing.T) {
	const docID = "doxcnNoTitle"
	api := newFakeAPI()
	api.docxContent[docID] = "# 仅正文"
	api.docxTitleErr[docID] = errors.New("title endpoint 503")

	doc, err := testConnector(api).Fetch(context.Background(), testConn(), docID, sourceport.FetchOptions{})
	require.NoError(t, err, "标题获取失败应降级而非报错")
	assert.Empty(t, doc.Title, "标题降级为空")
	assert.Equal(t, "docx", doc.ObjType)
	assert.Equal(t, 1, api.getTitleCalls, "DocxGetTitle 应被调用一次")
}

// TestListTreeWikiPropagatesGetNodeError 验证 WikiGetNode 返回 ErrSourceNotFound 时，
// ListTree 通过 errors.Is 暴露该错误。
func TestListTreeWikiPropagatesGetNodeError(t *testing.T) {
	const rootToken = "wikcnMissing"
	api := newFakeAPI()
	api.getNodeErr[rootToken] = sourceport.ErrSourceNotFound

	_, err := testConnector(api).ListTree(context.Background(), testConn(),
		model.SyncRoot{Kind: sourceport.SyncRootWikiNode, Token: rootToken})
	require.Error(t, err)
	assert.True(t, errors.Is(err, sourceport.ErrSourceNotFound),
		"应传播 ErrSourceNotFound，got %v", err)
}

// TestListTreeWikiPropagatesListChildrenError 验证 WikiListChildren 返回致命错误（鉴权失败）时，
// ListTree 通过 errors.Is 暴露 ErrSourceUnavailable，且不返回可应用 snapshot。
func TestListTreeWikiPropagatesListChildrenError(t *testing.T) {
	const (
		spaceID   = "spcErr"
		rootToken = "wikcnRootErr"
	)
	api := newFakeAPI()
	api.wikiNodes[rootToken] = &larkwiki.Node{
		NodeToken: strPtr(rootToken), ObjToken: strPtr("doxcnErr"),
		ObjType: strPtr("docx"), SpaceId: strPtr(spaceID),
	}
	// 注入致命错误（鉴权失败），应作为 fatal 传播而非 partial snapshot。
	api.listChildrenErr[rootToken] = fmt.Errorf("%w: 鉴权失败", sourceport.ErrSourceUnavailable)

	snapshot, err := testConnector(api).ListTree(context.Background(), testConn(),
		model.SyncRoot{Kind: sourceport.SyncRootWikiNode, Token: rootToken})
	require.Error(t, err)
	assert.True(t, errors.Is(err, sourceport.ErrSourceUnavailable),
		"致命错误应传播 ErrSourceUnavailable，got %v", err)
	assert.Empty(t, snapshot.Nodes, "致命错误不应返回可应用 snapshot")
}

// TestListTreeDrivePagination 验证 drive 分页：第一页 hasMore=true + nextPageToken，
// 第二页返回剩余文件；两页文件应都被收集。
func TestListTreeDrivePagination(t *testing.T) {
	const (
		folder = "fldcnPage"
		page1  = "tok1"
		page2  = "tok2"
	)
	api := newFakeAPI()
	// 第一页：2 文件 + hasMore=true，next=page2。
	api.drivePages[drivePageKey{folder, ""}] = []*larkdrive.File{
		{Token: strPtr("doxcnP1a"), Name: strPtr("页1文档A"), Type: strPtr("docx")},
		{Token: strPtr("doxcnP1b"), Name: strPtr("页1文档B"), Type: strPtr("docx")},
	}
	api.driveNext[drivePageKey{folder, ""}] = page2
	api.driveHasMore[drivePageKey{folder, ""}] = true
	// 第二页：1 文件，无更多。
	api.drivePages[drivePageKey{folder, page2}] = []*larkdrive.File{
		{Token: strPtr("doxcnP2a"), Name: strPtr("页2文档A"), Type: strPtr("docx")},
	}
	api.driveNext[drivePageKey{folder, page2}] = ""
	api.driveHasMore[drivePageKey{folder, page2}] = false

	snapshot, err := testConnector(api).ListTree(context.Background(), testConn(),
		model.SyncRoot{Kind: sourceport.SyncRootDriveFolder, Token: folder})
	require.NoError(t, err)
	nodes := snapshot.Nodes
	require.Len(t, nodes, 3, "两页文件都应被收集")
	assert.Equal(t, 2, api.driveListCalls, "DriveList 应被调用两次（两页）")

	byToken := map[string]model.ExternalNode{}
	for _, n := range nodes {
		byToken[n.Token] = n
	}
	assert.Contains(t, byToken, "doxcnP1a")
	assert.Contains(t, byToken, "doxcnP1b")
	assert.Contains(t, byToken, "doxcnP2a")
}

// ---- Task 4: 快照完整性与 bounded Fetch ----

// setWikiPagedChildren 设置 wiki 父节点下的分页子节点表。
// pages: pageToken → 子节点列表；nextPage: 当前页 → 下一页 token；hasMoreMap: 当前页 → hasMore。
func setWikiPagedChildren(api *fakeAPI, spaceID, parentToken string,
	pages map[string][]*larkwiki.Node, nextPage map[string]string, hasMoreMap map[string]bool) {
	key := wikiChildKey{spaceID: spaceID, parentToken: parentToken}
	api.wikiChildrenPaged[key] = pages
	api.wikiNextPage[key] = nextPage
	api.wikiHasMore[key] = hasMoreMap
}

// TestListTreeWikiIncompleteOnRecoverablePageFailure 验证 wiki 第二页返回可恢复错误时，
// snapshot 仍包含第一页节点，但 Complete=false 且带 warning。
func TestListTreeWikiIncompleteOnRecoverablePageFailure(t *testing.T) {
	const (
		spaceID   = "spcPart"
		rootToken = "wikcnPart"
		page2     = "tok2"
	)
	api := newFakeAPI()
	api.wikiNodes[rootToken] = &larkwiki.Node{
		NodeToken: strPtr(rootToken), ObjToken: strPtr("doxcnRoot"),
		ObjType: strPtr("docx"), SpaceId: strPtr(spaceID),
		Title: strPtr("根文档"),
	}
	key := wikiChildKey{spaceID: spaceID, parentToken: rootToken}
	setWikiPagedChildren(api, spaceID, rootToken,
		map[string][]*larkwiki.Node{
			"":    {wikiNode("wikcnA", "doxcnA", "docx", "", "文档A", false)},
			page2: {wikiNode("wikcnB", "doxcnB", "docx", "", "文档B", false)},
		},
		map[string]string{"": page2},
		map[string]bool{"": true},
	)
	// 第二页返回可恢复错误（非致命，应为 partial 而非 error）。
	if api.wikiChildrenPagedErr[key] == nil {
		api.wikiChildrenPagedErr[key] = map[string]error{}
	}
	api.wikiChildrenPagedErr[key][page2] = errors.New("飞书限流，请稍后重试")

	snapshot, err := testConnector(api).ListTree(context.Background(), testConn(),
		model.SyncRoot{Kind: sourceport.SyncRootWikiNode, Token: rootToken})
	require.NoError(t, err, "可恢复错误不应导致 ListTree 返回 error")
	assert.False(t, snapshot.Complete, "可恢复分页失败必须标记 Complete=false")
	require.NotEmpty(t, snapshot.Warnings, "partial snapshot 应携带 warning")
	// 根节点 + 第一页节点应都在结果中（第二页未收集）。
	require.GreaterOrEqual(t, len(snapshot.Nodes), 2, "第一页节点应被保留")
	tokens := map[string]bool{}
	for _, n := range snapshot.Nodes {
		tokens[n.Token] = true
	}
	assert.True(t, tokens["doxcnRoot"], "根节点应保留")
	assert.True(t, tokens["doxcnA"], "第一页节点应保留")
	assert.False(t, tokens["doxcnB"], "第二页失败不应被收集")
}

// TestListTreeDriveIncompleteOnRecoverablePageFailure 验证 drive 第二页返回可恢复错误时，
// snapshot 仍包含第一页文件，但 Complete=false 且带 warning。
func TestListTreeDriveIncompleteOnRecoverablePageFailure(t *testing.T) {
	const (
		folder = "fldcnPart"
		page2  = "tok2"
	)
	api := newFakeAPI()
	api.drivePages[drivePageKey{folder, ""}] = []*larkdrive.File{
		{Token: strPtr("doxcnP1a"), Name: strPtr("页1文档A"), Type: strPtr("docx")},
	}
	api.driveNext[drivePageKey{folder, ""}] = page2
	api.driveHasMore[drivePageKey{folder, ""}] = true
	api.drivePages[drivePageKey{folder, page2}] = []*larkdrive.File{
		{Token: strPtr("doxcnP2a"), Name: strPtr("页2文档A"), Type: strPtr("docx")},
	}
	api.driveNext[drivePageKey{folder, page2}] = ""
	api.driveHasMore[drivePageKey{folder, page2}] = false
	// 第二页返回可恢复错误。
	api.driveListPagedErr[drivePageKey{folder, page2}] = errors.New("飞书限流，请稍后重试")

	snapshot, err := testConnector(api).ListTree(context.Background(), testConn(),
		model.SyncRoot{Kind: sourceport.SyncRootDriveFolder, Token: folder})
	require.NoError(t, err, "可恢复错误不应导致 ListTree 返回 error")
	assert.False(t, snapshot.Complete, "可恢复分页失败必须标记 Complete=false")
	require.NotEmpty(t, snapshot.Warnings, "partial snapshot 应携带 warning")
	// 第一页文件应被保留。
	tokens := map[string]bool{}
	for _, n := range snapshot.Nodes {
		tokens[n.Token] = true
	}
	assert.True(t, tokens["doxcnP1a"], "第一页文件应保留")
	assert.False(t, tokens["doxcnP2a"], "第二页失败不应被收集")
}

// TestListTreeWikiSubtreeRecoverableFailure 验证子树列举出现可恢复错误时，
// 整体 Complete=false，但其他子树仍被收集。
func TestListTreeWikiSubtreeRecoverableFailure(t *testing.T) {
	const (
		spaceID      = "spcSub"
		rootToken    = "wikcnSub"
		childAToken  = "wikcnA"
		childBToken  = "wikcnB"
		grandAToken  = "wikcnGA"
		grandB1Token = "wikcnGB1"
		grandB2Token = "wikcnGB2"
	)
	api := newFakeAPI()
	api.wikiNodes[rootToken] = &larkwiki.Node{
		NodeToken: strPtr(rootToken), ObjToken: strPtr("doxcnRoot"),
		ObjType: strPtr("docx"), SpaceId: strPtr(spaceID),
		Title: strPtr("根文档"),
	}
	// 根下两个 folder 子节点。
	api.wikiChildren[wikiChildKey{spaceID, rootToken}] = []*larkwiki.Node{
		wikiNode(childAToken, "fldcnA", "folder", "", "子A", true),
		wikiNode(childBToken, "fldcnB", "folder", "", "子B", true),
	}
	// 子A 正常：一个 grandChild。
	api.wikiChildren[wikiChildKey{spaceID, childAToken}] = []*larkwiki.Node{
		wikiNode(grandAToken, "doxcnGA", "docx", childAToken, "孙A", false),
	}
	// 子B 可恢复失败。
	api.listChildrenErr[childBToken] = errors.New("子B 子树限流")
	// 引用 grandB1Token / grandB2Token 避免 unused 报错（保留语义占位）。
	_ = grandB1Token
	_ = grandB2Token

	snapshot, err := testConnector(api).ListTree(context.Background(), testConn(),
		model.SyncRoot{Kind: sourceport.SyncRootWikiNode, Token: rootToken})
	require.NoError(t, err)
	assert.False(t, snapshot.Complete, "子树失败应标记 Complete=false")
	require.NotEmpty(t, snapshot.Warnings)

	tokens := map[string]bool{}
	for _, n := range snapshot.Nodes {
		tokens[n.Token] = true
	}
	assert.True(t, tokens["doxcnRoot"], "根节点应保留")
	assert.True(t, tokens["doxcnGA"], "未失败的子树仍应被收集")
	// 子B 自身作为根下子节点已收集，但其孙子不应存在。
}

// TestListTreeCompleteCalculatesMaxEditTime 验证完整遍历时
// MaxEditTime = 所有非零 EditTime 的最大值。
func TestListTreeCompleteCalculatesMaxEditTime(t *testing.T) {
	const (
		spaceID   = "spcEdit"
		rootToken = "wikcnEdit"
	)
	// 三个节点的 ObjEditTime（毫秒 unix）：1000、3000、（零/空）。
	const (
		editMsRoot = "1000"
		editMsA    = "3000"
	)
	api := newFakeAPI()
	api.wikiNodes[rootToken] = &larkwiki.Node{
		NodeToken: strPtr(rootToken), ObjToken: strPtr("doxcnRoot"),
		ObjType: strPtr("docx"), SpaceId: strPtr(spaceID),
		Title:       strPtr("根文档"),
		ObjEditTime: strPtr(editMsRoot),
	}
	api.wikiChildren[wikiChildKey{spaceID, rootToken}] = []*larkwiki.Node{
		wikiNodeWithEdit("wikcnA", "doxcnA", "docx", "", "文档A", editMsA, false),
		// ObjEditTime 为 nil/空：EditTime 应为零值，不应参与 max 计算。
		wikiNode("wikcnB", "doxcnB", "docx", "", "文档B无编辑时间", false),
	}

	snapshot, err := testConnector(api).ListTree(context.Background(), testConn(),
		model.SyncRoot{Kind: sourceport.SyncRootWikiNode, Token: rootToken})
	require.NoError(t, err)
	require.True(t, snapshot.Complete, "完整遍历应 Complete=true")
	// MaxEditTime 应等于 editMsA（3000ms）对应的时间。
	expected := parseEditTime(strPtr(editMsA))
	assert.True(t, snapshot.MaxEditTime.Equal(expected),
		"MaxEditTime 应为非零最大值 %v，got %v", expected, snapshot.MaxEditTime)
	assert.False(t, snapshot.MaxEditTime.IsZero(), "存在非零 EditTime 时 MaxEditTime 不应为零")
}

// TestListTreePartialStillReportsMaxEditTime 验证 partial snapshot 也应填充 MaxEditTime
// （adapter 只报告值，是否推进由 application 决定）。
func TestListTreePartialStillReportsMaxEditTime(t *testing.T) {
	const (
		spaceID   = "spcEditP"
		rootToken = "wikcnEditP"
		page2     = "tok2"
	)
	const editMsRoot = "2000"
	api := newFakeAPI()
	api.wikiNodes[rootToken] = &larkwiki.Node{
		NodeToken: strPtr(rootToken), ObjToken: strPtr("doxcnRoot"),
		ObjType: strPtr("docx"), SpaceId: strPtr(spaceID),
		Title:       strPtr("根文档"),
		ObjEditTime: strPtr(editMsRoot),
	}
	key := wikiChildKey{spaceID: spaceID, parentToken: rootToken}
	setWikiPagedChildren(api, spaceID, rootToken,
		map[string][]*larkwiki.Node{
			"":    {wikiNodeWithEdit("wikcnA", "doxcnA", "docx", "", "文档A", "5000", false)},
			page2: {wikiNode("wikcnB", "doxcnB", "docx", "", "文档B", false)},
		},
		map[string]string{"": page2},
		map[string]bool{"": true},
	)
	if api.wikiChildrenPagedErr[key] == nil {
		api.wikiChildrenPagedErr[key] = map[string]error{}
	}
	api.wikiChildrenPagedErr[key][page2] = errors.New("第二页限流")

	snapshot, err := testConnector(api).ListTree(context.Background(), testConn(),
		model.SyncRoot{Kind: sourceport.SyncRootWikiNode, Token: rootToken})
	require.NoError(t, err)
	assert.False(t, snapshot.Complete)
	// 已收集节点中最大 EditTime = 5000ms。
	expected := parseEditTime(strPtr("5000"))
	assert.True(t, snapshot.MaxEditTime.Equal(expected),
		"partial snapshot 也应报告 MaxEditTime %v，got %v", expected, snapshot.MaxEditTime)
}

// TestListTreeFatalAuthErrorReturnsNoSnapshot 验证致命鉴权错误返回 error 且不返回可应用 snapshot。
func TestListTreeFatalAuthErrorReturnsNoSnapshot(t *testing.T) {
	const (
		spaceID   = "spcFatal"
		rootToken = "wikcnFatal"
	)
	api := newFakeAPI()
	api.wikiNodes[rootToken] = &larkwiki.Node{
		NodeToken: strPtr(rootToken), ObjToken: strPtr("doxcnRoot"),
		ObjType: strPtr("docx"), SpaceId: strPtr(spaceID),
	}
	api.listChildrenErr[rootToken] = fmt.Errorf("%w: token 失效", sourceport.ErrSourceUnavailable)

	snapshot, err := testConnector(api).ListTree(context.Background(), testConn(),
		model.SyncRoot{Kind: sourceport.SyncRootWikiNode, Token: rootToken})
	require.Error(t, err)
	assert.True(t, errors.Is(err, sourceport.ErrSourceUnavailable),
		"致命鉴权错误应传播 ErrSourceUnavailable，got %v", err)
	assert.Empty(t, snapshot.Nodes, "致命错误不应返回可应用 snapshot")
	assert.False(t, snapshot.Complete)
}

// TestListTreeEmptyAndCompleteIsLegal 验证「根下无可见节点」返回 Complete=true 的合法空快照。
func TestListTreeEmptyAndCompleteIsLegal(t *testing.T) {
	const (
		spaceID   = "spcEmpty"
		rootToken = "wikcnEmpty"
	)
	api := newFakeAPI()
	api.wikiNodes[rootToken] = &larkwiki.Node{
		NodeToken: strPtr(rootToken), ObjToken: strPtr("doxcnRoot"),
		ObjType: strPtr("docx"), SpaceId: strPtr(spaceID),
		Title: strPtr("根文档"),
	}
	// 根下无子节点。
	api.wikiChildren[wikiChildKey{spaceID, rootToken}] = nil

	snapshot, err := testConnector(api).ListTree(context.Background(), testConn(),
		model.SyncRoot{Kind: sourceport.SyncRootWikiNode, Token: rootToken})
	require.NoError(t, err)
	assert.True(t, snapshot.Complete, "空且完整的快照应 Complete=true")
	// 根节点自身应在结果中（walkWiki 总会先放入根节点）。
	require.Len(t, snapshot.Nodes, 1, "根节点应被返回")
}

// TestFetchStopsAtMaxContentBytes 验证内容超过 MaxContentBytes 时返回 ErrSourceContentTooLarge。
func TestFetchStopsAtMaxContentBytes(t *testing.T) {
	const docID = "doxcnBig"
	api := newFakeAPI()
	api.docxContent[docID] = string(bytes.Repeat([]byte("x"), 1024))
	api.docxTitle[docID] = "大文档"

	_, err := testConnector(api).Fetch(context.Background(), testConn(), docID,
		sourceport.FetchOptions{MaxContentBytes: 128})
	require.Error(t, err)
	assert.True(t, errors.Is(err, sourceport.ErrSourceContentTooLarge),
		"超限应返回 ErrSourceContentTooLarge，got %v", err)
}

// TestFetchUnderLimitSucceeds 验证内容不超过 MaxContentBytes 时正常返回。
func TestFetchUnderLimitSucceeds(t *testing.T) {
	const docID = "doxcnSmall"
	api := newFakeAPI()
	api.docxContent[docID] = "# 小文档\n正文"
	api.docxTitle[docID] = "小文档"

	doc, err := testConnector(api).Fetch(context.Background(), testConn(), docID,
		sourceport.FetchOptions{MaxContentBytes: 1024})
	require.NoError(t, err)
	assert.Equal(t, "小文档", doc.Title)
	assert.Equal(t, "docx", doc.ObjType)
	assert.Contains(t, string(doc.Markdown), "# 小文档")
}

// TestFetchZeroOrUnsetLimitNoCap 验证 MaxContentBytes <= 0 时不设上限。
func TestFetchZeroOrUnsetLimitNoCap(t *testing.T) {
	const docID = "doxcnNoCap"
	api := newFakeAPI()
	api.docxContent[docID] = string(bytes.Repeat([]byte("y"), 2048))
	api.docxTitle[docID] = "不限文档"

	// 0 表示不限。
	doc, err := testConnector(api).Fetch(context.Background(), testConn(), docID,
		sourceport.FetchOptions{MaxContentBytes: 0})
	require.NoError(t, err, "MaxContentBytes<=0 不应受限")
	assert.Len(t, doc.Markdown, 2048)

	// 负数同样视为不限。
	doc, err = testConnector(api).Fetch(context.Background(), testConn(), docID,
		sourceport.FetchOptions{MaxContentBytes: -1})
	require.NoError(t, err, "MaxContentBytes 负数也应视为不限")
	assert.Len(t, doc.Markdown, 2048)
}

// TestFetchExactlyAtLimit 验证内容字节数恰好等于上限时不算超限（边界）。
func TestFetchExactlyAtLimit(t *testing.T) {
	const docID = "doxcnExact"
	api := newFakeAPI()
	body := bytes.Repeat([]byte("z"), 256)
	api.docxContent[docID] = string(body)
	api.docxTitle[docID] = "恰好"

	doc, err := testConnector(api).Fetch(context.Background(), testConn(), docID,
		sourceport.FetchOptions{MaxContentBytes: int64(len(body))})
	require.NoError(t, err, "恰好等于上限不应超限")
	assert.Len(t, doc.Markdown, 256)
}
