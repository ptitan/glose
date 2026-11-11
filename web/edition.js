// Glose: paragraph editor and proposal previews.
"use strict";
window.GloseEdition = (() => {
  function creer({ $, S, brouillons, avis, boite, el, md, agents, panneau, cibleSaisie, saisieEnCours, zone, STRUCTURES, lireG, fermerOutils, charger, api, viser, garderBrouillon, preparerBrouillon, apresEnvoiBrouillon, ressources }) {
  const NOM_STRUCTURE = { TABLE: "le tableau", UL: "la liste", OL: "la liste", DL: "la liste de définitions", BLOCKQUOTE: "l'encadré" };
  const DE_STRUCTURE = { TABLE: "du tableau", UL: "de la liste", OL: "de la liste", DL: "de la liste de définitions", BLOCKQUOTE: "de l'encadré" };
  const TOUT = { TABLE: "Tout le tableau", UL: "Toute la liste", OL: "Toute la liste", DL: "Toute la liste de définitions", BLOCKQUOTE: "Tout l'encadré" };
  const panneauEdition = panneau({ dialogue: $("edition"), cible: () => cibleSaisie(S.edition), large: true });
  // ---------- Edit a paragraph (proposals) ----------
  async function modifierBloc(bloc) {
    if (saisieEnCours()) return;
    if (!bloc || !zone().contains(bloc)) { fermerOutils(); return; }
    if (bloc.matches(STRUCTURES)) { fermerOutils(); modifierStructure(bloc); return; }
    const sp = bloc.querySelector("span[data-g]");
    const g = sp && lireG(sp.dataset.g);
    if (!g) { avis("Ce bloc (code, contenu généré) ne peut pas être modifié à l'écran.", true); return; }
    const hash = S.hashes[g.f];
    const r = await api("/api/block?file=" + g.f + "&hash=" + hash + "&at=" + g.s);
    if (!r.ok) {
      avis(r.data.message || "Ce bloc ne peut pas être modifié.", true);
      if (r.status === 409) await charger(true);
      return;
    }
    fermerOutils();
    ouvrirEdition("Suggérer une réécriture du paragraphe", "",
      { mode: "block", file: g.f, hash, at: g.s, start: r.data.start, end: r.data.end, original: r.data.text, element: bloc });
  }

  // Add an element before or after a block (paragraph, heading, structure,
  // image): the panel opens empty, with a choice of element type;
  // the new element will be a single addition proposal.
  function ajouter(bloc, ou) {
    if (saisieEnCours()) return;
    if (!bloc || !zone().contains(bloc)) return;
    // An image alone or a figure: target its containing block.
    const cible = bloc.tagName === "IMG" ? (bloc.closest("p, figure") || bloc) : bloc;
    const mots = Array.from(cible.querySelectorAll("span[data-g]")).map((m) => lireG(m.dataset.g)).filter(Boolean);
    if (!mots.length) { avis("Ce bloc ne contient aucun texte de la source : ajoutez depuis le paragraphe voisin.", true); return; }
    const f = mots[0].f;
    const dans = mots.filter((m) => m.f === f);
    const at = Math.min(...dans.map((m) => m.s)), to = Math.max(...dans.map((m) => m.e));
    const liste = cible.tagName === "LI" ? cible.parentElement : null;
    const contexte = { mode: "insert", where: ou, file: f, hash: S.hashes[f], at, to, start: at, end: to, original: "",
      element: cible, tight: !!liste };
    ouvrirEdition(ou === "before" ? "Ajouter avant" : "Ajouter après",
      liste ? "Nouvel élément de la liste, " + (ou === "before" ? "avant" : "après") + " celui-ci." :
        "Nouvel élément " + (ou === "before" ? "avant" : "après") + " ce bloc : choisissez son type, puis écrivez-le.", contexte);
    if (liste) {
      modeEdition("source");
      insererSource(liste.tagName === "OL" ? "1. " : "- ", "", false);
    }
  }

  // Templates for the element types to add.
  async function choisirType(type) {
    for (const b of $("edition-types").querySelectorAll("button")) b.setAttribute("aria-pressed", String(b.dataset.type === type));
    if (type === "paragraphe") {
      if (!modeEdition("mef")) modeEdition("source");
      return;
    }
    if (type === "tableau") { outilMef("tableau"); return; }
    let sq = { titre: "## Titre", liste: "- premier élément\n- second élément", encadre: "> Texte de l'encadré.", code: "```\n\n```" }[type];
    if (type === "image") {
      const v = await boite({ titre: "Insérer une image", message: "L'image doit se trouver dans le projet.", icone: "image", ton: "info",
        ok: "Insérer", champs: [
          { nom: "chemin", libelle: "Chemin de l'image", valeur: "img/", placeholder: "img/figure.png" },
          { nom: "legende", libelle: "Légende", aide: " (facultative)", placeholder: "Ce que montre l'image" },
        ], valider: (x) => (/^\S+\.\w+$/.test(x.chemin.trim()) ? "" : "Indiquez le chemin d'un fichier, sans espace (par exemple img/figure.png).") });
      if (!v) return;
      sq = "![" + v.legende.trim().replace(/[\[\]]/g, "") + "](" + v.chemin.trim() + ")";
    }
    if (!sq) return;
    modeEdition("source");
    const t = $("edition-texte");
    // The template replaces still-empty text, otherwise it is inserted at the cursor.
    if (!t.value.trim()) { t.value = ""; t.setSelectionRange(0, 0); } else { t.setSelectionRange(t.value.length, t.value.length); }
    insererSource(sq, "", true);
  }

  // A structure (table, list, blockquote) is rewritten in source, using
  // complete lines, from its first word to its last: each added,
  // deleted or modified line becomes a proposal.
  async function modifierStructure(st) {
    if (saisieEnCours()) return;
    const mots = Array.from(st.querySelectorAll("span[data-g]")).map((m) => lireG(m.dataset.g)).filter(Boolean);
    if (!mots.length) { avis("Cette structure ne contient aucun texte de la source.", true); return; }
    const f = mots[0].f;
    const dansFichier = mots.filter((m) => m.f === f);
    const at = Math.min(...dansFichier.map((m) => m.s)), to = Math.max(...dansFichier.map((m) => m.e));
    const hash = S.hashes[f];
    const r = await api("/api/block?file=" + f + "&hash=" + hash + "&at=" + at + "&to=" + to);
    if (!r.ok) {
      avis(r.data.message || "Cette structure ne peut pas être réécrite.", true);
      if (r.status === 409) await charger(true);
      return;
    }
    const de = DE_STRUCTURE[st.tagName] || "de la structure";
    ouvrirEdition("Suggérer une réécriture " + de,
      "Source " + de + " : chaque ligne ajoutée, supprimée ou modifiée deviendra une proposition.",
      { mode: "lines", file: f, hash, at, to, start: r.data.start, end: r.data.end, original: r.data.text, element: st });
  }

  function ouvrirEdition(titre, info, contexte, recupere) {
    if (!brouillons.pret) { avis("Ouvrez Brouillons pour rétablir la sauvegarde avant de saisir.", true); return; }
    // The request that led here may have taken long: a panel opened meanwhile
    // keeps its input.
    if (saisieEnCours()) return;
    S.edition = contexte;
    $("edition-titre").textContent = titre;
    $("edition-ok").firstChild.textContent = contexte.mode === "revise" ? "Enregistrer " : "Ajouter la suggestion ";
    $("edition-info").textContent = info;
    $("edition-texte").value = recupere ? recupere.text : contexte.original;
    S.modeEdition = "source";
    const prefere = recupere ? recupere.editor : localStorage.getItem("glose-editeur") || "mef";
    $("edition-note").value = recupere ? recupere.note : "";
    $("edition-brouillon").textContent = recupere ? "Brouillon récupéré." : "";
    $("edition-message").textContent = "";
    // D-32: a single proposal, a choice retained between visits;
    // irrelevant when correcting your own proposal.
    $("edition-tenant").checked = recupere ? recupere.whole : localStorage.getItem("glose-tenant") === "1";
    $("edition-tenant-case").hidden = contexte.mode === "revise";
    $("menu-selection").hidden = true;
    agents.preparerDestinataire("edition");
    $("edition-pour-ligne").hidden = $("edition-pour-ligne").hidden || contexte.mode === "revise";
    // D-57: a direct change, never on a proposal being corrected.
    $("edition-direct").hidden = contexte.mode === "revise";
    majDirect();
    panneauEdition.ouvrir();
    if (prefere !== "mef" || !modeEdition("mef", true)) modeEdition("source", true);
    $("edition-changements").textContent = "";
    $("edition-changements").append(el("li", "vide", "Aucune pour l'instant : modifiez le texte."));
    elargissements(contexte);
    $("edition-types").hidden = !(contexte.mode === "insert" && !contexte.tight);
    for (const b of $("edition-types").querySelectorAll("button")) b.setAttribute("aria-pressed", String(b.dataset.type === "paragraphe"));
    viser(contexte);
    panneauEdition.placer();
    if (recupere) changementsBientot();
  }

  // A list item, a cell, a blockquote paragraph: the enclosing
  // structure can be rewritten as a whole (adding a row, an item).
  function elargissements(x) {
    const zoneEl = $("edition-elargir");
    zoneEl.textContent = "";
    const out = [];
    for (let n = x.element && x.element.parentElement; n && n !== zone(); n = n.parentElement) {
      if (n.matches(STRUCTURES) && out.length < 2) out.push(n);
    }
    zoneEl.hidden = !out.length;
    if (!out.length) return;
    zoneEl.append(el("span", "", "Réécrire plutôt :"));
    for (const st of out) {
      const nom = NOM_STRUCTURE[st.tagName] || "la structure";
      const b = el("button", "", TOUT[st.tagName] || "Toute la structure");
      b.type = "button";
      b.dataset.infobulle = "Ouvrir la source " + (DE_STRUCTURE[st.tagName] || "de la structure") + " (pour ajouter ou supprimer une ligne, un élément)";
      b.addEventListener("click", async () => {
        if (S.envoi) return;
        garderBrouillon("edition");
        if (x.brouillon) { x.brouillon.active = false; brouillons.sauver(x.brouillon); }
        S.edition = null;
        $("edition").close();
        modifierStructure(st);
      });
      zoneEl.append(b);
    }
  }

  // Three modes: "Mis en forme" (write in the rendered text;
  // Markdown is rewritten identically outside changes, visuel.js),
  // "Source" (Markdown), "Aperçu" (the result and list of
  // proposals, without writing anything).
  function texteEdition() {
    if (S.modeEdition === "mef") return GloseVisuel.ecrire($("edition-visuel"), S.optionsMef);
    return $("edition-texte").value;
  }

  // Switch to mode m; return false if that mode is unavailable.
  function modeEdition(m, ouverture) {
    const texte = texteEdition();
    $("edition-message").textContent = "";
    $("edition-message").className = "";
    if (m === "mef") {
      const opt = GloseVisuel.ouvrir(texte, $("edition-visuel"));
      if (!opt) {
        if (!ouverture) $("edition-message").textContent =
          "Ce passage ne peut pas être modifié mis en forme (titre, liste, tableau, ou syntaxe que l'éditeur ne réécrirait pas à l'identique) : utilisez la source.";
        return false;
      }
      S.optionsMef = opt;
    }
    $("edition-texte").value = texte;
    S.modeEdition = m;
    $("edition-texte").hidden = m !== "source";
    $("edition-aide-source").hidden = m !== "source";
    $("edition-visuel").hidden = m !== "mef";
    $("edition-outils").hidden = m === "apercu";
    $("edition-apercu").hidden = m !== "apercu";
    for (const [id, mm] of [["edition-mef", "mef"], ["edition-source", "source"], ["edition-voir", "apercu"]]) {
      $(id).setAttribute("aria-selected", String(m === mm));
    }
    if (m !== "apercu") localStorage.setItem("glose-editeur", m);
    if (m === "mef") $("edition-visuel").focus();
    if (m === "source") $("edition-texte").focus();
    if (!ouverture) garderBrouillon("edition");
    return true;
  }

  function basculerEdition(apercu) { modeEdition(apercu ? "apercu" : "source"); }

  // Insert Markdown into the source, replacing the selection; a block
  // (table, list) is separated from neighbouring text by blank lines.
  function insererSource(avant, apres, bloc) {
    const t = $("edition-texte");
    const d = t.selectionStart, f = t.selectionEnd;
    let texte = avant + t.value.slice(d, f) + apres;
    if (bloc) {
      const prec = t.value.slice(0, d), suiv = t.value.slice(f);
      if (prec && !/\n\s*\n\s*$/.test(prec)) texte = (/\n$/.test(prec) ? "\n" : "\n\n") + texte;
      if (suiv && !/^\s*\n\s*\n/.test(suiv)) texte += /^\n/.test(suiv) ? "\n" : "\n\n";
    }
    t.focus();
    t.setRangeText(texte, d, f, "end");
    t.dispatchEvent(new Event("input"));
  }

  async function squeletteTableau() {
    const v = await boite({ titre: "Insérer un tableau", message: "Il s'écrit dans la source : vous remplirez ensuite les cases.",
      icone: "tableau", ton: "info", ok: "Insérer", champs: [
        { nom: "colonnes", libelle: "Colonnes", type: "number", min: 1, max: 12, valeur: 2, court: true },
        { nom: "lignes", libelle: "Lignes", type: "number", min: 1, max: 50, valeur: 2, court: true },
      ], valider: (x) => (+x.colonnes >= 1 && +x.colonnes <= 12 && +x.lignes >= 1 && +x.lignes <= 50 ? "" :
        "De 1 à 12 colonnes, et de 1 à 50 lignes.") });
    if (!v) return null;
    const c = +v.colonnes, l = +v.lignes;
    const ligne = (f) => "|" + Array.from({ length: c }, (_, k) => " " + f(k) + " |").join("");
    const lignes = [ligne((k) => "Colonne " + (k + 1)), ligne(() => "---"), ...Array.from({ length: l }, () => ligne(() => "   "))];
    return lignes.join("\n");
  }

  // Editor toolbar (Mis en forme and Source modes).
  // Ask for a link address, or the text of a note.
  const demanderLien = () => boite({ titre: "Insérer un lien", icone: "lien", ton: "info", ok: "Insérer",
    champs: [{ nom: "url", libelle: "Adresse", valeur: "https://", placeholder: "https://…" }],
    valider: (x) => {
      const u = x.url.trim();
      if (!/^\S+$/.test(u) || u === "https://") return "Indiquez une adresse, sans espace.";
      // A web link, email, anchor or relative path;
      // never a scheme that would execute anything.
      if (/^(javascript|data|vbscript|file):/i.test(u)) return "Ce type d'adresse n'est pas permis.";
      return "";
    } });
  const demanderNote = () => boite({ titre: "Insérer une note de bas de page", message: "L'appel se place à l'endroit du curseur.",
    icone: "note", ton: "info", ok: "Insérer", champs: [{ nom: "texte", libelle: "Texte de la note", type: "textarea" }],
    valider: (x) => (x.texte.trim() ? "" : "Écrivez le texte de la note.") });

  async function outilMef(cmd) {
    if (cmd === "tableau" || cmd === "liste") {
      // A structure can only be written in source mode.
      const sq = cmd === "tableau" ? await squeletteTableau() : "- premier élément\n- second élément";
      if (!sq) return;
      if (S.modeEdition !== "source") {
        modeEdition("source");
        const t = $("edition-texte");
        t.setSelectionRange(t.value.length, t.value.length);
      }
      insererSource(sq, "", true);
      $("edition-message").textContent = cmd === "tableau" ? "Tableau inséré dans la source : remplacez « Colonne 1 »… et remplissez les cases." : "";
      $("edition-message").className = "info";
      return;
    }
    if (S.modeEdition === "source") {
      if (cmd === "bold") insererSource("**", "**");
      else if (cmd === "italic") insererSource("*", "*");
      else if (cmd === "lien" || cmd === "note") {
        // The dialog takes focus: retain the text selection, then restore it.
        const t = $("edition-texte");
        const [d, f] = [t.selectionStart, t.selectionEnd];
        const v = await (cmd === "lien" ? demanderLien() : demanderNote());
        t.focus();
        t.setSelectionRange(d, f);
        if (v && cmd === "lien") insererSource("[", "](" + v.url.trim() + ")");
        if (v && cmd === "note") insererSource("^[" + v.texte.trim().replace(/[\[\]]/g, (c) => "\\" + c) + "]", "");
      }
      return;
    }
    const zone = $("edition-visuel");
    zone.focus();
    if (cmd === "bold" || cmd === "italic") { document.execCommand(cmd); garderBrouillon("edition"); return; }
    // The dialog takes focus: retain the editor selection, then restore it.
    const sel0 = getSelection();
    const garde = sel0.rangeCount && zone.contains(sel0.anchorNode) ? sel0.getRangeAt(0).cloneRange() : null;
    const rendre = () => {
      zone.focus();
      if (garde) { const s2 = getSelection(); s2.removeAllRanges(); s2.addRange(garde); }
    };
    if (cmd === "lien") {
      const v = await demanderLien();
      rendre();
      if (v) { document.execCommand("createLink", false, v.url.trim()); garderBrouillon("edition"); }
      return;
    }
    if (cmd === "note") {
      const v = await demanderNote();
      rendre();
      if (!v) return;
      const t = v.texte;
      const sel = getSelection();
      if (!sel.rangeCount || !zone.contains(sel.anchorNode)) return;
      const r = sel.getRangeAt(0);
      r.collapse(false);
      const a = GloseVisuel.atome("^[" + t.trim().replace(/[\[\]]/g, (c) => "\\" + c) + "]", "note", "v-note");
      r.insertNode(a);
      r.setStartAfter(a);
      sel.removeAllRanges();
      sel.addRange(r);
      garderBrouillon("edition");
    }
  }

  // The list of proposals to be created, kept up to date while
  // typing; with rendering, also the passage preview.
  let numeroChangements = 0;
  async function majChangements(rendu) {
    const x = S.edition;
    if (!x) return false;
    const texte = texteEdition();
    const ul = $("edition-changements");
    if (texte === x.original) {
      ul.textContent = "";
      ul.append(el("li", "vide", "Aucune pour l'instant : modifiez le texte."));
      return false;
    }
    if (x.mode === "revise") {
      ul.textContent = "";
      const li = el("li");
      if (x.kind === "addition") li.append(md("ins", "", texte));
      else if (!texte) li.append(md("del", "", x.ancien));
      else li.append(md("del", "", x.ancien), document.createTextNode("→"), md("ins", "", texte));
      ul.append(li);
      if (rendu) {
        $("edition-rendu").textContent = "";
        $("edition-rendu").append(md("p", "", texte || "(suppression)"));
      }
      return true;
    }
    const n = ++numeroChangements;
    const r = await api("/api/preview", { file: x.file, hash: x.hash, mode: x.mode, at: x.at, to: x.to || 0, where: x.where || "", tight: !!x.tight,
      start: x.start, end: x.end, text: texte, note: "", whole: $("edition-tenant").checked });
    if (n !== numeroChangements || S.edition !== x) return false; // a more recent keystroke
    if (!r.ok) { if (rendu) $("edition-message").textContent = r.data.message || "L'aperçu a échoué."; return false; }
    if (rendu) {
      // Like the text: parsed in an inert template; images receive
      // the token before loading (otherwise refused, hence broken).
      const modele = document.createElement("template");
      modele.innerHTML = r.data.html;
      ressources(modele.content);
      for (const a of modele.content.querySelectorAll("a[href]")) a.removeAttribute("href");
      $("edition-rendu").replaceChildren(modele.content);
    }
    ul.textContent = "";
    for (const c of r.data.changes) {
      const li = el("li");
      if (c.kind === "substitution") li.append(md("del", "", c.text), document.createTextNode("→"), md("ins", "", c.new));
      else if (c.kind === "addition") li.append(/\n/.test(c.text.trim()) ? el("ins", "bloc", c.text.replace(/^\n+|\s+$/g, "")) : md("ins", "", c.text));
      else if (c.kind === "deletion") li.append(/\n/.test(c.text.trim()) ? el("del", "bloc", c.text.replace(/^\n+|\s+$/g, "")) : md("del", "", c.text));
      else if (c.kind === "note") li.append(document.createTextNode(c.deleted ? "Note supprimée :" : "Note ajoutée :"), c.deleted ? el("del", "", c.text) : el("ins", "", c.text));
      ul.append(li);
    }
    if (!r.data.changes.length) ul.append(el("li", "vide", "Aucune proposition."));
    return true;
  }

  let minuterieChangements = 0;
  function changementsBientot() {
    clearTimeout(minuterieChangements);
    minuterieChangements = setTimeout(() => majChangements(false), 500);
  }

  async function apercu() {
    // A pending automatic refresh must not invalidate this explicit preview
    // while its response is in flight (numeroChangements).
    clearTimeout(minuterieChangements);
    $("edition-message").textContent = "";
    if (texteEdition() === S.edition.original) { $("edition-message").textContent = "Aucune modification pour l'instant."; return; }
    if (await majChangements(true)) modeEdition("apercu");
  }

  // A message for the agent asks for an answer: it goes with a suggestion,
  // not with a direct change (D-57).
  function majDirect() {
    const b = $("edition-direct");
    b.disabled = !!$("edition-note").value.trim();
    b.dataset.infobulle = b.disabled
      ? "Un message pour l'agent appelle une réponse : retirez-le pour appliquer directement, ou ajoutez la suggestion"
      : "Modifier le texte tout de suite, sans proposition : l'agent ne la discute pas (Ctrl+Maj+Entrée)";
  }

  async function enregistrerEdition(direct) {
    const x = S.edition;
    if (!x || S.envoi) return;
    direct = direct === true && x.mode !== "revise";
    if (direct && $("edition-note").value.trim()) {
      $("edition-message").textContent = "Une modification directe ne s'adresse pas à l'agent : retirez le message, ou ajoutez la suggestion.";
      return;
    }
    if (x.envoiIncertain) { $("edition-message").textContent = "Un enregistrement a été tenté : vérifiez le texte avant de recommencer. Votre saisie reste dans Brouillons."; return; }
    const texte = texteEdition();
    if (texte === x.original) { $("edition-message").textContent = "Aucune modification."; return; }
    const correction = x.mode === "revise";
    $("edition-message").className = "";
    const dest = correction ? { pour: "", texte: $("edition-note").value.trim() } : agents.destinataire("edition", $("edition-note").value.trim());
    const req = correction
      ? { file: x.file, hash: x.hash, start: x.annotation, text: texte, note: $("edition-note").value.trim() }
      : { file: x.file, hash: x.hash, mode: x.mode, at: x.at, to: x.to || 0, where: x.where || "", tight: !!x.tight,
        start: x.start, end: x.end, text: texte, note: dest.texte.trim(), whole: $("edition-tenant").checked, recipient: dest.pour };
    if (direct) Object.assign(req, { note: "", recipient: "", whole: false, direct: true });
    S.envoi = true;
    S.occupe++;
    let r = null;
    // The panel and the writes are released whatever happens (audit A14):
    // an unexpected exception must not leave them frozen until a reload.
    try {
      $("edition-ok").disabled = true;
      $("edition-direct").disabled = true;
      $("edition").querySelector(".corps").inert = true;
      const pret = await preparerBrouillon("edition");
      r = pret ? await api(correction ? "/api/revise" : "/api/edit", req)
        : { ok: false, status: 400, data: { message: "Brouillon non confirmé : vérifiez sa sauvegarde avant d'enregistrer." } };
      await apresEnvoiBrouillon(x, r);
    } catch (e) {
      r = r || { ok: false, status: 0, data: { message: "L'enregistrement a échoué (" + e.message + ") : votre saisie reste dans Brouillons." } };
    } finally {
      $("edition").querySelector(".corps").inert = false;
      $("edition-ok").disabled = false;
      majDirect();
      S.envoi = false;
    }
    try {
      if (r.ok) {
        S.annuler.push(correction
          ? { type: "correction", file: req.file, start: r.data.start, hashApres: r.data.hash, avant: x.original, apres: texte }
          : direct ? { type: "directe", file: req.file, hashApres: r.data.hash, req }
          : { type: "edition", file: req.file, start: r.data.start, end: r.data.end, hashApres: r.data.hash, req });
        S.retablir = [];
        S.edition = null;
        $("edition").close();
        avis(direct ? "Texte modifié directement (Ctrl+Z pour annuler)." : correction ? "Proposition modifiée." + (req.note ? " Votre message est dans son fil." : "")
          : req.note ? "Proposition enregistrée, avec votre message." : "Modifications enregistrées comme propositions.");
        await charger(true);
        return;
      }
      if (r.status === 409 && r.data.error !== "turn") {
        $("edition-message").textContent = "Le fichier a changé sur le disque : copiez votre texte, fermez, puis rouvrez le paragraphe.";
        await charger(true);
        return;
      }
      $("edition-message").textContent = r.data.message || "L'enregistrement a échoué.";
    } finally {
      S.occupe--;
    }
  }

  function brancher(compose) {
    $("edition").querySelector("form").addEventListener("submit", (e) => { e.preventDefault(); enregistrerEdition(); });
    // Closing retains the input; "Abandonner" in Brouillons removes it.
    const fermerEdition = async () => {
      const x = S.edition;
      // While saving, the panel stays open: a refusal (conflict)
      // must be able to leave the text there.
      if (S.envoi) { avis("Enregistrement en cours : le panneau se fermera de lui-même."); return; }
      garderBrouillon("edition");
      if (x && x.brouillon) { x.brouillon.active = false; brouillons.sauver(x.brouillon); }
      S.edition = null;
      $("edition").close();
    };
    $("edition-fermer").addEventListener("click", fermerEdition);
    $("edition-annuler").addEventListener("click", fermerEdition);
    $("edition-direct").addEventListener("click", () => enregistrerEdition(true));
    $("edition").addEventListener("cancel", (e) => { e.preventDefault(); fermerEdition(); });
    // Editor links do not open: clicking would leave Glose.
    $("edition-visuel").addEventListener("click", (e) => { if (e.target.closest("a")) e.preventDefault(); });
    $("edition-visuel").addEventListener("dragstart", (e) => { if (e.target.closest && e.target.closest("a")) e.preventDefault(); });
    $("edition-source").addEventListener("click", () => modeEdition("source"));
    $("edition").addEventListener("close", () => { S.edition = null; clearTimeout(minuterieChangements); viser(null); });
    // During composition, the text is not yet the author's final input:
    // the proposal list waits until composition ends.
    for (const id of ["edition-visuel", "edition-texte"]) {
      $(id).addEventListener("input", (e) => { if (!e.isComposing) { changementsBientot(); garderBrouillon("edition"); } });
      $(id).addEventListener("compositionend", () => { changementsBientot(); garderBrouillon("edition"); });
    }
    $("edition-mef").addEventListener("click", () => modeEdition("mef"));
    $("edition-voir").addEventListener("click", apercu);
    for (const b of $("edition-outils").querySelectorAll("button")) {
      b.addEventListener("mousedown", (e) => e.preventDefault()); // preserve the selection
      b.addEventListener("click", () => outilMef(b.dataset.cmd));
    }
    const zv = $("edition-visuel");
    zv.addEventListener("keydown", (e) => {
      if (compose(e)) return;
      if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); if (!e.repeat) enregistrerEdition(e.shiftKey); return; }
      if (e.key === "Enter" && !e.shiftKey) { e.preventDefault(); document.execCommand("insertParagraph"); }
      if (e.key === "Enter" && e.shiftKey) e.preventDefault();
    });
    zv.addEventListener("paste", (e) => {
      e.preventDefault();
      document.execCommand("insertText", false, (e.clipboardData.getData("text/plain") || "").replace(/\s*\n\s*/g, " "));
    });
    document.execCommand("defaultParagraphSeparator", false, "p");
    $("edition-tenant").addEventListener("change", () => {
      localStorage.setItem("glose-tenant", $("edition-tenant").checked ? "1" : "0");
      garderBrouillon("edition");
      if (S.edition) majChangements(S.modeEdition === "apercu");
    });
    for (const id of ["edition-texte", "edition-note"]) {
      $(id).addEventListener("keydown", (e) => {
        if (compose(e)) return;
        if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); if (!e.repeat) enregistrerEdition(e.shiftKey); }
      });
    }
  }

  function brancherTypes() {
    for (const b of $("edition-types").querySelectorAll("button")) b.addEventListener("click", () => choisirType(b.dataset.type));
  }

  function brancherNote() {
    $("edition-note").addEventListener("input", () => { garderBrouillon("edition"); majDirect(); });
  }

    return { modifierBloc, ajouter, ouvrirEdition, texteEdition, brancher, brancherTypes, brancherNote };
  }
  return { creer };
})();
