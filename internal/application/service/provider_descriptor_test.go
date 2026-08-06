package service

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	domainerrors "github.com/dajee/langhuan/internal/domain/errors"
	"github.com/dajee/langhuan/internal/domain/value"
)

func TestProviderDescriptorRegistrySupportsMultipleCapabilities(t *testing.T) {
	t.Parallel()
	registry, err := NewProviderDescriptorRegistry(ProviderDescriptor{
		Key:              " SILICONFLOW ",
		Capabilities:     []value.ProviderCapability{value.CapabilityRerank, value.CapabilityEmbedding, value.CapabilityRerank},
		CredentialFields: []string{"api_key"},
		DecodeProvider: func(value.ModelScope, json.RawMessage, json.RawMessage) (ProviderDecodeResult, error) {
			return ProviderDecodeResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := registry.Descriptor("siliconflow")
	if err != nil {
		t.Fatal(err)
	}
	want := []value.ProviderCapability{value.CapabilityEmbedding, value.CapabilityRerank}
	if !reflect.DeepEqual(got.Capabilities, want) {
		t.Fatalf("capabilities = %#v, want %#v", got.Capabilities, want)
	}
	if !registry.SupportsModelType("siliconflow", value.ModelTypeEmbedding) ||
		!registry.SupportsModelType("siliconflow", value.ModelTypeRerank) {
		t.Fatalf("descriptor does not support both model types: %#v", got)
	}
	if registry.SupportsModelType("siliconflow", value.ModelTypeLLM) {
		t.Fatal("descriptor must not expose unsupported LLM capability")
	}
}

func TestProviderDescriptorRegistryRejectsInvalidAndDuplicateDescriptors(t *testing.T) {
	t.Parallel()
	valid := ProviderDescriptor{
		Key:          "openai",
		Capabilities: []value.ProviderCapability{value.CapabilityEmbedding},
		DecodeProvider: func(value.ModelScope, json.RawMessage, json.RawMessage) (ProviderDecodeResult, error) {
			return ProviderDecodeResult{}, nil
		},
	}
	tests := []struct {
		name        string
		descriptors []ProviderDescriptor
	}{
		{name: "duplicate", descriptors: []ProviderDescriptor{valid, valid}},
		{name: "empty key", descriptors: []ProviderDescriptor{{Capabilities: valid.Capabilities, DecodeProvider: valid.DecodeProvider}}},
		{name: "empty capabilities", descriptors: []ProviderDescriptor{{Key: "empty", DecodeProvider: valid.DecodeProvider}}},
		{name: "nil decoder", descriptors: []ProviderDescriptor{{Key: "nil", Capabilities: valid.Capabilities}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewProviderDescriptorRegistry(tt.descriptors...); err == nil {
				t.Fatal("expected descriptor validation error")
			}
		})
	}
}

func TestProviderDescriptorRegistryReturnsUnsupportedProvider(t *testing.T) {
	t.Parallel()
	registry, err := NewProviderDescriptorRegistry()
	if err != nil {
		t.Fatal(err)
	}
	_, err = registry.Descriptor("missing")
	if !errors.Is(err, domainerrors.ErrUnsupportedProvider) {
		t.Fatalf("error = %v", err)
	}
}

func TestProviderDescriptorRegistryAcceptsExtensibleCapabilities(t *testing.T) {
	t.Parallel()
	registry, err := NewProviderDescriptorRegistry(ProviderDescriptor{
		Key:          "future-provider",
		Capabilities: []value.ProviderCapability{"llm", "asr_v2"},
		DecodeProvider: func(value.ModelScope, json.RawMessage, json.RawMessage) (ProviderDecodeResult, error) {
			return ProviderDecodeResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := registry.Descriptor("future-provider")
	if err != nil {
		t.Fatal(err)
	}
	want := []value.ProviderCapability{"asr_v2", "llm"}
	if !reflect.DeepEqual(got.Capabilities, want) {
		t.Fatalf("capabilities = %#v, want %#v", got.Capabilities, want)
	}
}

func TestProviderDescriptorOptionsExposeModelCatalog(t *testing.T) {
	t.Parallel()
	registry, err := NewProviderDescriptorRegistry(ProviderDescriptor{
		Key: "catalog-provider", Capabilities: []value.ProviderCapability{"embedding"}, ModelCatalog: &recordingModelCatalog{},
		DecodeProvider: func(value.ModelScope, json.RawMessage, json.RawMessage) (ProviderDecodeResult, error) {
			return ProviderDecodeResult{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	options := registry.Options()
	if len(options) != 1 || !options[0].ModelCatalog {
		t.Fatalf("options = %#v", options)
	}
}

func TestProviderDescriptorRegistryRejectsInvalidCapabilityIdentifier(t *testing.T) {
	t.Parallel()
	_, err := NewProviderDescriptorRegistry(ProviderDescriptor{
		Key:          "future-provider",
		Capabilities: []value.ProviderCapability{"LLM chat"},
		DecodeProvider: func(value.ModelScope, json.RawMessage, json.RawMessage) (ProviderDecodeResult, error) {
			return ProviderDecodeResult{}, nil
		},
	})
	if err == nil {
		t.Fatal("expected invalid capability identifier error")
	}
}
