// Glose : interface de relecture. Le texte affiché est produit par le serveur
// (pandoc, à partir d'un arbre où le HTML brut est neutralisé). Chaque mot
// porte data-g="fichier:début,fin[,t]", sa place en octets dans la source.
// L'interface n'envoie que des intentions ; le serveur valide et écrit.
"use strict";
(() => {
  // Fonctions pures, testées sans navigateur (web/donnees.js).
  const { lireG, court, signature, cleAnnotation, premierApres, compter, rayon, bilan, heure, quand } = GloseDonnees;
  // Références prises avant toute injection du texte : un titre du texte ne
  // peut pas leur être confondu (ses identifiants sont de plus préfixés).
  const UI = {};
  for (const id of ["bouton-sommaire", "titre", "etat", "annuler", "retablir", "anomalies", "bandeau", "sommaire",
    "texte", "marge", "bouton-commenter", "poignee", "menu-bloc", "menu-commenter", "menu-suggerer", "saisie", "saisie-titre", "saisie-extrait",
    "saisie-texte", "saisie-message", "saisie-autre", "saisie-fermer", "saisie-ok", "avis", "outils-bloc",
    "edition", "edition-texte", "edition-message", "edition-ok", "edition-fermer",
    "bouton-reglages", "reglages", "r-police", "r-taille", "r-taille-val", "r-interligne", "r-interligne-val",
    "r-largeur", "r-largeur-val", "r-justifie", "r-defaut", "menu-selection", "bouton-suggerer", "edition-note",
    "edition-titre", "edition-info", "navette", "remettre", "remise", "remise-titre", "remise-info", "remise-note",
    "remise-message", "remise-ok", "remise-force", "remise-fermer", "edition-source", "edition-voir", "edition-apercu",
    "edition-rendu", "edition-changements", "edition-mef", "edition-visuel", "edition-outils", "edition-aide-source", "vues", "edition-elargir", "edition-types", "menu-avant", "menu-apres", "remettre-texte", "remettre-icone",
    "bandeau-agent", "bandeau-reprendre", "boite", "boite-icone", "boite-titre", "boite-message", "boite-champs", "boite-erreur", "boite-ok", "boite-annuler", "edition-tenant", "edition-tenant-case",
    "onglets", "onglet-compte", "onglet-titre", "poignee-colonne", "cote", "app", "annotations", "filtres", "annotations-liste", "historique", "fichiers",
    "bouton-brouillons", "brouillons-compte", "brouillons", "brouillons-contenu", "brouillons-fermer", "saisie-brouillon", "edition-brouillon"]) {
    UI[id] = document.getElementById(id);
  }
  const $ = (id) => UI[id];
  const enc = new TextEncoder();
  const S = {
    jeton: "", doc: null, hashes: {}, spans: [], parFichier: new Map(), cartes: [],
    annuler: [], retablir: [], saisie: null, occupe: 0, generation: 0,
    actif: -1, bloc: null, echecs: 0, attente: 0, envoi: false,
    // Vue du texte (F3) : relecture et actuel sont rendues sur la vue Actuel
    // du serveur, propose sur sa vue Proposé ; un rendu par vue du serveur est
    // gardé pour basculer sans attendre.
    vue: ["relecture", "actuel", "propose"].includes(localStorage.getItem("glose-vue")) ? localStorage.getItem("glose-vue") : "relecture",
    rendus: {}, zones: {}, zone: null,
  };
  // Le conteneur du rendu affiché (un par vue du serveur, dans #texte).
  const zone = () => S.zone || $("texte");
  const vueServeur = (v) => (v === "propose" ? "proposed" : "current");
  const BLOCS = "p, li, dd, dt, h1, h2, h3, h4, h5, h6, td, th, figure, figcaption, pre";
  // Structures réécrites d'un tenant, par lignes entières de la source.
  const STRUCTURES = "table, ul, ol, dl, blockquote";
  const NOM_STRUCTURE = { TABLE: "le tableau", UL: "la liste", OL: "la liste", DL: "la liste de définitions", BLOCKQUOTE: "l'encadré" };
  const DE_STRUCTURE = { TABLE: "du tableau", UL: "de la liste", OL: "de la liste", DL: "de la liste de définitions", BLOCKQUOTE: "de l'encadré" };
  const TOUT = { TABLE: "Tout le tableau", UL: "Toute la liste", OL: "Toute la liste", DL: "Toute la liste de définitions", BLOCKQUOTE: "Tout l'encadré" };
  const NOMS = {
    comment: "Commentaire", highlight: "Passage commenté", addition: "Ajout proposé",
    deletion: "Suppression proposée", substitution: "Remplacement proposé",
  };
  const PROPOSITION = { addition: true, deletion: true, substitution: true };
  // Icônes des boutons : celles de Lucide (web/icones.js) ; chaque bouton
  // garde son nom en aria-label et en infobulle.
  function icone(nom) {
    return window.GloseIcones ? GloseIcones.svg(nom) : "";
  }
  const QUI = { author: "Auteur", agent: "Agent", unknown: "Inconnu" };
  const douceur = () => (matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth");
  // Un « % » qui n'introduit pas un octet encodé (image « taux_50%.png »,
  // adresse recopiée) ferait lever decodeURIComponent : la chaîne est gardée.
  function decoder(s) {
    try { return decodeURIComponent(s); } catch { return s; }
  }
  // Une valeur retenue illisible (écriture interrompue, autre version) ne doit
  // pas empêcher l'affichage : on repart de la valeur par défaut.
  function lireJSON(stockage, cle, defaut, valide) {
    try {
      const v = JSON.parse(stockage.getItem(cle) || "null");
      return v !== null && valide(v) ? v : defaut;
    } catch {
      return defaut;
    }
  }

  // ---------- Jeton de session ----------
  // Il arrive dans la partie « #t=… » de l'adresse, que le navigateur n'envoie
  // jamais au serveur ; il est gardé dans le stockage de session, propre à ce
  // port, et retiré de la barre d'adresse.
  function lireJeton() {
    const m = /(?:^#|&)t=([0-9a-f]{64})/.exec(location.hash);
    // La partie du projet demandée (glose open livre-1/, D-35).
    const f = /(?:^#|&)f=([^&]*)/.exec(location.hash);
    if (f) S.focus = decoder(f[1]);
    if (m) {
      sessionStorage.setItem("glose-jeton", m[1]);
      history.replaceState(null, "", location.pathname);
    }
    return sessionStorage.getItem("glose-jeton") || "";
  }

  // ---------- Boîtes de dialogue ----------
  // À la place des fenêtres du système (confirm, prompt) : une boîte de
  // l'interface. Sans champ, elle confirme et rend true ou false ; avec des
  // champs, elle rend leurs valeurs ({nom: valeur}) ou null si l'on annule.
  // valider(valeurs) peut rendre un message d'erreur, qui garde la boîte ouverte.
  function boite({ titre, message = "", icone = "alerte", ton = "", champs = [], ok = "Valider", annuler = "Annuler", valider }) {
    return new Promise((resoudre) => {
      const d = $("boite");
      d.className = "boite " + ton;
      $("boite-icone").innerHTML = window.GloseIcones ? GloseIcones.traces[icone] || "" : "";
      $("boite-titre").textContent = titre;
      $("boite-message").textContent = message;
      $("boite-erreur").textContent = "";
      $("boite-ok").textContent = ok;
      $("boite-annuler").textContent = annuler;
      const zoneChamps = $("boite-champs");
      zoneChamps.textContent = "";
      const entrees = {};
      let rang = null;
      for (const c of champs) {
        const l = el("label");
        l.append(document.createTextNode(c.libelle));
        if (c.aide) l.append(el("small", "", c.aide));
        const e = el(c.type === "textarea" ? "textarea" : "input");
        if (c.type !== "textarea") e.type = c.type || "text";
        if (c.min !== undefined) e.min = c.min;
        if (c.max !== undefined) e.max = c.max;
        e.value = c.valeur ?? "";
        if (c.placeholder) e.placeholder = c.placeholder;
        e.spellcheck = c.type === "textarea";
        entrees[c.nom] = e;
        l.append(e);
        // Deux champs courts côte à côte (colonnes et lignes d'un tableau).
        if (c.court) {
          if (!rang) { rang = el("div", "rang"); zoneChamps.append(rang); }
          rang.append(l);
        } else {
          rang = null;
          zoneChamps.append(l);
        }
      }
      const valeurs = () => Object.fromEntries(Object.entries(entrees).map(([k, e]) => [k, e.value]));
      const fin = (v) => {
        d.removeEventListener("cancel", surAnnulation);
        d.querySelector("form").onsubmit = null;
        $("boite-annuler").onclick = null;
        if (d.open) d.close();
        resoudre(v);
      };
      const surAnnulation = (e) => { e.preventDefault(); fin(champs.length ? null : false); };
      d.addEventListener("cancel", surAnnulation);
      $("boite-annuler").onclick = () => fin(champs.length ? null : false);
      d.querySelector("form").onsubmit = (e) => {
        e.preventDefault();
        const v = valeurs();
        const erreur = valider && valider(v);
        if (erreur) { $("boite-erreur").textContent = erreur; return; }
        fin(champs.length ? v : true);
      };
      for (const e of Object.values(entrees)) {
        if (e.tagName === "TEXTAREA") {
          e.addEventListener("keydown", (ev) => {
            if (ev.key === "Enter" && (ev.ctrlKey || ev.metaKey)) { ev.preventDefault(); d.querySelector("form").requestSubmit(); }
          });
        }
      }
      d.showModal();
      const premier = Object.values(entrees)[0];
      if (premier) { premier.focus(); if (premier.select) premier.select(); } else $("boite-ok").focus();
    });
  }

  // ---------- Infobulles ----------
  // Une seule infobulle, dessinée par Glose (pas celle du navigateur), pour
  // tout élément qui porte data-infobulle : au-dessus de lui, ou en dessous
  // faute de place, toujours dans l'écran ; dans le panneau ou la boîte
  // ouverts, elle est placée dans leur couche, au premier plan.
  const bulle = document.createElement("div");
  bulle.className = "infobulle";
  bulle.setAttribute("role", "tooltip");
  bulle.hidden = true;
  let bulleCible = null, bulleMinuterie = 0;
  function cacherBulle() {
    clearTimeout(bulleMinuterie);
    bulleCible = null;
    bulle.hidden = true;
    bulle.classList.remove("visible");
  }
  function montrerBulle(t) {
    if (!t.isConnected || !t.dataset.infobulle) return;
    const hote = t.closest("dialog[open]") || document.body;
    if (bulle.parentNode !== hote) hote.append(bulle);
    // Une infobulle se lit d'un coup d'œil : le texte est borné ici, et à
    // quatre lignes par la feuille de style.
    const texte = t.dataset.infobulle;
    bulle.textContent = texte.length > 240 ? texte.slice(0, 239).trimEnd() + "…" : texte;
    bulle.hidden = false;
    const r = t.getBoundingClientRect();
    const w = bulle.offsetWidth, h = bulle.offsetHeight;
    let top = r.top - h - 8;
    bulle.classList.toggle("dessous", top < 6);
    if (top < 6) top = r.bottom + 8;
    const left = Math.max(6, Math.min(window.innerWidth - w - 6, r.left + r.width / 2 - w / 2));
    // Dans une boîte (position fixe, parfois animée), les coordonnées sont
    // relatives à elle.
    const base = hote === document.body ? { left: 0, top: 0 } : hote.getBoundingClientRect();
    bulle.style.position = hote === document.body ? "fixed" : "absolute";
    bulle.style.left = left - base.left + "px";
    bulle.style.top = top - base.top + "px";
    requestAnimationFrame(() => bulle.classList.add("visible"));
  }
  document.addEventListener("pointerover", (e) => {
    const t = e.target.closest && e.target.closest("[data-infobulle]");
    if (t === bulleCible) return;
    cacherBulle();
    if (!t) return;
    bulleCible = t;
    bulleMinuterie = setTimeout(() => montrerBulle(t), 350);
  });
  document.addEventListener("focusin", (e) => {
    const t = e.target.closest && e.target.closest("[data-infobulle]");
    cacherBulle();
    if (t && t.matches(":focus-visible")) { bulleCible = t; montrerBulle(t); }
  });
  for (const ev of ["pointerdown", "focusout", "scroll", "wheel"]) document.addEventListener(ev, cacherBulle, true);
  document.addEventListener("keydown", (e) => { if (e.key === "Escape") cacherBulle(); });

  // ---------- Outils ----------
  let minuterieAvis = 0;
  function avis(msg, erreur) {
    const el = $("avis");
    el.textContent = msg;
    el.className = "visible" + (erreur ? " erreur" : "");
    clearTimeout(minuterieAvis);
    minuterieAvis = setTimeout(() => { el.className = ""; }, erreur ? 8000 : 3000);
  }

  // Bandeau durable, pour ce qui empêche de travailler (session, rendu).
  function bandeau(msg) {
    $("bandeau").textContent = msg || "";
    $("bandeau").hidden = !msg;
  }

  async function api(chemin, corps) {
    const opt = { headers: { "X-Glose-Token": S.jeton } };
    // La liste de fichiers affichée : le serveur refuse une requête qui
    // désigne un fichier par son rang si la liste a changé depuis (D-38).
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
    try { data = await r.json(); } catch (e) { /* réponse sans JSON */ }
    if (r.status === 409 && data && data.error === "files") {
      // Liste des fichiers changée : nouvel affichage ; le geste est à refaire.
      Promise.resolve().then(() => charger(true));
    }
    if (r.status === 401) {
      bandeau("Session absente ou périmée : ouvrez de nouveau l'adresse complète affichée par « glose open » (ou relancez-le).");
    }
    return { ok: r.ok, status: r.status, data };
  }

  // ---------- Saisies privées persistantes (P5.1) ----------
  let stockageBrouillons;
  try { stockageBrouillons = localStorage; } catch (_) { /* disque seul */ }
  const brouillons = GloseBrouillons.creer({ api, stockage: stockageBrouillons,
    identifiant: () => crypto.randomUUID().replaceAll("-", ""), changement: (entrees, avertissement) => {
      $("brouillons-compte").textContent = String(entrees.length);
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
    return { kind: edit ? "edit" : x.mode === "reply" ? "reply" : "comment",
      path: S.doc.files[x.file].path, hash: x.hash, mode: x.mode,
      start: x.start || 0, end: x.end || 0, at: x.at || 0, to: x.to || 0, annotation: x.annotation || 0,
      annotationID: x.annotationID || "", proposalKind: x.kind || "", old: x.ancien || "",
      original: edit ? x.original : "", text: edit ? texteEdition() : $("saisie-texte").value,
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
      if (d.kind === "edit" ? d.text === d.original && !d.note : !d.text) return null;
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
      bloc.append(el("h3", "", (d.kind === "edit" ? "Réécriture" : d.kind === "reply" ? "Réponse" : "Commentaire") + " · " + d.path));
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
            annotationID: d.annotationID, brouillon: e };
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
      actions.append(reprendre, retirer, sauver); bloc.append(actions); liste.append(bloc);
    }
    $("brouillons").showModal();
  }

  // Un petit morceau de Markdown (gras, italique, code, note…) affiché mis en
  // forme, par la lecture de l'éditeur (visuel.js) ; à défaut, le texte brut.
  function md(tag, classe, texte) {
    const e = el(tag, classe);
    const zone = document.createElement("div");
    // Les espaces du début et de la fin restent du texte : lues seules, elles
    // passeraient pour un préfixe de bloc (encadré, liste).
    const [, avant, coeur, apres] = /^(\s*)([\s\S]*?)(\s*)$/.exec(texte);
    if (coeur && window.GloseVisuel && !/\n\s*\n/.test(coeur) && GloseVisuel.lire(coeur, zone) && zone.firstChild) {
      for (const a of zone.querySelectorAll("[contenteditable]")) a.removeAttribute("contenteditable");
      e.append(avant, ...zone.firstChild.childNodes, apres);
    } else {
      e.textContent = texte;
    }
    return e;
  }

  function el(tag, classe, texte) {
    const e = document.createElement(tag);
    if (classe) e.className = classe;
    if (texte !== undefined) e.textContent = texte;
    return e;
  }

  // Texte des mots de la source compris dans [s, e) d'un fichier, sans le
  // contenu généré (numéros, renvois calculés).
  function texteSource(f, s, e) {
    const mots = S.parFichier.get(f) || [];
    let t = "";
    for (let j = premierApres(mots, s); j < mots.length && mots[j].e <= e; j++) t += mots[j].el.textContent;
    return t;
  }

  // ---------- Largeur de la colonne ----------
  // Retenue d'une visite à l'autre ; bornée pour laisser la place au texte.
  const COLONNE = { min: 12, max: 36, defaut: 16.5 };
  function largeurColonne(rem, retenir) {
    const v = Math.min(COLONNE.max, Math.max(COLONNE.min, Math.round(rem * 4) / 4));
    S.colonne = v;
    appliquerGrille();
    $("poignee-colonne").setAttribute("aria-valuenow", String(v));
    if (retenir) localStorage.setItem("glose-colonne", String(v));
    disposer();
  }
  // La largeur est posée sur la grille elle-même, jamais par une variable
  // CSS de la page : une variable se transmet à tous les éléments, et en
  // changer recalculait le style de tout le texte (2,6 s sur un gros ouvrage
  // entier, contre 0,8 s). Colonne masquée ou écran étroit : la feuille de
  // style décide.
  const etroit = matchMedia("(max-width: 1050px)");
  function appliquerGrille() {
    $("app").style.gridTemplateColumns = $("cote").hidden || etroit.matches || !S.colonne ? "" : S.colonne + "rem minmax(0, 1fr)";
  }

  function brancherColonne() {
    etroit.addEventListener("change", () => { appliquerGrille(); disposer(); });
    const p = $("poignee-colonne");
    const memo = parseFloat(localStorage.getItem("glose-colonne") || "");
    largeurColonne(memo > 0 ? memo : COLONNE.defaut, false);
    const rem = () => parseFloat(getComputedStyle(document.documentElement).fontSize) || 16;
    // Pendant le glisser, seul un trait guide suit la souris : changer la
    // largeur à chaque mouvement referait la mise en page de tout le texte
    // (lent sur un gros ouvrage). La largeur est appliquée au relâchement.
    p.addEventListener("pointerdown", (e) => {
      if (e.button !== 0) return;
      e.preventDefault();
      p.setPointerCapture(e.pointerId);
      p.classList.add("tire");
      document.body.classList.add("redimension");
      const cote = $("cote").getBoundingClientRect();
      const taille = rem();
      // La largeur suit l'écart au point de départ (et non la position de la
      // souris) : un simple clic sur la poignée, large de 6 px, ne la change pas.
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

  // ---------- Partie affichée (P6.8) ----------
  // La clé de la partie retenue, par projet (son dossier, donné par le serveur).
  function clePartie() {
    return "glose-partie:" + (S.racine || "");
  }

  // Affiche une autre partie du projet : "" (tout), un fichier, un dossier
  // terminé par « / ». Sans rendu par fichier (filtre d'un seul tenant), tout
  // est déjà affiché : on se rend à la partie.
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
  const recherche = GloseRecherche.creer({ api, courant: () => ({ doc: S.doc, view: vueServeur(S.vue) }),
    ouvrir: (m) => navigation.ouvrir(m, m.hash), avis });

  // L'arbre des fichiers : une vraie arborescence, dossiers repliables
  // (chevron) et fichiers, chacun avec son icône et le nombre d'annotations
  // en attente ; un clic sur un nom affiche cette partie (P6.8). Les dossiers
  // ouverts sont retenus d'un affichage à l'autre.
  function arbre() {
    const nav = $("fichiers");
    const defile = nav.scrollTop;
    nav.textContent = "";
    if (!S.doc) return;
    S.dossiersOuverts = S.dossiersOuverts || new Set(lireJSON(localStorage, "glose-dossiers:" + (S.racine || ""), [], Array.isArray));
    S.dossiersFermes = S.dossiersFermes || new Set(); // repliés à la main : la partie affichée ne les rouvre pas
    const retenir = () => localStorage.setItem("glose-dossiers:" + (S.racine || ""), JSON.stringify([...S.dossiersOuverts]));
    const parFichier = new Map();
    for (const a of S.doc.annotations) if (!a.partners || !a.partners.length || a.primary) parFichier.set(a.file, (parFichier.get(a.file) || 0) + 1);
    const svgIcone = (nom, classe) => { // icone() donne un <svg> complet
      const t = document.createElement("template");
      t.innerHTML = icone(nom);
      const s = t.content.firstElementChild || el("span");
      if (classe) s.classList.add(classe);
      return s;
    };
    // Une ligne de l'arbre : [chevron] icône nom [compte].
    const ligne = (profondeur, { nom, partie, n, iconeNom, dossier, ouvert, basculer, infobulle }) => {
      const li = el("li", "noeud" + (dossier ? " dossier" : " doc"));
      li.setAttribute("role", "treeitem");
      if (dossier) li.setAttribute("aria-expanded", String(ouvert));
      const rang = el("div", "rang");
      rang.style.setProperty("--profondeur", profondeur);
      if (dossier) {
        const ch = el("button", "chevron" + (ouvert ? " ouvert" : ""));
        ch.type = "button";
        ch.setAttribute("aria-label", (ouvert ? "Replier " : "Déplier ") + nom);
        ch.append(svgIcone("a-chevron"));
        ch.addEventListener("click", basculer);
        rang.append(ch);
      } else {
        rang.append(el("span", "chevron vide"));
      }
      const a = el("a", "partie");
      a.href = "#";
      a.append(svgIcone(iconeNom, "type"), el("span", "nom", nom));
      if (n) a.append(el("span", "compte", String(n)));
      if (S.partie === partie) a.setAttribute("aria-current", "true");
      if (infobulle) a.dataset.infobulle = infobulle;
      a.addEventListener("click", (e) => {
        e.preventDefault();
        if (dossier && !ouvert) basculer();
        choisirPartie(partie);
      });
      rang.append(a);
      li.append(rang);
      return li;
    };
    const total = [...parFichier.values()].reduce((x, y) => x + y, 0);
    const racine = { dossiers: new Map(), items: [] };
    S.doc.files.forEach((f, i) => {
      const parts = f.path.split("/");
      let d = racine, chemin = "";
      for (const nom of parts.slice(0, -1)) {
        chemin += nom + "/";
        if (!d.dossiers.has(nom)) { const n = { nom, chemin, dossiers: new Map(), items: [] }; d.dossiers.set(nom, n); d.items.push(n); }
        d = d.dossiers.get(nom);
      }
      d.items.push({ fichier: i, nom: parts[parts.length - 1] });
    });
    const compteDossier = (d) => d.items.reduce((t, x) => t + (x.dossiers ? compteDossier(x) : parFichier.get(x.fichier) || 0), 0);
    const liste = (d, profondeur) => {
      const ul = el("ul");
      ul.setAttribute("role", profondeur ? "group" : "tree");
      for (const x of d.items) {
        if (x.dossiers) {
          // Ouvert : retenu, ou contenant la partie affichée.
          const ouvert = S.dossiersOuverts.has(x.chemin) ||
            (S.partie !== "" && S.partie.startsWith(x.chemin) && !S.dossiersFermes.has(x.chemin));
          const li = ligne(profondeur, {
            nom: x.nom, partie: x.chemin, n: compteDossier(x), iconeNom: ouvert ? "a-dossier-ouvert" : "a-dossier", dossier: true, ouvert,
            infobulle: x.chemin + " : afficher tout le dossier",
            basculer: () => {
              if (ouvert) { S.dossiersOuverts.delete(x.chemin); S.dossiersFermes.add(x.chemin); }
              else { S.dossiersOuverts.add(x.chemin); S.dossiersFermes.delete(x.chemin); }
              retenir();
              arbre();
            },
          });
          if (ouvert) li.append(liste(x, profondeur + 1));
          ul.append(li);
        } else {
          const f = S.doc.files[x.fichier];
          ul.append(ligne(profondeur, {
            nom: x.nom.replace(/\.md$/i, ""), partie: f.path, n: parFichier.get(x.fichier), iconeNom: "a-fichier",
            infobulle: f.path + " · " + Math.max(1, Math.round(f.size / 1024)) + " Ko",
          }));
        }
      }
      return ul;
    };
    const tete = el("ul", "tete");
    tete.setAttribute("role", "presentation");
    tete.append(ligne(0, {
      nom: "Tout le projet", partie: "", n: total, iconeNom: "a-projet",
      infobulle: S.doc.files.length + " fichier" + (S.doc.files.length > 1 ? "s" : ""),
    }));
    nav.append(tete);
    if (!S.doc.perFile) nav.append(el("p", "note", "Le filtre d'affichage numérote tout le projet : il est affiché en entier, un clic s'y rend."));
    nav.append(liste(racine, 0));
    nav.scrollTop = defile;
  }

  // ---------- Chargement ----------
  // Renvoie true si l'affichage a été mis à jour.
  async function charger(garderPosition) {
    const gen = ++S.generation;
    const repere = garderPosition ? capturer() : null;
    const focus = document.activeElement && document.activeElement.closest && document.activeElement.closest(".carte");
    // Les cartes se retrouvent par l'identité de leur annotation, non par
    // leur rang, qui change quand une annotation apparaît ou disparaît.
    const iFocus = focus ? S.cartes.findIndex((c) => c.carte === focus) : -1;
    const carteFocus = iFocus >= 0 ? cleAnnotation(S.cartes[iFocus].a) : null;
    S.occupe++;
    $("texte").setAttribute("aria-busy", "true");
    if (S.doc) $("etat").textContent = "mise à jour…";
    const vs = vueServeur(S.vue);
    // La partie du projet affichée (P6.8) : choisie, demandée par « glose
    // open livre-1/ », retenue d'une fois sur l'autre, ou choisie par le
    // serveur (« ? » : tout si le projet est petit, sinon le premier fichier).
    if (S.partie === undefined) {
      if (S.racine === undefined) {
        const p = await api("/api/ping");
        S.racine = p.ok ? p.data.root : "";
      }
      const retenue = localStorage.getItem(clePartie()); // "" : tout le projet
      S.partie = S.focus || (retenue !== null ? retenue : "?");
    }
    const url = () => "/api/doc?part=" + encodeURIComponent(S.partie) + (vs === "current" ? "" : "&view=" + vs);
    let r = await api(url());
    // Texte changé pendant le rendu (remise, écriture extérieure) : le rendu
    // est périmé, on le redemande sans rien signaler, quelques fois au plus.
    for (let essai = 0; essai < 3 && !r.ok && r.status === 409 && r.data && r.data.error === "stale-render" && gen === S.generation; essai++) {
      r = await api(url());
    }
    S.occupe--;
    if (gen !== S.generation) return false; // un chargement plus récent a eu lieu
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
    // Les rendus des autres vues ne sont gardés que s'ils sont à jour.
    for (const k of Object.keys(S.rendus)) if (signature(S.rendus[k]) !== signature(r.data)) delete S.rendus[k];
    S.rendus[vs] = r.data;
    afficher(r.data, repere, carteFocus);
    if (S.focus) { // une seule fois, au premier affichage
      const cible = [...zone().querySelectorAll(".glose-file")].find((d) => d.dataset.path === S.focus || d.dataset.path.startsWith(S.focus));
      S.focus = "";
      if (cible && !S.doc.perFile) cible.scrollIntoView({ block: "start" });
    }
    return true;
  }

  // Change de vue sans recharger la page et sans perdre la position de lecture
  // (F3). Relecture et Actuel partagent le même rendu ; Proposé a le sien,
  // gardé tant que les fichiers et le journal n'ont pas changé.
  async function changerVue(v) {
    if (v === S.vue || !["relecture", "actuel", "propose"].includes(v)) return;
    S.vue = v;
    localStorage.setItem("glose-vue", v);
    majVues();
    if (!S.doc) return;
    const garde = S.rendus[vueServeur(v)];
    if (garde && signature(garde) === signature(S.doc)) {
      S.generation++; // un chargement en cours pour l'autre vue est abandonné
      afficher(garde, capturer(), -1);
    } else {
      await charger(true);
    }
    avis("Vue « " + { relecture: "Relecture", actuel: "Actuel", propose: "Proposé" }[v] + " ».");
  }

  function majVues() {
    for (const b of $("vues").querySelectorAll("button")) b.setAttribute("aria-checked", String(b.dataset.vue === S.vue));
    document.body.dataset.vue = S.vue;
  }

  // Affiche un rendu du serveur dans la vue courante. Si ce rendu est déjà
  // dans la page (Relecture et Actuel partagent le leur), seules les
  // annotations sont refaites : le HTML n'est pas analysé de nouveau.
  function afficher(d, repere, carteFocus) {
    S.actifCle = S.cartes[S.actif] ? cleAnnotation(S.cartes[S.actif].a) : null;
    S.doc = d;
    S.hashes = {};
    d.files.forEach((f) => { S.hashes[f.index] = f.hash; });
    bandeau("");
    $("titre").textContent = d.title;
    document.title = d.title + " — Glose";
    const texte = $("texte");
    // Le bloc survolé appartient à l'ancien affichage : on l'oublie.
    fermerOutils();
    S.bloc = null;
    // Un conteneur par vue du serveur : celui qui n'est pas affiché garde sa
    // mise en page (content-visibility: hidden), la bascule est immédiate.
    for (const c of Array.from(texte.children)) if (!c.classList.contains("rendu")) c.remove();
    const vs = vueServeur(S.vue);
    let z = S.zones[vs];
    if (!z) {
      // La classe reste sur le conteneur : changer une classe de #texte
      // obligerait à recalculer aussi le rendu caché.
      z = S.zones[vs] = el("div", "rendu" + (vs === "proposed" ? " vue-propose" : ""));
      z.dataset.vue = vs;
      texte.append(z);
    }
    for (const [k, autre] of Object.entries(S.zones)) {
      autre.classList.toggle("garde", k !== vs);
      // Un rendu périmé (fichiers ou journal changés depuis) est vidé.
      if (k !== vs && autre.doc && !(S.rendus[k] === autre.doc)) { autre.replaceChildren(); autre.doc = null; }
    }
    S.zone = z;
    if (z.doc === d) {
      nettoyer();
      S.spans = z.spans; S.parFichier = z.parFichier;
    } else {
      // Analysé dans un modèle inerte : les images n'y sont pas chargées
      // avant d'avoir reçu le jeton (ressources).
      const modele = document.createElement("template");
      modele.innerHTML = d.html;
      ressources(modele.content);
      z.replaceChildren(modele.content);
      z.doc = d;
      indexer();
      z.spans = S.spans; z.parFichier = S.parFichier;
    }
    texte.setAttribute("aria-busy", "false");
    anomalies();
    annoter();
    sommaire();
    majEtat();
    majBoutons();
    if (repere) restaurer(repere);
    disposerMaintenant();
    // La carte active reste la même annotation. Si elle a disparu (décidée) :
    // au clavier, la suivante devient active (J, A, A… enchaîne) ; à la
    // souris, aucune.
    if (S.actifCle) {
      const k = S.cartes.findIndex((c) => !c.cache && cleAnnotation(c.a) === S.actifCle);
      if (k >= 0) S.actif = k;
      else if (!S.clavier) S.actif = -1;
      else {
        const suivantes = S.cartes.map((c, i) => i).filter((i) => i >= S.actif && !S.cartes[i].cache);
        S.actif = suivantes.length ? suivantes[0] : -1;
      }
    }
    if (S.actif >= 0 && S.actif < S.cartes.length) activer(S.actif, false, S.clavier);
    else { S.actif = -1; for (const x of document.querySelectorAll(".carte.active, .g-actif")) x.classList.remove("active", "g-actif", "clavier"); }
    const iCarte = carteFocus ? S.cartes.findIndex((c) => cleAnnotation(c.a) === carteFocus) : -1;
    if (iCarte >= 0) S.cartes[iCarte].carte.focus({ preventScroll: true });
  }

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

  // Retire ce que annoter() a ajouté au texte affiché (numéros, texte ajouté
  // de la vue Relecture, classes), pour annoter de nouveau le même rendu.
  function nettoyer() {
    for (const x of zone().querySelectorAll(".g-ajout-hote, .g-ancre, ins.g-ajout")) x.remove();
    for (const x of zone().querySelectorAll("[data-a]")) {
      x.classList.remove("g-prop", "g-hl", "g-barre", "g-actif");
      delete x.dataset.a;
    }
  }

  // Liens extérieurs dans un nouvel onglet ; images du projet avec le jeton.
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
        u.searchParams.set("t", S.jeton); // avant un éventuel fragment (#…)
        img.src = u.href;
      }
    }
  }

  // Sommaire : la progression de la relecture, puis les titres, chacun avec
  // le nombre d'annotations de sa section (sous-sections comprises). Les
  // onglets « Annotations » et « Navette » sont tenus à jour en même temps.
  function sommaire() {
    listeAnnotations();
    arbre();
    if (S.onglet === "navette") historique();
    const nav = $("sommaire");
    nav.textContent = "";
    const titres = Array.from(zone().querySelectorAll("h1, h2, h3"));
    const comptes = titres.map(() => 0);
    for (const c of S.cartes) {
      if (!c.badge) continue;
      // Dernier titre qui précède l'annotation.
      let lo = 0, hi = titres.length;
      while (lo < hi) {
        const m = (lo + hi) >> 1;
        if (titres[m].compareDocumentPosition(c.badge) & Node.DOCUMENT_POSITION_FOLLOWING) lo = m + 1; else hi = m;
      }
      let k = lo - 1;
      if (k < 0) continue;
      let niveau = +titres[k].tagName[1];
      comptes[k]++;
      for (let j = k - 1; j >= 0 && niveau > 1; j--) {
        const n = +titres[j].tagName[1];
        if (n < niveau) { comptes[j]++; niveau = n; }
      }
    }
    // Progression de la relecture : un état par titre, gardé par le serveur
    // dans .glose/progression.json (jamais dans le texte).
    const etats = S.progression || {};
    const cle = (h) => {
      const f = h.closest(".glose-file");
      // Le préfixe de rendu dépend du rang, pas l'identité de la section :
      // garder les clés historiques et la progression après un réordonnancement.
      return ((f && f.dataset.path) || "") + "#" + (h.id.replace(/^gl-f\d+-a-/, "") || h.textContent.trim());
    };
    let relues = 0, enCours = 0;
    for (const h of titres) {
      const e = etats[cle(h)];
      if (e && e.status === "relue") relues++;
      else if (e && e.status === "en-cours") enCours++;
    }
    if (titres.length) {
      const pr = el("div", "progression");
      const pct = (n) => (100 * n / titres.length).toFixed(1) + "%";
      const barre = el("div", "barre");
      const b1 = el("span", "relue"); b1.style.width = pct(relues);
      const b2 = el("span", "en-cours"); b2.style.width = pct(enCours);
      barre.append(b1, b2);
      pr.append(el("div", "libelle", "Relecture : " + relues + " / " + titres.length + " section" + (titres.length > 1 ? "s" : "") +
        " relue" + (relues > 1 ? "s" : "") + (enCours ? " · " + enCours + " en cours" : "")), barre);
      pr.dataset.infobulle = "Cliquez sur la pastille d'un titre pour le marquer « en cours », puis « relu ».";
      nav.append(pr);
    }
    let fichier = null;
    const plusieurs = S.doc.files.length > 1;
    titres.forEach((h, k) => {
      const f = h.closest(".glose-file");
      if (plusieurs && f && f !== fichier) {
        fichier = f;
        nav.append(el("div", "fichier", f.dataset.path || ""));
      }
      const k2 = cle(h);
      const etat = (etats[k2] && etats[k2].status) || "";
      const ligne = el("div", "ligne n" + h.tagName[1] + (etat ? " " + etat : ""));
      const pastille = el("button", "pastille " + (etat || "a-relire"));
      pastille.type = "button";
      const NOM_ETAT = { "": "à relire", "en-cours": "en cours", relue: "relue" };
      const SUIVANT = { "": "en-cours", "en-cours": "relue", relue: "" };
      pastille.innerHTML = icone(etat || "a-relire");
      pastille.setAttribute("aria-label", "Section " + NOM_ETAT[etat] + " : passer à « " + NOM_ETAT[SUIVANT[etat]] + " »");
      pastille.dataset.infobulle = "Section " + NOM_ETAT[etat] + " — cliquer : « " + NOM_ETAT[SUIVANT[etat]] + " »";
      pastille.addEventListener("click", () => marquerSection(k2, h.textContent.trim(), SUIVANT[etat]));
      ligne.append(pastille);
      const a = el("a", "", court(h.textContent, 90));
      if (comptes[k]) {
        const b = el("span", "compte", String(comptes[k]));
        b.dataset.infobulle = comptes[k] + " annotation" + (comptes[k] > 1 ? "s" : "") + " dans cette section";
        a.append(b);
      }
      a.href = "#";
      a.addEventListener("click", (e) => { e.preventDefault(); h.scrollIntoView({ block: "start" }); });
      ligne.append(a);
      nav.append(ligne);
    });
  }

  // ---------- Onglets de la colonne (P4.1) ----------
  // Le nom de l'onglet affiché, sous la rangée d'icônes, avec le nombre
  // d'annotations en attente pour l'onglet Annotations.
  function titreOnglet() {
    const b = $("onglets").querySelector('[data-onglet="' + (S.onglet || "sommaire") + '"]');
    if (!b) return;
    const n = $("onglet-compte").hidden ? "" : $("onglet-compte").textContent;
    $("onglet-titre").textContent = b.dataset.nom + (S.onglet === "annotations" && n ? " · " + n : "");
  }

  function choisirOnglet(nom, focus) {
    if (!["sommaire", "fichiers", "annotations", "navette"].includes(nom)) nom = "sommaire";
    S.onglet = nom;
    localStorage.setItem("glose-onglet", nom);
    for (const b of $("onglets").querySelectorAll("[role=tab]")) {
      const oui = b.dataset.onglet === nom;
      b.setAttribute("aria-selected", oui ? "true" : "false");
      b.tabIndex = oui ? 0 : -1;
      if (oui && focus) b.focus();
    }
    titreOnglet();
    $("sommaire").hidden = nom !== "sommaire";
    $("fichiers").hidden = nom !== "fichiers";
    $("annotations").hidden = nom !== "annotations";
    $("historique").hidden = nom !== "navette";
    if (nom === "navette" && S.doc) historique();
  }

  // Filtres des annotations : statut, auteur, type, fichier, tour. Gardés
  // pour la session ; une valeur qui ne correspond plus à rien est oubliée.
  const FILTRES = [
    ["statut", "Statut", [["", "Tous"], ["new", "De ce tour"], ["open", "Remises, en attente"]], (a) => a.status || ""],
    ["auteur", "Auteur", [["", "Tous"], ["author", "Vous"], ["agent", "L'agent"]], (a) => (a.by === "agent" ? "agent" : "author")],
    ["type", "Type", [["", "Tous"], ["discussion", "Commentaires"], ["proposition", "Propositions"]],
      (a) => (PROPOSITION[a.kind] ? "proposition" : "discussion")],
  ];

  function listeAnnotations() {
    const corps = $("annotations-liste"), zoneF = $("filtres");
    corps.textContent = "";
    zoneF.textContent = "";
    if (!S.doc) return;
    S.filtres = S.filtres || lireJSON(sessionStorage, "glose-filtres", {}, (v) => typeof v === "object" && !Array.isArray(v));
    const visibles = S.cartes.map((c, i) => ({ c, i })).filter((x) => !x.c.cache || x.c.ailleurs);
    const compte = $("onglet-compte");
    compte.hidden = !visibles.length;
    compte.textContent = String(visibles.length);
    $("onglets").querySelector('[data-onglet="annotations"]').setAttribute("aria-label",
      "Annotations" + (visibles.length ? " (" + visibles.length + " en attente)" : ""));
    titreOnglet();
    const tour = (a) => (a.turn ? String(Math.floor((a.turn + 1) / 2)) : "");
    const defs = FILTRES.slice();
    if (S.doc.files.length > 1) {
      defs.push(["fichier", "Fichier", [["", "Tous"]].concat(S.doc.files.map((f, k) => [String(k), f.path])), (a) => String(a.file)]);
    }
    const tours = [...new Set(visibles.map((x) => tour(x.c.a)).filter(Boolean))].sort((x, y) => x - y);
    if (tours.length > 1) defs.push(["tour", "Tour", [["", "Tous"]].concat(tours.map((t) => [t, "Tour " + t])), (a) => tour(a)]);
    for (const [cle, libelle, options] of defs) {
      if (!options.some(([v]) => v === S.filtres[cle])) delete S.filtres[cle];
      const id = "filtre-" + cle;
      const l = el("label", "", libelle);
      l.htmlFor = id;
      const sel = el("select");
      sel.id = id;
      for (const [v, t] of options) {
        const o = el("option", "", t);
        o.value = v;
        sel.append(o);
      }
      sel.value = S.filtres[cle] || "";
      sel.addEventListener("change", () => {
        if (sel.value) S.filtres[cle] = sel.value; else delete S.filtres[cle];
        sessionStorage.setItem("glose-filtres", JSON.stringify(S.filtres));
        listeAnnotations();
      });
      zoneF.append(l, sel);
    }
    const retenues = visibles.filter((x) => defs.every(([cle, , , valeur]) => !S.filtres[cle] || valeur(x.c.a) === S.filtres[cle]));
    const resume = el("div", "resume", retenues.length === visibles.length
      ? visibles.length + " annotation" + (visibles.length > 1 ? "s" : "") + " en attente"
      : retenues.length + " sur " + visibles.length);
    if (Object.keys(S.filtres).length) {
      const b = el("button", "", "Effacer les filtres");
      b.type = "button";
      b.addEventListener("click", () => { S.filtres = {}; sessionStorage.removeItem("glose-filtres"); listeAnnotations(); });
      resume.append(b);
    }
    zoneF.append(resume);
    if (!retenues.length) {
      corps.append(el("p", "vide", visibles.length ? "Aucune annotation ne répond à ces filtres." : "Aucune annotation en attente."));
    }
    const bloc = el("div", "annotations");
    let fichier = -1;
    for (const { c, i } of retenues) {
      if (S.doc.files.length > 1 && c.a.file !== fichier) {
        fichier = c.a.file;
        bloc.append(el("div", "fichier", S.doc.files[fichier].path));
      }
      const a = el("a");
      a.href = "#";
      const resumeA = c.a.primary && c.a.note ? "Note : " + c.a.note : c.a.kind === "comment" ? c.a.text :
        c.a.kind === "substitution" ? c.a.text + " → " + c.a.new : (c.a.comments[0] || c.a.text);
      a.append(el("span", "num" + (PROPOSITION[c.a.kind] ? " prop" : ""), String(i + 1)), md("span", "", court(resumeA, 70)));
      const meta = el("span", "meta", (c.a.by === "agent" ? "agent" : "vous") + (tour(c.a) ? " · t" + tour(c.a) : ""));
      a.append(meta);
      a.dataset.infobulle = NOMS[c.a.kind] + (c.a.by === "agent" ? " de l'agent" : "") + " — " + S.doc.files[c.a.file].path +
        ", ligne " + c.a.line + (c.a.status === "new" ? " · de ce tour" : c.a.status === "open" ? " · remise, en attente" : "");
      a.dataset.carte = String(i);
      if (i === S.actif) a.setAttribute("aria-current", "true");
      if (c.ailleurs) {
        a.classList.add("ailleurs");
        a.dataset.infobulle += " · dans une autre partie : un clic l'affiche";
        a.addEventListener("click", (e) => {
          e.preventDefault();
          const cle = cleAnnotation(c.a);
          choisirPartie(S.doc.files[c.a.file].path, () => {
            const k = S.cartes.findIndex((x) => !x.cache && cleAnnotation(x.a) === cle);
            if (k >= 0) activer(k, true);
          });
        });
      } else {
        a.addEventListener("click", (e) => { e.preventDefault(); activer(i, true); });
      }
      bloc.append(a);
    }
    corps.append(bloc);
    // Décisions en lot, fichier par fichier (F12) : sur toutes les
    // propositions en attente du fichier, filtres ou non.
    const parFichier = new Map();
    for (const c of S.cartes) if (PROPOSITION[c.a.kind] && !c.cache) parFichier.set(c.a.file, (parFichier.get(c.a.file) || 0) + 1);
    for (const [f, n] of parFichier) {
      if (n < 2) continue;
      const lot = el("div", "lot");
      lot.append(el("span", "", (S.doc.files.length > 1 ? S.doc.files[f].path + " : " : "") + "les " + n + " propositions en attente"));
      for (const [libelle, decision] of [["Tout accepter", "accept"], ["Tout rejeter", "reject"]]) {
        const b = el("button", decision === "accept" ? "accepter" : "", libelle);
        b.type = "button";
        b.addEventListener("click", () => deciderTout(f, decision, n));
        lot.append(b);
      }
      corps.append(lot);
    }
  }

  // L'histoire de la navette en frise (onglet « Navette ») : deux couloirs,
  // vous et l'agent ; chaque main est un nœud dans le couloir de celui qui
  // l'avait, chaque remise une flèche vers l'autre couloir. L'anneau d'un
  // nœud dit ce qui a été fait pendant cette main (SEGMENTS) ; un clic sur
  // un nœud en donne le détail. Tout est construit par createElementNS et
  // textContent : aucun texte du journal n'est lu comme du HTML.
  const SEGMENTS = GloseDonnees.SEGMENTS;

  const SVG = "http://www.w3.org/2000/svg";
  function svg(tag, attrs, texte) {
    const e = document.createElementNS(SVG, tag);
    for (const [k, v] of Object.entries(attrs || {})) e.setAttribute(k, String(v));
    if (texte !== undefined) e.textContent = texte;
    return e;
  }

  async function historique() {
    const gen = (S.genHistorique = (S.genHistorique || 0) + 1);
    const r = await api("/api/log");
    if (gen !== S.genHistorique) return;
    const zoneH = $("historique");
    if (!r.ok) { zoneH.replaceChildren(el("p", "vide", "Historique illisible : " + (r.data.message || "erreur"))); return; }
    const parts = (r.data.turns || []).map((p) => Object.assign({}, p, { qui: p.number % 2 === 1 ? "author" : "agent", n: compter(p.operations) }));
    if (!parts.length) { zoneH.replaceChildren(el("p", "vide", "Aucun tour pour l'instant.")); return; }
    const premiere = !zoneH.querySelector(".frise");
    const defile = zoneH.scrollTop;

    // Résumé : tours, propositions décidées.
    const tot = compter(parts.flatMap((p) => p.operations));
    const resume = el("div", "frise-resume");
    const tours = parts[parts.length - 1].round;
    resume.append(el("strong", "", tours + " tour" + (tours > 1 ? "s" : "")),
      el("span", "", tot.acceptees + " acceptée" + (tot.acceptees > 1 ? "s" : "") + " · " + tot.rejetees + " rejetée" + (tot.rejetees > 1 ? "s" : "") +
        " · " + tot.discussion + " message" + (tot.discussion > 1 ? "s" : "")));
    const decidees = tot.acceptees + tot.rejetees + tot.resolues;
    if (decidees) {
      const barre = el("div", "barre");
      for (const k of ["acceptees", "rejetees", "resolues"]) {
        if (!tot[k]) continue;
        const b = el("span", "seg-" + k);
        b.style.flexGrow = String(tot[k]);
        barre.append(b);
      }
      barre.dataset.infobulle = "Décisions : " + tot.acceptees + " acceptée(s), " + tot.rejetees + " rejetée(s), " + tot.resolues + " discussion(s) résolue(s)";
      resume.append(barre);
    }

    // La frise.
    // Largeur fixe, mise à l'échelle de la colonne (viewBox).
    const W = 224;
    const X = { author: 50, agent: W - 40 };
    const GAP = 92, Y0 = 52;
    const y = (i) => Y0 + i * GAP;
    const H = y(parts.length - 1) + 44;
    const libelle = parts.map((p) => (p.qui === "agent" ? "L'agent" : "Vous") + ", tour " + p.round + " : " + (bilan(p.operations) || "rien d'écrit")).join(". ");
    const frise = svg("svg", { class: "frise", width: "100%", viewBox: "0 0 " + W + " " + H, role: "group", "aria-label": "Frise de la navette. " + libelle });
    const defs = svg("defs");
    const fleche = svg("marker", { id: "frise-fleche", viewBox: "0 0 10 10", refX: 9, refY: 5, markerWidth: 7, markerHeight: 7, orient: "auto-start-reverse" });
    fleche.append(svg("path", { d: "M0,0 L10,5 L0,10 z", class: "pointe" }));
    defs.append(fleche);
    frise.append(defs);
    // Bandes des tours, en alternance.
    const parTour = new Map();
    parts.forEach((p, i) => { if (!parTour.has(p.round)) parTour.set(p.round, []); parTour.get(p.round).push(i); });
    for (const [n, idx] of parTour) {
      const y1 = y(idx[0]) - GAP / 2 + 6, y2 = y(idx[idx.length - 1]) + GAP / 2 - 6;
      frise.append(svg("rect", { x: 2, y: y1, width: W - 4, height: y2 - y1, rx: 10, class: "bande" + (n % 2 ? "" : " paire") }));
      const ym = (y1 + y2) / 2; // à la verticale, dans la marge gauche de la bande
      frise.append(svg("text", { x: 13, y: ym, class: "tour", "text-anchor": "middle", transform: "rotate(-90 13 " + ym + ")" }, "Tour " + n));
    }
    // Couloirs.
    for (const [qui, nom] of [["author", "Vous"], ["agent", "Agent"]]) {
      frise.append(svg("line", { x1: X[qui], y1: 22, x2: X[qui], y2: H - 8, class: "couloir " + qui }));
      frise.append(svg("text", { x: X[qui], y: 14, class: "couloir-nom", "text-anchor": "middle" }, nom));
    }
    // Remises : une flèche courbe d'un couloir à l'autre.
    parts.forEach((p, i) => {
      if (i === parts.length - 1) return;
      const a = { x: X[p.qui], y: y(i) }, b = { x: X[parts[i + 1].qui], y: y(i + 1) };
      const ra = rayon(p.n), rb = rayon(parts[i + 1].n);
      const dir = Math.sign(b.x - a.x) || 1;
      const d = "M" + (a.x + dir * (ra + 2)) + "," + a.y + " C" + (a.x + dir * 60) + "," + a.y + " " + (b.x - dir * 60) + "," + b.y + " " + (b.x - dir * (rb + 3)) + "," + b.y;
      const chemin = svg("path", { d, class: "remise" + (p.reclaimed ? " reprise" : ""), "marker-end": "url(#frise-fleche)" });
      frise.append(chemin);
      // L'heure de la remise, et sa note, au milieu de la flèche.
      const mx = (a.x + b.x) / 2, my = (a.y + b.y) / 2;
      const t = svg("text", { x: mx, y: my + 15, class: "remise-heure", "text-anchor": "middle" }, (p.reclaimed ? "reprise " : "") + heure(p.time));
      frise.append(t);
      if (p.note) {
        const g = svg("g", { class: "remise-note", transform: "translate(" + (mx - 7) + "," + (my - 5.5) + ")" });
        g.append(svg("rect", { width: 14, height: 11, rx: 3 }), svg("path", { d: "M3,4 h8 M3,7 h5" }));
        g.append(svg("title", {}, "Note : " + court(p.note, 400)));
        frise.append(g);
      }
    });
    // Nœuds.
    if (S.partieChoisie === undefined || !parts.some((p) => p.number === S.partieChoisie)) {
      const actives = parts.filter((p) => p.n.total || p.actor);
      S.partieChoisie = (actives.length ? actives[actives.length - 1] : parts[parts.length - 1]).number;
    }
    const detail = el("div", "frise-detail");
    detail.setAttribute("aria-live", "polite");
    const choisir = (p, focus) => {
      S.partieChoisie = p.number;
      for (const g of frise.querySelectorAll(".noeud")) g.classList.toggle("choisi", g.dataset.partie === String(p.number));
      detail.replaceChildren(el("div", "titre", "Tour " + p.round + " · " + (p.qui === "agent" ? "l'agent" : "vous")), partie(p));
      if (focus) frise.querySelector('.noeud[data-partie="' + p.number + '"]').focus();
    };
    parts.forEach((p, i) => {
      const cx = X[p.qui], cy = y(i), R = rayon(p.n);
      const g = svg("g", { class: "noeud " + p.qui + (p.actor ? "" : " en-cours"), tabindex: 0, role: "button", "data-partie": p.number,
        "aria-label": (p.qui === "agent" ? "L'agent" : "Vous") + ", tour " + p.round + " : " + (bilan(p.operations) || "rien d'écrit") });
      g.append(svg("circle", { cx, cy, r: R + 5, class: "halo" }));
      g.append(svg("circle", { cx, cy, r: R, class: "fond" }));
      const C = 2 * Math.PI * R;
      let deja = 0;
      if (p.n.total) {
        for (const [k] of SEGMENTS) {
          if (!p.n[k]) continue;
          const l = (C * p.n[k]) / p.n.total;
          g.append(svg("circle", { cx, cy, r: R, class: "seg seg-" + k, "stroke-dasharray": l + " " + (C - l), "stroke-dashoffset": -deja,
            transform: "rotate(-90 " + cx + " " + cy + ")" }));
          deja += l;
        }
        g.append(svg("text", { x: cx, y: cy + 4, class: "nombre", "text-anchor": "middle" }, String(p.n.total)));
      } else {
        g.append(svg("circle", { cx, cy, r: R, class: "vide" }));
      }
      const detailBulle = SEGMENTS.filter(([k]) => p.n[k]).map(([k, nom]) => p.n[k] + " " + nom).join(", ");
      g.append(svg("title", {}, (p.qui === "agent" ? "L'agent" : "Vous") + " — " + (detailBulle || "rien d'écrit")));
      g.addEventListener("click", () => choisir(p, false));
      g.addEventListener("keydown", (e) => {
        if (e.key === "Enter" || e.key === " ") { e.preventDefault(); choisir(p, false); }
        else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
          e.preventDefault();
          const k = parts.indexOf(p) + (e.key === "ArrowDown" ? 1 : -1);
          if (parts[k]) choisir(parts[k], true);
        }
      });
      frise.append(g);
    });
    // Légende.
    const legende = el("div", "frise-legende");
    for (const [k, nom] of SEGMENTS) {
      const l = el("span", "");
      l.append(el("i", "pastille seg-" + k), document.createTextNode(nom));
      legende.append(l);
    }
    const cadre = el("div", "frise-cadre");
    cadre.append(frise);
    zoneH.replaceChildren(resume, cadre, detail, legende);
    choisir(parts.find((p) => p.number === S.partieChoisie), false);
    if (premiere) {
      const n = frise.querySelector('.noeud[data-partie="' + S.partieChoisie + '"]');
      if (n) zoneH.scrollTop = Math.max(0, n.getBoundingClientRect().top - zoneH.getBoundingClientRect().top + zoneH.scrollTop - 90);
    } else zoneH.scrollTop = defile;
  }

  function partie(p, main) {
    // Qui avait la main pendant cette part : l'auteur pour les parts impaires.
    const qui = p.number % 2 === 1 ? "author" : "agent";
    const d = el("div", "part " + qui);
    d.append(el("span", "qui", qui === "agent" ? "A" : "V"));
    const nom = qui === "agent" ? "L'agent" : "Vous";
    let ligne;
    if (!p.actor) ligne = nom + (qui === "agent" ? " a la main" : " avez la main") + " (en cours)";
    else if (p.reclaimed) ligne = "Main reprise par vous " + quand(new Date(p.time));
    else ligne = nom + (qui === "agent" ? " a remis son tour " : " avez remis votre tour ") + quand(new Date(p.time));
    const q = el("span", "quand", ligne);
    if (p.time) q.dataset.infobulle = new Date(p.time).toLocaleString("fr-FR");
    d.append(q);
    if (p.note) {
      const n = el("span", "note", "« " + court(p.note, 160) + " »");
      if (p.note.length > 160) n.dataset.infobulle = court(p.note, 600);
      d.append(n);
    }
    const fait = el("span", "fait", bilan(p.operations) || (p.actor ? "Rien d'écrit." : "Rien d'écrit pour l'instant."));
    fait.dataset.infobulle = "Une suggestion est une réécriture enregistrée d'un coup : elle peut créer plusieurs propositions.";
    d.append(fait);
    d.append(GloseArchives.afficher(p.operations));
    return d;
  }

  async function chargerProgression() {
    const r = await api("/api/progress");
    S.progression = r.ok ? r.data.sections || {} : {};
    if (r.ok && r.data.error) avis("Progression de la relecture : " + r.data.error, true);
  }

  // Marque une section (titre du sommaire) : à relire (""), en cours, relue.
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
    const n = S.cartes.filter((c) => !c.cache).length;
    $("etat").textContent = S.doc.files.length + " fichier" + (S.doc.files.length > 1 ? "s" : "") + " · " +
      n + " annotation" + (n > 1 ? "s" : "") + " · rendu en " + S.doc.millis + " ms";
  }

  // ---------- Annotations ----------
  // Une note de bas de page ajoutée ou supprimée tient en deux propositions
  // liées (appel et définition) : une seule carte, celle de l'appel.
  function principale(a) {
    if (!a.partners || !a.partners.length || a.primary) return -1;
    return S.doc.annotations.findIndex((b) => b.file === a.file && b.primary && a.partners.includes(b.start));
  }

  // Plage de la source dont les mots sont visibles dans la vue courante, pour
  // une annotation : le passage commenté ; l'ancien texte d'une proposition
  // (Relecture, Actuel) ou son nouveau texte (Proposé).
  function plageVisible(a) { return GloseDonnees.plageVisible(a, S.vue); }

  // Mots d'un fichier qui chevauchent [d, f) (une sélection peut commencer au milieu d'un mot).
  function motsDe(fichier, d, f) {
    const mots = S.parFichier.get(fichier) || [];
    const out = [];
    let k = premierApres(mots, d);
    if (k > 0 && mots[k - 1].e > d) k--;
    for (let j = k; j < mots.length && mots[j].s < f; j++) out.push(mots[j]);
    return out;
  }

  // Place des lignes ajoutées (bloc) à côté du mot ancre : après la ligne de
  // tableau ou l'élément de liste qui le contient (dans une ligne ou un
  // élément à part), sinon après son bloc ; avant, si l'ancre est le mot qui
  // suit l'ajout.
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

  // Texte ajouté par une proposition, montré en Relecture. Des lignes
  // entières (ligne de tableau, élément de liste, paragraphe) forment un bloc,
  // montré en source après la structure où elles entrent.
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
    S.cartes = [];
    S.doc.annotations.forEach((a, i) => {
      // Fichier non affiché (P6.8) : rien dans le texte ni la marge ; la
      // liste des annotations la garde, un clic ouvre son fichier.
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
      // Ancre : après le dernier mot visé, sinon après le mot qui précède l'annotation.
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
      // En Relecture, le texte ajouté s'insère à sa place, puis le numéro.
      const ins = S.vue === "relecture" && (a.kind === "addition" || a.kind === "substitution") ? insertion(a, i) : null;
      if (!ancre && ins) {
        // Aucun mot dans la vue Actuel (fichier vide, ajout seul) : l'ajout
        // se montre au début de son fichier.
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
          // Garde l'ordre des numéros quand plusieurs annotations partagent un mot.
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

  function fabriquerCarte(a, i) {
    const c = el("div", "carte" + (PROPOSITION[a.kind] ? " prop" : ""));
    c.tabIndex = 0;
    c.setAttribute("role", "button");
    const lieu = S.doc.files[a.file] ? S.doc.files[a.file].path : "?";
    c.setAttribute("aria-label", "Annotation " + (i + 1) + ", " + NOMS[a.kind] + ", " + lieu + " ligne " + a.line);
    const tete = el("div", "tete");
    const nom = a.primary && a.note ? (a.kind === "deletion" ? "Note supprimée" : a.kind === "addition" ? "Note ajoutée" : "Note modifiée") : NOMS[a.kind];
    tete.append(el("span", "num", String(i + 1)), el("span", "", nom));
    // À qui est la proposition : c'est elle que décident Accepter et Rejeter,
    // quoi que disent les réponses du fil.
    if (PROPOSITION[a.kind] && QUI[a.by]) tete.append(el("span", "qui", a.by === "author" ? "Vous" : a.by === "agent" ? "Agent" : QUI[a.by]));
    if (a.status === "new") tete.append(el("span", "nouveau", "ce tour"));
    tete.append(el("span", "espace"), el("span", "", "l. " + a.line));
    tete.dataset.infobulle = lieu + ", ligne " + a.line;
    c.append(tete);
    const ex = el("div", "extrait");
    if (a.kind === "substitution") {
      ex.append(md("del", "", court(a.text, 120)), document.createTextNode(" → "), md("ins", "", court(a.new, 120)));
    } else if (a.kind === "addition") {
      ex.append(md("ins", "", court(a.text, 160)));
    } else if (a.kind === "deletion") {
      ex.append(md("del", "", court(a.text, 160)));
    } else if (a.kind === "highlight") {
      ex.textContent = "« " + court(a.text, 160) + " »";
    }
    if (ex.childNodes.length && !(a.primary && a.note && a.kind !== "substitution")) c.append(ex);
    if (a.primary && a.note) {
      const n = el("div", "extrait note");
      n.append(el("span", "qui", "Note"), document.createTextNode(court(a.note, 300)));
      c.append(n);
    }
    for (const m of a.messages) {
      if (m.fix) { c.append(blocCorrection(a, m)); continue; }
      const p = el("p", "msg" + (m.by === "agent" ? " agent" : ""));
      if (QUI[m.by]) p.append(el("span", "qui", QUI[m.by]));
      p.append(document.createTextNode(m.text));
      c.append(p);
    }
    const outils = el("div", "outils");
    const bouton = (libelle, titre, classe, action, bulle) => {
      const b = el("button", "icone " + classe);
      b.innerHTML = icone(classe); // constante de ce fichier, jamais du texte du document
      b.type = "button";
      b.dataset.infobulle = bulle || libelle;
      b.setAttribute("aria-description", titre);
      b.setAttribute("aria-label", libelle);
      b.addEventListener("click", (e) => { e.stopPropagation(); action(); });
      outils.append(b);
    };
    bouton("Répondre", "Répondre dans le fil de cette annotation", "repondre", () => repondre(a, i), "Répondre (touche R)");
    if (a.kind === "comment" || a.kind === "highlight") {
      if (a.status === "new") bouton("Retirer", "Retirer cette annotation, qui n'a pas encore été remise à l'agent", "retirer", () => retirer(a));
      else bouton("Résoudre", "Clore la discussion : les marques disparaissent du fichier, le fil est gardé au journal", "resoudre", () => resoudre(a));
    } else {
      if (corrigeable(a)) bouton("Corriger", "Corriger votre proposition : elle reste une seule proposition, avec son fil", "corriger", () => corriger(a, i), "Corriger (touche E)");
      // Ce que décide chaque bouton, sans ambiguïté : la proposition de la
      // carte (la vôtre ou celle de l'agent), jamais un texte d'une réponse.
      const de = a.by === "author" ? "votre proposition" : a.by === "agent" ? "la proposition de l'agent" : "la proposition";
      // Un extrait court ; pour un long texte, sa taille, pour juger de
      // l'ampleur du changement sans tout lire.
      const q = (t) => {
        const plat = t.replace(/\s+/g, " ").trim();
        if (plat.length <= 60) return "« " + plat + " »";
        const mots = plat.split(" ").length;
        const phrases = (plat.match(/[.!?…](\s|$)/g) || []).length;
        return "« " + court(plat, 45) + " » (" + (phrases > 1 ? phrases + " phrases, " : "") + mots + " mots)";
      };
      const accepte = a.kind === "substitution" ? "Le texte devient " + q(a.new) : a.kind === "addition" ? "Ajoute " + q(a.text) : "Supprime " + q(a.text);
      const rejete = a.kind === "addition" ? "N'ajoute rien" : "Garde " + q(a.text);
      bouton("Rejeter", "Rejeter " + de + " : le texte d'origine reste", "rejeter", () => decider(a, "reject"), "Rejeter " + de + " (touche X)\n" + rejete);
      bouton("Accepter", "Accepter " + de + " : le texte proposé remplace l'original", "accepter", () => decider(a, "accept"), "Accepter " + de + " (touche A)\n" + accepte);
    }
    c.append(outils);
    c.addEventListener("click", () => activer(i, true));
    c.addEventListener("keydown", (e) => {
      if (e.target === c && (e.key === "Enter" || e.key === " ")) { e.preventDefault(); activer(i, true); }
    });
    return c;
  }

  // ---------- Clavier (P4.2) ----------
  // J et K : annotation suivante, précédente. A, X, R, E : accepter, rejeter,
  // répondre, corriger, sur la carte active seulement (mise en avant). Renvoie
  // true si la touche a servi.
  function clavierAnnotation(k) {
    const ordre = S.cartes.map((c, i) => i).filter((i) => !S.cartes[i].cache);
    if (k === "j" || k === "k") {
      if (!ordre.length) { avis("Aucune annotation en attente."); return true; }
      const pos = ordre.indexOf(S.actif);
      const suiv = pos < 0 ? (k === "j" ? 0 : ordre.length - 1) : Math.min(ordre.length - 1, Math.max(0, pos + (k === "j" ? 1 : -1)));
      if (pos >= 0 && suiv === pos) { avis(k === "j" ? "C'est la dernière annotation." : "C'est la première annotation."); return true; }
      activer(ordre[suiv], true, true);
      return true;
    }
    if (S.occupe) return true; // une écriture est en cours : pas de seconde décision sur une carte périmée
    const c = S.cartes[S.actif];
    if (!c || c.cache) { avis("Choisissez d'abord une annotation : J (suivante) ou K (précédente)."); return true; }
    const a = c.a;
    if (S.doc && S.doc.state && S.doc.state.hand === "agent") { avis("L'agent a la main : vous ne pouvez pas écrire avant qu'il vous la rende.", true); return true; }
    if (k === "r") { repondre(a, S.actif); return true; }
    if (k === "e") {
      if (corrigeable(a)) corriger(a, S.actif);
      else avis(PROPOSITION[a.kind] ? "Seules vos propositions se corrigent ; répondez (R) pour demander une correction à l'agent." : "E corrige une proposition : cette annotation est une discussion.", true);
      return true;
    }
    if (!PROPOSITION[a.kind]) { avis((k === "a" ? "A accepte" : "X rejette") + " une proposition : cette annotation est une discussion (R pour répondre).", true); return true; }
    decider(a, k === "a" ? "accept" : "reject");
    return true;
  }

  // clavier : la carte est choisie au clavier (J, K) ; elle le reste après le
  // rechargement qui suit une décision.
  function activer(i, defiler, clavier) {
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
    c.carte.classList.add("active");
    if (S.clavier) c.carte.classList.add("clavier");
    for (const x of zone().querySelectorAll('[data-a="' + i + '"]')) x.classList.add("g-actif");
    if (c.badge) {
      c.badge.classList.add("g-actif");
      if (defiler) c.badge.scrollIntoView({ block: "center", behavior: douceur() });
    }
  }

  // Place les cartes en face de leur ancre, sans chevauchement. Toutes les
  // mesures sont lues avant toute écriture : une seule mise en page.
  function disposerMaintenant() {
    const marge = $("marge");
    if (!S.cartes.length) { marge.style.minHeight = ""; return; }
    if (getComputedStyle(S.cartes[0].carte).position !== "absolute") { marge.style.minHeight = ""; return; }
    const base = marge.getBoundingClientRect().top;
    const liste = S.cartes.map((c) => ({
      c, y: c.badge ? c.badge.getBoundingClientRect().top - base - 6 : Infinity, h: c.carte.offsetHeight,
    }));
    liste.sort((x, y) => x.y - y.y);
    let bas = 0;
    for (const x of liste) {
      x.top = Math.max(x.y === Infinity ? bas : x.y, bas);
      bas = x.top + x.h + 6;
    }
    for (const x of liste) x.c.carte.style.top = x.top + "px";
    marge.style.minHeight = bas + "px";
  }

  function disposer() {
    if (S.attente) return;
    S.attente = requestAnimationFrame(() => { S.attente = 0; disposerMaintenant(); });
  }

  // ---------- Position de lecture ----------
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

  // Fait défiler la page pour que el soit à haut pixels du haut de l'écran.
  // Un fichier jamais affiché n'a qu'une hauteur estimée (content-visibility) :
  // scrollBy peut buter au bas du document et laisser el loin sous le pli. On
  // passe alors par scrollIntoView, qui laisse le document grandir, et on ajuste.
  function amener(el, haut) {
    window.scrollBy(0, el.getBoundingClientRect().top - haut);
    if (Math.abs(el.getBoundingClientRect().top - haut) > 2) {
      el.scrollIntoView({ block: "start", behavior: "instant" });
      window.scrollBy(0, el.getBoundingClientRect().top - haut);
    }
  }

  // ---------- Sélection → octets de la source ----------
  // Borne d'une sélection du navigateur (positions UTF-16) en octets de la
  // source. « ajuste » signale une borne déplacée vers le mot connu le plus
  // proche : l'extrait affiché est alors recalculé, rien n'est élargi en silence.
  function borne(noeud, offset, debut) {
    const parent = noeud.nodeType === Node.TEXT_NODE ? noeud.parentElement : null;
    if (parent && parent.dataset.g !== undefined) {
      const g = lireG(parent.dataset.g);
      const t = noeud.data;
      if (g) {
        if (offset <= 0) return { f: g.f, o: g.s, ajuste: false };
        if (offset >= t.length) return { f: g.f, o: g.e, ajuste: false };
        if (g.t) return { f: g.f, o: debut ? g.s : g.e, ajuste: true }; // mot transformé : ses bornes seulement
        // Au milieu d'une paire de substitution (emoji) : au bord du caractère,
        // vers l'extérieur ; ajusterGrapheme fait le reste.
        const c = t.charCodeAt(offset - 1);
        const milieu = c >= 0xd800 && c <= 0xdbff;
        if (milieu) offset += debut ? -1 : 1;
        return { f: g.f, o: g.s + enc.encode(t.slice(0, offset)).length, ajuste: milieu };
      }
    }
    // Ailleurs (numéro, renvoi calculé, espace sans position) : mot connu le plus proche, vers l'intérieur.
    const r = document.createRange();
    r.setStart(noeud, offset);
    const n = S.spans.length;
    let lo = 0, hi = n;
    if (debut) {
      while (lo < hi) { const m = (lo + hi) >> 1; if (r.comparePoint(S.spans[m], 0) >= 0) hi = m; else lo = m + 1; }
      if (lo >= n) return null;
      const g = lireG(S.spans[lo].dataset.g);
      return g && { f: g.f, o: g.s, ajuste: true, span: S.spans[lo] };
    }
    while (lo < hi) {
      const m = (lo + hi) >> 1;
      const sp = S.spans[m];
      if (r.comparePoint(sp, sp.childNodes.length) <= 0) lo = m + 1; else hi = m;
    }
    if (lo === 0) return null;
    const g = lireG(S.spans[lo - 1].dataset.g);
    return g && { f: g.f, o: g.e, ajuste: true, span: S.spans[lo - 1] };
  }

  // Ramène une borne (octet o du fichier f) au bord du caractère tel que le
  // lecteur le voit : un emoji et ses modificateurs, un drapeau, une lettre
  // et son accent ne se coupent pas (pandoc en fait parfois plusieurs mots).
  // Vers l'extérieur de la sélection : en arrière pour un début, en avant
  // pour une fin. Renvoie la borne, éventuellement déplacée.
  const segmenteur = window.Intl && Intl.Segmenter ? new Intl.Segmenter("fr", { granularity: "grapheme" }) : null;
  function ajusterGrapheme(f, o, debut) {
    if (!segmenteur) return o;
    const mots = S.parFichier.get(f) || [];
    let k = premierApres(mots, o);
    if (k > 0 && mots[k - 1].e > o) k--; // le mot qui contient o
    // Les mots contigus autour de o, sans mot transformé (ses octets diffèrent de son texte).
    let a = Math.max(0, k - 1), b = Math.min(mots.length - 1, k);
    while (a > 0 && mots[a - 1].e === mots[a].s && k - a < 6) a--;
    while (b + 1 < mots.length && mots[b].e === mots[b + 1].s && b - k < 6) b++;
    let texte = "", octet = mots[a] ? mots[a].s : o;
    const bornes = new Map(); // indice UTF-16 → octet
    for (let j = a; j <= b; j++) {
      if (j > a && mots[j - 1].e !== mots[j].s) { texte = ""; bornes.clear(); octet = mots[j].s; }
      if (/,t$/.test(mots[j].el.dataset.g)) return o;
      for (const c of mots[j].el.textContent) { bornes.set(texte.length, octet); texte += c; octet += enc.encode(c).length; }
      bornes.set(texte.length, octet);
    }
    const debuts = new Set([...segmenteur.segment(texte)].map((x) => bornes.get(x.index)));
    debuts.add(bornes.get(texte.length));
    if (debuts.has(o) || ![...bornes.values()].includes(o)) return o;
    const liste = [...debuts].filter((x) => x !== undefined).sort((x, y) => x - y);
    if (debut) return liste.filter((x) => x <= o).pop() ?? o;
    return liste.find((x) => x >= o) ?? o;
  }

  function selectionDuTexte() {
    const sel = window.getSelection();
    if (!sel || !sel.rangeCount || sel.isCollapsed) return null;
    const r = sel.getRangeAt(0);
    return zone().contains(r.commonAncestorContainer) ? r : null;
  }

  function majBoutonCommenter() {
    const b = $("menu-selection");
    const r = selectionDuTexte();
    if (!r || $("saisie").open || $("edition").open) { b.hidden = true; return; }
    const rect = r.getBoundingClientRect();
    b.style.left = (rect.left + window.scrollX + Math.min(rect.width, 40)) + "px";
    b.style.top = (rect.bottom + window.scrollY + 6) + "px";
    b.hidden = false;
  }

  function blocDe(noeud) {
    const e = noeud.nodeType === Node.ELEMENT_NODE ? noeud : noeud.parentElement;
    return e && e.closest(BLOCS);
  }

  // La sélection courante, convertie en octets de la source ; null (avec un
  // avis) si elle ne correspond à aucun texte de la source.
  function lireSelection() {
    const r = selectionDuTexte();
    if (!r) return null;
    const debut = borne(r.startContainer, r.startOffset, true);
    let fin = borne(r.endContainer, r.endOffset, false);
    // Triple-clic : la sélection déborde jusqu'au début du bloc suivant ;
    // on la ramène à la fin du bloc de départ.
    const blocDebut = blocDe(r.startContainer);
    if (fin && blocDebut && blocDe(r.endContainer) !== blocDebut && r.endOffset === 0) {
      const mots = blocDebut.querySelectorAll("span[data-g]");
      const g = mots.length && lireG(mots[mots.length - 1].dataset.g);
      if (g) fin = { f: g.f, o: g.e, ajuste: true };
    }
    for (const [x, d] of [[debut, true], [fin, false]]) {
      if (!x) continue;
      const o = ajusterGrapheme(x.f, x.o, d);
      if (o !== x.o) { x.o = o; x.ajuste = true; }
    }
    if (!debut || !fin || (debut.f === fin.f && debut.o >= fin.o)) {
      avis("Ce passage ne correspond à aucun texte de la source (numéro, renvoi calculé…). Utilisez « + » ou « ✎ » dans la marge.", true);
      return null;
    }
    if (debut.f !== fin.f) { avis("La sélection s'étend sur deux fichiers : sélectionnez un passage plus court.", true); return null; }
    const ajuste = debut.ajuste || fin.ajuste;
    const extrait = court(ajuste ? texteSource(debut.f, debut.o, fin.o) || r.toString() : r.toString(), 400);
    return { f: debut.f, start: debut.o, end: fin.o, extrait, ajuste };
  }

  // Une image ne se réécrit pas, mais se commente : la remarque va à la fin du
  // bloc qui la porte (sa légende, son paragraphe), ou, à défaut, à la fin du
  // texte qui la précède, en la nommant.
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
    const nom = (img.getAttribute("alt") || "").trim() || decoder((img.getAttribute("src") || "").split("?")[0].split("/").pop());
    // Le plus proche bloc qui l'entoure et contient du texte de la source (sa
    // légende, son paragraphe), sans quitter son fichier.
    let mot = null;
    for (let b = img.parentElement; b && b !== zone() && !b.classList.contains("glose-file"); b = b.parentElement) {
      mot = b.querySelector("span[data-g]");
      if (mot) break;
    }
    const prefixe = "Sur l'image « " + nom + " » : ";
    if (!mot) {
      // Le dernier mot du texte qui précède l'image dans son fichier, ou, si
      // l'image ouvre le fichier, le premier qui la suit : jamais un mot d'un
      // autre fichier (la remarque y serait écrite).
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
    const img = imageDeLaSelection();
    if (img && !texteDeLaSelection()) { commenterImage(img); return; }
    const x = lireSelection();
    if (!x) return;
    S.saisie = { mode: "passage", file: x.f, hash: S.hashes[x.f], start: x.start, end: x.end, extrait: x.extrait, prefixe: "" };
    ouvrirSaisie("Commenter un passage", x.extrait,
      x.ajuste ? "La sélection a été ramenée aux mots du texte source (sans numéros ni renvois calculés)." : "");
  }

  // Suggérer : réécrire le passage sélectionné ; la réécriture devient des
  // propositions mot à mot, avec un message facultatif.
  async function suggererSelection() {
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
      // Plusieurs paragraphes : leurs lignes entières, du premier mot au dernier.
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

  // Texte de la source compris dans la sélection (vide pour une image seule).
  function texteDeLaSelection() {
    const r = selectionDuTexte();
    if (!r) return "";
    for (const sp of S.spans) if (r.intersectsNode(sp) && sp.textContent.trim()) return sp.textContent;
    return "";
  }

  function commenterBloc(bloc) {
    if (!bloc || !zone().contains(bloc)) { fermerOutils(); return; }
    const img = S.imageSurvolee && bloc.contains(S.imageSurvolee) ? S.imageSurvolee : null;
    if (img) { fermerOutils(); commenterImage(img); return; }
    const mots = bloc.querySelectorAll("span[data-g]");
    // Une structure se commente à la fin de son dernier mot (sa dernière cellule).
    const g = mots.length && lireG(mots[bloc.matches(STRUCTURES) ? mots.length - 1 : 0].dataset.g);
    if (!g) { avis("Ce bloc (code, contenu généré) ne peut pas être annoté à l'écran.", true); return; }
    let t = "";
    for (const m of mots) t += m.textContent;
    const extrait = court(t, 200);
    S.saisie = { mode: "paragraph", file: g.f, hash: S.hashes[g.f], start: g.s, end: g.s, extrait, prefixe: "" };
    ouvrirSaisie("Commenter le paragraphe", extrait, "");
  }

  // ---------- Poignée de paragraphe ----------
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

  // ---------- Saisie ----------
  function ouvrirSaisie(titre, extrait, info, texte = "") {
    if (!brouillons.pret) { avis("Ouvrez Brouillons pour rétablir la sauvegarde avant de saisir.", true); return; }
    $("saisie-titre").textContent = titre;
    $("saisie-extrait").textContent = extrait;
    $("saisie-texte").value = texte;
    $("saisie-brouillon").textContent = S.saisie.brouillon ? "Brouillon récupéré." : "";
    message(info || "", true);
    const o = S.brouillonOrphelin;
    if (o && !$("saisie-texte").value && o.file === S.saisie.file) {
      // Une remarque dont le passage a changé (conflit) est reprise ici, dans
      // le même fichier ; le message rappelle le passage visé d'abord.
      $("saisie-texte").value = o.texte;
      S.brouillonOrphelin = null;
      message("Votre remarque précédente" + (o.extrait ? ", sur « " + court(o.extrait, 80) + " »," : "") +
        " n'a pas pu être enregistrée : elle est reprise ici. Effacez-la si ce n'est pas le bon passage.", true);
    }
    autre(null);
    $("menu-selection").hidden = true;
    $("saisie").showModal();
    viser(S.saisie);
    $("saisie-texte").focus();
  }

  function message(t, info) {
    const m = $("saisie-message");
    m.textContent = t;
    m.className = info ? "info" : "";
  }

  function autre(libelle, action) {
    const b = $("saisie-autre");
    b.hidden = !libelle;
    b.textContent = libelle || "";
    b.onclick = action || null;
  }

  // Ferme la saisie ; une remarque non enregistrée reste attachée à son passage.
  function fermerSaisie(enregistre) {
    if (S.envoi) return;
    if (!enregistre) garderBrouillon("saisie");
    if (S.saisie && S.saisie.brouillon) {
      S.saisie.brouillon.active = false;
      if (!enregistre) brouillons.sauver(S.saisie.brouillon);
    }
    if ($("saisie").open) $("saisie").close();
  }

  async function enregistrer() {
    const s = S.saisie;
    if (!s || S.envoi) return;
    if (s.envoiIncertain) { message("Un enregistrement a été tenté : vérifiez le texte avant de recommencer. Votre saisie reste dans Brouillons."); return; }
    const texte = $("saisie-texte").value.trim();
    if (!texte) { message("Écrivez une remarque."); return; }
    // L'empreinte est celle du texte affiché quand le passage a été choisi :
    // si le fichier a changé depuis, le serveur refuse (409) au lieu d'écrire ailleurs.
    const req = { file: s.file, hash: s.hash, mode: s.mode, start: s.start, end: s.end, text: s.prefixe + texte };
    S.envoi = true;
    S.occupe++;
    $("saisie-ok").disabled = true;
    $("saisie").querySelector(".corps").inert = true;
    const pret = await preparerBrouillon("saisie");
    const r = !pret ? { ok: false, status: 400, data: { error: "draft", message: "Brouillon non confirmé : vérifiez sa sauvegarde avant d'enregistrer." } } : s.mode === "reply"
      ? await api("/api/reply", { file: s.file, hash: s.hash, start: s.start, text: req.text })
      : await api("/api/comment", req);
    await apresEnvoiBrouillon(s, r);
    $("saisie").querySelector(".corps").inert = false;
    $("saisie-ok").disabled = false;
    S.envoi = false;
    try {
      if (r.ok) {
        S.annuler.push({ type: s.mode === "reply" ? "reponse" : "", file: req.file, start: r.data.start, hashApres: r.data.hash, req });
        S.retablir = [];
        fermerSaisie(true);
        avis((s.mode === "reply" ? "Réponse enregistrée dans " : "Commentaire enregistré dans ") + S.doc.files[req.file].path + ".");
        await charger(true);
        return;
      }
      const d = r.data;
      if (d.error === "turn") { message(d.message); return; }
      if (r.status === 409) {
        fermerSaisie(false);
        avis("Le fichier a changé sur le disque. Votre remarque est conservée : sélectionnez de nouveau le passage.", true);
        S.brouillonOrphelin = { texte, file: S.saisie.file, extrait: S.saisie.extrait };
        await charger(true);
        return;
      }
      if (d.error === "widen") {
        message(d.message, true);
        autre("Commenter le passage élargi", () => {
          s.start = d.start; s.end = d.end; s.extrait = court(d.excerpt, 400);
          $("saisie-extrait").textContent = s.extrait;
          autre(null);
          enregistrer();
        });
        return;
      }
      if (d.error === "fallback" && d.at !== undefined) {
        message("Impossible d'annoter ce passage : " + d.message + ".");
        autre("Commenter la fin du paragraphe, en citant l'extrait", () => {
          s.mode = "point"; s.start = s.end = d.at; s.prefixe = "« " + court(s.extrait, 200) + " » : ";
          autre(null);
          enregistrer();
        });
        return;
      }
      message(d.message || "L'enregistrement a échoué.");
    } finally {
      S.occupe--;
    }
  }

  async function retirer(a) {
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

  function repondre(a, i) {
    const resume = a.kind === "comment" ? a.text : a.kind === "substitution" ? a.text + " → " + a.new : a.text;
    S.saisie = { mode: "reply", file: a.file, hash: S.hashes[a.file], start: a.start, end: a.start, extrait: court(resume, 300), prefixe: "", annotationID: a.id || "" };
    ouvrirSaisie("Répondre à l'annotation " + (i + 1), court(resume, 300), "");
  }

  // Une correction de l'agent jointe à votre proposition : ce qu'elle devrait
  // proposer. Vous pouvez l'accepter telle quelle, ou la reprendre dans votre
  // proposition pour la retoucher ; sinon, elle reste un avis dans le fil.
  function blocCorrection(a, m) {
    const texte = m.text === "∅" ? "" : m.text;
    const b = el("div", "correction");
    const tete = el("div", "titre");
    tete.innerHTML = icone("corriger");
    tete.append(el("span", "", "Correction proposée par l'agent"));
    b.append(tete);
    const v = el("div", "version");
    if (a.kind === "substitution" || a.kind === "deletion") {
      v.append(md("del", "", court(a.text, 160)), document.createTextNode(" → "));
    }
    v.append(texte ? md("ins", "", court(texte, 200)) : el("em", "", "supprimer le passage"));
    b.append(v);
    if (a.by === "author") {
      const actions = el("div", "actions");
      const reprendre = el("button", "", "Reprendre");
      reprendre.type = "button";
      reprendre.dataset.infobulle = "Votre proposition prend ce texte, sans être acceptée : vous pourrez encore la retoucher";
      reprendre.addEventListener("click", (e) => { e.stopPropagation(); appliquerCorrection(a, m, false); });
      const accepter = el("button", "accepter", "Accepter cette version");
      accepter.type = "button";
      accepter.dataset.infobulle = "Le texte devient celui de l'agent : votre proposition est corrigée puis acceptée";
      accepter.addEventListener("click", (e) => { e.stopPropagation(); appliquerCorrection(a, m, true); });
      actions.append(reprendre, accepter);
      b.append(actions);
    }
    return b;
  }

  async function appliquerCorrection(a, m, accepter) {
    if (S.ecriture) { avis("Une écriture est en cours : réessayez dans un instant."); return; }
    S.ecriture = true;
    S.occupe++;
    try {
      const req = { file: a.file, hash: S.hashes[a.file], start: a.start, message: m.start, accept: accepter };
      const r = await api("/api/fix", req);
      if (r.ok && accepter) {
        // Annulable comme une décision (le serveur garde l'état d'avant).
        // Rétablir rejoue la même requête (« fix ») sur l'état d'avant.
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

  // Ses propres propositions, encore en attente, se corrigent (P3.2) ; pas
  // celles de l'agent, ni une note ajoutée ou supprimée (appel et définition).
  function corrigeable(a) {
    return PROPOSITION[a.kind] && (a.by === "author" || (!a.id && !a.by)) && !(a.partners && a.partners.length);
  }

  // Le texte que la proposition met à la place de l'original.
  function textePropose(a) {
    return a.kind === "substitution" ? a.new : a.kind === "addition" ? a.text : "";
  }

  function corriger(a, i) {
    const info = a.kind === "addition" ? "Texte ajouté par votre proposition." :
      "Texte qui remplacera « " + court(a.text, 120) + " »" + " ; laissez vide pour proposer de le supprimer.";
    ouvrirEdition("Corriger la proposition " + (i + 1), info, {
      mode: "revise", file: a.file, hash: S.hashes[a.file], at: a.start, start: a.textStart, end: a.textEnd,
      annotation: a.start, annotationID: a.id || "", kind: a.kind, ancien: a.kind === "addition" ? "" : a.text, original: textePropose(a),
    });
  }

  // Une décision de l'auteur s'annule (Ctrl+Z) et se rétablit (Ctrl+Y) : le
  // serveur garde l'état du fichier d'avant la décision.
  function retenirDecision(r, req) {
    if (!r.ok) return;
    S.annuler.push({ type: "decision", file: req.file, hashApres: r.data.hash, req });
    S.retablir = [];
    majBoutons();
  }

  async function decider(a, decision) {
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

  // Décisions en lot (F12) : toutes les propositions d'un fichier.
  async function deciderTout(f, decision, n) {
    if (S.ecriture) { avis("Une écriture est en cours : réessayez dans un instant."); return; }
    const verbe = decision === "accept" ? "Accepter" : "Rejeter";
    const accepter = decision === "accept";
    if (!(await boite({
      titre: verbe + " " + (n > 1 ? "les " + n + " propositions" : "la proposition") + " ?",
      message: (accepter ? "Le texte proposé remplacera l'original" : "Le texte d'origine restera") + " dans " + S.doc.files[f].path +
        ". Chaque décision est gardée au journal.",
      icone: accepter ? "tout-accepter" : "rejeter", ton: accepter ? "succes" : "danger",
      ok: (accepter ? "Accepter" : "Rejeter") + " " + (n > 1 ? "les " + n : "la proposition"),
    }))) return;
    S.ecriture = true;
    S.occupe++;
    try {
      const req = { file: f, hash: S.hashes[f], start: 0, end: Number.MAX_SAFE_INTEGER, decision };
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

  // ---------- Navette : à qui est la main, remise du tour ----------
  function majNavette() {
    const st = S.doc.state;
    const zone = $("navette");
    zone.textContent = "";
    if (!st) return;
    const agent = st.hand === "agent";
    document.body.classList.toggle("main-agent", agent);
    $("bandeau-agent").hidden = !agent;
    zone.className = agent ? "agent" : "";
    zone.append(el("strong", "", "Tour " + (st.round || st.turn) + (agent ? " · réponse de l'agent" : " · votre main")));
    if (st.since) {
      const d = new Date(st.since);
      zone.dataset.infobulle = (st.reclaimed ? "Main reprise le " : "Remis par " + (st.lastActor === "agent" ? "l'agent" : "vous") + " le ") +
        d.toLocaleString("fr-FR") + (st.note ? " : « " + st.note + " »" : "");
      zone.append(el("small", "", (st.reclaimed ? "Main reprise " : (st.lastActor === "agent" ? "L'agent a remis son tour " : "Vous avez remis votre tour ")) +
        quand(d) + (st.note ? " : « " + court(st.note, 90) + " »" : ".")));
    } else {
      zone.dataset.infobulle = "Aucun tour remis pour l'instant.";
      zone.append(el("small", "", "Commentez et suggérez, puis remettez la main à l'agent."));
    }
    if (st.problems && st.problems.length) {
      const b = el("span", "alerte", "⚠ " + st.problems.length);
      b.dataset.infobulle = st.problems.join("\n");
      zone.append(document.createTextNode(" "), b);
    }
    const bt = $("remettre");
    bt.hidden = false;
    $("remettre-texte").textContent = agent ? "Reprendre la main" : "Remettre à l'agent";
    $("remettre-icone").dataset.icone = agent ? "reprendre" : "remettre";
    if (window.GloseIcones) GloseIcones.remplir($("remettre"));
    bt.dataset.infobulle = agent ? "L'agent n'a pas remis son tour : reprendre la main (ses modifications deviennent des propositions)"
      : "Remettre votre tour à l'agent, avec une note facultative";
    if (agent && S.dernierEtat !== "agent" && S.dernierEtat !== undefined) avis("Tour remis à l'agent.");
    if (!agent && S.dernierEtat === "agent") avis("L'agent vous a remis la main" + (st.note ? " : « " + court(st.note, 120) + " »" : "."));
    S.dernierEtat = st.hand;
  }

  function ouvrirRemise() {
    const st = S.doc && S.doc.state;
    if (!st) return;
    const agent = st.hand === "agent";
    S.remise = { reprise: agent };
    $("remise-titre").textContent = agent ? "Reprendre la main" : "Remettre la main à l'agent";
    const nouv = (st.statuses && st.statuses["new"]) || 0;
    $("remise-info").textContent = agent
      ? "L'agent n'a pas remis son tour. Ses modifications des fichiers seront converties en propositions, comme par « glose done », puis la main vous reviendra."
      : (nouv > 1 ? nouv + " annotations de ce tour seront remises. " : nouv === 1 ? "L'annotation de ce tour sera remise. " : "") +
        "L'agent pourra répondre et proposer ; vous ne pourrez plus écrire jusqu'à sa remise, sauf à reprendre la main.";
    $("remise-note").value = "";
    $("remise-message").textContent = "";
    $("remise-force").hidden = true;
    $("remise-ok").textContent = agent ? "Reprendre la main" : "Remettre";
    $("remise").showModal();
    $("remise-note").focus();
  }

  async function remettre(force) {
    if (!S.remise || S.envoi) return;
    S.envoi = true;
    S.occupe++;
    $("remise-ok").disabled = true;
    const r = await api(S.remise.reprise ? "/api/reclaim" : "/api/handoff", { note: $("remise-note").value.trim(), force: !!force });
    $("remise-ok").disabled = false;
    S.envoi = false;
    try {
      if (r.ok) {
        $("remise").close();
        const d = r.data;
        let m = S.remise.reprise ? "Vous avez repris la main." : "Tour remis à l'agent.";
        if (d.converted) m += " " + d.converted + " proposition" + (d.converted > 1 ? "s" : "") + " tirée" + (d.converted > 1 ? "s" : "") + " des modifications de l'agent.";
        S.annuler = []; S.retablir = []; majBoutons();
        S.dernierEtat = undefined;
        avis(m);
        await charger(true);
        return;
      }
      const zone = $("remise-message");
      zone.textContent = r.data.message || "La remise a échoué.";
      if (r.data.problems) {
        const ul = el("ul");
        for (const p of r.data.problems) ul.append(el("li", "", p));
        zone.append(ul);
        if (S.remise.reprise) {
          $("remise-force").hidden = false;
          zone.append(el("p", "", "Vous pouvez reprendre la main sans convertir : les modifications de l'agent resteront dans les fichiers, sans propositions."));
        }
      }
    } finally {
      S.occupe--;
    }
  }

  // ---------- Modifier un paragraphe (propositions) ----------
  async function modifierBloc(bloc) {
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

  // Ajouter un élément avant ou après un bloc (paragraphe, titre, structure,
  // image) : le panneau s'ouvre vide, avec le choix du type d'élément ; le
  // nouvel élément sera une seule proposition d'ajout.
  function ajouter(bloc, ou) {
    if (!bloc || !zone().contains(bloc)) return;
    // Une image seule ou une figure : on vise le bloc qui la porte.
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

  // Squelettes des types d'élément à ajouter.
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
    // Le squelette remplace un texte encore vide, sinon il s'ajoute au curseur.
    if (!t.value.trim()) { t.value = ""; t.setSelectionRange(0, 0); } else { t.setSelectionRange(t.value.length, t.value.length); }
    insererSource(sq, "", true);
  }

  // Une structure (tableau, liste, encadré) se réécrit dans sa source, par
  // lignes entières, de son premier mot à son dernier : chaque ligne ajoutée,
  // supprimée ou modifiée devient une proposition.
  async function modifierStructure(st) {
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
    S.edition = contexte;
    $("edition-titre").textContent = titre;
    $("edition-info").textContent = info;
    $("edition-texte").value = recupere ? recupere.text : contexte.original;
    S.modeEdition = "source";
    const prefere = recupere ? recupere.editor : localStorage.getItem("glose-editeur") || "mef";
    $("edition-note").value = recupere ? recupere.note : "";
    $("edition-brouillon").textContent = recupere ? "Brouillon récupéré." : "";
    $("edition-message").textContent = "";
    // D-32 : une seule proposition, choix retenu d'une fois sur l'autre ;
    // sans objet pour la correction de sa propre proposition.
    $("edition-tenant").checked = recupere ? recupere.whole : localStorage.getItem("glose-tenant") === "1";
    $("edition-tenant-case").hidden = contexte.mode === "revise";
    $("menu-selection").hidden = true;
    $("edition").showModal();
    if (prefere !== "mef" || !modeEdition("mef", true)) modeEdition("source", true);
    $("edition-changements").textContent = "";
    $("edition-changements").append(el("li", "vide", "Aucune pour l'instant : modifiez le texte."));
    elargissements(contexte);
    $("edition-types").hidden = !(contexte.mode === "insert" && !contexte.tight);
    for (const b of $("edition-types").querySelectorAll("button")) b.setAttribute("aria-pressed", String(b.dataset.type === "paragraphe"));
    viser(contexte);
    if (recupere) changementsBientot();
  }

  // Un élément de liste, une cellule, un paragraphe d'encadré : la structure
  // qui l'entoure peut être réécrite en entière (ajouter une ligne, un élément).
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

  // Ce qui est réécrit ou commenté reste visible et marqué, à gauche du
  // panneau : le passage choisi, ou le paragraphe entier.
  function viser(x) {
    for (const v of document.querySelectorAll("#texte .vise")) v.classList.remove("vise");
    for (const v of document.querySelectorAll("#texte .vise-mot")) v.classList.remove("vise-mot");
    if (!x) return;
    if (x.mode === "insert" && x.element) {
      x.element.classList.add("vise");
      const r = x.element.getBoundingClientRect();
      if (r.top < 90 || r.top > window.innerHeight * 0.55) amener(x.element, 140);
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
    if (r.top < 90 || r.top > window.innerHeight * 0.55) amener(cible, 140);
  }

  // Trois modes : « Mis en forme » (on écrit dans le texte rendu ; le
  // Markdown est réécrit à l'identique hors des changements, visuel.js),
  // « Source » (le Markdown), « Aperçu » (le résultat et la liste des
  // propositions, sans rien écrire).
  function texteEdition() {
    if (S.modeEdition === "mef") return GloseVisuel.ecrire($("edition-visuel"), S.optionsMef);
    return $("edition-texte").value;
  }

  // Passe au mode m ; renvoie false si le mode n'est pas possible.
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

  // Insère du Markdown dans la source, à la place de la sélection ; un bloc
  // (tableau, liste) est séparé du texte voisin par des lignes vides.
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

  // Barre d'outils de l'éditeur (modes Mis en forme et Source).
  // Demande l'adresse d'un lien, ou le texte d'une note.
  const demanderLien = () => boite({ titre: "Insérer un lien", icone: "lien", ton: "info", ok: "Insérer",
    champs: [{ nom: "url", libelle: "Adresse", valeur: "https://", placeholder: "https://…" }],
    valider: (x) => {
      const u = x.url.trim();
      if (!/^\S+$/.test(u) || u === "https://") return "Indiquez une adresse, sans espace.";
      // Un lien vers le web, un courriel, une ancre ou un chemin relatif ;
      // jamais un schéma qui exécuterait quelque chose.
      if (/^(javascript|data|vbscript|file):/i.test(u)) return "Ce type d'adresse n'est pas permis.";
      return "";
    } });
  const demanderNote = () => boite({ titre: "Insérer une note de bas de page", message: "L'appel se place à l'endroit du curseur.",
    icone: "note", ton: "info", ok: "Insérer", champs: [{ nom: "texte", libelle: "Texte de la note", type: "textarea" }],
    valider: (x) => (x.texte.trim() ? "" : "Écrivez le texte de la note.") });

  async function outilMef(cmd) {
    if (cmd === "tableau" || cmd === "liste") {
      // Une structure ne s'écrit que dans la source.
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
        // La boîte prend le focus : la sélection du texte est gardée, puis rendue.
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
    // La boîte prend le focus : la sélection de l'éditeur est gardée, puis rendue.
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

  // La liste des propositions qui seront créées, tenue à jour pendant la
  // frappe ; avec rendu, l'aperçu du passage aussi.
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
    if (n !== numeroChangements || S.edition !== x) return false; // une frappe plus récente
    if (!r.ok) { if (rendu) $("edition-message").textContent = r.data.message || "L'aperçu a échoué."; return false; }
    if (rendu) {
      // Comme le texte : analysé dans un modèle inerte, les images reçoivent
      // le jeton avant d'être chargées (sinon refusées, donc cassées).
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

  async function enregistrerEdition() {
    const x = S.edition;
    if (!x || S.envoi) return;
    if (x.envoiIncertain) { $("edition-message").textContent = "Un enregistrement a été tenté : vérifiez le texte avant de recommencer. Votre saisie reste dans Brouillons."; return; }
    const texte = texteEdition();
    if (texte === x.original) { $("edition-message").textContent = "Aucune modification."; return; }
    const correction = x.mode === "revise";
    $("edition-message").className = "";
    const req = correction
      ? { file: x.file, hash: x.hash, start: x.annotation, text: texte, note: $("edition-note").value.trim() }
      : { file: x.file, hash: x.hash, mode: x.mode, at: x.at, to: x.to || 0, where: x.where || "", tight: !!x.tight,
        start: x.start, end: x.end, text: texte, note: $("edition-note").value.trim(), whole: $("edition-tenant").checked };
    S.envoi = true;
    S.occupe++;
    $("edition-ok").disabled = true;
    $("edition").querySelector(".corps").inert = true;
    const pret = await preparerBrouillon("edition");
    const r = pret ? await api(correction ? "/api/revise" : "/api/edit", req)
      : { ok: false, status: 400, data: { message: "Brouillon non confirmé : vérifiez sa sauvegarde avant d'enregistrer." } };
    await apresEnvoiBrouillon(x, r);
    $("edition").querySelector(".corps").inert = false;
    $("edition-ok").disabled = false;
    S.envoi = false;
    try {
      if (r.ok) {
        S.annuler.push(correction
          ? { type: "correction", file: req.file, start: r.data.start, hashApres: r.data.hash, avant: x.original, apres: texte }
          : { type: "edition", file: req.file, start: r.data.start, end: r.data.end, hashApres: r.data.hash, req });
        S.retablir = [];
        S.edition = null;
        $("edition").close();
        avis(correction ? "Proposition corrigée." + (req.note ? " Votre message est dans son fil." : "")
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

  // ---------- Réglages d'affichage ----------
  const POLICES = {
    serif: '"Sitka Text", "Iowan Old Style", Cambria, Georgia, serif',
    georgia: 'Georgia, "Times New Roman", serif',
    sans: 'system-ui, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif',
    lisible: 'Verdana, Tahoma, "DejaVu Sans", sans-serif',
    mono: 'ui-monospace, "Cascadia Code", "DejaVu Sans Mono", Consolas, monospace',
  };
  const DEFAUT = { font: "serif", size: 17, lineHeight: 1.6, justify: false, theme: "system", textWidth: 44 };

  function appliquerReglages(r) {
    S.reglages = r;
    const st = document.documentElement.style;
    st.setProperty("--police", POLICES[r.font] || POLICES.serif);
    st.setProperty("--taille", r.size + "px");
    st.setProperty("--interligne", String(r.lineHeight));
    st.setProperty("--alignement", r.justify ? "justify" : "left");
    st.setProperty("--texte", r.textWidth + "rem");
    document.documentElement.dataset.theme = r.theme;
    $("r-police").value = r.font;
    $("r-taille").value = r.size; $("r-taille-val").textContent = r.size + " px";
    $("r-interligne").value = r.lineHeight; $("r-interligne-val").textContent = Number(r.lineHeight).toFixed(2);
    $("r-largeur").value = r.textWidth; $("r-largeur-val").textContent = r.textWidth + " rem";
    $("r-justifie").checked = r.justify;
    for (const b of document.querySelectorAll('input[name="r-theme"]')) b.checked = b.value === r.theme;
    disposer();
  }

  let minuterieReglages = 0;
  function changerReglages() {
    const theme = document.querySelector('input[name="r-theme"]:checked');
    const r = {
      font: $("r-police").value, size: +$("r-taille").value, lineHeight: +$("r-interligne").value,
      justify: $("r-justifie").checked, theme: theme ? theme.value : "system", textWidth: +$("r-largeur").value,
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

  // ---------- Annuler, rétablir ----------
  // Chaque entrée garde l'empreinte du fichier juste après (annuler) ou juste
  // avant (rétablir) l'opération : si le fichier a changé entre-temps, le
  // serveur refuse au lieu d'agir sur un autre passage.
  function majBoutons() {
    $("annuler").disabled = !S.annuler.length;
    $("retablir").disabled = !S.retablir.length;
  }

  async function annulerDernier() {
    if (S.ecriture) { avis("Une écriture est en cours : réessayez dans un instant."); return; }
    const x = S.annuler.pop();
    if (!x) return;
    S.ecriture = true;
    S.occupe++;
    try {
      const r = x.type === "decision"
        ? await api("/api/undecide", { file: x.file, hash: x.hashApres })
        : x.type === "edition"
        ? await api("/api/decide", { file: x.file, hash: x.hashApres, start: x.start, end: x.end, decision: "reject" })
        : x.type === "correction"
          ? await api("/api/revise", { file: x.file, hash: x.hashApres, start: x.start, text: x.avant, note: "" })
          : await api("/api/withdraw", { file: x.file, hash: x.hashApres, start: x.start });
      if (r.ok) {
        if (x.type === "correction" || x.type === "decision") x.hashAvant = r.data.hash;
        S.retablir.push(x);
        avis(x.type === "decision" ? "Décision annulée : la proposition ou la discussion est de retour." :
          x.type === "edition" ? "Modification annulée." : x.type === "correction" ? "Correction annulée." :
          x.type === "reponse" ? "Réponse annulée." : "Commentaire annulé.");
      } else if (r.status === 0) {
        S.annuler.push(x); // erreur passagère : on pourra réessayer
        avis("Impossible d'annuler : " + r.data.message, true);
      } else if (x.type === "correction" || x.type === "decision") {
        avis("Impossible d'annuler : " + (r.data.message || "le fichier a changé depuis."), true);
      } else {
        avis("Impossible d'annuler : le fichier a changé depuis ce commentaire. Retirez-le depuis sa carte.", true);
      }
      majBoutons();
      await charger(true);
    } finally {
      S.occupe--;
      S.ecriture = false;
    }
  }

  async function retablirDernier() {
    if (S.ecriture) { avis("Une écriture est en cours : réessayez dans un instant."); return; }
    const x = S.retablir.pop();
    if (!x) return;
    S.ecriture = true;
    S.occupe++;
    try {
      const r = x.type === "decision"
        ? x.fix
          ? await api("/api/fix", Object.assign({}, x.fix, { hash: x.hashAvant }))
          : await api("/api/decide", Object.assign({}, x.req, { hash: x.hashAvant }))
        : x.type === "reponse"
        ? await api("/api/reply", { file: x.req.file, hash: x.req.hash, start: x.req.start, text: x.req.text })
        : x.type === "correction"
          ? await api("/api/revise", { file: x.file, hash: x.hashAvant, start: x.start, text: x.apres, note: "" })
          : await api(x.type === "edition" ? "/api/edit" : "/api/comment", x.req);
      if (r.ok) {
        S.annuler.push(x.type === "correction" || x.type === "decision" ? Object.assign({}, x, { start: x.type === "decision" ? x.start : r.data.start, hashApres: r.data.hash })
          : { type: x.type, file: x.file, start: r.data.start, end: r.data.end, hashApres: r.data.hash, req: x.req });
        avis(x.type === "decision" ? "Décision rétablie." : x.type === "edition" ? "Modification rétablie." :
          x.type === "correction" ? "Correction rétablie." : "Commentaire rétabli.");
      } else if (r.status === 0) {
        S.retablir.push(x);
        avis("Impossible de rétablir : " + r.data.message, true);
      } else if (x.type === "edition" || x.type === "correction" || x.type === "decision") {
        avis("Impossible de rétablir : le fichier a changé depuis.", true);
      } else {
        S.brouillonOrphelin = { texte: x.req.text, file: x.req.file, extrait: "" };
        avis("Impossible de rétablir : le fichier a changé. Le texte de la remarque est conservé : sélectionnez de nouveau le passage.", true);
      }
      majBoutons();
      await charger(true);
    } finally {
      S.occupe--;
      S.ecriture = false;
    }
  }

  // ---------- Suivi du disque ----------
  async function surveiller() {
    if (S.occupe || document.hidden) return;
    if (S.echecs > 2 && (S.echecs++ % 5) !== 0) return; // après des échecs, moins souvent
    if (!S.doc) { // premier chargement raté : on réessaie
      if (await charger(false)) S.echecs = 0; else S.echecs++;
      return;
    }
    const r = await api("/api/hashes");
    if (!r.ok || S.occupe) { if (!r.ok) S.echecs++; return; }
    // Un fichier apparu ou disparu (chapitre créé par l'agent, profil modifié) change aussi la liste.
    const change = r.data.filesGen !== S.doc.filesGen || r.data.files.length !== S.doc.files.length ||
      r.data.files.some((f) => S.hashes[f.index] !== f.hash) || r.data.seq !== S.doc.seq;
    if (!change) { S.echecs = 0; return; }
    const main = S.doc.state && S.doc.state.hand;
    if (await charger(true)) {
      S.echecs = 0;
      // Un changement de main a son propre avis (majNavette).
      if (!S.doc.state || S.doc.state.hand === main) avis("Le texte a changé sur le disque : affichage mis à jour.");
    } else {
      S.echecs++;
    }
  }

  // ---------- Événements ----------
  function brancher() {
    $("bouton-brouillons").addEventListener("click", ouvrirBrouillons);
    $("brouillons-fermer").addEventListener("click", () => $("brouillons").close());
    $("saisie-texte").addEventListener("input", (e) => { if (!e.isComposing) garderBrouillon("saisie"); });
    $("saisie-texte").addEventListener("compositionend", () => garderBrouillon("saisie"));
    $("edition-note").addEventListener("input", () => garderBrouillon("edition"));
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
    $("menu-selection").addEventListener("mousedown", (e) => e.preventDefault()); // garde la sélection
    bc.addEventListener("click", commenterSelection);
    $("bouton-suggerer").addEventListener("click", suggererSelection);

    const bp = $("outils-bloc");
    // Un lien interne mène à sa cible dans le rendu affiché (l'autre rendu,
    // gardé caché, porte les mêmes identifiants).
    $("texte").addEventListener("click", (e) => {
      const a = e.target.closest && e.target.closest('a[href]');
      if (!a) return;
      const href = a.getAttribute("href");
      if (href.startsWith('#') || (!/^(?:[a-z][a-z0-9+.-]*:|\/\/)/i.test(href) && /\.md(?:#|$)/i.test(href))) {
        e.preventDefault();
        navigation.suivre(a).catch(() => avis("Le renvoi ne peut pas être suivi.", true));
      }
    });
    $("texte").addEventListener("mousemove", (e) => {
      if (bp.classList.contains("ouvert")) return; // le menu ouvert reste attaché à son paragraphe
      let bloc = e.target.closest && e.target.closest(BLOCS);
      // Une image : la poignée se met devant elle, et commente l'image.
      const img = e.target.tagName === "IMG" ? e.target : null;
      if (img) bloc = img.closest("p, figure, li, dd") || img;
      // Un tableau se commente et se réécrit en entier, pas cellule par cellule.
      else if (bloc && bloc.closest("table")) bloc = bloc.closest("table");
      if (!bloc || bloc === S.bloc || !zone().contains(bloc) || (!img && !bloc.querySelector("span[data-g]"))) return;
      S.bloc = bloc;
      S.imageSurvolee = img;
      const rb = bloc.getBoundingClientRect();
      const rt = $("texte").getBoundingClientRect();
      const ligne = parseFloat(getComputedStyle(bloc).lineHeight) || 24;
      bp.style.top = (rb.top + window.scrollY + Math.max(0, (ligne - 26) / 2) - 2) + "px";
      bp.style.left = (rb.left + window.scrollX - 38) + "px";
      if (bp.hidden) { bp.hidden = false; $("poignee").style.animation = "none"; void $("poignee").offsetWidth; $("poignee").style.animation = ""; }
    });
    // La poignée est hors de la zone du texte : aller du paragraphe à elle ne
    // doit pas la masquer. Elle disparaît quand la souris quitte les deux,
    // sauf si son menu est ouvert.
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
    for (const b of $("edition-types").querySelectorAll("button")) b.addEventListener("click", () => choisirType(b.dataset.type));
    $("menu-bloc").addEventListener("keydown", (e) => {
      const items = Array.from($("menu-bloc").querySelectorAll("button"));
      const k = items.indexOf(document.activeElement);
      // Barre horizontale : flèches gauche et droite (haut et bas aussi).
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
    $("edition").querySelector("form").addEventListener("submit", (e) => { e.preventDefault(); enregistrerEdition(); });
    // Fermer conserve la saisie ; « Abandonner » dans Brouillons la retire.
    const fermerEdition = async () => {
      const x = S.edition;
      // Pendant l'enregistrement, le panneau reste ouvert : un refus (conflit)
      // doit pouvoir y laisser le texte.
      if (S.envoi) { avis("Enregistrement en cours : le panneau se fermera de lui-même."); return; }
      garderBrouillon("edition");
      if (x && x.brouillon) { x.brouillon.active = false; brouillons.sauver(x.brouillon); }
      S.edition = null;
      $("edition").close();
    };
    $("edition-fermer").addEventListener("click", fermerEdition);
    $("edition").addEventListener("cancel", (e) => { e.preventDefault(); fermerEdition(); });
    // Un lien de l'éditeur ne s'ouvre pas : un clic quitterait Glose.
    $("edition-visuel").addEventListener("click", (e) => { if (e.target.closest("a")) e.preventDefault(); });
    $("edition-visuel").addEventListener("dragstart", (e) => { if (e.target.closest && e.target.closest("a")) e.preventDefault(); });
    $("edition-source").addEventListener("click", () => modeEdition("source"));
    $("edition").addEventListener("close", () => { S.edition = null; clearTimeout(minuterieChangements); viser(null); });
    $("saisie").addEventListener("close", () => viser(null));
    // Pendant une composition, le texte n'est pas encore celui de l'auteur :
    // la liste des propositions attend la fin de la composition.
    for (const id of ["edition-visuel", "edition-texte"]) {
      $(id).addEventListener("input", (e) => { if (!e.isComposing) { changementsBientot(); garderBrouillon("edition"); } });
      $(id).addEventListener("compositionend", () => { changementsBientot(); garderBrouillon("edition"); });
    }
    $("edition-mef").addEventListener("click", () => modeEdition("mef"));
    $("edition-voir").addEventListener("click", apercu);
    for (const b of $("edition-outils").querySelectorAll("button")) {
      b.addEventListener("mousedown", (e) => e.preventDefault()); // garde la sélection
      b.addEventListener("click", () => outilMef(b.dataset.cmd));
    }
    const zv = $("edition-visuel");
    // Une saisie par méthode d'entrée (IME : japonais, chinois, accents
    // morts…) se valide souvent par Entrée : pendant la composition, les
    // touches appartiennent à la méthode d'entrée.
    const compose = (e) => e.isComposing || e.keyCode === 229;
    zv.addEventListener("keydown", (e) => {
      if (compose(e)) return;
      if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); if (!e.repeat) enregistrerEdition(); return; }
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
        if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); if (!e.repeat) enregistrerEdition(); }
      });
    }
    $("bouton-reglages").addEventListener("click", () => {
      const p = $("reglages");
      p.hidden = !p.hidden;
      $("bouton-reglages").setAttribute("aria-expanded", String(!p.hidden));
      if (!p.hidden) $("r-police").focus();
    });
    // Échap ferme les réglages et rend le focus à leur bouton.
    $("reglages").addEventListener("keydown", (e) => {
      if (e.key !== "Escape") return;
      e.preventDefault();
      $("reglages").hidden = true;
      $("bouton-reglages").setAttribute("aria-expanded", "false");
      $("bouton-reglages").focus();
    });
    document.addEventListener("click", (e) => {
      // Le bouton contient une icône : un clic sur elle vient aussi du bouton.
      if (!$("reglages").hidden && !$("reglages").contains(e.target) && !$("bouton-reglages").contains(e.target)) {
        $("reglages").hidden = true;
        $("bouton-reglages").setAttribute("aria-expanded", "false");
      }
    });
    for (const id of ["r-police", "r-taille", "r-interligne", "r-largeur", "r-justifie"]) $(id).addEventListener("input", changerReglages);
    for (const b of document.querySelectorAll('input[name="r-theme"]')) b.addEventListener("change", changerReglages);
    $("r-defaut").addEventListener("click", () => { appliquerReglages(DEFAUT); enregistrerReglages(DEFAUT); });

    $("saisie").querySelector("form").addEventListener("submit", (e) => { e.preventDefault(); enregistrer(); });
    $("saisie-fermer").addEventListener("click", () => fermerSaisie(false));
    $("saisie").addEventListener("cancel", (e) => { e.preventDefault(); fermerSaisie(false); });
    $("saisie-texte").addEventListener("keydown", (e) => {
      if (compose(e)) return;
      if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); if (!e.repeat) enregistrer(); }
    });
    $("remettre").addEventListener("click", ouvrirRemise);
    $("bandeau-reprendre").addEventListener("click", ouvrirRemise);
    $("remise").querySelector("form").addEventListener("submit", (e) => { e.preventDefault(); remettre(false); });
    $("remise-force").addEventListener("click", () => remettre(true));
    $("remise-fermer").addEventListener("click", () => { S.remise = null; $("remise").close(); });
    $("remise-note").addEventListener("keydown", (e) => {
      if (compose(e)) return;
      if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); if (!e.repeat) remettre(false); }
    });
    $("annuler").addEventListener("click", annulerDernier);
    for (const b of $("vues").querySelectorAll("button")) b.addEventListener("click", () => changerVue(b.dataset.vue));
    majVues();
    $("retablir").addEventListener("click", retablirDernier);
    for (const b of $("onglets").querySelectorAll("[role=tab]")) {
      b.addEventListener("click", () => choisirOnglet(b.dataset.onglet));
    }
    // Flèches entre les onglets (motif ARIA des onglets).
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
    $("bouton-sommaire").addEventListener("click", () => {
      const c = document.getElementById("cote");
      c.hidden = !c.hidden;
      document.getElementById("app").classList.toggle("sans-colonne", c.hidden);
      appliquerGrille();
      $("bouton-sommaire").setAttribute("aria-expanded", String(!c.hidden));
      disposer();
    });
    document.addEventListener("keydown", (e) => {
      if (compose(e)) return;
      // Jamais pendant une saisie : champ, éditeur, panneau ou boîte ouverts.
      const actif = document.activeElement || document.body;
      if (document.getElementById("archive").open || document.getElementById("recherche").open || $("boite").open || $("brouillons").open || $("saisie").open || $("edition").open || $("remise").open || !$("reglages").hidden ||
        /^(TEXTAREA|INPUT|SELECT)$/.test(actif.tagName) || actif.isContentEditable) return;
      const k = e.key.toLowerCase();
      if ((e.ctrlKey || e.metaKey) && e.shiftKey && !e.altKey && k === "f") { e.preventDefault(); recherche.montrer(); return; }
      const seule = !e.ctrlKey && !e.metaKey && !e.altKey;
      // Un menu ouvert (sélection, « + ») prépare une annotation : A, X, R, E
      // n'y décident rien ; J et K y restent permis.
      const menu = !$("menu-selection").hidden || $("poignee").getAttribute("aria-expanded") === "true" || $("outils-bloc").contains(actif);
      if (seule && "axre".includes(k) && k.length === 1 && menu) return;
      if (seule && "jkaxre".includes(k) && k.length === 1) {
        if (clavierAnnotation(k)) e.preventDefault();
        return;
      }
      if ((e.ctrlKey || e.metaKey) && k === "z" && !e.shiftKey) { e.preventDefault(); annulerDernier(); }
      else if ((e.ctrlKey || e.metaKey) && (k === "y" || (k === "z" && e.shiftKey))) { e.preventDefault(); retablirDernier(); }
      else if (k === "c" && !e.ctrlKey && !e.metaKey && !e.altKey && selectionDuTexte()) { e.preventDefault(); commenterSelection(); }
      else if (k === "s" && !e.ctrlKey && !e.metaKey && !e.altKey && selectionDuTexte()) { e.preventDefault(); suggererSelection(); }
      else if (/^[123]$/.test(k) && !e.ctrlKey && !e.metaKey && !e.altKey) { e.preventDefault(); changerVue(["relecture", "actuel", "propose"][+k - 1]); }
      // Alt+1 à Alt+4 : les onglets de la colonne (par la touche physique :
      // Alt+chiffre donne un autre caractère sur certains claviers).
      else if (e.altKey && !e.ctrlKey && !e.metaKey && /^Digit[1-4]$/.test(e.code)) {
        e.preventDefault();
        choisirOnglet(["sommaire", "fichiers", "annotations", "navette"][+e.code.slice(5) - 1], true);
      }
    });
    new ResizeObserver(disposer).observe($("texte"));
    window.addEventListener("resize", () => { fermerOutils(); disposer(); });
    if (S.jeton) chargerReglages();
    setInterval(surveiller, 2000);
  }

  S.jeton = lireJeton();
  if (S.jeton) {
    // La feuille de style du projet est un fichier du projet : elle exige le jeton.
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
      // Sans cela, la page resterait sur « Chargement du texte… » sans rien dire.
      bandeau("L'affichage n'a pas pu se faire : " + (e && e.message ? e.message : e) + ". Rechargez la page.");
      $("texte").setAttribute("aria-busy", "false");
    });
  }
})();
