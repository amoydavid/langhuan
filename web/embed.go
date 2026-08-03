//go:build web_embed

package webspa

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// SPA contains the production Vite bundle rooted at dist.
var SPA = mustSub(embedded, "dist")

func mustSub(root fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(root, dir)
	if err != nil {
		panic("加载内嵌 Web SPA 失败: " + err.Error())
	}
	return sub
}
