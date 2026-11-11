package cli

import (
	"strconv"
	"strings"

	"github.com/ptitan/glose/internal/project"
)

// profileHelpText is printed by « glose help profile »: everything an author,
// or an agent that only has the executable, needs to write a profile and a
// rendering filter (docs/ARCHITECTURE.md, « Profil » and « contrat de
// provenance »). The example filter and stylesheet are tested
// (TestProfileHelpExampleRenders): they must keep working as printed.
var profileHelpText = strings.NewReplacer(
	"{{FILTER}}", indent(exampleFilter),
	"{{CSS}}", indent(exampleCSS),
).Replace(`Le profil glose.json

Facultatif, à la racine du projet. Tous les champs sont facultatifs ; un
champ inconnu est refusé. Les chemins sont relatifs au projet, séparés par
des barres obliques, et ne peuvent pas en sortir. « glose init » crée un
profil : les fichiers de la racine, puis chaque sous-dossier, à trier.

  {
    "title": "Économie sociale et solidaire",
    "files": ["introduction.md", "chapitres/", "annexes/*.md"],
    "exclude": ["chapitres/brouillons"],
    "filter": "glose/rendu.lua",
    "css": "glose/rendu.css",
    "resources": ["img"]
  }

  title      titre affiché (par défaut : le nom du dossier)
  files      ce qui est relu, dans l'ordre : des fichiers, des dossiers, des
             motifs (par défaut : « . », tout le projet)
  exclude    dossiers, fichiers ou motifs écartés de ce que trouvent les
             dossiers et les motifs de « files »
  filter     filtre Lua de pandoc propre au texte, pour l'affichage (voir
             ci-dessous ; par défaut : le rendu HTML de pandoc)
  filterPerFile  vrai si le filtre traite chaque fichier à part (aucune
             numérotation d'un fichier à l'autre) : le HTML est alors fait et
             gardé fichier par fichier, ce qui accélère beaucoup un gros
             projet (par défaut : faux ; sans filtre, toujours par fichier)
  css        feuille de style .css ajoutée à celle de Glose
  resources  dossiers dont les images peuvent être affichées (par défaut :
             tout le projet)
  pandoc     programme pandoc à utiliser (par défaut : pandoc)
  layout     mise en page sans filtre Lua (D-66), appliquée par Glose :
               "layout": {
                 "numbers": { "within": "—", "skipItalic": true },
                 "boxes": [ { "prefix": "À retenir", "class": "retenir" } ],
                 "references": true
               }
             numbers : numérote les paragraphes de premier niveau, d'un fichier
             à l'autre ; within : seulement sous les titres de niveau 1 qui
             contiennent ce texte ; skipItalic : sauf un chapeau tout en
             italique ; « {#p-nom} » en tête d'un paragraphe le nomme (ancre).
             boxes : une citation dont les premiers mots en gras commencent
             par prefix devient un encadré de classe class. references : un
             lien vide [](#p-nom) affiche le numéro du paragraphe nommé
             (« ? » s'il n'existe pas). Numéros et renvois font rendre le
             projet d'un seul tenant. Rien à approuver : pas de code.

  agent      ce que ce texte demande à l'agent (lot P10), par exemple :
               "agent": {
                 "alias": "Sol",
                 "instructions": "Vérifie chaque référence citée.",
                 "requires": ["mcp:bibliographie", "skill:relecture"]
               }
             alias : un nom court des réglages généraux (glose set) ;
             inconnu sur la machine, le nom court par défaut est pris ;
             instructions : consignes propres au texte, données à l'agent
             avec celles de la navette (4 000 caractères au plus) ;
             requires : outils que l'agent doit déclarer, « mcp:NOM » ou
             « skill:NOM ». Jamais de chemin ni de clé : le dépôt peut être
             public ; l'agent garde sa propre configuration.
  project    ce que vous dites du projet, montré par « glose home » ; à changer
             par « glose project » (une version antérieure de Glose refuse un
             profil qui porte cette clé) :
               "project": {
                 "type": "article",
                 "status": "review",
                 "deadline": "2026-12-31",
                 "nextAction": "Vérifier les références"
               }
             type : texte libre (40 caractères au plus) ; status : draft,
             active, review, done ou archived (done sert à trier, il ne clôt
             rien) ; deadline : AAAA-MM-JJ ; nextAction : une ligne de 200
             caractères au plus. Ces quatre clés seulement, ni chemin ni clé
             d'accès : le dépôt peut être public. Une valeur invalide est
             ignorée avec un avertissement (glose doctor) et n'empêche jamais
             d'ouvrir le texte.

Le serveur n'exécute le filtre et le programme pandoc du profil qu'une fois
approuvés (D-64) : glose open, tapé dans un terminal, les approuve à la
première ouverture du projet ; modifiés ensuite, ils attendent glose approve,
tapé lui aussi dans un terminal. Un pandoc placé dans le projet n'est jamais
approuvé.

Les entrées de « files » et de « exclude »

  - un fichier est pris tel quel, quelle que soit son extension ;
  - un dossier apporte ses fichiers .md et ceux de ses sous-dossiers ;
  - un motif (avec *, ? ou [ ]) apporte les fichiers .md qu'il désigne, ou
    qui sont dans un dossier qu'il désigne : « livre-* » prend livre-1/,
    livre-2/… ; « ** » vaut un nombre quelconque de dossiers (« **/*.md ») ;
    un motif qui ne trouve rien n'est pas une erreur ;
  - dans un dossier ou par un motif, Glose saute les fichiers et dossiers
    cachés (nom commençant par un point : .git, .glose…) et les consignes
    d'agent (AGENTS.md, CLAUDE.md, GEMINI.md) ; il les prend s'ils sont
    nommés dans « files » ;
  - ce que trouve un dossier ou un motif suit l'ordre d'une table des
    matières : dans chaque dossier, ses fichiers, puis ses sous-dossiers,
    chacun dans l'ordre des octets du nom (chiffres, majuscules, minuscules,
    puis lettres accentuées ; « 01.05--a.md » vient avant « 01.05.01--b.md ») ;
  - un fichier déjà pris garde sa première place : « ["intro.md", "."] »
    met intro.md en tête, puis tout le reste ;
  - au-delà de ` + strconv.Itoa(project.MaxFiles) + ` fichiers .md sous un dossier (dans tout le projet
    pour un motif), Glose s'arrête : nommer alors les dossiers du texte.

Le filtre d'affichage

Il adapte l'affichage aux conventions du texte : numéros de paragraphes,
encadrés, renvois. Il vit dans le dossier du texte, jamais dans Glose. Glose
lit chaque fichier avec pandoc (format commonmark_x), prépare l'arbre, puis
appelle pandoc avec le filtre (--lua-filter) pour produire le HTML. Le filtre
reçoit :

  - chaque fichier dans un Div de classe glose-file (attributs data-f : son
    rang ; data-path : son chemin) ;
  - les mots et les espaces du texte, chacun dans un Span qui porte
    l'attribut data-g (« fichier:début,fin ») : c'est lui qui relie
    l'affichage aux octets du fichier, pour que les annotations tombent au
    bon endroit. Un mot est donc le plus souvent un Span, parfois un Str
    nu : traiter les deux cas (fonction mot de l'exemple) ;
  - la métadonnée glose, qui vaut vrai : un même filtre peut servir hors de
    Glose (compilation PDF, par exemple) et s'y conduire autrement ;
  - parfois un fragment seulement : l'aperçu d'une réécriture passe par le
    filtre avec le seul passage réécrit, et avec « filterPerFile », chaque
    fichier passe seul. Ne pas supposer le document entier.

Règles :
  - ne jamais changer le texte ni les attributs d'un Span data-g ; ne pas en
    supprimer (le texte disparaîtrait de l'écran) ni en fabriquer ;
  - ajouter, envelopper, déplacer, donner des classes : permis. Ce que le
    filtre ajoute est du texte généré, affiché mais pas annotable ;
  - dans le HTML, les identifiants du texte sont préfixés par « gl- » (pandoc
    préfixe aussi les liens internes) : la feuille de style vise donc des
    classes plutôt que des identifiants.

Exemple : numéroter les paragraphes et encadrer ceux qui commencent par
« Attention ». Fichier glose/rendu.lua :

{{FILTER}}
Fichier glose/rendu.css :

{{CSS}}
Contrôler :
  glose render > page.html   le HTML affiché, filtre compris ; une erreur du
                             filtre empêche le rendu et s'affiche ici
  glose doctor               le profil, le filtre et la feuille de style
  glose check                le profil, pandoc, les marques et le journal
`)

// exampleFilter is the example filter of profileHelpText.
const exampleFilter = `-- Texte d'un mot : Str nu, ou Span data-g posé par Glose.
local function mot(el)
  if el.t == "Str" then return el.text end
  if el.t == "Span" and #el.content == 1 and el.content[1].t == "Str" then
    return el.content[1].text
  end
end

local n = 0
local function bloc(b)
  if b.t ~= "Para" then return b end
  local premier = mot(b.content[1] or pandoc.Str("")) or ""
  n = n + 1
  -- Numéro ajouté : du texte généré, sans data-g, donc pas annotable.
  b.content:insert(1, pandoc.Span({ pandoc.Str(n .. ". ") },
    pandoc.Attr("", { "num" })))
  if premier:match("^Attention") then
    return pandoc.Div({ b }, pandoc.Attr("", { "encadre" }))  -- enveloppé
  end
  return b
end

function Pandoc(doc)
  if not doc.meta.glose then return nil end  -- hors de Glose : rien
  for _, f in ipairs(doc.blocks) do
    if f.t == "Div" and f.classes:includes("glose-file") then
      f.content = f.content:map(bloc)
    end
  end
  return doc
end
`

// exampleCSS is the example stylesheet of profileHelpText.
const exampleCSS = `.num { color: #888; font-size: 0.85em; }
.encadre { border-left: 3px solid #c80; padding-left: 1em; }
`

// indent prefixes every non-empty line of s with four spaces.
func indent(s string) string {
	lines := strings.SplitAfter(s, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			lines[i] = "    " + l
		}
	}
	return strings.Join(lines, "")
}
