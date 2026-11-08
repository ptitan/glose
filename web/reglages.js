"use strict";
// Display settings and column resizing (P8.3).
window.GloseReglages = (() => {
  function creer({ $, S, api, avis, el, disposer }) {
    // ---------- Column width ----------
    // Persist between visits; clamp to leave room for the text.
    const COLONNE = { min: 12, max: 36, defaut: 16.5 };
    function largeurColonne(rem, retenir) {
      const v = Math.min(COLONNE.max, Math.max(COLONNE.min, Math.round(rem * 4) / 4));
      S.colonne = v;
      appliquerGrille();
      $("poignee-colonne").setAttribute("aria-valuenow", String(v));
      if (retenir) localStorage.setItem("glose-colonne", String(v));
      disposer();
    }
    // Set the width on the grid itself, never through a page CSS variable:
    // variables propagate to every element, and changing one recalculated
    // styles for the entire text (2.6 s for a large book, versus 0.8 s).
    // When the column is hidden or the screen is narrow, the stylesheet decides.
    const etroit = matchMedia("(max-width: 1050px)");
    function appliquerGrille() {
      // A medium window starts from a narrower column, until the author sets one.
      const choisie = etroit.matches ? localStorage.getItem("glose-colonne") : S.colonne;
      $("app").style.gridTemplateColumns = $("cote").hidden || !S.colonne || !choisie ? "" : S.colonne + "rem minmax(0, 1fr)";
    }

    function brancherColonne() {
      etroit.addEventListener("change", () => { appliquerGrille(); disposer(); });
      const p = $("poignee-colonne");
      const memo = parseFloat(localStorage.getItem("glose-colonne") || "");
      largeurColonne(memo > 0 ? memo : COLONNE.defaut, false);
      const rem = () => parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
      // During dragging, only a guide follows the pointer: changing the width
      // on every move would lay out the entire text again (slow for a large
      // book). Apply the width on release.
      p.addEventListener("pointerdown", (e) => {
        if (e.button !== 0) return;
        e.preventDefault();
        p.setPointerCapture(e.pointerId);
        p.classList.add("tire");
        document.body.classList.add("redimension");
        const cote = $("cote").getBoundingClientRect();
        const taille = rem();
        // Use the distance from the starting point rather than the pointer's
        // position: clicking the 6 px handle alone must not change the width.
        const depart = e.clientX, initiale = cote.width / taille;
        const largeur = (x) => Math.min(COLONNE.max, Math.max(COLONNE.min, initiale + (x - depart) / taille));
        const guide = el("div", "guide-colonne");
        guide.style.left = cote.right + "px";
        document.body.append(guide);
        let x = depart, prevu = 0;
        const fin = new AbortController();
        const bouger = (m) => {
          x = m.clientX;
          if (!prevu) prevu = requestAnimationFrame(() => { prevu = 0; guide.style.left = cote.left + largeur(x) * taille + "px"; });
        };
        const finir = () => {
          fin.abort();
          if (prevu) cancelAnimationFrame(prevu);
          guide.remove();
          p.classList.remove("tire");
          document.body.classList.remove("redimension");
          if (x !== depart) largeurColonne(largeur(x), true);
        };
        p.addEventListener("pointermove", bouger, { signal: fin.signal });
        p.addEventListener("pointerup", finir, { signal: fin.signal });
        p.addEventListener("pointercancel", finir, { signal: fin.signal });
        p.addEventListener("lostpointercapture", finir, { signal: fin.signal });
      });
      p.addEventListener("dblclick", () => largeurColonne(COLONNE.defaut, true));
      p.addEventListener("keydown", (e) => {
        const actuelle = $("cote").getBoundingClientRect().width / rem() || COLONNE.defaut;
        const pas = e.shiftKey ? 2 : 0.5;
        if (e.key === "ArrowRight") { e.preventDefault(); largeurColonne(actuelle + pas, true); }
        else if (e.key === "ArrowLeft") { e.preventDefault(); largeurColonne(actuelle - pas, true); }
        else if (e.key === "Home") { e.preventDefault(); largeurColonne(COLONNE.defaut, true); }
      });
    }

    // ---------- Display settings ----------
    const POLICES = {
      source: '"Source Serif 4", Georgia, "Times New Roman", serif',
      libertinus: '"Libertinus Serif", "Source Serif 4", Georgia, serif',
      serif: '"Sitka Text", "Iowan Old Style", Cambria, Georgia, serif',
      georgia: 'Georgia, "Times New Roman", serif',
      sans: '"Inter", system-ui, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif',
      lisible: 'Verdana, Tahoma, "DejaVu Sans", sans-serif',
      mono: 'ui-monospace, "Cascadia Code", "DejaVu Sans Mono", Consolas, monospace',
    };
    const DEFAUT = { font: "source", size: 17, lineHeight: 1.6, justify: false, theme: "system", textWidth: 44 };

    function appliquerReglages(r) {
      S.reglages = r;
      const st = document.documentElement.style;
      st.setProperty("--police", POLICES[r.font] || POLICES.source);
      st.setProperty("--taille", r.size + "px");
      st.setProperty("--interligne", String(r.lineHeight));
      st.setProperty("--alignement", r.justify ? "justify" : "left");
      st.setProperty("--texte", r.textWidth + "rem");
      GloseThemes.appliquer(r.theme);
      $("r-police").value = r.font;
      $("r-taille").value = r.size; $("r-taille-val").textContent = r.size + " px";
      $("r-interligne").value = r.lineHeight; $("r-interligne-val").textContent = Number(r.lineHeight).toFixed(2);
      $("r-largeur").value = r.textWidth; $("r-largeur-val").textContent = r.textWidth + " rem";
      $("r-justifie").checked = r.justify;
      $("r-theme").value = r.theme;
      disposer();
    }

    let minuterieReglages = 0;
    function changerReglages() {
      const r = {
        font: $("r-police").value, size: +$("r-taille").value, lineHeight: +$("r-interligne").value,
        justify: $("r-justifie").checked, theme: $("r-theme").value || "system", textWidth: +$("r-largeur").value,
      };
      appliquerReglages(r);
      clearTimeout(minuterieReglages);
      minuterieReglages = setTimeout(() => enregistrerReglages(r), 400);
    }

    async function enregistrerReglages(r) {
      const rep = await api("/api/settings", r);
      if (!rep.ok) avis("Réglages appliqués à cette page, mais non enregistrés : " + (rep.data.message || "erreur"), true);
    }

    async function chargerReglages() {
      const r = await api("/api/settings");
      appliquerReglages(r.ok ? r.data : DEFAUT);
    }

    function brancherReglages() {
      $("bouton-reglages").addEventListener("click", () => {
        const p = $("reglages");
        p.hidden = !p.hidden;
        $("bouton-reglages").setAttribute("aria-expanded", String(!p.hidden));
        if (!p.hidden) $("r-police").focus();
      });
      // Escape closes the settings and returns focus to their button.
      $("reglages").addEventListener("keydown", (e) => {
        if (e.key !== "Escape") return;
        e.preventDefault();
        $("reglages").hidden = true;
        $("bouton-reglages").setAttribute("aria-expanded", "false");
        $("bouton-reglages").focus();
      });
      document.addEventListener("click", (e) => {
        // The button contains an icon: clicking it also counts as a button click.
        if (!$("reglages").hidden && !$("reglages").contains(e.target) && !$("bouton-reglages").contains(e.target)) {
          $("reglages").hidden = true;
          $("bouton-reglages").setAttribute("aria-expanded", "false");
        }
      });
      for (const id of ["r-police", "r-taille", "r-interligne", "r-largeur", "r-justifie"]) $(id).addEventListener("input", changerReglages);
      GloseThemes.remplir($("r-theme"));
      $("r-theme").addEventListener("input", changerReglages);
      $("r-defaut").addEventListener("click", () => { appliquerReglages(DEFAUT); enregistrerReglages(DEFAUT); });
    }

    return { appliquerGrille, brancherColonne, brancherReglages, chargerReglages };
  }
  return { creer };
})();
