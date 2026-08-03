//go:build web_embed

package webspa

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEmbeddedSPAContainsViteBundle(t *testing.T) {
	indexInfo, err := fs.Stat(SPA, "index.html")
	if err != nil {
		t.Fatalf("embedded SPA missing index.html: %v", err)
	}
	if indexInfo.IsDir() {
		t.Fatal("embedded SPA index.html is a directory")
	}

	assetsInfo, err := fs.Stat(SPA, "assets")
	if err != nil {
		t.Fatalf("embedded SPA missing assets directory: %v", err)
	}
	if !assetsInfo.IsDir() {
		t.Fatal("embedded SPA assets is not a directory")
	}

	var assetFound bool
	err = fs.WalkDir(SPA, "assets", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path != "assets" && !entry.IsDir() && strings.HasPrefix(path, "assets/") {
			assetFound = true
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded SPA assets: %v", err)
	}
	if !assetFound {
		t.Fatal("embedded SPA assets directory contains no files")
	}
}
