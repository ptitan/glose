package render

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"path"
	"regexp"
	"strings"

	"github.com/ptitan/glose/internal/projection"
)

// Destination is a location in the project, never an arbitrary disk path.
type Destination struct {
	File   int    `json:"file"`
	Path   string `json:"path"`
	Anchor string `json:"anchor"`
}

var fileAnchorPrefix = regexp.MustCompile(`^gl-f\d+-a-`)
var fileWrapper = regexp.MustCompile(`<div\b[^>]*[ \t\r\n]id="gl-glose-f(\d+)"[^>]*>`)

// hasAnchor inspects tags only: a quoted id in inline code is plain prose.
func hasAnchor(h, id string) bool {
	for {
		start := strings.IndexByte(h, '<')
		if start < 0 {
			return false
		}
		end := strings.IndexByte(h[start:], '>')
		if end < 0 {
			return false
		}
		end += start + 1
		for _, attr := range anchorAttr.FindAllStringSubmatch(h[start:end], -1) {
			if attr[2] == "id" && attr[3] == html.EscapeString(id) {
				return true
			}
		}
		h = h[end:]
	}
}

// Resolve follows a rendered local link. A bare fragment first belongs to its
// source file; outside it, only a unique target is accepted. Unshown files are
// rendered on demand with the same renderer/cache and view as the document.
func (r *Renderer) Resolve(ctx context.Context, from, href string, view projection.View) (Destination, error) {
	u, err := url.Parse(href)
	if err != nil || u.IsAbs() || u.Host != "" || u.RawQuery != "" {
		return Destination{}, fmt.Errorf("lien local invalide")
	}
	indices := []int{}
	if u.Path != "" {
		p := path.Clean(path.Join(path.Dir(from), u.Path))
		if strings.HasPrefix(u.Path, "/") {
			p = path.Clean(strings.TrimPrefix(u.Path, "/"))
		}
		for i, f := range r.P.Files {
			if f == p {
				indices = append(indices, i)
			}
		}
	} else {
		for i, f := range r.P.Files {
			if f == from {
				indices = append(indices, i)
			}
		}
		for i, f := range r.P.Files {
			if f != from {
				indices = append(indices, i)
			}
		}
	}
	if len(indices) == 0 {
		return Destination{}, fmt.Errorf("ce fichier n'appartient pas au texte relu")
	}
	fragment := u.Fragment
	if u.Path == "" {
		fragment = strings.TrimPrefix(fileAnchorPrefix.ReplaceAllString(fragment, ""), "gl-")
	}
	var found []Destination
	var whole *Doc
	if !r.P.PerFile {
		whole, err = r.RenderView(ctx, view)
		if err != nil {
			return Destination{}, err
		}
	}
	for _, i := range indices {
		dst := Destination{File: i, Path: r.P.Files[i]}
		if fragment == "" {
			return dst, nil
		}
		id := "gl-" + fragment
		if r.P.PerFile {
			id = AnchorID(i, id)
		}
		h := ""
		if whole == nil {
			d, err := r.RenderPart(ctx, view, dst.Path)
			if err != nil {
				return Destination{}, err
			}
			h = d.HTML
		} else {
			// Each file has one wrapper, also in a project filter's output.
			wrappers := fileWrapper.FindAllStringSubmatchIndex(whole.HTML, -1)
			for k, w := range wrappers {
				if whole.HTML[w[2]:w[3]] != fmt.Sprint(i) {
					continue
				}
				end := len(whole.HTML)
				if k+1 < len(wrappers) {
					end = wrappers[k+1][0]
				}
				h = whole.HTML[w[0]:end]
				break
			}
		}
		if hasAnchor(h, id) {
			dst.Anchor = id
			if u.Path != "" || dst.Path == from {
				return dst, nil
			}
			found = append(found, dst)
		}
	}
	if len(found) == 1 {
		return found[0], nil
	}
	if len(found) > 1 {
		return Destination{}, fmt.Errorf("ce renvoi est ambigu : indiquez le fichier cible dans le lien")
	}
	return Destination{}, fmt.Errorf("la cible de ce renvoi est absente de la vue affichée")
}
