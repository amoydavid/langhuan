package service

import (
	"reflect"
	"testing"
)

func TestWorkspaceStoresExposeUseCaseLocalTransactions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		store      any
		wantMethod string
	}{
		{name: "knowledge base create", store: (*KnowledgeBaseCreateStore)(nil), wantMethod: "WithinWorkspace"},
		{name: "document ingest", store: (*DocumentIngestStore)(nil), wantMethod: "WithinWorkspace"},
		{name: "file tree", store: (*FileTreeStore)(nil), wantMethod: "WithinWorkspace"},
		{name: "FAQ revision", store: (*FAQRevisionStore)(nil), wantMethod: "WithinWorkspace"},
		{name: "document publish", store: (*DocumentPublishStore)(nil), wantMethod: "WithinWorkspace"},
		{name: "chunk edit", store: (*ChunkEditStore)(nil), wantMethod: "WithinWorkspace"},
		{name: "index generation", store: (*IndexGenerationStore)(nil), wantMethod: "WithinWorkspace"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			storeType := reflect.TypeOf(tt.store).Elem()
			if storeType.Kind() != reflect.Interface {
				t.Fatalf("%s kind = %s, want interface", storeType, storeType.Kind())
			}
			method, ok := storeType.MethodByName(tt.wantMethod)
			if !ok {
				t.Fatalf("%s missing %s", storeType, tt.wantMethod)
			}
			if method.Type.NumIn() != 3 || method.Type.NumOut() != 1 {
				t.Fatalf("%s.%s signature = %s", storeType, method.Name, method.Type)
			}
		})
	}
}
