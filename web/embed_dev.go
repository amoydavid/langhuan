//go:build !web_embed

package webspa

import "io/fs"

// SPA is nil in development builds so Vite remains the frontend server.
var SPA fs.FS
