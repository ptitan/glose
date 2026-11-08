/* Export to PDF through the printing of the browser (D-55): the version chosen
   is displayed, its comments are set in the margin of the paper, the browser
   prints, and the screen comes back as it was. Nothing is written to any file. */
"use strict";
window.GloseExport = (() => {
  const VERSIONS = { relecture: "Modifications apparentes", actuel: "Actuel", propose: "Proposé" };

  function creer({ $, S, el, riche, zone, avis, changerVue, desactiver, nomAnnotation, PROPOSITION, QUI }) {
    let impression = null; // { vue, titre }: what comes back after printing.

    const qui = (m) => (m.by === "author" ? "Auteur" : m.via || QUI[m.by] || "");
    const tonDe = (m) => (m.by === "author" ? "auteur" : m.by === "agent" ? "agent" : "inconnu");
    const racine = () => (S.racine || "").split(/[\\/]/).filter(Boolean).pop() || "Texte";

    // ---------- Comments on paper ----------
    // Only what the text cannot show: its nature, who, and the discussion. The
    // words of a proposal are already in the text, marked.
    function note(a, i) {
      const n = el("aside", "note-export" + (PROPOSITION[a.kind] ? " prop" : ""));
      const origine = PROPOSITION[a.kind] ? a : (a.messages.find((m) => !m.reply && !m.fix) || a);
      const tete = el("div", "tete-note");
      tete.append(el("span", "num", String(i + 1)), el("strong", "", nomAnnotation(a)));
      // A proposal has no message of its own; a comment speaks through its first one.
      const auteur = PROPOSITION[a.kind] || !a.messages.length ? qui(origine) : "";
      if (auteur) tete.append(el("span", "qui " + tonDe(origine), auteur));
      if (a.to) tete.append(el("span", "pour", "pour " + a.to));
      n.append(tete);
      if (a.primary && a.note) n.append(el("p", "message", "Note : " + a.note));
      for (const m of a.messages) {
        const p = riche("p", "message " + tonDe(m), m.text === "∅" ? "(rien)" : m.text.trim());
        const nom = m.fix ? "Correction de " + qui(m) : qui(m);
        if (nom) p.prepend(el("span", "qui", nom + " : "));
        n.append(p);
      }
      return n;
    }

    // A note floats out of its block, by its right edge: the block has to reach
    // the right edge of the text and not to clip it, or the note leaves before it.
    function placer(n, ancre) {
      const fichier = ancre.closest(".glose-file") || zone();
      const droite = fichier.getBoundingClientRect().right;
      let sortie = null;
      for (let x = ancre.parentElement; x && x !== fichier; x = x.parentElement) {
        const st = getComputedStyle(x);
        if (st.display.startsWith("inline") && !x.matches("table")) continue;
        if (x.matches("table, figure, pre, details, ins, del") || st.overflowX !== "visible") sortie = x;
        // Set in from the right edge: by itself, or by the padding of its parent.
        else if (x.getBoundingClientRect().right < droite - 1) sortie = x.parentElement === fichier ? x : x.parentElement;
      }
      if (sortie) { sortie.before(n); return; }
      const bloc = ancre.closest("p, h1, h2, h3, h4, h5, h6, li, dd, dt");
      if (!bloc || !fichier.contains(bloc)) { ancre.before(n); return; }
      if (bloc.matches("li, dd, dt")) {
        // Inside the item, so that a list keeps only items; after the notes already there.
        let suite = bloc.firstChild;
        while (suite && suite.classList && suite.classList.contains("note-export")) suite = suite.nextSibling;
        bloc.insertBefore(n, suite);
        return;
      }
      (bloc.closest("ins, del") || bloc).before(n);
    }

    function preparer(commentaires) {
      nettoyerPage();
      desactiver();
      const b = document.body;
      const relecture = S.vue === "relecture";
      const cartes = relecture && commentaires ? S.cartes.map((c, i) => ({ c, i })).filter((x) => !x.c.cache) : [];
      b.classList.add("export-impression");
      b.classList.toggle("export-sans-marques", !relecture);
      b.classList.toggle("export-marge", cartes.length > 0);
      const sansPassage = [];
      for (const { c, i } of cartes) {
        if (c.badge && c.badge.isConnected) placer(note(c.a, i), c.badge);
        else sansPassage.push(note(c.a, i));
      }
      if (sansPassage.length) {
        const fin = el("section", "fin-export");
        fin.append(el("h2", "", "Annotations sans passage affiché"), ...sansPassage);
        zone().append(fin);
      }
      copier();
    }

    // The screen wraps every word in a span of its own, for the positions in the
    // source. A browser makes a tagged PDF of them: several objects a word, ten
    // times the weight of the text. Paper gets a copy with the words set back in
    // their sentences, before the original so that its links lead to it.
    function copier() {
      const z = zone();
      const copie = z.cloneNode(true);
      for (const x of z.querySelectorAll(".note-export, .fin-export")) x.remove();
      copie.classList.add("export-copie");
      for (const m of copie.querySelectorAll("span[data-g]")) {
        if (!m.className && m.dataset.a === undefined) m.replaceWith(...m.childNodes);
      }
      copie.normalize();
      z.before(copie);
    }

    function nettoyerPage() {
      for (const x of document.querySelectorAll(".export-copie")) x.remove();
      document.body.classList.remove("export-impression", "export-sans-marques", "export-marge");
    }

    // The browser proposes the title as the name of the file.
    function debut(vue) {
      impression = { vue, titre: document.title };
      const partie = (S.partie || "").replace(/\/$/, "").split("/").pop().replace(/\.(md|markdown)$/i, "");
      document.title = (partie || racine()) + " — " + VERSIONS[S.vue];
    }

    function fin() {
      if (!impression) return;
      const { vue, titre } = impression;
      impression = null;
      nettoyerPage();
      document.title = titre;
      if (vue && vue !== S.vue) changerVue(vue);
    }

    // ---------- The panel ----------
    const versionChoisie = () => (document.querySelector('input[name="export-version"]:checked') || {}).value;

    function majPanneau() {
      $("export-commentaires-ligne").hidden = versionChoisie() !== "relecture";
    }

    function portee() {
      if (S.partie) return (S.partie.endsWith("/") ? "Partie affichée : " : "Fichier affiché : ") + S.partie;
      const fichiers = S.doc ? S.doc.files : [];
      return fichiers.length === 1 ? "Fichier affiché : " + fichiers[0].path : "Tout le projet est affiché : " + fichiers.length + " fichiers.";
    }

    function montrer(ouvert, rendreFocus) {
      $("export").hidden = !ouvert;
      $("exporter").setAttribute("aria-expanded", String(ouvert));
      if (ouvert) {
        const choisi = document.querySelector('input[name="export-version"][value="' + S.vue + '"]');
        choisi.checked = true;
        $("export-commentaires").checked = true;
        $("export-portee").textContent = portee();
        majPanneau();
        choisi.focus();
      } else if (rendreFocus) $("exporter").focus();
    }

    async function exporter() {
      if (!S.doc) return;
      if (document.querySelector("dialog[open]")) { avis("Fermez d'abord le panneau de saisie.", true); return; }
      const version = versionChoisie(), commentaires = $("export-commentaires").checked;
      montrer(false, true);
      fin(); // a printing whose end the browser never told
      const vue = S.vue;
      if (version !== vue && !(await changerVue(version))) {
        avis("Cette version n'a pas pu être préparée : rien n'est exporté.", true);
        if (S.vue !== vue) changerVue(vue);
        return;
      }
      debut(vue);
      preparer(commentaires);
      await document.fonts.ready;
      window.print();
    }

    function brancher() {
      $("exporter").addEventListener("click", () => montrer($("export").hidden, false));
      $("export-ok").addEventListener("click", exporter);
      for (const r of document.querySelectorAll('input[name="export-version"]')) r.addEventListener("change", majPanneau);
      $("export").addEventListener("keydown", (e) => {
        if (e.key !== "Escape") return;
        e.preventDefault();
        montrer(false, true);
      });
      document.addEventListener("click", (e) => {
        if (!$("export").hidden && !$("export").contains(e.target) && !$("exporter").contains(e.target)) montrer(false, false);
      });
      // Ctrl+P: the version displayed, its comments in the margin.
      window.addEventListener("beforeprint", () => {
        if (impression || !S.doc) return;
        debut(null);
        preparer(true);
      });
      window.addEventListener("afterprint", fin);
    }

    return { brancher };
  }

  return { creer };
})();
