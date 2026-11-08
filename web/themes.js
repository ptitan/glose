// Généré par tools/gen-themes/gen.py : ne pas modifier à la main.
// Loaded before the page is painted: the theme last chosen comes at once.
"use strict";
window.GloseThemes = (() => {
  const LISTE = [
    {
      "id": "system",
      "nom": "Système (Glose Default)",
      "teinte": "system"
    },
    {
      "id": "light",
      "nom": "Glose Default (clair)",
      "teinte": "light"
    },
    {
      "id": "nord-clair",
      "nom": "Nord clair",
      "teinte": "light"
    },
    {
      "id": "catppuccin-latte",
      "nom": "Catppuccin Latte",
      "teinte": "light"
    },
    {
      "id": "rose-pine-dawn",
      "nom": "Rosé Pine Dawn",
      "teinte": "light"
    },
    {
      "id": "solarized-clair",
      "nom": "Solarized clair",
      "teinte": "light"
    },
    {
      "id": "gruvbox-clair",
      "nom": "Gruvbox clair",
      "teinte": "light"
    },
    {
      "id": "everforest-clair",
      "nom": "Everforest clair",
      "teinte": "light"
    },
    {
      "id": "kanagawa-lotus",
      "nom": "Kanagawa Lotus",
      "teinte": "light"
    },
    {
      "id": "dark",
      "nom": "Glose Default (sombre)",
      "teinte": "dark"
    },
    {
      "id": "nord",
      "nom": "Nord",
      "teinte": "dark"
    },
    {
      "id": "tokyo-night",
      "nom": "Tokyo Night",
      "teinte": "dark"
    },
    {
      "id": "catppuccin-mocha",
      "nom": "Catppuccin Mocha",
      "teinte": "dark"
    },
    {
      "id": "rose-pine",
      "nom": "Rosé Pine",
      "teinte": "dark"
    },
    {
      "id": "gruvbox",
      "nom": "Gruvbox",
      "teinte": "dark"
    },
    {
      "id": "everforest",
      "nom": "Everforest",
      "teinte": "dark"
    },
    {
      "id": "kanagawa",
      "nom": "Kanagawa",
      "teinte": "dark"
    },
    {
      "id": "solarized",
      "nom": "Solarized",
      "teinte": "dark"
    }
  ];
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
