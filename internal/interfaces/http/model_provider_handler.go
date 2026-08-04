package http

import (
	"context"
	"encoding/json"
	stdhttp "net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
)

// ModelProviderHTTPService is the handler-side contract for model connections.
type ModelProviderHTTPService interface {
	CreateWorkspace(context.Context, uuid.UUID, service.CreateModelProviderInput) (*dto.ModelProvider, error)
	CreatePlatform(context.Context, service.CreateModelProviderInput) (*dto.ModelProvider, error)
	ListWorkspace(context.Context, uuid.UUID) ([]*dto.ModelProvider, error)
	ListPlatform(context.Context) ([]*dto.ModelProvider, error)
	GetWorkspace(context.Context, uuid.UUID, uuid.UUID) (*dto.ModelProvider, error)
	GetPlatform(context.Context, uuid.UUID) (*dto.ModelProvider, error)
	UpdateWorkspace(context.Context, uuid.UUID, uuid.UUID, service.UpdateModelProviderInput) (*dto.ModelProvider, error)
	UpdatePlatform(context.Context, uuid.UUID, service.UpdateModelProviderInput) (*dto.ModelProvider, error)
	DeleteWorkspace(context.Context, uuid.UUID, uuid.UUID) error
	DeletePlatform(context.Context, uuid.UUID) error
	// SupportedProviders 返回当前可用的 provider 键列表，供前端渲染 Provider 选项。
	SupportedProviders() []string
}

// providerOptionsResponse 是 GET .../model-providers/options 的响应。
type providerOptionsResponse struct {
	SupportedProviders []string `json:"supported_providers"`
}

type createModelProviderRequest struct {
	Name        string          `json:"name"`
	DisplayName string          `json:"display_name"`
	Description string          `json:"description"`
	Provider    string          `json:"provider"`
	Config      json.RawMessage `json:"config"`
	Credentials json.RawMessage `json:"credentials"`
}

type updateModelProviderRequest struct {
	DisplayName *string            `json:"display_name"`
	Description *string            `json:"description"`
	Config      optionalRawMessage `json:"config"`
	Credentials optionalRawMessage `json:"credentials"`
	Status      *value.ModelStatus `json:"status"`
}

// optionalRawMessage preserves the difference between a missing PATCH field
// and an explicitly supplied JSON value, including null. Provider-specific
// strict decoders decide whether the supplied value is valid.
type optionalRawMessage struct {
	value json.RawMessage
	set   bool
}

func (m *optionalRawMessage) UnmarshalJSON(data []byte) error {
	m.set = true
	m.value = append(m.value[:0], data...)
	return nil
}

func (m optionalRawMessage) pointer() *json.RawMessage {
	if !m.set {
		return nil
	}
	value := append(json.RawMessage(nil), m.value...)
	return &value
}

type modelProviderHandler struct {
	service ModelProviderHTTPService
}

func (h modelProviderHandler) createWorkspace(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	req, ok := decodeModelProviderCreate(c)
	if !ok {
		return
	}
	result, err := h.service.CreateWorkspace(c.Request.Context(), authCtx.WorkspaceID, service.CreateModelProviderInput{
		ActorID: authCtx.UserID, Name: req.Name, DisplayName: req.DisplayName,
		Description: req.Description, Provider: req.Provider,
		Config: req.Config, Credentials: req.Credentials,
	})
	writeModelProviderResult(c, stdhttp.StatusCreated, result, err)
}

func (h modelProviderHandler) createPlatform(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	req, ok := decodeModelProviderCreate(c)
	if !ok {
		return
	}
	result, err := h.service.CreatePlatform(c.Request.Context(), service.CreateModelProviderInput{
		ActorID: authCtx.UserID, Name: req.Name, DisplayName: req.DisplayName,
		Description: req.Description, Provider: req.Provider,
		Config: req.Config, Credentials: req.Credentials,
	})
	writeModelProviderResult(c, stdhttp.StatusCreated, result, err)
}

func decodeModelProviderCreate(c *gin.Context) (createModelProviderRequest, bool) {
	var req createModelProviderRequest
	if err := decodeStrictJSON(c, &req); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "请求 JSON 无效")
		return req, false
	}
	return req, true
}

func (h modelProviderHandler) listWorkspace(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	items, err := h.service.ListWorkspace(c.Request.Context(), authCtx.WorkspaceID)
	writeModelProviderList(c, items, err)
}

func (h modelProviderHandler) listPlatform(c *gin.Context) {
	items, err := h.service.ListPlatform(c.Request.Context())
	writeModelProviderList(c, items, err)
}

// options 返回当前可用的 provider 键列表，供前端渲染 Provider 下拉选项。
func (h modelProviderHandler) options(c *gin.Context) {
	supported := h.service.SupportedProviders()
	if supported == nil {
		supported = []string{}
	}
	c.JSON(stdhttp.StatusOK, providerOptionsResponse{SupportedProviders: supported})
}

func writeModelProviderList(c *gin.Context, items []*dto.ModelProvider, err error) {
	if err != nil {
		writeServiceError(c, err)
		return
	}
	if items == nil {
		items = make([]*dto.ModelProvider, 0)
	}
	c.JSON(stdhttp.StatusOK, items)
}

func (h modelProviderHandler) getWorkspace(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	providerID, ok := parseUUIDParam(c, "provider_id")
	if !ok {
		return
	}
	result, err := h.service.GetWorkspace(c.Request.Context(), authCtx.WorkspaceID, providerID)
	writeModelProviderResult(c, stdhttp.StatusOK, result, err)
}

func (h modelProviderHandler) getPlatform(c *gin.Context) {
	providerID, ok := parseUUIDParam(c, "provider_id")
	if !ok {
		return
	}
	result, err := h.service.GetPlatform(c.Request.Context(), providerID)
	writeModelProviderResult(c, stdhttp.StatusOK, result, err)
}

func (h modelProviderHandler) updateWorkspace(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	providerID, ok := parseUUIDParam(c, "provider_id")
	if !ok {
		return
	}
	input, ok := decodeModelProviderUpdate(c)
	if !ok {
		return
	}
	result, err := h.service.UpdateWorkspace(c.Request.Context(), authCtx.WorkspaceID, providerID, input)
	writeModelProviderResult(c, stdhttp.StatusOK, result, err)
}

func (h modelProviderHandler) updatePlatform(c *gin.Context) {
	providerID, ok := parseUUIDParam(c, "provider_id")
	if !ok {
		return
	}
	input, ok := decodeModelProviderUpdate(c)
	if !ok {
		return
	}
	result, err := h.service.UpdatePlatform(c.Request.Context(), providerID, input)
	writeModelProviderResult(c, stdhttp.StatusOK, result, err)
}

func decodeModelProviderUpdate(c *gin.Context) (service.UpdateModelProviderInput, bool) {
	var req updateModelProviderRequest
	if err := decodeStrictJSON(c, &req); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "请求 JSON 无效")
		return service.UpdateModelProviderInput{}, false
	}
	return service.UpdateModelProviderInput{
		DisplayName: req.DisplayName, Description: req.Description,
		Config: req.Config.pointer(), Credentials: req.Credentials.pointer(), Status: req.Status,
	}, true
}

func (h modelProviderHandler) deleteWorkspace(c *gin.Context) {
	authCtx, ok := requireHandlerAuthContext(c)
	if !ok {
		return
	}
	providerID, ok := parseUUIDParam(c, "provider_id")
	if !ok {
		return
	}
	if err := h.service.DeleteWorkspace(c.Request.Context(), authCtx.WorkspaceID, providerID); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(stdhttp.StatusNoContent)
}

func (h modelProviderHandler) deletePlatform(c *gin.Context) {
	providerID, ok := parseUUIDParam(c, "provider_id")
	if !ok {
		return
	}
	if err := h.service.DeletePlatform(c.Request.Context(), providerID); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Status(stdhttp.StatusNoContent)
}

func writeModelProviderResult(c *gin.Context, status int, result *dto.ModelProvider, err error) {
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(status, result)
}

func requireHandlerAuthContext(c *gin.Context) (value.AuthContext, bool) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return value.AuthContext{}, false
	}
	return authCtx, true
}

func parseUUIDParam(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", name+" 必须是有效 UUID")
		return uuid.Nil, false
	}
	return id, true
}
