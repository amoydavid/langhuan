package service

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/dajee/langhuan/internal/domain/model"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestSelectMinerUPrefersNewest(t *testing.T) {
	ctx := context.Background()
	workspaceID := uuid.New()

	older := &model.ModelProvider{
		ID:       uuid.New(),
		Provider: "mineru",
		Status:   value.ModelStatusActive,
		Scope:    value.ModelScopeWorkspace,
	}
	newer := &model.ModelProvider{
		ID:       uuid.New(),
		Provider: "mineru",
		Status:   value.ModelStatusActive,
		Scope:    value.ModelScopePlatform,
	}
	// newer is actually newer
	newer.CreatedAt = older.CreatedAt.Add(1000000)
	// 设置可解密的 ciphertext（fakeCredentialCipher 要求 cipher:{id}:{plaintext} 格式）
	older.CredentialsCiphertext = []byte("cipher:" + older.ID.String() + ":old-creds")
	newer.CredentialsCiphertext = []byte("cipher:" + newer.ID.String() + ":new-creds")

	repo := newFakeModelProviderRepository()
	repo.items[older.ID] = older
	repo.items[newer.ID] = newer

	selector := NewParserProviderSelector(repo, fakeCredentialCipher{})
	got, err := selector.SelectMinerU(ctx, workspaceID)
	if err != nil {
		t.Fatalf("SelectMinerU() error = %v", err)
	}
	if got.Provider.ID != newer.ID {
		t.Fatalf("selected provider = %v, want newest", got.Provider.ID)
	}
	if string(got.CredentialsJSON) != "new-creds" {
		t.Fatalf("credentials = %q, want new-creds", string(got.CredentialsJSON))
	}
}

func TestSelectMinerUSkipsInactive(t *testing.T) {
	ctx := context.Background()
	workspaceID := uuid.New()

	disabled := &model.ModelProvider{
		ID:       uuid.New(),
		Provider: "mineru",
		Status:   value.ModelStatus("disabled"),
	}
	repo := newFakeModelProviderRepository()
	repo.items[disabled.ID] = disabled

	selector := NewParserProviderSelector(repo, fakeCredentialCipher{})
	_, err := selector.SelectMinerU(ctx, workspaceID)
	if err == nil {
		t.Fatal("expected error when only disabled provider exists")
	}
}

func TestSelectMinerUSkipsNonMinerU(t *testing.T) {
	ctx := context.Background()
	workspaceID := uuid.New()

	openai := &model.ModelProvider{
		ID:       uuid.New(),
		Provider: "openai",
		Status:   value.ModelStatusActive,
	}
	repo := newFakeModelProviderRepository()
	repo.items[openai.ID] = openai

	selector := NewParserProviderSelector(repo, fakeCredentialCipher{})
	_, err := selector.SelectMinerU(ctx, workspaceID)
	if err == nil {
		t.Fatal("expected error when no mineru provider exists")
	}
}
