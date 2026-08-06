package feishu

import (
	"context"
	"errors"
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
	listChildrenErr map[string]error // by parent token
	driveListErr    map[string]error // by folder token
	docxContentErr  map[string]error // by document id
	docxTitleErr    map[string]error // by document id

	// 计数器：校验调用次数。
	getNodeCalls      int
	listChildrenCalls int
	driveListCalls    int
	rawContentCalls   int
	getTitleCalls     int
}

func newFakeAPI() *fakeAPI {
	return &fakeAPI{
		wikiNodes:         map[string]*larkwiki.Node{},
		wikiChildren:      map[wikiChildKey][]*larkwiki.Node{},
		wikiChildrenPaged: map[wikiChildKey]map[string][]*larkwiki.Node{},
		wikiNextPage:      map[wikiChildKey]map[string]string{},
		wikiHasMore:       map[wikiChildKey]map[string]bool{},
		driveFiles:        map[string][]*larkdrive.File{},
		drivePages:        map[drivePageKey][]*larkdrive.File{},
		driveNext:         map[drivePageKey]string{},
		driveHasMore:      map[drivePageKey]bool{},
		docxContent:       map[string]string{},
		docxTitle:         map[string]string{},
		getNodeErr:        map[string]error{},
		listChildrenErr:   map[string]error{},
		driveListErr:      map[string]error{},
		docxContentErr:    map[string]error{},
		docxTitleErr:      map[string]error{},
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

	nodes, err := testConnector(api).ListTree(context.Background(), testConn(),
		model.SyncRoot{Kind: sourceport.SyncRootWikiNode, Token: rootToken})
	require.NoError(t, err)
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

	nodes, err := testConnector(api).ListTree(context.Background(), testConn(),
		model.SyncRoot{Kind: sourceport.SyncRootDriveFolder, Token: rootFolder})
	require.NoError(t, err)
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

	doc, err := testConnector(api).Fetch(context.Background(), testConn(), docID)
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

	doc, err := testConnector(api).Fetch(context.Background(), testConn(), docID)
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

// TestListTreeWikiPropagatesListChildrenError 验证 WikiListChildren 返回错误时被传播。
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
	api.listChildrenErr[rootToken] = errors.New("list children 500")

	_, err := testConnector(api).ListTree(context.Background(), testConn(),
		model.SyncRoot{Kind: sourceport.SyncRootWikiNode, Token: rootToken})
	require.Error(t, err)
	// list children 的原始错误应能在 err 链中被识别。
	assert.Contains(t, err.Error(), "list children 500")
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

	nodes, err := testConnector(api).ListTree(context.Background(), testConn(),
		model.SyncRoot{Kind: sourceport.SyncRootDriveFolder, Token: folder})
	require.NoError(t, err)
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
