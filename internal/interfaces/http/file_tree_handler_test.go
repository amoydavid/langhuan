package http

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestFileTreePatchMapsCycleToStableConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspaceID, knowledgeBaseID, nodeID, parentID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	fake := &fakeFileTreeHTTPService{updateErr: domainerrors.ErrFileTreeCycle}
	handler := fileTreeHandler{service: fake}
	router := gin.New()
	router.PATCH("/knowledge-bases/:id/file-tree/nodes/:node_id", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{WorkspaceID: workspaceID})
		handler.update(c)
	})

	body := []byte(`{"parent_id":"` + parentID.String() + `"}`)
	req := httptest.NewRequest(http.MethodPatch, "/knowledge-bases/"+knowledgeBaseID.String()+"/file-tree/nodes/"+nodeID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict || !bytes.Contains(rec.Body.Bytes(), []byte(`"code":"file_tree_cycle"`)) {
		t.Fatalf("status/body = %d %s", rec.Code, rec.Body.String())
	}
	if fake.updateInput == nil || fake.updateInput.NodeID != nodeID || fake.updateInput.ParentID == nil || *fake.updateInput.ParentID != parentID {
		t.Fatalf("update input = %#v", fake.updateInput)
	}
}

type fakeFileTreeHTTPService struct {
	updateInput *service.UpdateFileTreeNodeInput
	updateErr   error
}

func (s *fakeFileTreeHTTPService) List(context.Context, uuid.UUID, uuid.UUID) (*dto.FileTree, error) {
	return nil, nil
}

func (s *fakeFileTreeHTTPService) CreateFolder(context.Context, service.CreateFileTreeFolderInput) (*dto.FileTreeNode, error) {
	return nil, nil
}

func (s *fakeFileTreeHTTPService) Update(_ context.Context, input service.UpdateFileTreeNodeInput) error {
	s.updateInput = &input
	return s.updateErr
}

func (s *fakeFileTreeHTTPService) Delete(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error {
	return nil
}
