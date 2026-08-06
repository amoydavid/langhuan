package model

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestNewSourceConnectionRejectsEmptyCredentials(t *testing.T) {
	_, err := NewSourceConnection(NewSourceConnectionInput{
		WorkspaceID:           uuid.New(),
		Provider:              "feishu",
		Name:                  "主公司飞书",
		AppID:                 "cli_a1b2",
		CredentialsCiphertext: []byte{},
	})
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("want ErrValidation for empty ciphertext, got %v", err)
	}
}

func TestNewSourceConnectionRejectsUnknownProvider(t *testing.T) {
	_, err := NewSourceConnection(NewSourceConnectionInput{
		WorkspaceID:           uuid.New(),
		Provider:              "notion",
		Name:                  "x",
		AppID:                 "cli_a1b2",
		CredentialsCiphertext: []byte("cipher"),
	})
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("want ErrValidation for unknown provider, got %v", err)
	}
}

func TestNewSourceConnectionStoresAppIDInConfig(t *testing.T) {
	conn, err := NewSourceConnection(NewSourceConnectionInput{
		WorkspaceID:           uuid.New(),
		Provider:              " feishu ",
		Name:                  "  主公司飞书  ",
		AppID:                 "cli_a1b2",
		CredentialsCiphertext: []byte("cipher"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if conn.Provider != "feishu" {
		t.Fatalf("provider = %q, want feishu", conn.Provider)
	}
	if conn.Name != "主公司飞书" {
		t.Fatalf("name = %q", conn.Name)
	}
	if got := conn.Config["app_id"]; got != "cli_a1b2" {
		t.Fatalf("config.app_id = %v", got)
	}
	if conn.Status != "active" {
		t.Fatalf("status = %q", conn.Status)
	}
}

func TestNewKnowledgeBaseWithSourceFeishuRequiresConnection(t *testing.T) {
	_, err := NewKnowledgeBaseWithSource(uuid.New(), "x", "", uuid.New(), nil, map[string]any{},
		value.SourceTypeFeishuWiki, map[string]any{"root_token": "wikcnB"}, nil)
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("want ErrValidation for feishu without connection, got %v", err)
	}
}

func TestNewKnowledgeBaseWithSourceRejectsUnknownType(t *testing.T) {
	connID := uuid.New()
	_, err := NewKnowledgeBaseWithSource(uuid.New(), "x", "", uuid.New(), nil, map[string]any{},
		"unknown", map[string]any{}, &connID)
	if !errors.Is(err, domainerrors.ErrValidation) {
		t.Fatalf("want ErrValidation for unknown source type, got %v", err)
	}
}

func TestNewKnowledgeBaseDefaultsToUpload(t *testing.T) {
	kb, err := NewKnowledgeBase(uuid.New(), "kb", "", uuid.New(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if kb.SourceType != value.SourceTypeUpload {
		t.Fatalf("source_type = %q, want upload", kb.SourceType)
	}
	if kb.SourceConfig == nil {
		t.Fatal("source_config should default to empty map")
	}
}

func TestNewKnowledgeBaseWithSourceFeishuBindsConnection(t *testing.T) {
	connID := uuid.New()
	kb, err := NewKnowledgeBaseWithSource(uuid.New(), "飞书KB", "", uuid.New(), nil, map[string]any{},
		value.SourceTypeFeishuWiki, map[string]any{"root_token": "wikcnB"}, &connID)
	if err != nil {
		t.Fatal(err)
	}
	if kb.SourceType != value.SourceTypeFeishuWiki {
		t.Fatalf("source_type = %q", kb.SourceType)
	}
	if kb.SourceConnectionID == nil || *kb.SourceConnectionID != connID {
		t.Fatal("connection not bound")
	}
	if kb.SourceConfig["root_token"] != "wikcnB" {
		t.Fatal("source_config not stored")
	}
}
