package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/ptitan/glose/internal/agents"
	"github.com/ptitan/glose/internal/store"
)

// Settings are the display preferences of the author. They are kept in the
// user configuration (not in a project): the same everywhere, and kept from
// one server to the next although the port changes.
type Settings struct {
	Font       string  `json:"font"`       // serif, georgia, sans, lisible, mono
	Size       int     `json:"size"`       // pixels
	LineHeight float64 `json:"lineHeight"` // multiple of the size
	Justify    bool    `json:"justify"`    // justified text
	Theme      string  `json:"theme"`      // system, light, dark
	TextWidth  int     `json:"textWidth"`  // width of the text column, in rem
}

// DefaultSettings are used when nothing has been saved.
var DefaultSettings = Settings{Font: "serif", Size: 17, LineHeight: 1.6, Justify: false, Theme: "system", TextWidth: 44}

var fonts = map[string]bool{"serif": true, "georgia": true, "sans": true, "lisible": true, "mono": true}
var themes = map[string]bool{"system": true, "light": true, "dark": true}

func (st Settings) valid() bool {
	return fonts[st.Font] && themes[st.Theme] && st.Size >= 12 && st.Size <= 30 &&
		st.LineHeight >= 1.1 && st.LineHeight <= 2.5 && st.TextWidth >= 28 && st.TextWidth <= 70
}

// settingsPath is ~/.config/glose/reglages.json (or the equivalent of the
// system); GLOSE_CONFIG_DIR replaces the directory (tests).
func settingsPath() (string, error) {
	dir, err := agents.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "reglages.json"), nil
}

// LoadSettings reads the saved settings, or the defaults.
func LoadSettings() Settings {
	st := DefaultSettings
	path, err := settingsPath()
	if err != nil {
		return st
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	var saved Settings
	if json.Unmarshal(data, &saved) == nil && saved.valid() {
		return saved
	}
	return st
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, LoadSettings())
}

func (s *Server) setSettings(w http.ResponseWriter, r *http.Request) {
	var st Settings
	if !decode(w, r, &st) {
		return
	}
	if !st.valid() {
		jsonError(w, http.StatusBadRequest, "settings", "réglages hors des limites")
		return
	}
	path, err := settingsPath()
	if err == nil {
		data, _ := json.MarshalIndent(st, "", "  ")
		err = store.WriteAtomic(path, data, 0o600)
		if errors.Is(err, store.ErrNotSynced) {
			err = nil // enregistrés ; seule la synchronisation du dossier a échoué
		}
	}
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "write", "réglages non enregistrés : "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, st)
}
