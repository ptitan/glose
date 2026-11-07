// Package web holds the interface of Glose, embedded in the executable
// (D-16): no framework, no build step.
package web

import "embed"

// Files are the files of the interface.
//
//go:embed index.html app.js visuel.js icones.js brouillons.js donnees.js navigation.js recherche.js archives.js style.css
var Files embed.FS
