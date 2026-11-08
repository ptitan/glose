package cli

import (
	"flag"
	"io"

	"github.com/ptitan/glose/internal/home"
	"github.com/ptitan/glose/internal/navette"
)

// NavetteSection is the section « Navette Glose » (navette.Section).
const NavetteSection = navette.Section

// Markers around the section in the instructions file (home.Init).
const (
	sectionStart = home.SectionStart
	sectionEnd   = home.SectionEnd
)

func cmdInit(args []string, stdout, stderr io.Writer) int {
	var print bool
	var to, kind string
	path, ok := parse("init", args, stderr, func(fs *flag.FlagSet) {
		fs.BoolVar(&print, "print", false, "")
		fs.StringVar(&to, "to", "", "")
		fs.StringVar(&kind, "type", "", "")
	})
	if !ok {
		return ExitUsage
	}
	if print {
		if kind != "" {
			return usageError(stderr, "init : --print n'écrit rien, --type n'a pas de sens avec lui")
		}
		io.WriteString(stdout, NavetteSection)
		return ExitOK
	}
	if err := home.Init(path, to, kind, stdout, stderr); err != nil {
		return failure(stderr, err)
	}
	return ExitOK
}
