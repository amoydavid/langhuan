package http

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
)

// ModelHTTPService is the handler-side contract for configured models.
type ModelHTTPService interface {
	CreateWorkspace(context.Context, uuid.UUID, service.CreateModelInput) (*dto.Model, error)
	CreatePlatform(context.Context, service.CreateModelInput) (*dto.Model, error)
	ListWorkspace(context.Context, uuid.UUID, uuid.UUID) ([]*dto.Model, error)
	ListPlatform(context.Context, uuid.UUID) ([]*dto.Model, error)
	ListSelectableWorkspace(context.Context, uuid.UUID, value.ModelType, bool) ([]*dto.Model, error)
	GetWorkspace(context.Context, uuid.UUID, uuid.UUID) (*dto.Model, error)
	GetPlatform(context.Context, uuid.UUID) (*dto.Model, error)
	UpdateWorkspace(context.Context, uuid.UUID, uuid.UUID, service.UpdateModelInput) (*dto.Model, error)
	UpdatePlatform(context.Context, uuid.UUID, service.UpdateModelInput) (*dto.Model, error)
	DeleteWorkspace(context.Context, uuid.UUID, uuid.UUID) error
	DeletePlatform(context.Context, uuid.UUID) error
}

// ModelConnectionTestHTTPService is the handler-side live test contract.
type ModelConnectionTestHTTPService interface {
	TestWorkspace(context.Context, uuid.UUID, uuid.UUID) (*dto.ConnectionTestResult, error)
	TestPlatform(context.Context, uuid.UUID) (*dto.ConnectionTestResult, error)
}

type createModelRequest struct {
	Name        string          `json:"name"`
	DisplayName string          `json:"display_name"`
	Description string          `json:"description"`
	Type        value.ModelType `json:"type"`
	ModelName   string          `json:"model_name"`
	Dimensions  optionalInt     `json:"dimensions"`
	Parameters  json.RawMessage `json:"parameters"`
}

type updateModelRequest struct {
	DisplayName *string            `json:"display_name"`
	Description *string            `json:"description"`
	ModelName   *string            `json:"model_name"`
	Dimensions  optionalInt        `json:"dimensions"`
	Parameters  optionalRawMessage `json:"parameters"`
	Status      *value.ModelStatus `json:"status"`
}

type modelHandler struct {
	models      ModelHTTPService
	connections ModelConnectionTestHTTPService
}

func (h modelHandler) createWorkspace(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	providerID, ok := parseUUIDParam(c, "provider_id")
	if !ok {
		return
	}
	input, ok := decodeModelCreate(c, providerID, authCtx.UserID)
	if !ok {
		return
	}
	result, err := h.models.CreateWorkspace(c.Request.Context(), authCtx.WorkspaceID, input)
	writeModelResult(c, stdhttp.StatusCreated, result, err)
}

func (h modelHandler) createPlatform(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	providerID, ok := parseUUIDParam(c, "provider_id")
	if !ok {
		return
	}
	input, ok := decodeModelCreate(c, providerID, authCtx.UserID)
	if !ok {
		return
	}
	result, err := h.models.CreatePlatform(c.Request.Context(), input)
	writeModelResult(c, stdhttp.StatusCreated, result, err)
}

func decodeModelCreate(c *gin.Context, providerID, actorID uuid.UUID) (service.CreateModelInput, bool) {
	var req createModelRequest
	if err := decodeStrictJSON(c, &req); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "请求 JSON 无效")
		return service.CreateModelInput{}, false
	}
	return service.CreateModelInput{
		ProviderID: providerID, ActorID: actorID, Name: req.Name,
		DisplayName: req.DisplayName, Description: req.Description, Type: req.Type,
		ModelName: req.ModelName, Dimensions: req.Dimensions.pointer(), Parameters: req.Parameters,
	}, true
}

func (h modelHandler) listWorkspace(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	providerID, ok := parseUUIDParam(c, "provider_id")
	if !ok {
		return
	}
	items, err := h.models.ListWorkspace(c.Request.Context(), authCtx.WorkspaceID, providerID)
	writeModelList(c, items, err)
}

func (h modelHandler) listPlatform(c *gin.Context) {
	providerID, ok := parseUUIDParam(c, "provider_id")
	if !ok {
		return
	}
	items, err := h.models.ListPlatform(c.Request.Context(), providerID)
	writeModelList(c, items, err)
}

// listSelectable 返回当前 Workspace 可见的、指定类型的模型，供 Generation 选择。
// type 必须是 embedding 或 rerank；active 默认 false。
func (h modelHandler) listSelectable(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	rawType := strings.TrimSpace(c.Query("type"))
	modelType := value.ModelType(rawType)
	if modelType != value.ModelTypeEmbedding && modelType != value.ModelTypeRerank {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "type 必须是 embedding 或 rerank")
		return
	}
	active, err := parseOptionalBool(c.DefaultQuery("active", "false"))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "active 必须是布尔值")
		return
	}
	items, err := h.models.ListSelectableWorkspace(c.Request.Context(), authCtx.WorkspaceID, modelType, active)
	writeModelList(c, items, err)
}

func writeModelList(c *gin.Context, items []*dto.Model, err error) {
	if err != nil {
		writeServiceError(c, err)
		return
	}
	if items == nil {
		items = make([]*dto.Model, 0)
	}
	c.JSON(stdhttp.StatusOK, items)
}

func (h modelHandler) getWorkspace(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	modelID, ok := parseUUIDParam(c, "model_id")
	if !ok {
		return
	}
	result, err := h.models.GetWorkspace(c.Request.Context(), authCtx.WorkspaceID, modelID)
	writeModelResult(c, stdhttp.StatusOK, result, err)
}

func (h modelHandler) getPlatform(c *gin.Context) {
	modelID, ok := parseUUIDParam(c, "model_id")
	if !ok {
		return
	}
	result, err := h.models.GetPlatform(c.Request.Context(), modelID)
	writeModelResult(c, stdhttp.StatusOK, result, err)
}

func (h modelHandler) updateWorkspace(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	modelID, ok := parseUUIDParam(c, "model_id")
	if !ok {
		return
	}
	input, ok := decodeModelUpdate(c)
	if !ok {
		return
	}
	result, err := h.models.UpdateWorkspace(c.Request.Context(), authCtx.WorkspaceID, modelID, input)
	writeModelResult(c, stdhttp.StatusOK, result, err)
}

func (h modelHandler) updatePlatform(c *gin.Context) {
	modelID, ok := parseUUIDParam(c, "model_id")
	if !ok {
		return
	}
	input, ok := decodeModelUpdate(c)
	if !ok {
		return
	}
	result, err := h.models.UpdatePlatform(c.Request.Context(), modelID, input)
	writeModelResult(c, stdhttp.StatusOK, result, err)
}

func decodeModelUpdate(c *gin.Context) (service.UpdateModelInput, bool) {
	var req updateModelRequest
	if err := decodeStrictJSON(c, &req); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "请求 JSON 无效")
		return service.UpdateModelInput{}, false
	}
	return service.UpdateModelInput{
		DisplayName: req.DisplayName, Description: req.Description,
		ModelName: req.ModelName, Dimensions: req.Dimensions.pointer(),
		Parameters: req.Parameters.pointer(), Status: req.Status,
	}, true
}

func (h modelHandler) deleteWorkspace(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	modelID, ok := parseUUIDParam(c, "model_id")
	if !ok {
		return
	}
	if err := h.models.DeleteWorkspace(c.Request.Context(), authCtx.WorkspaceID, modelID); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(stdhttp.StatusNoContent)
}

func (h modelHandler) deletePlatform(c *gin.Context) {
	modelID, ok := parseUUIDParam(c, "model_id")
	if !ok {
		return
	}
	if err := h.models.DeletePlatform(c.Request.Context(), modelID); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(stdhttp.StatusNoContent)
}

func (h modelHandler) testWorkspace(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	modelID, ok := parseUUIDParam(c, "model_id")
	if !ok {
		return
	}
	result, err := h.connections.TestWorkspace(c.Request.Context(), authCtx.WorkspaceID, modelID)
	writeConnectionTestResult(c, result, err)
}

func (h modelHandler) testPlatform(c *gin.Context) {
	modelID, ok := parseUUIDParam(c, "model_id")
	if !ok {
		return
	}
	result, err := h.connections.TestPlatform(c.Request.Context(), modelID)
	writeConnectionTestResult(c, result, err)
}

func writeModelResult(c *gin.Context, status int, result *dto.Model, err error) {
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(status, result)
}

func writeConnectionTestResult(c *gin.Context, result *dto.ConnectionTestResult, err error) {
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, result)
}
