package http

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestFAQDocumentCreateMapsCompletePayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspaceID, userID, knowledgeBaseID := uuid.New(), uuid.New(), uuid.New()
	fake := &fakeFAQDocumentHTTPService{result: &dto.FAQDocument{Answer: "回答", Questions: []string{"问题"}}}
	handler := faqDocumentHandler{service: fake}
	router := gin.New()
	router.POST("/knowledge-bases/:id/documents/faq", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{WorkspaceID: workspaceID, UserID: userID})
		handler.create(c)
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/knowledge-bases/"+knowledgeBaseID.String()+"/documents/faq",
		bytes.NewBufferString(`{"title":"退款","questions":["如何退款？"],"answer":"请申请退款。"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status/body = %d %s", recorder.Code, recorder.Body.String())
	}
	if fake.createInput == nil || fake.createInput.WorkspaceID != workspaceID ||
		fake.createInput.KnowledgeBaseID != knowledgeBaseID || fake.createInput.CreatedBy == nil ||
		*fake.createInput.CreatedBy != userID || len(fake.createInput.Questions) != 1 {
		t.Fatalf("create input = %#v", fake.createInput)
	}
}

func TestFAQDocumentUpdateMapsRevisionConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	workspaceID, documentID, baseRevisionID := uuid.New(), uuid.New(), uuid.New()
	fake := &fakeFAQDocumentHTTPService{updateErr: domainerrors.ErrRevisionConflict}
	handler := faqDocumentHandler{service: fake}
	router := gin.New()
	router.PUT("/documents/:document_id/faq", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{WorkspaceID: workspaceID, UserID: uuid.New()})
		handler.update(c)
	})

	body := `{"base_revision_id":"` + baseRevisionID.String() + `","questions":["问题"],"answer":"回答"}`
	req := httptest.NewRequest(http.MethodPut, "/documents/"+documentID.String()+"/faq", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusConflict || !bytes.Contains(recorder.Body.Bytes(), []byte(`"code":"revision_conflict"`)) {
		t.Fatalf("status/body = %d %s", recorder.Code, recorder.Body.String())
	}
	if fake.updateInput == nil || fake.updateInput.DocumentID != documentID || fake.updateInput.BaseRevisionID != baseRevisionID {
		t.Fatalf("update input = %#v", fake.updateInput)
	}
}

func TestFAQDocumentCreateRejectsInvalidJSONWithStableMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &fakeFAQDocumentHTTPService{}
	handler := faqDocumentHandler{service: fake}
	router := gin.New()
	router.POST("/knowledge-bases/:id/documents/faq", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{WorkspaceID: uuid.New(), UserID: uuid.New()})
		handler.create(c)
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/knowledge-bases/"+uuid.NewString()+"/documents/faq",
		bytes.NewBufferString(`{"title":"FAQ","questions":["Q"],"answer":"A","unexpected":true}`),
	)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	assertFAQInvalidJSONResponse(t, recorder)
	if fake.createInput != nil {
		t.Fatalf("create service called with %#v", fake.createInput)
	}
}

func TestFAQDocumentUpdateRejectsInvalidJSONWithStableMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	fake := &fakeFAQDocumentHTTPService{}
	handler := faqDocumentHandler{service: fake}
	router := gin.New()
	router.PUT("/documents/:document_id/faq", func(c *gin.Context) {
		c.Set(authContextKey, value.AuthContext{WorkspaceID: uuid.New(), UserID: uuid.New()})
		handler.update(c)
	})
	req := httptest.NewRequest(
		http.MethodPut,
		"/documents/"+uuid.NewString()+"/faq",
		bytes.NewBufferString(`{"base_revision_id":"`+uuid.NewString()+`","questions":["Q"],"answer":"A","unexpected":true}`),
	)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	assertFAQInvalidJSONResponse(t, recorder)
	if fake.updateInput != nil {
		t.Fatalf("update service called with %#v", fake.updateInput)
	}
}

func assertFAQInvalidJSONResponse(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status/body = %d %s", recorder.Code, recorder.Body.String())
	}
	var body errorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "validation_error" || body.Error.Message != "请求 JSON 无效" {
		t.Fatalf("error = %#v", body.Error)
	}
}

type fakeFAQDocumentHTTPService struct {
	result         *dto.FAQDocument
	createInput    *service.CreateFAQDocumentInput
	updateInput    *service.UpdateFAQDocumentInput
	getWorkspaceID uuid.UUID
	getDocumentID  uuid.UUID
	updateErr      error
}

func (s *fakeFAQDocumentHTTPService) Create(_ context.Context, input service.CreateFAQDocumentInput) (*dto.FAQDocument, error) {
	s.createInput = &input
	return s.result, nil
}

func (s *fakeFAQDocumentHTTPService) Update(_ context.Context, input service.UpdateFAQDocumentInput) (*dto.FAQDocument, error) {
	s.updateInput = &input
	return s.result, s.updateErr
}

func (s *fakeFAQDocumentHTTPService) Get(_ context.Context, workspaceID, documentID uuid.UUID) (*dto.FAQDocument, error) {
	s.getWorkspaceID = workspaceID
	s.getDocumentID = documentID
	return s.result, nil
}
