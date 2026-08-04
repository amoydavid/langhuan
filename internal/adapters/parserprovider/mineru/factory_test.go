package mineru

import (
	"encoding/json"
	"errors"
	"testing"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	parserproviderport "github.com/dajee/langhuan/internal/ports/parserprovider"
)

func TestFactoryProviderName(t *testing.T) {
	f := NewFactory()
	if f.Provider() != "mineru" {
		t.Fatalf("Provider() = %q, want mineru", f.Provider())
	}
}

func TestFactoryCredentialFields(t *testing.T) {
	f := NewFactory()
	fields := f.CredentialFields()
	if len(fields) != 1 || fields[0] != "token" {
		t.Fatalf("CredentialFields() = %v, want [token]", fields)
	}
}

func TestFactoryDecodeProviderValid(t *testing.T) {
	f := NewFactory()
	config, creds, err := f.DecodeProvider(parserproviderport.ProviderDecodeInput{
		Config:      json.RawMessage(`{"base_url":"https://mineru.net","model_version":"vlm"}`),
		Credentials: json.RawMessage(`{"token":"test-token-abc"}`),
	})
	if err != nil {
		t.Fatalf("DecodeProvider() error = %v", err)
	}
	if config["base_url"] != "https://mineru.net" {
		t.Fatalf("base_url = %v", config["base_url"])
	}
	if config["model_version"] != "vlm" {
		t.Fatalf("model_version = %v", config["model_version"])
	}

	var parsed mineruCredentials
	if err := json.Unmarshal(creds, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Token != "test-token-abc" {
		t.Fatalf("token = %q", parsed.Token)
	}
}

func TestFactoryDecodeProviderDefaultsBaseURL(t *testing.T) {
	f := NewFactory()
	config, _, err := f.DecodeProvider(parserproviderport.ProviderDecodeInput{
		Config:      json.RawMessage(`{}`),
		Credentials: json.RawMessage(`{"token":"x"}`),
	})
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if config["base_url"] != "https://mineru.net" {
		t.Fatalf("default base_url = %v", config["base_url"])
	}
	if config["model_version"] != "vlm" {
		t.Fatalf("default model_version = %v", config["model_version"])
	}
}

func TestFactoryDecodeProviderRejectsEmptyToken(t *testing.T) {
	f := NewFactory()
	_, _, err := f.DecodeProvider(parserproviderport.ProviderDecodeInput{
		Config:      json.RawMessage(`{}`),
		Credentials: json.RawMessage(`{"token":""}`),
	})
	if err == nil {
		t.Fatal("expected error for empty token")
	}
	if !errors.Is(err, domainerrors.ErrCredentialsRequired) {
		t.Fatalf("error = %v, want ErrCredentialsRequired", err)
	}
}

func TestFactoryDecodeProviderRejectsBadConfig(t *testing.T) {
	f := NewFactory()
	_, _, err := f.DecodeProvider(parserproviderport.ProviderDecodeInput{
		Config:      json.RawMessage(`{invalid`),
		Credentials: json.RawMessage(`{"token":"x"}`),
	})
	if err == nil {
		t.Fatal("expected error for invalid config JSON")
	}
	if !errors.Is(err, domainerrors.ErrInvalidProviderConfig) {
		t.Fatalf("error = %v, want ErrInvalidProviderConfig", err)
	}
}
