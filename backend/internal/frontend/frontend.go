package frontend

import (
	"embed"
	"io/fs"
)

// FS holds the embedded frontend dist when the binary was built with assets
// (i.e. frontend/dist/ was populated before go build). When building without
// assets the directory contains only .gitkeep and HasAssets() returns false,
// causing the server to fall back to the FRONTEND_DIR disk path instead.
//
//go:embed all:dist
var FS embed.FS

// HasAssets reports whether the embedded dist directory contains a built
// frontend (index.html present). Returns false for dev builds that skipped
// the frontend build step.
func HasAssets() bool {
	f, err := FS.Open("dist/index.html")
	if err != nil {
		return false
	}
	f.Close()
	return true
}

// Sub returns an fs.FS rooted at the dist/ subdirectory, ready to pass to
// http.FileServer(http.FS(…)).
func Sub() (fs.FS, error) {
	return fs.Sub(FS, "dist")
}
