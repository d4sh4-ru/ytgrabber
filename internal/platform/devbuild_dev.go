//go:build dev

package platform

// IsDevBuild is true exactly when compiled via `wails dev`, which always
// passes the "dev" build tag (see wails' own internal app_dev.go — this
// mirrors that mechanism rather than inventing a new one).
const IsDevBuild = true
