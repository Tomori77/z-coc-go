// Package web exposes the embedded static frontend. The embedded tree is
// rooted at this directory so it can be served directly at the URL root.
package web

import "embed"

//go:embed index.html 404.html favicon.svg manifest.json sw.js assets Workshop Library Admin
var FS embed.FS
