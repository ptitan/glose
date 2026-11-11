// Glose: review interface. The displayed text is produced by the server
// (pandoc, from a tree where raw HTML is neutralized). Each word
// carries data-g="fichier:début,fin[,t]", its byte position in the source.
// The interface only sends intentions; the server validates and writes.
"use strict";
(() => {
  // Pure functions, tested without a browser (web/donnees.js).
  const { lireG, court, signature, cleAnnotation, premierApres, compter, rayon, bilan, heure, quand, modeleCourt } = GloseDonnees;
  // References captured before any text injection: a heading in the text
  // cannot be mistaken for them (its identifiers are also prefixed).
  const UI = {};
  for (const id of ["bouton-sommaire", "consignes-barre", "remise-consignes", "remise-consigne", "remise-fichiers", "remise-acces", "remise-blocages", "titre", "etat", "annuler", "retablir", "anomalies", "bandeau", "sommaire",
    "texte", "marge", "bouton-commenter", "poignee", "menu-bloc", "menu-commenter", "menu-suggerer", "saisie", "saisie-titre", "saisie-extrait",
    "saisie-texte", "saisie-message", "saisie-autre", "saisie-fermer", "saisie-ok", "saisie-annuler", "edition-annuler", "avis", "outils-bloc",
    "edition", "edition-texte", "edition-message", "edition-ok", "edition-direct", "edition-fermer",
    "bouton-reglages", "reglages", "r-police", "r-taille", "r-taille-val", "r-interligne", "r-interligne-val",
    "r-largeur", "r-largeur-val", "r-justifie", "r-defaut", "menu-selection", "bouton-suggerer", "edition-note",
    "edition-titre", "edition-info", "navette", "remettre", "remise", "remise-titre", "remise-info", "remise-alerte", "remise-lier", "remise-lancement", "remise-lancer", "remise-lancer-texte", "remise-outils", "saisie-pour-ligne", "edition-pour-ligne", "remise-note",
    "remise-message", "remise-ok", "remise-force", "remise-fermer", "edition-source", "edition-voir", "edition-apercu",
    "edition-rendu", "edition-changements", "edition-mef", "edition-visuel", "edition-outils", "edition-aide-source", "vues", "edition-elargir", "edition-types", "menu-avant", "menu-apres", "carte-tour", "puces", "navette-annonce", "rail", "rail-ouvrir", "boite", "boite-icone", "boite-titre", "boite-message", "boite-champs", "boite-erreur", "boite-ok", "boite-annuler", "edition-tenant", "edition-tenant-case",
    "onglets", "onglet-compte", "onglet-titre", "poignee-colonne", "cote", "app", "annotations", "filtres", "annotations-liste", "historique", "fichiers",
    "bouton-brouillons", "brouillons-compte", "brouillons", "brouillons-contenu", "brouillons-fermer", "saisie-brouillon", "edition-brouillon", "avancement", "barre", "main-navette", "menu-navette", "ouvrir-recherche", "recherche", "recherche-texte", "remettre-court", "remettre-libelle", "reserve-barres", "portee", "pied", "exporter", "export", "export-ok", "export-portee", "export-commentaires", "export-commentaires-ligne", "annotations-traitees", "r-theme"]) {
    UI[id] = document.getElementById(id);
  }
  const $ = (id) => UI[id];
  const S = {
    jeton: "", doc: null, hashes: {}, spans: [], parFichier: new Map(), cartes: [],
    annuler: [], retablir: [], saisie: null, occupe: 0, generation: 0,
    actif: -1, bloc: null, echecs: 0, attente: 0, envoi: false,
    // Text view (F3): relecture and actuel use the server's Actuel view,
    // propose uses its Proposé view; one rendering per server view is
    // kept to switch without waiting.
    vue: ["relecture", "actuel", "propose", "depuis"].includes(localStorage.getItem("glose-vue")) ? localStorage.getItem("glose-vue") : "relecture",
    rendus: {}, zones: {}, zone: null,
  };
  // The container of the displayed rendering (one per server view, inside #texte).
  const zone = () => S.zone || $("texte");
  const vueServeur = (v) => (v === "propose" ? "proposed" : "current");
  const BLOCS = "p, li, dd, dt, h1, h2, h3, h4, h5, h6, td, th, figure, figcaption, pre";
  // Structures rewritten as a whole, using complete source lines.
  const STRUCTURES = "table, ul, ol, dl, blockquote";
  const NOMS = {
    comment: "Commentaire", highlight: "Passage commenté", addition: "Ajout proposé",
    deletion: "Suppression proposée", substitution: "Remplacement proposé",
  };
  const PROPOSITION = { addition: true, deletion: true, substitution: true };
  const QUI = { author: "Auteur", agent: "Agent", unknown: "Inconnu" };
  const douceur = () => (matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth");
  // A "%" that does not introduce an encoded byte (image "taux_50%.png",
  // copied address) would make decodeURIComponent throw: keep the string.
  function decoder(s) {
    try { return decodeURIComponent(s); } catch { return s; }
  }
  // An unreadable stored value (interrupted write, another version) must
  // not prevent display: fall back to the default value.
  function lireJSON(stockage, cle, defaut, valide) {
    try {
      const v = JSON.parse(stockage.getItem(cle) || "null");
      return v !== null && valide(v) ? v : defaut;
    } catch {
      return defaut;
    }
  }

  // ---------- Session token ----------
  // It arrives in the "#t=…" part of the address, which the browser never
  // sends to the server; it is kept in session storage specific to this
  // port, and removed from the address bar.
  function lireJeton() {
    const m = /(?:^#|&)t=([0-9a-f]{64})/.exec(location.hash);
    // The requested project part (glose open livre-1/, D-35).
    const f = /(?:^#|&)f=([^&]*)/.exec(location.hash);
    if (f) S.focus = decoder(f[1]);
    if (m) {
      sessionStorage.setItem("glose-jeton", m[1]);
      history.replaceState(null, "", location.pathname);
    }
    return sessionStorage.getItem("glose-jeton") || "";
  }

  const { boite, avis, bandeau, el, icone, panneau, riche } = GloseInterface.creer({ $ });
  const { majBoutons, annulerDernier, retablirDernier } = GloseHistorique.creer({ $, S, api, avis, charger });

  const { fabriquerCarte, nomAnnotation, disposer, disposerMaintenant, brancherRepli } = GloseMarge.creer({
    $, S, el, icone, md, riche, court, modeleCourt, NOMS, PROPOSITION, QUI, activer,
    modifiable, outilsMessage, teteModifiable, modifier, retirer, resoudre,
    corrigeable, corriger, repondre, decider, appliquerCorrection,
  });

  async function api(chemin, corps) {
    const opt = { headers: { "X-Glose-Token": S.jeton } };
    // The displayed file list: the server refuses a request identifying
    // a file by its index if the list has changed since (D-38).
    if (S.doc && S.doc.filesGen !== undefined) opt.headers["X-Glose-Files"] = String(S.doc.filesGen);
    if (corps !== undefined) {
      opt.method = "POST";
      opt.headers["Content-Type"] = "application/json";
      opt.body = JSON.stringify(corps);
    }
    let r;
    try { r = await fetch(chemin, opt); } catch (e) {
      return { ok: false, status: 0, data: { message: "Le serveur Glose ne répond pas (est-il arrêté ?)." } };
    }
    let data = {};
    try { data = await r.json(); } catch (e) { /* response without JSON */ }
    if (r.status === 409 && data && data.error === "files") {
      // File list changed: render again; the action must be repeated.
      Promise.resolve().then(() => charger(true));
    }
    if (r.status === 401) {
      bandeau("Session absente ou périmée : ouvrez de nouveau l'adresse complète affichée par « glose open » (ou relancez-le).");
    }
    return { ok: r.ok, status: r.status, data };
  }

  // ---------- Persistent private inputs (P5.1) ----------
  let stockageBrouillons;
  try { stockageBrouillons = localStorage; } catch (_) { /* disk only */ }
  const brouillons = GloseBrouillons.creer({ api, stockage: stockageBrouillons,
    identifiant: () => crypto.randomUUID().replaceAll("-", ""), changement: (entrees, avertissement) => {
      $("brouillons-compte").textContent = String(entrees.length);
      $("bouton-brouillons").classList.toggle("plein", entrees.length > 0);
      for (const type of ["saisie", "edition"]) {
        const x = S[type], e = x && x.brouillon;
        const p = $(type + "-brouillon");
        p.textContent = !e || e.closed ? "" : e.error || (e.pending || e.busy ? "Sauvegarde du brouillon…"
          : e.draft.sending ? "Brouillon sauvegardé ; enregistrement du texte à vérifier." : "Brouillon sauvegardé dans le projet.");
        if (avertissement && e && (e.pending || e.busy)) p.textContent += " " + avertissement;
        p.classList.toggle("erreur", !!(e && e.error));
      }
    } });

  function contenuBrouillon(type) {
    const x = S[type];
    if (!x || !S.doc || !S.doc.files[x.file]) return null;
    const edit = type === "edition";
    return { kind: edit ? "edit" : x.mode === "reply" ? "reply" : x.mode === "amend" ? "amend" : "comment",
      path: S.doc.files[x.file].path, hash: x.hash, mode: x.mode,
      start: x.start || 0, end: x.end || 0, at: x.at || 0, to: x.to || 0, annotation: x.annotation || 0,
      annotationID: x.annotationID || "", proposalKind: x.kind || "", old: x.ancien || "",
      original: x.original || "", text: edit ? texteEdition() : $("saisie-texte").value,
      note: edit ? $("edition-note").value : "", excerpt: x.extrait || (edit ? x.original : ""), prefix: x.prefixe || "",
      where: x.where || "", tight: !!x.tight, whole: edit && $("edition-tenant").checked,
      editor: edit ? S.modeEdition : "source", title: $(type + "-titre").textContent,
      info: edit ? $("edition-info").textContent : "", sending: !!x.envoiIncertain };
  }

  function garderBrouillon(type) {
    if (!brouillons.pret || !$(type).open || S.envoi) return null;
    const x = S[type], d = contenuBrouillon(type);
    if (!d) return null;
    if (!x.brouillon || x.brouillon.closed) {
      if (d.kind === "edit" ? d.text === d.original && !d.note : d.kind === "amend" ? d.text.trim() === d.original.trim() : !d.text) return null;
      x.brouillon = brouillons.nouveau(d);
    } else brouillons.modifier(x.brouillon, d);
    x.brouillon.active = true;
    return x.brouillon;
  }

  async function preparerBrouillon(type) {
    const x = S[type], d = contenuBrouillon(type);
    if (!d || !brouillons.pret) return false;
    d.sending = true;
    if (!x.brouillon || x.brouillon.closed) x.brouillon = brouillons.nouveau(d);
    else brouillons.modifier(x.brouillon, d);
    x.brouillon.active = true;
    return brouillons.sauver(x.brouillon);
  }

  async function apresEnvoiBrouillon(x, r) {
    const e = x && x.brouillon;
    if (!e) return;
    if (r.ok) {
      e.active = false;
      if (!(await brouillons.retirer(e))) avis("Texte enregistré ; le brouillon reste à vérifier dans Brouillons.", true);
    } else if (r.status >= 400 && r.status < 500) {
      // A definite refusal did not apply the operation. Network errors and
      // server errors remain uncertain and cannot be automatically resubmitted.
      brouillons.modifier(e, Object.assign({}, e.draft, { sending: false }));
      await brouillons.sauver(e);
    } else {
      x.envoiIncertain = true;
    }
  }

  async function ouvrirBrouillons() {
    if (S.envoi) return;
    garderBrouillon("saisie"); garderBrouillon("edition");
    if (S.doc) await charger(true);
    if (!(await brouillons.charger())) { avis("Impossible de lire les brouillons du projet.", true); return; }
    const liste = $("brouillons-contenu");
    liste.textContent = "";
    for (const p of brouillons.problemes) liste.append(el("p", "erreur", p));
    if (!brouillons.entrees.size) liste.append(el("p", "", "Aucun brouillon."));
    for (const e of brouillons.entrees.values()) {
      const d = e.draft, bloc = el("section", "brouillon");
      bloc.append(el("h3", "", (d.kind === "edit" ? "Réécriture" : d.kind === "reply" ? "Réponse" : d.kind === "amend" ? "Message modifié" : "Commentaire") + " · " + d.path));
      if (d.excerpt) bloc.append(el("blockquote", "", d.excerpt));
      const texte = el("textarea"); texte.readOnly = true; texte.value = d.text;
      texte.setAttribute("aria-label", "Texte du brouillon à copier"); bloc.append(texte);
      if (d.note) { const note = el("textarea"); note.readOnly = true; note.value = d.note; note.setAttribute("aria-label", "Message pour l'agent à copier"); bloc.append(note); }
      const f = S.doc && S.doc.files.find((f) => f.path === d.path);
      const main = S.doc && S.doc.state && S.doc.state.hand === "agent";
      const raison = d.sending ? "Un enregistrement a été tenté : vérifiez le texte et les annotations avant toute nouvelle proposition."
        : !f || f.hash !== d.hash ? "Le fichier a changé ou n'est plus affiché. Copiez la saisie et sélectionnez de nouveau sa cible ; rien n'est réappliqué automatiquement."
        : main ? "L'agent a la main. Le brouillon reste conservé ; reprenez-le quand vous aurez la main." : "";
      if (raison || e.error) bloc.append(el("p", "", raison || e.error));
      const actions = el("div", "actions");
      const reprendre = el("button", "", "Reprendre"); reprendre.type = "button"; reprendre.disabled = !!raison;
      reprendre.addEventListener("click", async () => {
        if (S.envoi) return;
        // A panel already open must not be silently replaced, even if it
        // currently contains only a message and no textual change.
        if ($("saisie").open || $("edition").open) { avis("Fermez d'abord la saisie en cours : son brouillon sera conservé.", true); return; }
        $("brouillons").close();
        if (!(await charger(true))) { avis("Impossible de vérifier la cible : le brouillon reste conservé.", true); return; }
        const file = S.doc.files.findIndex((f) => f.path === d.path && f.hash === d.hash);
        if (file < 0 || S.doc.state.hand === "agent") { avis("Le contexte a changé ; retrouvez le texte dans Brouillons.", true); return; }
        e.active = true;
        if (d.kind === "edit") {
          ouvrirEdition(d.title, d.info, { mode: d.mode, file, hash: d.hash, at: d.at, to: d.to, start: d.start, end: d.end,
            annotation: d.annotation, annotationID: d.annotationID, kind: d.proposalKind, ancien: d.old, original: d.original, brouillon: e }, d);
        } else {
          S.saisie = { mode: d.mode, file, hash: d.hash, start: d.start, end: d.end, extrait: d.excerpt, prefixe: d.prefix,
            annotationID: d.annotationID, original: d.original, reponse: d.title.startsWith("Modifier votre réponse"), brouillon: e };
          ouvrirSaisie(d.title, d.excerpt, "Brouillon récupéré.", d.text);
        }
      });
      const retirer = el("button", "", "Abandonner"); retirer.type = "button";
      retirer.addEventListener("click", async () => {
        if (e.active) { avis("Fermez d'abord le panneau de cette saisie.", true); return; }
        if (!(await boite({ titre: "Abandonner ce brouillon ?", message: "Sa saisie sera supprimée, sans modifier le manuscrit.", ok: "Abandonner", ton: "danger" }))) return;
        if (await brouillons.retirer(e)) bloc.remove(); else avis(e.error, true);
      });
      const sauver = el("button", "", "Réessayer la sauvegarde"); sauver.type = "button"; sauver.hidden = !e.error;
      sauver.addEventListener("click", async () => { await brouillons.sauver(e); avis(e.error || "Brouillon sauvegardé.", !!e.error); });
      const copier = el("button", "", "Copier"); copier.type = "button";
      copier.addEventListener("click", async () => {
        try {
          await navigator.clipboard.writeText(d.text + (d.note ? "\n\n" + d.note : ""));
          avis("Brouillon copié.");
        } catch (_) { texte.focus(); texte.select(); avis("Sélectionnez et copiez le texte du brouillon.", true); }
      });
      actions.append(reprendre, retirer, sauver, copier); bloc.append(actions); liste.append(bloc);
    }
    $("brouillons").showModal();
  }

  // A small Markdown fragment (bold, italic, code, note…) displayed with
  // formatting by the editor's parser (visuel.js); plain text as a fallback.
  function md(tag, classe, texte) {
    const e = el(tag, classe);
    const zone = document.createElement("div");
    // Leading and trailing spaces remain text: parsed alone, they
    // would be mistaken for a block prefix (blockquote, list).
    const [, avant, coeur, apres] = /^(\s*)([\s\S]*?)(\s*)$/.exec(texte);
    if (coeur && window.GloseVisuel && !/\n\s*\n/.test(coeur) && GloseVisuel.lire(coeur, zone) && zone.firstChild) {
      for (const a of zone.querySelectorAll("[contenteditable]")) a.removeAttribute("contenteditable");
      e.append(avant, ...zone.firstChild.childNodes, apres);
    } else {
      e.textContent = texte;
    }
    return e;
  }

  // Text of source words within [s, e) in a file, without
  // generated content (numbers, computed references).
  function texteSource(f, s, e) {
    const mots = S.parFichier.get(f) || [];
    let t = "";
    for (let j = premierApres(mots, s); j < mots.length && mots[j].e <= e; j++) t += mots[j].el.textContent;
    return t;
  }

  const { appliquerGrille, brancherColonne, brancherReglages, chargerReglages } =
    GloseReglages.creer({ $, S, api, avis, el, disposer });

  // Hiding the column leaves a rail on the left: reopen it, or go straight
  // to a tab (P9.2). The choice persists between sessions.
  function montrerColonne(ouvrir, discret, retenir = true) {
    const c = $("cote");
    if (c.hidden === !ouvrir) return;
    c.hidden = !ouvrir;
    $("rail").hidden = ouvrir;
    $("app").classList.toggle("sans-colonne", !ouvrir);
    $("bouton-sommaire").setAttribute("aria-expanded", String(ouvrir));
    $("rail-ouvrir").setAttribute("aria-expanded", String(ouvrir));
    if (retenir) localStorage.setItem("glose-colonne-masquee", ouvrir ? "0" : "1");
    appliquerGrille();
    disposer();
    if (!discret) (ouvrir ? $("bouton-sommaire") : $("rail-ouvrir")).focus({ preventScroll: true });
  }

  // The narrow drawer contains the original cards; no second review state.
  const fenetreEtroite = matchMedia("(max-width: 800px)");
  let retourAnnotations = null;
  const annotationsOuvrir = el("button", "", "Annotations (0)");
  annotationsOuvrir.id = "annotations-ouvrir";
  annotationsOuvrir.type = "button";
  annotationsOuvrir.setAttribute("aria-controls", "marge");
  annotationsOuvrir.setAttribute("aria-expanded", "false");
  const annotationsFermer = el("button", "", "Fermer les annotations");
  annotationsFermer.id = "annotations-fermer";
  annotationsFermer.type = "button";
  function panneauAnnotations(ouvrir, focus = false) {
    if (ouvrir && !fenetreEtroite.matches) return;
    if (ouvrir && !document.body.classList.contains("annotations-ouvertes")) retourAnnotations = document.activeElement;
    document.body.classList.toggle("annotations-ouvertes", ouvrir);
    annotationsOuvrir.setAttribute("aria-expanded", String(ouvrir));
    disposer();
    if (focus) (ouvrir ? annotationsFermer : retourAnnotations && retourAnnotations.isConnected ? retourAnnotations : annotationsOuvrir).focus({ preventScroll: true });
  }
  annotationsOuvrir.addEventListener("click", () => panneauAnnotations(!document.body.classList.contains("annotations-ouvertes"), true));
  annotationsFermer.addEventListener("click", () => panneauAnnotations(false, true));
  document.querySelector(".barre-document").append(annotationsOuvrir);
  document.addEventListener("keydown", (e) => {
    if (e.key !== "Escape" || e.defaultPrevented || e.isComposing || document.querySelector("dialog[open]") ||
      !document.body.classList.contains("annotations-ouvertes")) return;
    e.preventDefault();
    panneauAnnotations(false, true);
  });
  fenetreEtroite.addEventListener("change", () => panneauAnnotations(false));

  // ---------- Displayed part (P6.8) ----------
  // The key of the retained part, per project (its directory, supplied by the server).
  function clePartie() {
    return "glose-partie:" + (S.racine || "");
  }

  // Display another project part: "" (all), a file, or a directory
  // ending with "/". Without per-file rendering (a whole-document filter),
  // everything is already displayed: navigate to the part.
  async function choisirPartie(partie, apres) {
    if (S.doc && !S.doc.perFile) {
      const cible = [...zone().querySelectorAll(".glose-file")].find((d) => d.dataset.path === partie || d.dataset.path.startsWith(partie));
      if (cible) cible.scrollIntoView({ block: "start", behavior: douceur() });
      if (apres) apres();
      return !!cible;
    }
    S.partie = partie;
    S.rendus = {};
    if (await charger(false)) {
      document.scrollingElement.scrollTop = 0;
      if (apres) apres();
      return true;
    }
    return false;
  }

  const navigation = GloseNavigation.creer({ api, courant: () => ({ doc: S.doc, zone: zone() }),
    afficherPartie: choisirPartie, avis, amener: (cible) => amener(cible, Math.max(100, document.getElementById("barre").getBoundingClientRect().height + 24)) });
  const agents = GloseAgents.creer({ api, etat: () => S.doc && S.doc.state, avis, jeton: () => S.jeton, doc: () => S.doc,
    recharger: () => charger(true) });
  const navette = GloseNavette.creer({ $, S, api, avis, el, icone, court, quand, agents, majBoutons, charger, saisieEnCours, voirAnnotation: async (b) => {
    await choisirPartie(b.file, () => { const i = S.doc.annotations.findIndex(a => S.doc.files[a.file].path === b.file && a.start === b.start); if (i >= 0) activer(i, true, true); });
  } });
  const { majNavette } = navette;
  function saisieEnCours() {
    if (S.vue === "depuis") { avis("La vue Depuis est en lecture seule."); return true; }
    const d = $("saisie").open ? $("saisie") : $("edition").open ? $("edition") : null;
    if (!d) return false;
    avis("Fermez d'abord la saisie en cours : son brouillon sera conservé.");
    return true;
  }

  // Resolve the target from certified source positions, including recovered drafts.
  function cibleSaisie(x) {
    if (!x) return null;
    if (x.element && x.element.isConnected) return x.element;
    const mots = S.parFichier.get(x.file) || [];
    let k = premierApres(mots, x.start || x.at || 0);
    if (k > 0 && mots[k - 1].e > (x.start || x.at || 0)) k--;
    const mot = mots[Math.min(k, mots.length - 1)];
    return mot && (mot.el.closest(BLOCS) || mot.el);
  }
  const panneauSaisie = panneau({ dialogue: $("saisie"), cible: () => cibleSaisie(S.saisie) });

  const edition = GloseEdition.creer({ $, S, brouillons, avis, boite, el, md, agents, panneau, cibleSaisie, saisieEnCours, zone, STRUCTURES, lireG, fermerOutils, charger, api, viser, garderBrouillon, preparerBrouillon, apresEnvoiBrouillon, ressources });
  const { modifierBloc, ajouter, ouvrirEdition, texteEdition } = edition;

  const { ouvrirSaisie, fermerSaisie, enregistrer } = GloseSaisie.creer({ $, S, brouillons, avis, court, agents, viser, panneauSaisie, garderBrouillon, preparerBrouillon, apresEnvoiBrouillon, api, charger });

  const { sommaire, choisirOnglet } = GloseColonne.creer({ $, S, lireJSON, el, icone, choisirPartie, zone, court, retenirTitres, marquerSection, PROPOSITION, NOMS, md, cleAnnotation, activer, deciderTout, api, compter, bilan, rayon, heure, quand });

  const recherche = GloseRecherche.creer({ api, courant: () => ({ doc: S.doc, view: vueServeur(S.vue) }),
    ouvrir: (m) => navigation.ouvrir(m, m.hash), avis });

  // ---------- Loading ----------
  // Return true if the display was updated.
  async function charger(garderPosition) {
    const gen = ++S.generation;
    const repere = (garderPosition && capturer()) || lectureRetenue();
    const focus = document.activeElement && document.activeElement.closest && document.activeElement.closest(".carte");
    // Cards are found by their annotation's identity, rather than their
    // index, which changes when an annotation appears or disappears.
    const iFocus = focus ? S.cartes.findIndex((c) => c.carte === focus) : -1;
    const carteFocus = iFocus >= 0 ? cleAnnotation(S.cartes[iFocus].a) : null;
    S.occupe++;
    $("texte").setAttribute("aria-busy", "true");
    if (S.doc) $("etat").textContent = "mise à jour…";
    const vs = vueServeur(S.vue);
    // The displayed project part (P6.8): selected, requested by "glose
    // open livre-1/", retained between visits, or chosen by the
    // server ("?": all for a small project, otherwise the first file).
    if (S.partie === undefined) {
      if (S.racine === undefined) {
        const p = await api("/api/ping");
        S.racine = p.ok ? p.data.root : "";
      }
      const retenue = localStorage.getItem(clePartie()); // "": the entire project
      S.partie = S.focus || (retenue !== null ? retenue : "?");
    }
    const url = () => "/api/doc?part=" + encodeURIComponent(S.partie) + (vs === "current" ? "" : "&view=" + vs);
    let r = await api(url());
    // Text changed during rendering (handoff, external write): the rendering
    // is stale; request it again silently, up to a few times.
    for (let essai = 0; essai < 3 && !r.ok && r.status === 409 && r.data && r.data.error === "stale-render" && gen === S.generation; essai++) {
      r = await api(url());
    }
    S.occupe--;
    if (gen !== S.generation) return false; // a more recent load has occurred
    if (r.ok) {
      S.partie = r.data.part;
      localStorage.setItem(clePartie(), S.partie);
    }
    if (!r.ok) {
      $("texte").setAttribute("aria-busy", "false");
      if (S.doc) majEtat();
      if (r.status !== 401) avis(r.data.message || "Le rendu du texte a échoué.", true);
      return false;
    }
    // Renderings of other views are only retained if up to date.
    for (const k of Object.keys(S.rendus)) if (signature(S.rendus[k]) !== signature(r.data)) delete S.rendus[k];
    S.rendus[vs] = r.data;
    afficher(r.data, repere, carteFocus);
    if (S.focus) { // once only, on first display
      const cible = [...zone().querySelectorAll(".glose-file")].find((d) => d.dataset.path === S.focus || d.dataset.path.startsWith(S.focus));
      S.focus = "";
      if (cible && !S.doc.perFile) cible.scrollIntoView({ block: "start" });
    }
    return true;
  }

  // Switch views without reloading the page or losing the reading position
  // (F3). Relecture and Actuel share a rendering; Proposé has its own,
  // kept until the files or journal change.
  async function changerVue(v) {
    if ($("saisie").open || $("edition").open) { avis("Fermez la saisie avant de changer de vue."); return false; }
    if (v === S.vue || !["relecture", "actuel", "propose", "depuis"].includes(v)) return false;
    S.vue = v;
    localStorage.setItem("glose-vue", v);
    majVues();
    if (!S.doc) return false;
    const garde = v === "depuis" ? null : S.rendus[vueServeur(v)];
    if (garde && signature(garde) === signature(S.doc)) {
      S.generation++; // abandon an ongoing load for the other view
      afficher(garde, capturer(), -1);
    } else if (!(await charger(true))) return false;
    avis("Vue « " + { relecture: "Relecture", actuel: "Actuel", propose: "Proposé", depuis: "Depuis" }[v] + " ».");
    return true;
  }

  function majVues() {
    for (const b of $("vues").querySelectorAll("button")) b.setAttribute("aria-checked", String(b.dataset.vue === S.vue));
    document.body.dataset.vue = S.vue;
  }

  // Display a server rendering in the current view. If it is already
  // in the page (Relecture and Actuel share theirs), only rebuild
  // annotations: the HTML is not parsed again.
  function afficher(d, repere, carteFocus) {
    S.actifCle = S.cartes[S.actif] ? cleAnnotation(S.cartes[S.actif].a) : null;
    S.doc = d;
    S.hashes = {};
    d.files.forEach((f) => { S.hashes[f.index] = f.hash; });
    bandeau("");
    $("titre").textContent = d.title;
    $("titre").dataset.infobulle = d.title;
    document.title = d.title && d.title !== "Glose" ? d.title + " · Glose" : "Glose";
    document.title = d.title + " — Glose";
    sessionStorage.setItem("glose-texte", d.title || "");
    const texte = $("texte");
    // The hovered block belongs to the old display: forget it.
    fermerOutils();
    S.bloc = null;
    // One container per server view: the hidden one retains its
    // layout (content-visibility: hidden), so switching is immediate.
    for (const c of Array.from(texte.children)) if (!c.classList.contains("rendu")) c.remove();
    const vs = vueServeur(S.vue);
    let z = S.zones[vs];
    if (!z) {
      // The class stays on the container: changing a class on #texte
      // would also force recalculation of the hidden rendering.
      z = S.zones[vs] = el("div", "rendu" + (vs === "proposed" ? " vue-propose" : ""));
      z.dataset.vue = vs;
      texte.append(z);
    }
    for (const [k, autre] of Object.entries(S.zones)) {
      autre.classList.toggle("garde", k !== vs);
      // Clear a stale rendering (files or journal changed since).
      if (k !== vs && autre.doc && !(S.rendus[k] === autre.doc)) { autre.replaceChildren(); autre.doc = null; }
    }
    S.zone = z;
    if (z.doc === d) {
      nettoyer();
      S.spans = z.spans; S.parFichier = z.parFichier;
    } else {
      // Parse into an inert template: images are not loaded there
      // before receiving the token (ressources).
      const modele = document.createElement("template");
      modele.innerHTML = d.html;
      ressources(modele.content);
      z.replaceChildren(modele.content);
      z.doc = d;
      indexer();
      z.spans = S.spans; z.parFichier = S.parFichier;
    }
    texte.setAttribute("aria-busy", "false");
    zone().querySelectorAll(".g-depuis").forEach(e => e.classList.remove("g-depuis"));
    anomalies();
    majMissions();
    annoter();
    sommaire();
    majEtat();
    majBoutons();
    majTypo();
    if (repere) restaurer(repere);
    disposerMaintenant();
    // The active card remains the same annotation. If it disappeared (decided):
    // with the keyboard, the next becomes active (J, A, A… chains actions);
    // with the mouse, none does.
    if (S.actifCle) {
      const k = S.cartes.findIndex((c) => !c.cache && cleAnnotation(c.a) === S.actifCle);
      if (k >= 0) S.actif = k;
      else if (!S.clavier) S.actif = -1;
      else {
        const suivantes = S.cartes.map((c, i) => i).filter((i) => i >= S.actif && !S.cartes[i].cache);
        S.actif = suivantes.length ? suivantes[0] : -1;
      }
    }
    if (S.vue === "depuis") afficherDepuis();
    document.getElementById("depuis-choix").hidden = S.vue !== "depuis";
    if (S.actif >= 0 && S.actif < S.cartes.length) activer(S.actif, false, S.clavier, true);
    else desactiver();
    const iCarte = carteFocus ? S.cartes.findIndex((c) => cleAnnotation(c.a) === carteFocus) : -1;
    if (iCarte >= 0) S.cartes[iCarte].carte.focus({ preventScroll: true });
  }

  function majMissions() {
    const choix = document.getElementById("mission-filtre");
    const ids = [...new Set(S.doc.annotations.flatMap(a => [a.task, ...a.messages.map(m => m.task)]).filter(Boolean))];
    choix.replaceChildren(el("option", "", "Toutes")); choix.firstChild.value = "";
    for (const id of ids) { const o = el("option", "", S.doc.taskLabels?.[id]?.name || id); o.value = id; choix.append(o); }
    if (S.filtreMission && !ids.includes(S.filtreMission)) S.filtreMission = "";
    choix.value = S.filtreMission || "";
    // Shown only when there is something to filter (P20).
    document.getElementById("mission-choix").hidden = !ids.length;
  }
  document.getElementById("mission-filtre").addEventListener("change", e => {
    S.filtreMission = e.target.value; afficher(S.doc, capturer(), -1);
  });

  function majTypo() {
    const choix = document.getElementById("typo-fichier"), avant = choix.value;
    choix.replaceChildren(...S.doc.files.filter(f => f.shown).map(f => { const o = el("option", "", f.path); o.value = f.index; return o; }));
    if ([...choix.options].some(o => o.value === avant)) choix.value = avant;
    document.getElementById("typo-choix").hidden = choix.options.length < 2;
    const interdit = S.vue === "depuis" || S.doc.state?.hand === "agent";
    document.getElementById("typo-lancer").disabled = interdit || !choix.options.length;
    document.getElementById("typo-accepter").hidden = interdit || !S.doc.annotations.some(a => a.via === "Typo" && a.file === Number(choix.value) && PROPOSITION[a.kind]);
  }
  document.getElementById("typo-fichier").addEventListener("change", majTypo);
  document.getElementById("typo-lancer").addEventListener("click", async () => {
    if (saisieEnCours() || S.ecriture) return;
    S.ecriture = true; S.occupe++;
    try {
      const file = Number(document.getElementById("typo-fichier").value);
      const r = await api("/api/typo", {file, hash:S.hashes[file]});
      avis(r.ok ? "Les corrections de Typo sont proposées dans la marge." : r.data.message, !r.ok);
      await charger(true);
    } finally { S.ecriture = false; S.occupe--; }
  });
  document.getElementById("typo-accepter").addEventListener("click", () => {
    const f = Number(document.getElementById("typo-fichier").value);
    deciderTout(f, "accept", S.doc.annotations.filter(a => a.file === f && a.via === "Typo" && PROPOSITION[a.kind]).length, "Typo");
  });

  let generationDepuis = 0;
  async function afficherDepuis() {
    const gen = ++generationDepuis, doc = S.doc;
    zone().querySelectorAll(".g-depuis").forEach(e => e.classList.remove("g-depuis"));
    const r = await api("/api/since" + (S.referenceDepuis ? "?turn=" + S.referenceDepuis : ""));
    if (gen !== generationDepuis || S.vue !== "depuis" || S.doc !== doc) return;
    if (!r.ok) { avis(r.data.message || "La comparaison a échoué.", true); return; }
    if (r.data.turn && (r.data.filesGen !== doc.filesGen || doc.files.some(f => f.hash && r.data.hashes[f.path] !== f.hash))) { avis("Le texte a changé pendant la comparaison : actualisation."); charger(true); return; }
    const choix = document.getElementById("depuis-reference");
    choix.replaceChildren(...r.data.references.map(ref => { const o = el("option", "", "Depuis le tour " + ref.turn); o.value = ref.turn; return o; }));
    choix.value = String(r.data.turn);
    const bilan = el("div", "depuis-bilan");
    bilan.textContent = r.data.turn ? "Depuis le tour " + r.data.turn + " — lecture seule" : "Aucune copie de remise disponible.";
    for (const c of r.data.changes) {
      const f = doc.files.find(f => f.path === c.path);
      if (f) for (const mot of S.parFichier.get(f.index) || []) if (mot.s < c.end && mot.e > c.start) mot.el.classList.add("g-depuis");
      const ligne = el("p", "", c.path + " : ");
      if (c.status) ligne.append(document.createTextNode(c.status === "appeared" ? "fichier apparu" : "fichier disparu"));
      else { if (c.old) ligne.append(el("del", "", c.old)); if (c.new) ligne.append(el("ins", "", c.new)); }
      bilan.append(ligne);
    }
    document.getElementById("marge").append(bilan);
  }
  document.getElementById("depuis-reference").addEventListener("change", e => { S.referenceDepuis = e.target.value; charger(true); });

  function indexer() {
    S.spans = Array.from(zone().querySelectorAll("span[data-g]"));
    S.parFichier = new Map();
    for (const sp of S.spans) {
      const g = lireG(sp.dataset.g);
      if (!g) continue;
      if (!S.parFichier.has(g.f)) S.parFichier.set(g.f, []);
      S.parFichier.get(g.f).push({ s: g.s, e: g.e, el: sp });
    }
    for (const l of S.parFichier.values()) l.sort((a, b) => a.s - b.s);
  }

  // Remove what annoter() added to the displayed text (numbers, added text
  // in the Relecture view, classes), to annotate the same rendering again.
  function nettoyer() {
    for (const x of zone().querySelectorAll(".g-ajout-hote, .g-ancre, ins.g-ajout")) x.remove();
    for (const x of zone().querySelectorAll("[data-a]")) {
      x.classList.remove("g-prop", "g-hl", "g-barre", "g-actif");
      delete x.dataset.a;
    }
  }

  // External links in a new tab; project images with the token.
  function ressources(racine) {
    for (const a of racine.querySelectorAll("a[href]")) {
      if (/^[a-z][a-z0-9+.-]*:/i.test(a.getAttribute("href"))) {
        a.target = "_blank";
        a.rel = "noopener noreferrer";
      }
    }
    for (const img of racine.querySelectorAll("img[src]")) {
      const u = new URL(img.getAttribute("src"), location.href);
      if (u.origin === location.origin) {
        u.searchParams.set("t", S.jeton); // before any fragment (#…)
        img.src = u.href;
      }
    }
  }

  // Only the browser sees rendered headings: it retains those of each
  // displayed file, which the home page crosses with marked sections (P11.3).
  // Nothing is retained in Proposé, whose headings may differ, or if
  // a filter moved a heading outside its file: that file's list
  // would be wrong. A refusal is not reported here: chargerProgression does
  // that, and the home page then shows the last known value.
  const envoiTitres = GloseDonnees.creerEnvoiTitres((headings) => api("/api/progress", { headings }));
  function retenirTitres(titres, cle) {
    if (S.vue === "propose") return;
    const vus = {};
    for (const f of zone().querySelectorAll(".glose-file")) if (f.dataset.path) vus[f.dataset.path] = [];
    for (const h of titres) {
      const f = h.closest(".glose-file");
      const liste = f && vus[f.dataset.path];
      if (!liste) return;
      liste.push(cle(h).slice(f.dataset.path.length + 1));
    }
    envoiTitres.vouloir(vus);
  }

  async function chargerProgression() {
    const r = await api("/api/progress");
    S.progression = r.ok ? r.data.sections || {} : {};
    envoiTitres.connaitre(r.ok ? r.data.headings : null);
    if (r.ok && r.data.error) avis("Progression de la relecture : " + r.data.error, true);
  }

  // Mark a section (table-of-contents heading): to review (""), in progress, reviewed.
  async function marquerSection(cle, titre, etat) {
    const defile = $("sommaire").scrollTop;
    const r = await api("/api/progress", { key: cle, title: titre, status: etat });
    if (!r.ok) { avis(r.data.message || "La progression n'a pas pu être enregistrée.", true); return; }
    S.progression = r.data.sections || {};
    sommaire();
    $("sommaire").scrollTop = defile;
  }

  function anomalies() {
    const zone = $("anomalies");
    zone.textContent = "";
    const lignes = [];
    for (const f of S.doc.files) if (f.error) lignes.push(f.path + " : " + f.error);
    for (const i of S.doc.issues) {
      const f = S.doc.files[i.file];
      lignes.push((f ? f.path : "?") + ", ligne " + i.line + ", colonne " + i.column + " : " + i.message);
    }
    zone.hidden = lignes.length === 0;
    if (!lignes.length) return;
    zone.append(el("strong", "", "Anomalies dans les fichiers (les zones voisines ne peuvent pas être annotées) :"));
    const ul = el("ul");
    for (const l of lignes) ul.append(el("li", "", l));
    zone.append(ul);
  }

  function majEtat() {
    majNavette();
    // P20: what is shown, under the title (the file, or how many).
    const montres = S.doc.files.filter((f) => f.shown);
    $("portee").textContent = montres.length === 1 ? montres[0].path
      : montres.length === S.doc.files.length ? S.doc.files.length + " fichiers"
      : montres.length + " fichiers affichés sur " + S.doc.files.length;
    const n = S.cartes.filter((c) => !c.cache).length;
    $("etat").textContent = S.doc.files.length + " fichier" + (S.doc.files.length > 1 ? "s" : "") + " · " +
      n + " annotation" + (n > 1 ? "s" : "") + " · rendu en " + S.doc.millis + " ms";
  }

  // ---------- Annotations ----------
  // An added or deleted footnote consists of two linked proposals
  // (reference and definition): a single card, that of the reference.
  function principale(a) {
    if (!a.partners || !a.partners.length || a.primary) return -1;
    return S.doc.annotations.findIndex((b) => b.file === a.file && b.primary && a.partners.includes(b.start));
  }

  // Source range whose words are visible in the current view, for an
  // annotation: the commented passage; a proposal's old text
  // (Relecture, Actuel) or its new text (Proposé).
  function plageVisible(a) { return GloseDonnees.plageVisible(a, S.vue); }

  // File words overlapping [d, f) (a selection may start within a word).
  function motsDe(fichier, d, f) {
    const mots = S.parFichier.get(fichier) || [];
    const out = [];
    let k = premierApres(mots, d);
    if (k > 0 && mots[k - 1].e > d) k--;
    for (let j = k; j < mots.length && mots[j].s < f; j++) out.push(mots[j]);
    return out;
  }

  // Place added lines (block) beside the anchor word: after the table row
  // or list item containing it (in a separate row or item),
  // otherwise after its block; before it if the anchor is the word
  // following the addition.
  function placerBloc(ins, ancre, apres) {
    const tr = ancre.closest("tr"), li = ancre.closest("li");
    let hote, noeud = ins;
    if (tr) {
      hote = tr;
      noeud = el("tr", "g-ajout-hote");
      const td = el("td");
      td.colSpan = tr.children.length || 1;
      td.append(ins);
      noeud.append(td);
    } else if (li) {
      hote = li;
      noeud = el("li", "g-ajout-hote");
      noeud.append(ins);
    } else {
      hote = ancre.closest(BLOCS) || ancre;
    }
    if (apres) {
      while (hote.nextElementSibling && hote.nextElementSibling.classList.contains("g-ajout-hote")) hote = hote.nextElementSibling;
      hote.after(noeud);
    } else {
      hote.before(noeud);
    }
  }

  // Text added by a proposal, shown in Relecture. Complete lines
  // (table row, list item, paragraph) form a block,
  // shown as source after the structure they enter.
  function insertion(a, i) {
    const t = a.kind === "addition" ? a.text : a.new;
    let ins;
    if (a.kind === "addition" && /\n/.test(t)) {
      ins = el("ins", "g-ajout bloc", t.replace(/\s+$/, ""));
    } else {
      ins = md("ins", "g-ajout", t.replace(/\s*\n\s*\n\s*/g, " ¶ "));
    }
    ins.dataset.a = i;
    return ins;
  }

  function annoter() {
    const marge = $("marge");
    marge.textContent = "";
    marge.append(annotationsFermer);
    annotationsOuvrir.textContent = "Annotations (" + S.doc.annotations.length + ")";
    S.cartes = [];
    if (S.vue === "depuis") return;
    S.doc.annotations.forEach((a, i) => {
      if (S.filtreMission && a.task !== S.filtreMission && !a.messages.some(m => m.task === S.filtreMission)) {
        const carte = el("div", "carte"); carte.hidden = true;
        S.cartes.push({ a, badge:null, carte, cache:true, ailleurs:false }); return;
      }
      // File not displayed (P6.8): nothing in the text or margin; the
      // annotation list retains it, and clicking opens its file.
      if (S.doc.files[a.file] && !S.doc.files[a.file].shown) {
        const carte = el("div", "carte");
        carte.hidden = true;
        S.cartes.push({ a, badge: null, carte, cache: true, ailleurs: principale(a) < 0 });
        return;
      }
      const p = principale(a);
      if (p >= 0) {
        const pl = plageVisible(a);
        for (const m of pl ? motsDe(a.file, pl[0], pl[1]) : []) {
          m.el.classList.add("g-prop");
          if (S.vue === "relecture") m.el.classList.add("g-barre");
          m.el.dataset.a = p;
        }
        const carte = el("div", "carte");
        carte.hidden = true;
        S.cartes.push({ a, badge: null, carte, cache: true });
        return;
      }
      const mots = S.parFichier.get(a.file) || [];
      const pl = plageVisible(a);
      const cibles = pl ? motsDe(a.file, pl[0], pl[1]) : [];
      const classe = a.kind === "highlight" ? "g-hl" : "g-prop";
      const barre = S.vue === "relecture" && (a.kind === "deletion" || a.kind === "substitution");
      for (const m of cibles) { m.el.classList.add(classe); if (barre) m.el.classList.add("g-barre"); m.el.dataset.a = i; }
      // Anchor: after the last targeted word, otherwise after the word preceding the annotation.
      let ancre = cibles.length ? cibles[cibles.length - 1].el : null;
      let apres = true;
      if (!ancre) {
        const k = premierApres(mots, a.start + 1);
        let j = k - 1;
        while (j >= 0 && mots[j].e > a.start) j--;
        if (j >= 0) ancre = mots[j].el;
        else if (k < mots.length) { ancre = mots[k].el; apres = false; }
      }
      const badge = el("span", "g-ancre" + (PROPOSITION[a.kind] ? " prop" : ""), String(i + 1));
      badge.tabIndex = 0;
      badge.setAttribute("role", "button");
      badge.setAttribute("aria-label", "Annotation " + (i + 1) + " : " + NOMS[a.kind]);
      badge.addEventListener("click", () => activer(i, false));
      badge.addEventListener("keydown", (e) => {
        if (e.key === "Enter" || e.key === " ") { e.preventDefault(); activer(i, false); S.cartes[i].carte.focus(); }
      });
      // In Relecture, added text is inserted in place, followed by the number.
      const ins = S.vue === "relecture" && (a.kind === "addition" || a.kind === "substitution") ? insertion(a, i) : null;
      if (!ancre && ins) {
        // No word in Actuel (empty file, addition only): show the addition
        // at the start of its file.
        const f = zone().querySelector('.glose-file[data-f="' + a.file + '"]') || zone();
        const p = el("p", "g-ajout-hote");
        p.append(badge, " ", ins);
        f.prepend(p);
        ancre = badge;
      } else if (ancre) {
        if (ins && ins.classList.contains("bloc")) {
          placerBloc(ins, ancre, apres);
          ins.prepend(badge, " ");
        } else if (apres) {
          // Preserve number order when several annotations share a word.
          while (ancre.nextElementSibling && /\bg-(ancre|ajout)\b/.test(ancre.nextElementSibling.className)) ancre = ancre.nextElementSibling;
          if (ins) { ancre.after(ins); ancre = ins; }
          ancre.after(badge);
        } else {
          if (ins) ancre.before(ins);
          ancre.before(badge);
        }
      }
      const carte = fabriquerCarte(a, i);
      marge.append(carte);
      S.cartes.push({ a, badge: ancre ? badge : null, carte });
    });
  }

  // ---------- Keyboard (P4.2) ----------
  // J and K: next, previous annotation. A, X, R, E: accept, reject,
  // reply, correct, on the active (highlighted) card only. Return
  // true if the key was used.
  function clavierAnnotation(k) {
    const ordre = S.cartes.map((c, i) => i).filter((i) => !S.cartes[i].cache);
    if (k === "j" || k === "k") {
      if (!ordre.length) { avis("Aucune annotation en attente."); return true; }
      const pos = ordre.indexOf(S.actif >= 0 ? S.actif : S.cartes.findIndex((c) => cleAnnotation(c.a) === S.dernierActif));
      const suiv = pos < 0 ? (k === "j" ? 0 : ordre.length - 1) : Math.min(ordre.length - 1, Math.max(0, pos + (k === "j" ? 1 : -1)));
      if (pos >= 0 && suiv === pos) { activer(ordre[suiv], true, true); avis(k === "j" ? "C'est la dernière annotation." : "C'est la première annotation."); return true; }
      activer(ordre[suiv], true, true);
      return true;
    }
    if (S.occupe) return true; // a write is in progress: no second decision on a stale card
    const c = S.cartes[S.actif];
    if (!c || c.cache) { avis("Choisissez d'abord une annotation : J (suivante) ou K (précédente)."); return true; }
    const a = c.a;
    if (S.doc && S.doc.state && S.doc.state.hand === "agent") { avis("L'agent a la main : vous ne pouvez pas écrire avant qu'il vous la rende.", true); return true; }
    if (k === "r") { repondre(a, S.actif); return true; }
    if (k === "e") {
      const tete = !PROPOSITION[a.kind] && teteModifiable(a);
      if (corrigeable(a)) corriger(a, S.actif);
      else if (tete) modifier(a, tete, S.actif);
      else avis(PROPOSITION[a.kind] ? "Seules vos propositions se modifient ; répondez (R) pour demander une correction à l'agent."
        : "Seuls vos commentaires pas encore remis à l'agent se modifient ; répondez (R) dans le fil.", true);
      return true;
    }
    if (!PROPOSITION[a.kind]) { avis((k === "a" ? "A accepte" : "X rejette") + " une proposition : cette annotation est une discussion (R pour répondre).", true); return true; }
    decider(a, k === "a" ? "accept" : "reject");
    return true;
  }

  // clavier: the card was selected with the keyboard (J, K); it remains so
  // after the reload following a decision.
  function activer(i, defiler, clavier, conserverPanneau = false) {
    S.clavier = !!clavier;
    for (const x of document.querySelectorAll(".carte.clavier")) x.classList.remove("clavier");
    for (const x of document.querySelectorAll(".g-actif")) x.classList.remove("g-actif");
    for (const x of document.querySelectorAll(".carte.active")) x.classList.remove("active");
    S.actif = i;
    for (const x of $("annotations-liste").querySelectorAll("a[data-carte]")) {
      if (x.dataset.carte === String(i)) x.setAttribute("aria-current", "true"); else x.removeAttribute("aria-current");
    }
    const c = S.cartes[i];
    if (!c) return;
    if (fenetreEtroite.matches && !conserverPanneau) panneauAnnotations(true);
    c.carte.classList.add("active");
    if (S.clavier) c.carte.classList.add("clavier");
    for (const x of zone().querySelectorAll('[data-a="' + i + '"]')) x.classList.add("g-actif");
    if (c.badge) {
      c.badge.classList.add("g-actif");
      if (defiler) c.badge.scrollIntoView({ block: "center", behavior: douceur() });
    }
    if (fenetreEtroite.matches && document.body.classList.contains("annotations-ouvertes")) requestAnimationFrame(() => c.carte.scrollIntoView({ block: "nearest" }));
    disposer(); // the expanded card may extend beyond the bottom of the margin (P9.4)
  }

  // No card is active any more; J and K go on from the one that was.
  function desactiver() {
    const c = S.cartes[S.actif];
    if (c) S.dernierActif = cleAnnotation(c.a);
    S.actif = -1;
    S.clavier = false;
    for (const x of document.querySelectorAll(".carte.active, .carte.clavier, .g-actif")) x.classList.remove("active", "g-actif", "clavier");
    for (const x of $("annotations-liste").querySelectorAll("a[aria-current]")) x.removeAttribute("aria-current");
    disposer();
  }

  // ---------- Reading position ----------
  // Going to Vos projets, or reloading, then coming back finds the place again.
  history.scrollRestoration = "manual";
  window.addEventListener("pagehide", () => {
    const r = S.doc && capturer();
    if (r) sessionStorage.setItem("glose-lecture", JSON.stringify(r));
  });
  // Once, at the first display, unless a part was asked for (glose open livre-1/).
  function lectureRetenue() {
    const r = lireJSON(sessionStorage, "glose-lecture", null, (v) => v && Number.isInteger(v.f) && Number.isInteger(v.s));
    sessionStorage.removeItem("glose-lecture");
    return S.focus ? null : r;
  }

  function capturer() {
    const n = S.spans.length;
    let lo = 0, hi = n;
    while (lo < hi) {
      const m = (lo + hi) >> 1;
      if (S.spans[m].getBoundingClientRect().top >= 56) hi = m; else lo = m + 1;
    }
    if (lo >= n) return null;
    const g = lireG(S.spans[lo].dataset.g);
    return g && { f: g.f, s: g.s, top: S.spans[lo].getBoundingClientRect().top };
  }

  function restaurer(r) {
    const mots = S.parFichier.get(r.f);
    if (!mots || !mots.length) return;
    const k = Math.min(premierApres(mots, r.s), mots.length - 1);
    amener(mots[k].el, r.top);
  }

  // Scroll the page so el is haut pixels from the top of the screen.
  // A file never displayed has only an estimated height (content-visibility):
  // scrollBy may hit the bottom of the document and leave el far below the fold.
  // Use scrollIntoView to let the document grow, then adjust.
  function amener(el, haut) {
    window.scrollBy(0, el.getBoundingClientRect().top - haut);
    if (Math.abs(el.getBoundingClientRect().top - haut) > 2) {
      el.scrollIntoView({ block: "start", behavior: "instant" });
      window.scrollBy(0, el.getBoundingClientRect().top - haut);
    }
  }

  // ---------- Selection → source bytes (web/selection.js) ----------
  const { selectionDuTexte, lireSelection, blocDe } = GloseSelection.creer({
    zone, spans: () => S.spans, mots: (f) => S.parFichier.get(f) || [], blocs: BLOCS,
    lireG, premierApres, court, avis, texteSource,
  });

  function majBoutonCommenter() {
    const b = $("menu-selection");
    const r = selectionDuTexte();
    if (!r || $("saisie").open || $("edition").open) { b.hidden = true; return; }
    const rect = r.getBoundingClientRect();
    b.style.left = (rect.left + window.scrollX + Math.min(rect.width, 40)) + "px";
    b.style.top = (rect.bottom + window.scrollY + 6) + "px";
    b.hidden = false;
  }

  // An image cannot be rewritten, but can be commented on: the comment goes
  // at the end of its containing block (caption, paragraph), or, failing that,
  // at the end of the preceding text, naming the image.
  function imageDeLaSelection() {
    const r = selectionDuTexte();
    if (!r) return null;
    const dedans = r.cloneContents().querySelector && r.cloneContents().querySelector("img");
    if (dedans) {
      for (const img of zone().querySelectorAll("img")) if (r.intersectsNode(img)) return img;
    }
    const n = r.startContainer.nodeType === 1 ? r.startContainer : r.startContainer.parentElement;
    if (n && n.tagName === "IMG") return n;
    const fig = n && n.closest && n.closest("figure");
    return fig ? fig.querySelector("img") : null;
  }

  function commenterImage(img) {
    if (saisieEnCours()) return;
    const nom = (img.getAttribute("alt") || "").trim() || decoder((img.getAttribute("src") || "").split("?")[0].split("/").pop());
    // The nearest enclosing block containing source text (its caption,
    // its paragraph), without leaving its file.
    let mot = null;
    for (let b = img.parentElement; b && b !== zone() && !b.classList.contains("glose-file"); b = b.parentElement) {
      mot = b.querySelector("span[data-g]");
      if (mot) break;
    }
    const prefixe = "Sur l'image « " + nom + " » : ";
    if (!mot) {
      // The last word before the image in its file, or, if the image
      // opens the file, the first word after it: never a word from
      // another file (the comment would be written there).
      const fichier = img.closest(".glose-file");
      let suivant = null;
      for (const sp of S.spans) {
        if (fichier && !fichier.contains(sp)) continue;
        if (sp.compareDocumentPosition(img) & Node.DOCUMENT_POSITION_FOLLOWING) mot = sp;
        else { suivant = sp; break; }
      }
      if (!mot) mot = suivant;
    }
    const g = mot && lireG(mot.dataset.g);
    if (!g) { avis("Cette image ne peut pas être rattachée au texte source.", true); return; }
    getSelection().removeAllRanges();
    S.saisie = { mode: "paragraph", file: g.f, hash: S.hashes[g.f], start: g.s, end: g.s, extrait: "Image : " + nom, prefixe };
    ouvrirSaisie("Commenter l'image", "Image : " + nom, "");
  }

  function commenterSelection() {
    if (saisieEnCours()) return;
    const img = imageDeLaSelection();
    if (img && !texteDeLaSelection()) { commenterImage(img); return; }
    const x = lireSelection();
    if (!x) return;
    S.saisie = { mode: "passage", file: x.f, hash: S.hashes[x.f], start: x.start, end: x.end, extrait: x.extrait, prefixe: "" };
    ouvrirSaisie("Commenter un passage", x.extrait,
      x.ajuste ? "La sélection a été ramenée aux mots du texte source (sans numéros ni renvois calculés)." : "");
  }

  // Suggest: rewrite the selected passage; the rewrite becomes
  // word-by-word proposals, with an optional message.
  async function suggererSelection() {
    if (saisieEnCours()) return;
    if (imageDeLaSelection() && !texteDeLaSelection()) {
      avis("Une image ne se réécrit pas à l'écran : commentez-la (touche C), l'agent pourra la refaire.", true);
      return;
    }
    const x = lireSelection();
    if (!x) return;
    $("menu-selection").hidden = true;
    const hash = S.hashes[x.f];
    const sel = selectionDuTexte();
    const element = sel && blocDe(sel.startContainer);
    if (sel && element && blocDe(sel.endContainer) !== element) {
      // Several paragraphs: their complete lines, from the first word to the last.
      const mots = motsDe(x.f, x.start, x.end);
      if (!mots.length) return;
      const at = mots[0].s, to = mots[mots.length - 1].e;
      const r = await api("/api/block?file=" + x.f + "&hash=" + hash + "&at=" + at + "&to=" + to);
      if (!r.ok) {
        avis(r.data.message || "Ce passage ne peut pas être réécrit.", true);
        if (r.status === 409) await charger(true);
        return;
      }
      ouvrirEdition("Suggérer une réécriture", "Plusieurs paragraphes : leur source entière, ligne par ligne.",
        { mode: "lines", file: x.f, hash, at, to, start: r.data.start, end: r.data.end, original: r.data.text });
      return;
    }
    const r = await api("/api/passage?file=" + x.f + "&hash=" + hash + "&start=" + x.start + "&end=" + x.end);
    if (!r.ok) {
      avis(r.data.message || "Ce passage ne peut pas être réécrit.", true);
      if (r.status === 409) await charger(true);
      return;
    }
    ouvrirEdition("Suggérer une réécriture",
      r.data.widened ? "Le passage a été élargi pour ne pas couper une mise en forme (gras, lien…)." : "",
      { mode: "passage", file: x.f, hash, at: r.data.start, start: r.data.start, end: r.data.end, original: r.data.text, element });
  }

  // Source text within the selection (empty for an image alone).
  function texteDeLaSelection() {
    const r = selectionDuTexte();
    if (!r) return "";
    for (const sp of S.spans) if (r.intersectsNode(sp) && sp.textContent.trim()) return sp.textContent;
    return "";
  }

  function commenterBloc(bloc) {
    if (saisieEnCours()) return;
    if (!bloc || !zone().contains(bloc)) { fermerOutils(); return; }
    const img = S.imageSurvolee && bloc.contains(S.imageSurvolee) ? S.imageSurvolee : null;
    if (img) { fermerOutils(); commenterImage(img); return; }
    const mots = bloc.querySelectorAll("span[data-g]");
    // Comment on a structure at the end of its last word (its last cell).
    const g = mots.length && lireG(mots[bloc.matches(STRUCTURES) ? mots.length - 1 : 0].dataset.g);
    if (!g) { avis("Ce bloc (code, contenu généré) ne peut pas être annoté à l'écran.", true); return; }
    let t = "";
    for (const m of mots) t += m.textContent;
    const extrait = court(t, 200);
    S.saisie = { mode: "paragraph", file: g.f, hash: S.hashes[g.f], start: g.s, end: g.s, extrait, prefixe: "" };
    ouvrirSaisie("Commenter le paragraphe", extrait, "");
  }

  // ---------- Paragraph handle ----------
  let poigneeAuClavier = () => false;
  function ouvrirMenuBloc() {
    $("outils-bloc").classList.add("ouvert");
    $("poignee").setAttribute("aria-expanded", "true");
    $("menu-commenter").focus({ preventScroll: true });
  }

  function fermerMenuBloc(rendreFocus) {
    $("outils-bloc").classList.remove("ouvert");
    $("poignee").setAttribute("aria-expanded", "false");
    if (rendreFocus) $("poignee").focus({ preventScroll: true });
  }

  function fermerOutils() {
    fermerMenuBloc(false);
    $("outils-bloc").hidden = true;
    S.bloc = null;
  }

  // ---------- Annotation actions ----------
  async function retirer(a) {
    if (saisieEnCours()) return;
    if (S.ecriture) { avis("Une écriture est en cours : réessayez dans un instant."); return; }
    if (!(await boite({ titre: "Retirer cette annotation ?", message: "Elle disparaît du fichier. Elle n'avait pas encore été remise à l'agent.",
      icone: "retirer", ton: "danger", ok: "Retirer" }))) return;
    S.ecriture = true;
    S.occupe++;
    try {
      const r = await api("/api/withdraw", { file: a.file, hash: S.hashes[a.file], start: a.start });
      if (!r.ok) avis(r.data.message || "Le retrait a échoué.", true); else avis("Annotation retirée.");
      await charger(true);
    } finally {
      S.occupe--;
      S.ecriture = false;
    }
  }

  // Your messages (comment, reply) can be edited until they have been
  // handed over to the agent (P9.1); afterwards, reply in the thread.
  // An agent correction cannot be edited.
  function modifiable(m) {
    return m.status === "new" && !m.fix && (m.by === "author" || (!m.id && !m.by));
  }

  // The opening message of a discussion (the comment itself, or the
  // remark on a highlighted passage), if it is yours and editable.
  function teteModifiable(a) {
    const m = a.messages.find((x) => !x.reply);
    return m && modifiable(m) ? m : null;
  }

  function outilsMessage(a, m, i) {
    const o = el("span", "msg-outils");
    const b = (libelle, classe, action) => {
      const x = el("button", "icone " + classe);
      x.type = "button";
      x.innerHTML = icone(classe); // a constant from this file, never document text
      x.setAttribute("aria-label", libelle);
      x.dataset.infobulle = libelle;
      x.addEventListener("click", (e) => { e.stopPropagation(); action(); });
      o.append(x);
    };
    b("Modifier votre réponse", "corriger", () => modifier(a, m, i));
    b("Retirer votre réponse", "retirer", () => retirerReponse(a, m));
    return o;
  }

  function modifier(a, m, i) {
    if (saisieEnCours()) return;
    const extrait = m.reply ? court(a.kind === "comment" ? a.text : a.kind === "substitution" ? a.text + " → " + a.new : a.text, 300)
      : a.kind === "highlight" ? court(a.text, 300) : "";
    S.saisie = { mode: "amend", file: a.file, hash: S.hashes[a.file], start: m.start, end: m.start, extrait, prefixe: "",
      annotationID: a.id || "", original: m.text, reponse: !!m.reply };
    ouvrirSaisie((m.reply ? "Modifier votre réponse · annotation " : "Modifier votre commentaire · annotation ") + (i + 1), extrait, "", m.text);
  }

  async function retirerReponse(a, m) {
    if (saisieEnCours()) return;
    if (S.ecriture) { avis("Une écriture est en cours : réessayez dans un instant."); return; }
    if (!(await boite({ titre: "Retirer votre réponse ?", message: "Elle disparaît du fil. Elle n'avait pas encore été remise à l'agent.",
      icone: "retirer", ton: "danger", ok: "Retirer" }))) return;
    S.ecriture = true;
    S.occupe++;
    try {
      const r = await api("/api/withdraw", { file: a.file, hash: S.hashes[a.file], start: m.start });
      if (!r.ok) avis(r.data.message || "Le retrait a échoué.", true); else avis("Réponse retirée.");
      await charger(true);
    } finally {
      S.occupe--;
      S.ecriture = false;
    }
  }

  function repondre(a, i) {
    if (saisieEnCours()) return;
    const resume = a.kind === "comment" ? a.text : a.kind === "substitution" ? a.text + " → " + a.new : a.text;
    S.saisie = { mode: "reply", file: a.file, hash: S.hashes[a.file], start: a.start, end: a.start, extrait: court(resume, 300), prefixe: "", annotationID: a.id || "",
      pour: a.to || "" }; // the thread remains addressed to its recipient (P10.6)
    ouvrirSaisie("Répondre à l'annotation " + (i + 1), court(resume, 300), "");
  }

  // An agent correction attached to your proposal: what it should
  // propose. You can accept it as is, or bring it into your
  // proposal to adjust it; otherwise, it remains advice in the thread.
  async function appliquerCorrection(a, m, accepter) {
    if (S.ecriture) { avis("Une écriture est en cours : réessayez dans un instant."); return; }
    S.ecriture = true;
    S.occupe++;
    try {
      const req = { file: a.file, hash: S.hashes[a.file], start: a.start, message: m.start, accept: accepter };
      const r = await api("/api/fix", req);
      if (r.ok && accepter) {
        // Undoable like a decision (the server keeps the previous state).
        // Redo replays the same request ("fix") on the previous state.
        S.annuler.push({ type: "decision", file: a.file, hashApres: r.data.hash, req: null, fix: req });
        S.retablir = [];
        majBoutons();
      }
      if (r.ok) avis(accepter ? "Version de l'agent acceptée : le texte est modifié. Annuler (Ctrl+Z) pour revenir en arrière."
        : "Votre proposition reprend le texte de l'agent ; vous pouvez encore l'accepter, la rejeter ou la corriger.");
      else avis(r.data.message || "La correction n'a pas pu être appliquée.", true);
      await charger(true);
    } finally {
      S.occupe--;
      S.ecriture = false;
    }
  }

  // Your own pending proposals can be corrected (P3.2); not
  // the agent's, nor an added or deleted note (reference and definition).
  function corrigeable(a) {
    return PROPOSITION[a.kind] && (a.by === "author" || (!a.id && !a.by)) && !(a.partners && a.partners.length);
  }

  // The text the proposal puts in place of the original.
  function textePropose(a) {
    return a.kind === "substitution" ? a.new : a.kind === "addition" ? a.text : "";
  }

  function corriger(a, i) {
    if (saisieEnCours()) return;
    const info = a.kind === "addition" ? "Texte ajouté par votre proposition." :
      "Texte qui remplacera « " + court(a.text, 120) + " »" + " ; laissez vide pour proposer de le supprimer.";
    ouvrirEdition("Modifier la proposition " + (i + 1), info, {
      mode: "revise", file: a.file, hash: S.hashes[a.file], at: a.start, start: a.textStart, end: a.textEnd,
      annotation: a.start, annotationID: a.id || "", kind: a.kind, ancien: a.kind === "addition" ? "" : a.text, original: textePropose(a),
    });
  }

  // An author decision can be undone (Ctrl+Z) and redone (Ctrl+Y): the
  // server keeps the file state preceding the decision.
  function retenirDecision(r, req) {
    if (!r.ok) return;
    S.annuler.push({ type: "decision", file: req.file, hashApres: r.data.hash, req });
    S.retablir = [];
    majBoutons();
  }

  async function decider(a, decision) {
    if (saisieEnCours()) return;
    if (S.ecriture) { avis("Une écriture est en cours : réessayez dans un instant."); return; }
    S.ecriture = true;
    S.occupe++;
    try {
      const req = { file: a.file, hash: S.hashes[a.file], start: a.start, decision };
      const r = await api("/api/decide", req);
      retenirDecision(r, req);
      if (r.ok) avis((decision === "accept" ? "Proposition acceptée : le texte est modifié." : "Proposition rejetée : le texte d'origine reste.") +
        " Annuler (Ctrl+Z) pour revenir en arrière.");
      else avis(r.data.message || "La décision a échoué.", true);
      await charger(true);
    } finally {
      S.occupe--;
      S.ecriture = false;
    }
  }

  async function resoudre(a) {
    if (saisieEnCours()) return;
    if (S.ecriture) { avis("Une écriture est en cours : réessayez dans un instant."); return; }
    S.ecriture = true;
    S.occupe++;
    try {
      const req = { file: a.file, hash: S.hashes[a.file], start: a.start, decision: "resolve" };
      const r = await api("/api/decide", req);
      retenirDecision(r, req);
      if (r.ok) avis("Discussion close : elle est gardée au journal. Annuler (Ctrl+Z) pour la rouvrir.");
      else avis(r.data.message || "La résolution a échoué.", true);
      await charger(true);
    } finally {
      S.occupe--;
      S.ecriture = false;
    }
  }

  // Batch decisions (F12): all proposals in a file.
  async function deciderTout(f, decision, n, via = "") {
    if (saisieEnCours()) return;
    if (S.ecriture) { avis("Une écriture est en cours : réessayez dans un instant."); return; }
    const verbe = decision === "accept" ? "Accepter" : "Rejeter";
    const accepter = decision === "accept";
    if (!(await boite({
      titre: verbe + (via ? " (" + via + ")" : "") + " " + (n > 1 ? "les " + n + " propositions" : "la proposition") + " ?",
      message: (accepter ? "Le texte proposé remplacera l'original" : "Le texte d'origine restera") + " dans " + S.doc.files[f].path +
        ". Chaque décision est gardée au journal.",
      icone: accepter ? "tout-accepter" : "rejeter", ton: accepter ? "succes" : "danger",
      ok: (accepter ? "Accepter" : "Rejeter") + " " + (n > 1 ? "les " + n : "la proposition"),
    }))) return;
    S.ecriture = true;
    S.occupe++;
    try {
      const req = { file: f, hash: S.hashes[f], start: 0, end: Number.MAX_SAFE_INTEGER, decision, via };
      const r = await api("/api/decide", req);
      retenirDecision(r, req);
      if (r.ok) avis(n + " proposition" + (n > 1 ? "s" : "") + (decision === "accept" ? " acceptée" : " rejetée") + (n > 1 ? "s" : "") +
        ". Annuler (Ctrl+Z) pour revenir en arrière.");
      else avis(r.data.message || "La décision a échoué.", true);
      await charger(true);
    } finally {
      S.occupe--;
      S.ecriture = false;
    }
  }

  // What is rewritten or commented on remains visible and marked to the
  // left of the panel: the selected passage, or the entire paragraph.
  function viser(x) {
    for (const v of document.querySelectorAll("#texte .vise")) v.classList.remove("vise");
    for (const v of document.querySelectorAll("#texte .vise-mot")) v.classList.remove("vise-mot");
    if (!x) return;
    const haut = $("barre").getBoundingClientRect().bottom + 24;
    if (x.mode === "insert" && x.element) {
      x.element.classList.add("vise");
      const r = x.element.getBoundingClientRect();
      if (r.top < haut || r.top > window.innerHeight * 0.55) amener(x.element, haut);
      return;
    }
    const mots = S.parFichier.get(x.file) || [];
    let k = premierApres(mots, x.start);
    if (k > 0 && mots[k - 1].e > x.start) k--;
    const m = mots[k];
    if (!m) return;
    let cible = m.el;
    if ((x.mode === "passage" || x.mode === "revise" || x.mode === "lines") && x.end > x.start) {
      for (let j = k; j < mots.length && mots[j].s < x.end; j++) mots[j].el.classList.add("vise-mot");
    } else if (x.mode === "block" || x.mode === "paragraph" || !x.mode) {
      const b = m.el.closest(BLOCS);
      if (!b) return;
      b.classList.add("vise");
      cible = b;
    } else {
      return;
    }
    const r = cible.getBoundingClientRect();
    if (r.top < haut || r.top > window.innerHeight * 0.55) amener(cible, haut);
  }

  // ---------- Disk monitoring ----------
  async function surveiller() {
    if (S.occupe || document.hidden) return;
    if (S.echecs > 2 && (S.echecs++ % 5) !== 0) return; // less often after failures
    if (!S.doc) { // first load failed: retry
      if (await charger(false)) S.echecs = 0; else S.echecs++;
      return;
    }
    const r = await api("/api/hashes");
    if (!r.ok || S.occupe) { if (!r.ok) S.echecs++; return; }
    // An added or removed file (chapter created by the agent, profile changed) also changes the list.
    const change = r.data.filesGen !== S.doc.filesGen || r.data.files.length !== S.doc.files.length ||
      r.data.files.some((f) => S.hashes[f.index] !== f.hash) || r.data.seq !== S.doc.seq;
    // Agent presence changes without anything else changing (P10.1).
    const cle = (o) => JSON.stringify([o.agent || null, o.agentsConfigured, o.tasks || [], o.textAgent || null, o.textRequires || [], o.run || null, o.passes || null, o.aliases || [], o.link || null, o.owner || null]);
    if (S.doc.state && cle({ ...r.data }) !== cle({ ...S.doc, agent: S.doc.state.agent })) {
      S.doc.state.agent = r.data.agent || null;
      S.doc.agentsConfigured = r.data.agentsConfigured;
      S.doc.tasks = r.data.tasks || [];
      S.doc.textAgent = r.data.textAgent || null;
      S.doc.textRequires = r.data.textRequires || [];
      S.doc.run = r.data.run || null;
      S.doc.passes = r.data.passes || null;
      S.doc.aliases = r.data.aliases || [];
      S.doc.link = r.data.link || null;
      S.doc.owner = r.data.owner || null;
      if (!change) majNavette();
    }
    if (!change) { S.echecs = 0; return; }
    const main = S.doc.state && S.doc.state.hand;
    if (await charger(true)) {
      S.echecs = 0;
      // A change of turn has its own notice (majNavette).
      if (!S.doc.state || S.doc.state.hand === main) avis("Le texte a changé sur le disque : affichage mis à jour.");
    } else {
      S.echecs++;
    }
  }

  // ---------- Events ----------
  function brancher() {
    $("bouton-brouillons").addEventListener("click", ouvrirBrouillons);
    $("brouillons-fermer").addEventListener("click", () => $("brouillons").close());
    $("saisie-texte").addEventListener("input", (e) => { if (!e.isComposing) garderBrouillon("saisie"); });
    $("saisie-texte").addEventListener("compositionend", () => garderBrouillon("saisie"));
    edition.brancherNote();
    // Last attempts only: closing a page never guarantees delivery. Each
    // completed input also has a synchronous browser mirror.
    const garderTout = () => {
      for (const type of ["saisie", "edition"]) {
        const e = garderBrouillon(type);
        if (e) brouillons.sauver(e);
      }
    };
    document.addEventListener("visibilitychange", () => { if (document.hidden) garderTout(); });
    window.addEventListener("pagehide", garderTout);
    let attente = 0;
    document.addEventListener("selectionchange", () => {
      clearTimeout(attente);
      attente = setTimeout(majBoutonCommenter, 60);
    });
    const bc = $("bouton-commenter");
    $("menu-selection").addEventListener("mousedown", (e) => e.preventDefault()); // preserve the selection
    bc.addEventListener("click", commenterSelection);
    $("bouton-suggerer").addEventListener("click", suggererSelection);

    const bp = $("outils-bloc");
    // An internal link leads to its target in the displayed rendering (the
    // other rendering, kept hidden, has the same identifiers).
    $("texte").addEventListener("click", (e) => {
      const a = e.target.closest && e.target.closest('a[href]');
      if (!a) return;
      const href = a.getAttribute("href");
      if (href.startsWith('#') || (!/^(?:[a-z][a-z0-9+.-]*:|\/\/)/i.test(href) && /\.md(?:#|$)/i.test(href))) {
        e.preventDefault();
        navigation.suivre(a).catch(() => avis("Le renvoi ne peut pas être suivi.", true));
      }
    });
    const placerPoignee = (bloc, img) => {
      S.bloc = bloc;
      S.imageSurvolee = img;
      const rb = bloc.getBoundingClientRect();
      const rt = $("texte").getBoundingClientRect();
      const ligne = parseFloat(getComputedStyle(bloc).lineHeight) || 24;
      bp.style.top = (rb.top + window.scrollY + Math.max(0, (ligne - 26) / 2) - 2) + "px";
      // An item of a list: left of the list, so as not to hide its number.
      const liste = bloc.tagName === "LI" ? bloc.parentElement.getBoundingClientRect().left : rb.left;
      bp.style.left = (liste + window.scrollX - 38) + "px";
      if (bp.hidden) { bp.hidden = false; $("poignee").style.animation = "none"; void $("poignee").offsetWidth; $("poignee").style.animation = ""; }
    };
    // The keyboard reaches the handle too (key +): on the selected block, else
    // the first one read in the window; never during the agent's turn.
    poigneeAuClavier = () => {
      if (S.vue === "depuis" || document.body.classList.contains("main-agent")) return false;
      const r = selectionDuTexte();
      let bloc = r && blocDe(r.startContainer);
      if (bloc && bloc.closest("table")) bloc = bloc.closest("table");
      if (!bloc) {
        const haut = $("barre").getBoundingClientRect().bottom;
        const bas = $("pied").getBoundingClientRect().top;
        for (const x of zone().querySelectorAll(BLOCS)) {
          const b = x.getBoundingClientRect();
          if (b.top >= haut && b.top < bas && x.querySelector("span[data-g]") && !x.closest("table")) { bloc = x; break; }
        }
      }
      if (!bloc || !zone().contains(bloc)) return false;
      S.bloc = null;
      placerPoignee(bloc, null);
      ouvrirMenuBloc();
      return true;
    };
    $("texte").addEventListener("mousemove", (e) => {
      if (bp.classList.contains("ouvert")) return; // the open menu remains attached to its paragraph
      let bloc = e.target.closest && e.target.closest(BLOCS);
      // An image: put the handle before it, and comment on the image.
      const img = e.target.tagName === "IMG" ? e.target : null;
      if (img) bloc = img.closest("p, figure, li, dd") || img;
      // Comment on and rewrite a table as a whole, rather than cell by cell.
      else if (bloc && bloc.closest("table")) bloc = bloc.closest("table");
      if (!bloc || bloc === S.bloc || !zone().contains(bloc) || (!img && !bloc.querySelector("span[data-g]"))) return;
      placerPoignee(bloc, img);
    });
    // The handle is outside the text area: moving from the paragraph to it
    // must not hide it. It disappears when the mouse leaves both,
    // unless its menu is open.
    const masquerOutils = (e) => {
      if (bp.classList.contains("ouvert")) return;
      const vers = e.relatedTarget;
      if (vers && (bp.contains(vers) || document.getElementById("page").contains(vers))) return;
      fermerOutils();
    };
    document.getElementById("page").addEventListener("mouseleave", masquerOutils);
    bp.addEventListener("mouseleave", masquerOutils);
    $("poignee").addEventListener("click", () => (bp.classList.contains("ouvert") ? fermerMenuBloc(true) : ouvrirMenuBloc()));
    $("menu-commenter").addEventListener("click", () => { const b = S.bloc; fermerMenuBloc(false); if (b) commenterBloc(b); });
    $("menu-suggerer").addEventListener("click", () => { const b = S.bloc; fermerMenuBloc(false); if (b) modifierBloc(b); });
    $("menu-avant").addEventListener("click", () => { const b = S.bloc; fermerMenuBloc(false); fermerOutils(); if (b) ajouter(b, "before"); });
    $("menu-apres").addEventListener("click", () => { const b = S.bloc; fermerMenuBloc(false); fermerOutils(); if (b) ajouter(b, "after"); });
    edition.brancherTypes();
    $("menu-bloc").addEventListener("keydown", (e) => {
      const items = Array.from($("menu-bloc").querySelectorAll("button"));
      const k = items.indexOf(document.activeElement);
      // Horizontal toolbar: left and right arrows (up and down as well).
      if (["ArrowDown", "ArrowUp", "ArrowLeft", "ArrowRight"].includes(e.key)) {
        e.preventDefault();
        const avance = e.key === "ArrowDown" || e.key === "ArrowRight";
        items[(k + (avance ? 1 : items.length - 1)) % items.length].focus();
      } else if (e.key === "Escape") {
        e.preventDefault();
        fermerMenuBloc(true);
      }
    });
    document.addEventListener("mousedown", (e) => {
      if (bp.classList.contains("ouvert") && !bp.contains(e.target)) fermerMenuBloc(false);
    });
    brancherRepli(desactiver);
    GloseExport.creer({ $, S, el, riche, zone, avis, changerVue, desactiver, nomAnnotation, PROPOSITION, QUI }).brancher();
    // Input through an input method (IME: Japanese, Chinese, dead
    // accents…) is often confirmed with Enter: during composition,
    // keys belong to the input method.
    const compose = (e) => e.isComposing || e.keyCode === 229;
    edition.brancher(compose);
    $("saisie").addEventListener("close", () => viser(null));
    brancherReglages();

    $("saisie").querySelector("form").addEventListener("submit", (e) => { e.preventDefault(); enregistrer(); });
    $("saisie-annuler").addEventListener("click", () => fermerSaisie(false));
    $("saisie-fermer").addEventListener("click", () => fermerSaisie(false));
    $("saisie").addEventListener("cancel", (e) => { e.preventDefault(); fermerSaisie(false); });
    $("saisie-texte").addEventListener("keydown", (e) => {
      if (compose(e)) return;
      if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); if (!e.repeat) enregistrer(); }
    });
    navette.brancher(compose);
    $("annuler").addEventListener("click", annulerDernier);
    for (const b of $("vues").querySelectorAll("button")) b.addEventListener("click", () => changerVue(b.dataset.vue));
    majVues();
    $("retablir").addEventListener("click", retablirDernier);
    for (const b of $("onglets").querySelectorAll("[role=tab]")) {
      b.addEventListener("click", () => choisirOnglet(b.dataset.onglet));
    }
    // Arrows between tabs (ARIA tabs pattern).
    $("onglets").addEventListener("keydown", (e) => {
      const ordre = ["sommaire", "fichiers", "annotations", "navette"];
      const k = ordre.indexOf(S.onglet);
      if (e.key === "ArrowRight" || e.key === "ArrowLeft") {
        e.preventDefault();
        choisirOnglet(ordre[(k + (e.key === "ArrowRight" ? 1 : ordre.length - 1)) % ordre.length], true);
      }
    });
    choisirOnglet(localStorage.getItem("glose-onglet") || "sommaire");
    brancherColonne();
    $("bouton-sommaire").addEventListener("click", () => montrerColonne(false));
    $("rail-ouvrir").addEventListener("click", () => montrerColonne(true));
    for (const b of $("rail").querySelectorAll("[data-rail]")) {
      b.addEventListener("click", () => { montrerColonne(true); choisirOnglet(b.dataset.rail, true); });
    }
    const fenetreMoyenne = matchMedia("(max-width: 1050px)");
    const adapterColonne = () => {
      const choix = localStorage.getItem("glose-colonne-masquee");
      montrerColonne(choix === null ? !fenetreMoyenne.matches : choix !== "1", true, false);
    };
    adapterColonne();
    fenetreMoyenne.addEventListener("change", adapterColonne);
    document.addEventListener("keydown", (e) => {
      if (compose(e)) return;
      // Never during input: a field, editor, panel or dialog is open.
      const actif = document.activeElement || document.body;
      if (document.getElementById("archive").open || !document.getElementById("recherche").hidden || $("boite").open || $("brouillons").open || $("saisie").open || $("edition").open || $("remise").open || !$("reglages").hidden || agents.ouvert() ||
        /^(TEXTAREA|INPUT|SELECT)$/.test(actif.tagName) || actif.isContentEditable) return;
      const k = e.key.toLowerCase();
      if ((e.ctrlKey || e.metaKey) && e.shiftKey && !e.altKey && k === "f") { e.preventDefault(); document.body.classList.remove("barres-reduites"); recherche.montrer(); return; }
      const seule = !e.ctrlKey && !e.metaKey && !e.altKey;
      // An open menu (selection, "+") prepares an annotation: A, X, R, E
      // make no decisions there; J and K remain allowed.
      const menu = !$("menu-selection").hidden || $("poignee").getAttribute("aria-expanded") === "true" || $("outils-bloc").contains(actif);
      if (seule && "axre".includes(k) && k.length === 1 && menu) return;
      if (seule && "jkaxre".includes(k) && k.length === 1) {
        if (clavierAnnotation(k)) e.preventDefault();
        return;
      }
      if ((e.ctrlKey || e.metaKey) && k === "z" && !e.shiftKey) { e.preventDefault(); annulerDernier(); }
      else if ((e.ctrlKey || e.metaKey) && (k === "y" || (k === "z" && e.shiftKey))) { e.preventDefault(); retablirDernier(); }
      else if (e.key === "+" && !e.ctrlKey && !e.metaKey && !e.altKey) { if (poigneeAuClavier()) e.preventDefault(); }
      else if (k === "c" && !e.ctrlKey && !e.metaKey && !e.altKey && selectionDuTexte()) { e.preventDefault(); commenterSelection(); }
      else if (k === "s" && !e.ctrlKey && !e.metaKey && !e.altKey && selectionDuTexte()) { e.preventDefault(); suggererSelection(); }
      else if (/^[123]$/.test(k) && !e.ctrlKey && !e.metaKey && !e.altKey) { e.preventDefault(); changerVue(["relecture", "actuel", "propose", "depuis"][+k - 1]); }
      // Alt+1 to Alt+4: column tabs (by physical key:
      // Alt+digit produces another character on some keyboards).
      else if (e.altKey && !e.ctrlKey && !e.metaKey && /^Digit[1-4]$/.test(e.code)) {
        e.preventDefault();
        montrerColonne(true, true);
        choisirOnglet(["sommaire", "fichiers", "annotations", "navette"][+e.code.slice(5) - 1], true);
      }
      else if (e.altKey && !e.ctrlKey && !e.metaKey && e.code === "Digit0") { e.preventDefault(); montrerColonne($("cote").hidden); }
    });
    new ResizeObserver(disposer).observe($("texte"));
    // A newly displayed file initially has only an estimated height
    // (content-visibility): Firefox lays it out only in the next frame,
    // after card placement. The text height can then return to its
    // previous value without ResizeObserver detecting anything: cards
    // remained offset until reload. The event does not bubble,
    // hence capture.
    $("texte").addEventListener("contentvisibilityautostatechange", disposer, true);
    window.addEventListener("resize", () => { fermerOutils(); disposer(); });
    // Keep the original flow height while the fixed header becomes compact.
    const barre = $("barre"), reserve = $("reserve-barres");
    let pleineHauteur = 0, hauteurVue = 0, prevu = false;
    const mesurerBarres = () => {
      const compacte = document.body.classList.contains("barres-reduites");
      if (!compacte) pleineHauteur = barre.getBoundingClientRect().height;
      if (reserve.style.height !== pleineHauteur + "px") reserve.style.height = pleineHauteur + "px";
      const hauteur = barre.getBoundingClientRect().height;
      if (hauteur !== hauteurVue) {
        hauteurVue = hauteur;
        document.documentElement.style.setProperty("--haut-barre", hauteur + "px");
      }
    };
    const defile = () => {
      prevu = false;
      document.body.classList.toggle("defile", window.scrollY > 2);
      const occupe = $("recherche-texte") === document.activeElement || !$("recherche").hidden || !$("reglages").hidden || !$("menu-navette").hidden;
      const reduite = window.scrollY > 80 && !occupe;
      document.body.classList.toggle("barres-reduites", reduite);
      // What held the keyboard focus must not vanish with the full bars.
      const f = document.activeElement;
      if (reduite && f && f !== document.body && barre.contains(f) && !f.getClientRects().length) {
        document.body.classList.remove("barres-reduites");
      }
      mesurerBarres();
      let courante = null;
      for (const ligne of $("sommaire").querySelectorAll(".ligne")) {
        if (ligne._titre && ligne._titre.getBoundingClientRect().top <= hauteurVue + 40) courante = ligne;
      }
      for (const ligne of $("sommaire").querySelectorAll(".ligne")) {
        ligne.classList.toggle("courante", ligne === courante);
        const a = ligne.querySelector("a");
        if (ligne === courante) a.setAttribute("aria-current", "location"); else a.removeAttribute("aria-current");
      }
    };
    const planifierBarres = () => { if (!prevu) { prevu = true; requestAnimationFrame(defile); } };
    window.addEventListener("scroll", planifierBarres, { passive: true });
    window.addEventListener("resize", planifierBarres);
    new ResizeObserver(mesurerBarres).observe(barre);
    barre.addEventListener("focusin", () => {
      if (document.activeElement === $("recherche-texte")) {
        document.body.classList.remove("barres-reduites"); mesurerBarres();
      }
    });
    barre.addEventListener("focusout", planifierBarres);
    $("ouvrir-recherche").addEventListener("click", () => {
      document.body.classList.remove("barres-reduites"); mesurerBarres(); recherche.montrer();
    });
    // "Plus" (P20): what no longer fits in narrower windows. Each item relays
    // a click to the control it stands for, whose state it copies on opening.
    const plus = document.getElementById("plus-bouton"), menuPlus = document.getElementById("plus-menu");
    const ouvrirPlus = (ouvert) => {
      for (const b of menuPlus.querySelectorAll("[data-relais]")) b.disabled = document.getElementById(b.dataset.relais).disabled;
      menuPlus.hidden = !ouvert;
      plus.setAttribute("aria-expanded", String(ouvert));
      if (ouvert) { const premier = menuPlus.querySelector("button:not(:disabled)"); if (premier) premier.focus(); }
    };
    plus.addEventListener("click", () => ouvrirPlus(menuPlus.hidden));
    menuPlus.addEventListener("click", (e) => {
      const b = e.target.closest("[data-relais]");
      if (!b) return;
      ouvrirPlus(false);
      document.getElementById(b.dataset.relais).click();
    });
    menuPlus.addEventListener("keydown", (e) => {
      const items = [...menuPlus.querySelectorAll("button:not(:disabled)")], i = items.indexOf(document.activeElement);
      if (e.key === "Escape") { e.preventDefault(); ouvrirPlus(false); plus.focus(); }
      else if (e.key === "ArrowDown" && items.length) { e.preventDefault(); items[(i + 1) % items.length].focus(); }
      else if (e.key === "ArrowUp" && items.length) { e.preventDefault(); items[(i - 1 + items.length) % items.length].focus(); }
    });
    document.addEventListener("click", (e) => { if (!menuPlus.hidden && !menuPlus.contains(e.target) && !plus.contains(e.target)) ouvrirPlus(false); });
    mesurerBarres();
    defile();
    if (S.jeton) chargerReglages();
    setInterval(surveiller, 2000);
  }

  S.jeton = lireJeton();
  if (S.jeton) {
    // The project's stylesheet is a project file: it requires the token.
    const css = document.createElement("link");
    css.rel = "stylesheet";
    css.href = "/project.css?t=" + S.jeton;
    document.head.append(css);
  }
  brancher();
  if (!S.jeton) {
    bandeau("Session absente : ouvrez l'adresse complète affichée par « glose open » (elle contient le jeton de la session).");
    $("texte").setAttribute("aria-busy", "false");
    $("texte").textContent = "";
  } else {
    Promise.all([chargerProgression(), brouillons.charger()]).then(async ([, ok]) => {
      if (!ok) avis("Brouillons indisponibles : rechargez la page avant de saisir.", true);
      await charger(false);
    }).catch((e) => {
      // Otherwise, the page would remain on "Chargement du texte…" without explanation.
      bandeau("L'affichage n'a pas pu se faire : " + (e && e.message ? e.message : e) + ". Rechargez la page.");
      $("texte").setAttribute("aria-busy", "false");
    });
  }
})();
