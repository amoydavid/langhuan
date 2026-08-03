package http

import (
	"errors"
	"io/fs"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const spaIndex = "index.html"

func newSPAHandler(spaFS fs.FS) gin.HandlerFunc {
	if spaFS == nil {
		return nil
	}
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			c.Status(http.StatusNotFound)
			return
		}

		name, ok := validSPAPath(c.Request.URL.Path)
		if !ok {
			c.Status(http.StatusNotFound)
			return
		}

		info, err := fs.Stat(spaFS, name)
		if err == nil && !info.IsDir() {
			http.ServeFileFS(c.Writer, c.Request, spaFS, name)
			return
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			c.Status(http.StatusInternalServerError)
			return
		}

		if _, err := fs.Stat(spaFS, spaIndex); err != nil {
			c.Status(http.StatusInternalServerError)
			return
		}
		http.ServeFileFS(c.Writer, c.Request, spaFS, spaIndex)
	}
}

func validSPAPath(requestPath string) (string, bool) {
	if requestPath == "/" {
		return spaIndex, true
	}
	if !strings.HasPrefix(requestPath, "/") || strings.Contains(requestPath, `\`) {
		return "", false
	}

	name := strings.TrimPrefix(requestPath, "/")
	for _, segment := range strings.Split(name, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", false
		}
	}
	if !fs.ValidPath(name) {
		return "", false
	}
	return name, true
}

func isRESTAPIPath(requestPath string) bool {
	return pathHasPrefix(requestPath, "/api/v1")
}

func isNonSPAPath(requestPath string) bool {
	for _, prefix := range []string{
		"/mcp",
		"/healthz",
		"/auth",
		"/admin",
	} {
		if pathHasPrefix(requestPath, prefix) {
			return true
		}
	}
	return false
}

func pathHasPrefix(requestPath, prefix string) bool {
	return requestPath == prefix || strings.HasPrefix(requestPath, prefix+"/")
}
