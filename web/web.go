// Package web embeds the frontend: plain HTML, CSS and vanilla JavaScript.
// There is no build step and no dependency; the files are compiled into the
// binary and served from memory.
package web

import (
	"embed"
	"io/fs"
)

//go:embed index.html style.css app.js
var files embed.FS

// FS returns the embedded frontend filesystem.
func FS() fs.FS { return files }
