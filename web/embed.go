// Package web holds the interface of Glose, embedded in the executable
// (D-16): no framework, no build step.
package web

import "embed"

// Files are the files of the interface.
//
//go:embed index.html accueil.html accueil.js reglages-agents.html reglages-agents.js marge.js edition.js export.js espace.js themes.js app.js colonne.js saisie.js historique.js reglages.js navette.js interface.js visuel.js icones.js brouillons.js donnees.js selection.js navigation.js recherche.js archives.js agents.js style.css export.css themes.css polices/*.woff2 polices/LICENCE-*.txt
var Files embed.FS
