#!/usr/bin/env python3
"""Génère web/themes.css et web/themes.js : les thèmes de Glose, inspirés de
thèmes existants, à partir de leurs palettes publiées. Les couleurs de lecture
sont ajustées au besoin : un thème de bureau vise le code, Glose du texte long.
Aucun thème ne descend sous les contrastes que vérifie scripts/donnees.test.cjs.

Sources des palettes (relevées le 8 novembre 2026) :
- Nord : nordtheme/nord (MIT)
- Tokyo Night : folke/tokyonight.nvim (Apache-2.0)
- Catppuccin : catppuccin/palette (MIT)
- Rosé Pine : rose-pine/neovim (MIT)
- Kanagawa : rebelot/kanagawa.nvim (MIT)
- Gruvbox : morhetz/gruvbox
- Everforest : sainnhe/everforest (MIT)
- Solarized : altercation/vim-colors-solarized

Usage : python3 tools/gen-themes/gen.py
"""
import json
import os
import re

ICI = os.path.dirname(os.path.abspath(__file__))
WEB = os.path.join(ICI, "..", "..", "web")


def rvb(c):
    c = c.lstrip("#")
    return tuple(int(c[i:i + 2], 16) for i in (0, 2, 4))


def hexa(t):
    return "#%02x%02x%02x" % tuple(max(0, min(255, round(x))) for x in t)


def melange(a, b, part):
    """part de a, le reste de b."""
    ra, rb = rvb(a), rvb(b)
    return hexa(tuple(x * part + y * (1 - part) for x, y in zip(ra, rb)))


def luminance(c):
    def canal(v):
        v /= 255
        return v / 12.92 if v <= 0.03928 else ((v + 0.055) / 1.055) ** 2.4
    r, g, b = rvb(c)
    return 0.2126 * canal(r) + 0.7152 * canal(g) + 0.0722 * canal(b)


def contraste(a, b):
    la, lb = luminance(a), luminance(b)
    return (max(la, lb) + 0.05) / (min(la, lb) + 0.05)


def lisible(c, fonds, cible, vers):
    """Rapproche c de la couleur vers, juste assez pour atteindre la cible."""
    for i in range(0, 101):
        essai = melange(vers, c, i / 100)
        if all(contraste(essai, f) >= cible for f in fonds):
            return essai
    raise SystemExit("contraste inaccessible : %s sur %s" % (c, fonds))


# Identifiant, nom, teinte, puis la palette :
# papier (le texte), fond (la page), alt (surfaces), encre, encre2, discret,
# filet, accent (l'IA), vert (l'auteur, les ajouts), rouge (suppressions,
# erreurs), jaune (commentaires), violet (propositions), nav, encre de nav.
THEMES = [
    ("nord-clair", "Nord clair", "light", "#eceff4", "#e5e9f0", "#d8dee9", "#2e3440", "#3b4252", "#4c566a", "#d8dee9",
     "#5e81ac", "#5f7f4a", "#bf616a", "#a8741c", "#b48ead", "#3b4252", "#eceff4"),
    ("catppuccin-latte", "Catppuccin Latte", "light", "#eff1f5", "#e6e9ef", "#dce0e8", "#4c4f69", "#5c5f77", "#6c6f85", "#ccd0da",
     "#1e66f5", "#40a02b", "#d20f39", "#df8e1d", "#8839ef", "#4c4f69", "#eff1f5"),
    ("rose-pine-dawn", "Rosé Pine Dawn", "light", "#fffaf3", "#faf4ed", "#f2e9e1", "#464261", "#575279", "#797593", "#dfdad9",
     "#907aa9", "#286983", "#b4637a", "#ea9d34", "#d7827e", "#464261", "#faf4ed"),
    ("solarized-clair", "Solarized clair", "light", "#fdf6e3", "#eee8d5", "#e6dfc8", "#2f4a52", "#586e75", "#657b83", "#e0d9c3",
     "#268bd2", "#859900", "#dc322f", "#b58900", "#6c71c4", "#073642", "#eee8d5"),
    ("gruvbox-clair", "Gruvbox clair", "light", "#fbf1c7", "#f2e5bc", "#ebdbb2", "#3c3836", "#504945", "#7c6f64", "#d5c4a1",
     "#076678", "#79740e", "#9d0006", "#b57614", "#8f3f71", "#3c3836", "#fbf1c7"),
    ("everforest-clair", "Everforest clair", "light", "#fdf6e3", "#efebd4", "#f4f0d9", "#4a555b", "#5c6a72", "#829181", "#e0dcc7",
     "#3a94c5", "#8da101", "#f85552", "#dfa000", "#df69ba", "#4a555b", "#fdf6e3"),
    ("kanagawa-lotus", "Kanagawa Lotus", "light", "#f4efc6", "#ece3ae", "#e5ddb0", "#545464", "#43436c", "#716e61", "#d5cea3",
     "#4d699b", "#6f894e", "#c84053", "#836f4a", "#624c83", "#545464", "#f2ecbc"),
    ("nord", "Nord", "dark", "#333a47", "#2e3440", "#3b4252", "#eceff4", "#d8dee9", "#b4bccb", "#434c5e",
     "#88c0d0", "#a3be8c", "#bf616a", "#ebcb8b", "#b48ead", "#242933", "#eceff4"),
    ("tokyo-night", "Tokyo Night", "dark", "#1a1b26", "#16161e", "#292e42", "#c0caf5", "#a9b1d6", "#8d94bb", "#292e42",
     "#7aa2f7", "#9ece6a", "#f7768e", "#e0af68", "#bb9af7", "#0c0e14", "#c0caf5"),
    ("catppuccin-mocha", "Catppuccin Mocha", "dark", "#1e1e2e", "#181825", "#313244", "#cdd6f4", "#bac2de", "#a6adc8", "#313244",
     "#89b4fa", "#a6e3a1", "#f38ba8", "#f9e2af", "#cba6f7", "#11111b", "#cdd6f4"),
    ("rose-pine", "Rosé Pine", "dark", "#1f1d2e", "#191724", "#26233a", "#e0def4", "#cfcce6", "#908caa", "#403d52",
     "#c4a7e7", "#9ccfd8", "#eb6f92", "#f6c177", "#ebbcba", "#141320", "#e0def4"),
    ("gruvbox", "Gruvbox", "dark", "#282828", "#1d2021", "#3c3836", "#ebdbb2", "#d5c4a1", "#a89984", "#3c3836",
     "#83a598", "#b8bb26", "#fb4934", "#fabd2f", "#d3869b", "#1d2021", "#ebdbb2"),
    ("everforest", "Everforest", "dark", "#2d353b", "#232a2e", "#343f44", "#d3c6aa", "#c5bb9f", "#9da9a0", "#475258",
     "#7fbbb3", "#a7c080", "#e67e80", "#dbbc7f", "#d699b6", "#1e2326", "#d3c6aa"),
    ("kanagawa", "Kanagawa", "dark", "#1f1f28", "#181820", "#2a2a37", "#dcd7ba", "#c8c093", "#9c9a8c", "#363646",
     "#7e9cd8", "#98bb6c", "#e46876", "#e6c384", "#957fb8", "#16161d", "#dcd7ba"),
    ("solarized", "Solarized", "dark", "#002b36", "#00212b", "#073642", "#a3b1b1", "#93a1a1", "#839496", "#0c4351",
     "#268bd2", "#859900", "#dc322f", "#b58900", "#6c71c4", "#00212b", "#93a1a1"),
]


def variables(teinte, papier, fond, alt, encre, encre2, discret, filet, accent, vert, rouge, jaune, violet, nav, nav_encre):
    teinte_fond = lambda c, part: melange(c, papier, part)
    # Le contraste se règle dans le sens de la teinte : plus clair sur un fond sombre.
    extreme = "#ffffff" if teinte == "dark" else "#000000"
    v = {}
    v["fond"], v["fond-2"], v["cote"], v["carte"], v["bouton"], v["dialogue"] = fond, alt, fond, papier, melange(alt, papier, 0.35), papier
    v["encre"] = lisible(encre, [papier, fond, alt], 7, extreme)
    v["encre-2"] = lisible(encre2, [papier, fond, alt], 5.5, extreme)
    v["filet"], v["filet-fort"] = filet, melange(v["encre"], filet, 0.18)
    v["accent-doux"] = teinte_fond(accent, 0.16)
    v["ajout-fond"] = v["positif-fond"] = teinte_fond(vert, 0.14)
    v["retrait-fond"] = teinte_fond(rouge, 0.16)
    v["suggestion-fond"] = teinte_fond(violet, 0.16)
    v["commentaire-fond"] = v["surligne"] = v["alerte-fond"] = teinte_fond(jaune, 0.18)
    v["surligne-actif"] = teinte_fond(jaune, 0.34)
    v["selection"] = v["accent-doux"]
    v["discret"] = lisible(discret, [papier, fond, v["accent-doux"], v["positif-fond"]], 4.5, extreme)
    v["accent"] = lisible(accent, [papier, fond, v["accent-doux"]], 4.5, extreme)
    v["focus"] = v["accent"]
    v["accent-texte"] = papier if contraste(papier, v["accent"]) >= 4.5 else ("#000000" if teinte == "dark" else "#ffffff")
    v["ajout"] = v["positif"] = lisible(vert, [papier, v["positif-fond"]], 4.5, extreme)
    v["retrait"] = v["erreur"] = lisible(rouge, [papier, v["retrait-fond"]], 4.5, extreme)
    v["suggestion"] = v["proposition"] = lisible(violet, [papier, v["suggestion-fond"]], 4.5, extreme)
    v["commentaire"] = v["alerte-filet"] = lisible(jaune, [papier, v["commentaire-fond"]], 4.5, extreme)
    v["nav"], v["nav-encre"] = nav, lisible(nav_encre, [nav], 7, "#ffffff")
    v["nav-discret"] = lisible(melange(nav_encre, nav, 0.72), [nav], 4.5, "#ffffff")
    v["nav-actif"] = melange(nav_encre, nav, 0.16)
    v["ombre-encre"] = "#000000" if teinte == "dark" else v["encre"]
    v["image-fond"] = "#ffffff" if teinte == "dark" else "transparent"
    return v


def palette_de_base():
    """Les couleurs de Glose Default, reprises de style.css : un aperçu doit les
    montrer quel que soit le thème de la page, qui les remplace."""
    with open(os.path.join(WEB, "style.css"), encoding="utf-8") as f:
        texte = f.read()
    noms = ["carte", "encre", "ajout", "retrait", "accent", "filet"]
    return " ".join("--%s: %s;" % (n, re.search(r"--%s: (light-dark\([^)]*\));" % n, texte).group(1)) for n in noms)


def css():
    base = palette_de_base()
    lignes = ["/* Généré par tools/gen-themes/gen.py : ne pas modifier à la main.",
              "   À l'écran seulement : le papier garde la palette claire de Glose. */",
              ":root[data-theme=\"light\"] { color-scheme: light; }",
              ":root[data-theme=\"dark\"] { color-scheme: dark; }",
              ".theme-apercu[data-theme=\"light\"] { color-scheme: light; %s }" % base,
              ".theme-apercu[data-theme=\"dark\"] { color-scheme: dark; %s }" % base,
              ".theme-apercu[data-theme=\"system\"] { color-scheme: light dark; %s }" % base,
              "@media screen {"]
    for t in THEMES:
        ident, _, teinte = t[:3]
        v = variables(teinte, *t[3:])
        corps = " ".join("--%s: %s;" % kv for kv in sorted(v.items()))
        lignes.append("  :root[data-theme=\"%s\"], .theme-apercu[data-theme=\"%s\"] { color-scheme: %s; %s }" % (ident, ident, teinte, corps))
    lignes.append("}")
    return "\n".join(lignes) + "\n"


def js():
    liste = [{"id": "system", "nom": "Système (Glose Default)", "teinte": "system"},
             {"id": "light", "nom": "Glose Default (clair)", "teinte": "light"}]
    liste += [{"id": t[0], "nom": t[1], "teinte": t[2]} for t in THEMES if t[2] == "light"]
    liste.append({"id": "dark", "nom": "Glose Default (sombre)", "teinte": "dark"})
    liste += [{"id": t[0], "nom": t[1], "teinte": t[2]} for t in THEMES if t[2] == "dark"]
    return ("""// Généré par tools/gen-themes/gen.py : ne pas modifier à la main.
// Loaded before the page is painted: the theme last chosen comes at once.
"use strict";
window.GloseThemes = (() => {
  const LISTE = %s;
  function appliquer(id) {
    const theme = LISTE.some((t) => t.id === id) ? id : "system";
    document.documentElement.dataset.theme = theme;
    try { localStorage.setItem("glose-theme", theme); } catch (_) { /* private browsing */ }
  }
  // Options of a <select>: light themes, then dark ones.
  function remplir(select) {
    select.textContent = "";
    for (const [teinte, nom] of [["system", ""], ["light", "Clairs"], ["dark", "Sombres"]]) {
      const parent = nom ? select.appendChild(Object.assign(document.createElement("optgroup"), { label: nom })) : select;
      for (const t of LISTE) if (t.teinte === teinte) parent.append(Object.assign(document.createElement("option"), { value: t.id, textContent: t.nom }));
    }
  }
  try { const t = localStorage.getItem("glose-theme"); if (t) document.documentElement.dataset.theme = t; } catch (_) { /* private browsing */ }
  return { LISTE, appliquer, remplir };
})();
""" % json.dumps(liste, ensure_ascii=False, indent=2).replace("\n", "\n  "))


if __name__ == "__main__":
    with open(os.path.join(WEB, "themes.css"), "w", encoding="utf-8", newline="\n") as f:
        f.write(css())
    with open(os.path.join(WEB, "themes.js"), "w", encoding="utf-8", newline="\n") as f:
        f.write(js())
