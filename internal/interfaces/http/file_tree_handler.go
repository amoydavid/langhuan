package http

import (
	"context"
	stdhttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
)

// FileTreeHTTPService is the File-tree handler use-case contract.
type FileTreeHTTPService interface {
	List(context.Context, value.ResourceAccess, uuid.UUID) (*dto.FileTree, error)
	CreateFolder(context.Context, service.CreateFileTreeFolderInput) (*dto.FileTreeNode, error)
	Update(context.Context, service.UpdateFileTreeNodeInput) error
	Delete(context.Context, value.ResourceAccess, uuid.UUID, uuid.UUID) error
}

type fileTreeHandler struct{ service FileTreeHTTPService }

type createFileTreeFolderRequest struct {
	ParentID uuid.UUID `json:"parent_id"`
	Name     string    `json:"name"`
}

type updateFileTreeNodeRequest struct {
	Name     *string    `json:"name"`
	ParentID *uuid.UUID `json:"parent_id"`
}

func (h fileTreeHandler) list(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	knowledgeBaseID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	tree, err := h.service.List(c.Request.Context(), authCtx.ResourceAccess(), knowledgeBaseID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, tree)
}

func (h fileTreeHandler) createFolder(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	knowledgeBaseID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	var request createFileTreeFolderRequest
	if err := decodeStrictJSON(c, &request); err != nil || request.ParentID == uuid.Nil || strings.TrimSpace(request.Name) == "" {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "parent_id 与 name 必填")
		return
	}
	node, err := h.service.CreateFolder(c.Request.Context(), service.CreateFileTreeFolderInput{
		WorkspaceID: authCtx.WorkspaceID, KnowledgeBaseID: knowledgeBaseID, Access: authCtx.ResourceAccess(),
		ParentID: request.ParentID, Name: request.Name,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusCreated, node)
}

func (h fileTreeHandler) update(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	knowledgeBaseID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	nodeID, ok := parseUUIDParam(c, "node_id")
	if !ok {
		return
	}
	var request updateFileTreeNodeRequest
	if err := decodeStrictJSON(c, &request); err != nil || (request.Name == nil && request.ParentID == nil) {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "name 与 parent_id 至少提供一个")
		return
	}
	if request.ParentID != nil && *request.ParentID == uuid.Nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "parent_id 必须是有效 UUID")
		return
	}
	err := h.service.Update(c.Request.Context(), service.UpdateFileTreeNodeInput{
		WorkspaceID: authCtx.WorkspaceID, KnowledgeBaseID: knowledgeBaseID, Access: authCtx.ResourceAccess(),
		NodeID: nodeID, Name: request.Name, ParentID: request.ParentID,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(stdhttp.StatusNoContent)
}

func (h fileTreeHandler) delete(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	knowledgeBaseID, ok := parseUUIDParam(c, "id")
	if !ok {
		return
	}
	nodeID, ok := parseUUIDParam(c, "node_id")
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), authCtx.ResourceAccess(), knowledgeBaseID, nodeID); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(stdhttp.StatusNoContent)
}
