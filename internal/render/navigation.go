package render

import (
	"bytes"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
)

// resourceURL resolves a relative image against its Markdown file, not the
// server's root. URL escaping, query and fragment are preserved.
func resourceURL(file, raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Host != "" || u.Path == "" || strings.HasPrefix(u.Path, "/") {
		return raw
	}
	u.Path = path.Join(path.Dir(file), u.Path)
	u.RawPath = ""
	return u.String()
}

var anchorAttr = regexp.MustCompile(`([ \t\r\n])(id|href)="([^"]*)"`)
var generatedAnchor = regexp.MustCompile(`^gl-f\d+-(?:fn\d+|fnref\d+|footnotes|c\d+-footnotes)$`)

// AnchorID is the rendered identifier of an anchor in a separately rendered
// file. Keeping the original identifier intact also handles explicit IDs.
func AnchorID(file int, id string) string { return fmt.Sprintf("gl-f%d-a-%s", file, id) }

// namespaceAnchors keeps anchors unique when separately rendered files are
// joined. It only changes attributes, never prose, code or source positions.
func namespaceAnchors(h []byte, file int) []byte {
	return inTags(h, "", func(tag []byte) []byte {
		if !bytes.Contains(tag, []byte(`id="`)) && !bytes.Contains(tag, []byte(`href="#`)) {
			return tag
		}
		return anchorAttr.ReplaceAllFunc(tag, func(m []byte) []byte {
			p := anchorAttr.FindSubmatch(m)
			id := string(p[3])
			fragment := ""
			if string(p[2]) == "href" {
				if !strings.HasPrefix(id, "#") || len(id) < 2 {
					return m
				}
				fragment, id = "#", id[1:]
			}
			if id == "" || strings.HasPrefix(id, "gl-glose-f") || generatedAnchor.MatchString(id) {
				return m
			}
			return bytes.Join([][]byte{p[1], p[2], []byte(`="` + fragment + AnchorID(file, id) + `"`)}, nil)
		})
	})
}
