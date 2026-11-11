#!/usr/bin/env python3
"""Génère web/icones.js : les icônes de l'interface, tirées de Lucide
(https://lucide.dev, licence ISC ; certaines dérivent de Feather, licence MIT)
à une version fixée. Les tracés sont recopiés tels quels ; seules quelques
classes de couleur sont ajoutées (COULEURS). Aucune ressource n'est chargée
de l'extérieur à l'exécution (D-07).

Usage : python3 tools/gen-icones/gen.py > web/icones.js
"""
import json
import re
import urllib.request

VERSION = "1.70.0"
BASE = "https://raw.githubusercontent.com/lucide-icons/lucide/%s/" % VERSION

# Nom dans Glose : nom dans Lucide.
ICONES = {
    "relecture": "file-diff", "actuel": "file-text", "propose": "file-check",
    "annuler": "undo-2", "retablir": "redo-2", "reglages": "a-large-small",
    "remettre": "send", "reprendre": "log-in", "exporter": "file-down",
    "lien": "link", "note": "superscript", "tableau": "table", "liste": "list",
    "repondre": "reply", "corriger": "pencil", "rejeter": "x", "accepter": "check",
    "retirer": "trash", "resoudre": "circle-check",
    "commenter": "message-square", "suggerer": "pencil-line", "plus": "plus",
    "avant": "arrow-up-to-line", "apres": "arrow-down-to-line",
    "paragraphe": "pilcrow", "titre": "heading-2", "encadre": "text-quote", "image": "image", "code": "code",
    "tout-accepter": "check-check", "alerte": "triangle-exclamation-point",
    "a-relire": "circle-dashed", "en-cours": "circle-dot", "relue": "circle-check",
    "o-sommaire": "list-tree", "o-fichiers": "folder-tree", "o-annotations": "messages-square",
    "o-navette": "arrow-left-right",
    "a-dossier": "folder", "a-dossier-ouvert": "folder-open", "a-fichier": "file-text",
    "a-projet": "book-open", "a-chevron": "chevron-right",
    "chercher": "search", "colonne-fermer": "panel-left-close", "colonne-ouvrir": "panel-left-open",
    "brouillons": "notebook-pen", "vous": "user", "agent": "sparkles",
    "menu-capsule": "chevron-down", "copier": "copy", "arreter": "circle-stop", "sortie": "terminal",
    "configurer": "settings", "essayer": "play", "rafraichir": "refresh-cw",
    "accueil": "house", "calendrier": "calendar", "activite": "activity", "points": "ellipsis",
    "fleche": "arrow-right", "retour": "arrow-left", "horloge": "clock", "debranche": "unplug",
    "info": "info", "introuvable": "folder-x", "archive": "archive", "tri": "arrow-up-down", "clair": "sun", "sombre": "moon",
}

ICONES.update({nom: nom for nom in ['pen-line', 'pencil', 'pen-tool', 'wand-sparkles', 'spell-check', 'languages', 'scale', 'book-check', 'file-pen', 'list-checks', 'search-check', 'sparkles']})

# Éléments colorés : (icône, tracé exact) -> classe.
COULEURS = {
    ("relecture", 'd="M9 10h6"'): "vert", ("relecture", 'd="M12 13V7"'): "vert",
    ("relecture", 'd="M9 17h6"'): "rouge", ("propose", 'd="m9 15 2 2 4-4"'): "vert",
}


def lire(chemin):
    with urllib.request.urlopen(BASE + chemin) as r:
        return r.read().decode("utf-8")


def corps(nom, svg):
    inner = re.search(r"<svg[^>]*>(.*)</svg>", svg, re.S).group(1)
    elems = [e.strip() for e in re.findall(r"<[^>]+/>", inner)]
    out = []
    for e in elems:
        for (icone, trace), classe in COULEURS.items():
            if icone == nom and trace in e:
                e = e.replace("<path ", '<path class="%s" ' % classe, 1)
        out.append(re.sub(r"\s+/>", "/>", e))
    return "".join(out)


def main():
    licence = lire("LICENSE").strip()
    data = {nom: corps(nom, lire("icons/%s.svg" % lucide)) for nom, lucide in ICONES.items()}
    print("/*! Icônes de Glose : Lucide %s (https://lucide.dev), recopiées par tools/gen-icones/gen.py." % VERSION)
    print("    Noms d'origine : " + ", ".join("%s = %s" % (k, v) for k, v in ICONES.items()) + ".")
    print("")
    print("\n".join("    " + l if l else "" for l in licence.replace("*/", "* /").splitlines()))
    print("*/")
    print('"use strict";')
    print("// Rempli au chargement : tout <svg data-icone=\"nom\"> reçoit le tracé de l'icône.")
    print("window.GloseIcones = (() => {")
    print("  const traces = " + json.dumps(data, ensure_ascii=False, indent=2).replace("\n", "\n  ") + ";")
    print("""  const svg = (nom) => '<svg class="ic" viewBox="0 0 24 24" aria-hidden="true">' + (traces[nom] || "") + "</svg>";
  const remplir = (racine) => {
    for (const s of (racine || document).querySelectorAll("svg[data-icone]")) s.innerHTML = traces[s.dataset.icone] || "";
  };
  remplir(document);
  return { svg, remplir, traces };
})();""")


if __name__ == "__main__":
    main()
