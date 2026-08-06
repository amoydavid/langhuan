package http

import (
	"context"
	stdhttp "net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/application/dto"
	"github.com/dajee/langhuan/internal/application/service"
	"github.com/dajee/langhuan/internal/domain/value"
)

// FAQDocumentHTTPService is the complete FAQ revision use-case contract.
type FAQDocumentHTTPService interface {
	Create(context.Context, service.CreateFAQDocumentInput) (*dto.FAQDocument, error)
	Update(context.Context, service.UpdateFAQDocumentInput) (*dto.FAQDocument, error)
	Get(context.Context, value.ResourceAccess, uuid.UUID, uuid.UUID) (*dto.FAQDocument, error)
}

type faqDocumentHandler struct {
	service FAQDocumentHTTPService
}

const faqInvalidJSONMessage = "请求 JSON 无效"

type createFAQRequest struct {
	Title     string   `json:"title"`
	Questions []string `json:"questions"`
	Answer    string   `json:"answer"`
}

type updateFAQRequest struct {
	BaseRevisionID uuid.UUID `json:"base_revision_id"`
	Questions      []string  `json:"questions"`
	Answer         string    `json:"answer"`
}

func (h faqDocumentHandler) create(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	knowledgeBaseID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "id 必须是有效 UUID")
		return
	}
	var request createFAQRequest
	if err := decodeStrictJSON(c, &request); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", faqInvalidJSONMessage)
		return
	}
	var createdBy *uuid.UUID
	if !authCtx.IsAPIKey() {
		id := authCtx.UserID
		createdBy = &id
	}
	result, err := h.service.Create(c.Request.Context(), service.CreateFAQDocumentInput{
		WorkspaceID: authCtx.WorkspaceID, KnowledgeBaseID: knowledgeBaseID,
		Access: authCtx.ResourceAccess(),
		Title:  request.Title, Questions: request.Questions, Answer: request.Answer, CreatedBy: createdBy,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusCreated, result)
}

func (h faqDocumentHandler) get(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	knowledgeBaseID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "id 必须是有效 UUID")
		return
	}
	documentID, err := uuid.Parse(c.Param("document_id"))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "document_id 必须是有效 UUID")
		return
	}
	result, err := h.service.Get(c.Request.Context(), authCtx.ResourceAccess(), knowledgeBaseID, documentID)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusOK, result)
}

func (h faqDocumentHandler) update(c *gin.Context) {
	authCtx, ok := authFromContext(c)
	if !ok {
		writeError(c, stdhttp.StatusForbidden, "forbidden", "forbidden")
		return
	}
	knowledgeBaseID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "id 必须是有效 UUID")
		return
	}
	documentID, err := uuid.Parse(c.Param("document_id"))
	if err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", "document_id 必须是有效 UUID")
		return
	}
	var request updateFAQRequest
	if err := decodeStrictJSON(c, &request); err != nil {
		writeError(c, stdhttp.StatusBadRequest, "validation_error", faqInvalidJSONMessage)
		return
	}
	var createdBy *uuid.UUID
	if !authCtx.IsAPIKey() {
		id := authCtx.UserID
		createdBy = &id
	}
	result, err := h.service.Update(c.Request.Context(), service.UpdateFAQDocumentInput{
		WorkspaceID: authCtx.WorkspaceID, KnowledgeBaseID: knowledgeBaseID, Access: authCtx.ResourceAccess(), DocumentID: documentID, BaseRevisionID: request.BaseRevisionID,
		Questions: request.Questions, Answer: request.Answer, CreatedBy: createdBy,
	})
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.JSON(stdhttp.StatusAccepted, result)
}
