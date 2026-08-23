package web

import "embed"

// Files contains the reproducibly built independent administration UI.
//
//go:embed all:dist
var Files embed.FS
