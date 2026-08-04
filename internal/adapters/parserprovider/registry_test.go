package parserprovider

import (
	"context"
	"testing"

	parserproviderport "github.com/dajee/langhuan/internal/ports/parserprovider"
)

type fakeFactory struct {
	providerName string
	fields       []string
}

func (f *fakeFactory) Provider() string           { return f.providerName }
func (f *fakeFactory) CredentialFields() []string { return f.fields }
func (f *fakeFactory) DecodeProvider(parserproviderport.ProviderDecodeInput) (map[string]any, []byte, error) {
	return map[string]any{"ok": true}, []byte(`{}`), nil
}
func (f *fakeFactory) NewClient(context.Context, parserproviderport.ClientInput) (parserproviderport.ParserClient, error) {
	return nil, nil
}

func TestRegistryRoutesByProviderName(t *testing.T) {
	registry, err := NewRegistry(&fakeFactory{providerName: "mineru", fields: []string{"token"}})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	got, err := registry.Factory("mineru")
	if err != nil {
		t.Fatalf("Factory() error = %v", err)
	}
	if got.Provider() != "mineru" {
		t.Fatalf("Provider() = %q", got.Provider())
	}
}

func TestRegistrySupports(t *testing.T) {
	registry, err := NewRegistry(&fakeFactory{providerName: "mineru"})
	if err != nil {
		t.Fatal(err)
	}
	if !registry.Supports("mineru") {
		t.Fatal("Supports(mineru) = false")
	}
	if registry.Supports("unknown") {
		t.Fatal("Supports(unknown) = true")
	}
}

func TestRegistryUnknownProviderErrors(t *testing.T) {
	registry, err := NewRegistry(&fakeFactory{providerName: "mineru"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = registry.Factory("nonexistent")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestRegistryDuplicateProviderErrors(t *testing.T) {
	_, err := NewRegistry(
		&fakeFactory{providerName: "mineru"},
		&fakeFactory{providerName: "mineru"},
	)
	if err == nil {
		t.Fatal("expected duplicate provider error")
	}
}

func TestRegistryRejectsEmptyProvider(t *testing.T) {
	_, err := NewRegistry(&fakeFactory{providerName: ""})
	if err == nil {
		t.Fatal("expected empty provider error")
	}
}

func TestRegistryRejectsNilFactory(t *testing.T) {
	var nilFactory *fakeFactory
	_, err := NewRegistry(nilFactory)
	if err == nil {
		t.Fatal("expected nil factory error")
	}
}

// 确保 fakeFactory 实现 Factory 接口
var _ parserproviderport.Factory = (*fakeFactory)(nil)
