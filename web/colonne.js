// Glose: manuscript tabs and shuttle timeline.
"use strict";
window.GloseColonne = (() => {
  function creer({ $, S, lireJSON, el, icone, choisirPartie, zone, court, retenirTitres, marquerSection, PROPOSITION, NOMS, md, cleAnnotation, activer, deciderTout, api, compter, bilan, rayon, heure, quand }) {
  function arbre() {
    const nav = $("fichiers");
    const defile = nav.scrollTop;
    nav.textContent = "";
    if (!S.doc) return;
    S.dossiersOuverts = S.dossiersOuverts || new Set(lireJSON(localStorage, "glose-dossiers:" + (S.racine || ""), [], Array.isArray));
    S.dossiersFermes = S.dossiersFermes || new Set(); // manually collapsed: the displayed part does not reopen them
    const retenir = () => localStorage.setItem("glose-dossiers:" + (S.racine || ""), JSON.stringify([...S.dossiersOuverts]));
    const parFichier = new Map();
    for (const a of S.doc.annotations) if (!a.partners || !a.partners.length || a.primary) parFichier.set(a.file, (parFichier.get(a.file) || 0) + 1);
    const svgIcone = (nom, classe) => { // icone() returns a complete <svg>
      const t = document.createElement("template");
      t.innerHTML = icone(nom);
      const s = t.content.firstElementChild || el("span");
      if (classe) s.classList.add(classe);
      return s;
    };
    // One tree row: [chevron] icon name [count].
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
          // Open: retained, or containing the displayed part.
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
    if (S.doc.perFile && S.partie) {
      const fichiers = [...zone().querySelectorAll(".glose-file")];
      const titres = [...zone().querySelectorAll("h1, h2, h3")];
      const cles = GloseDonnees.clesDesTitres(titres);
      const relues = titres.filter((h) => {
        const e = S.progression && S.progression[cles.get(h)];
        return e && e.status === "relue";
      }).length;
      nav.append(el("p", "partie-affichee", "Seul « " + S.partie + " » est affiché : " + fichiers.length +
        " fichier(s) sur " + S.doc.files.length + ". Avancement connu : " + relues + " section(s) relue(s) sur " + titres.length + ", pour cette partie."));
      nav.append(el("p", "note", "La recherche porte sur tout le projet. Les annotations des autres fichiers restent comptées."));
    }
    nav.scrollTop = defile;
  }

  function sommaire() {
    listeAnnotations();
    arbre();
    if (S.onglet === "navette") historique();
    if (S.onglet === "annotations") chargerTraitees();
    const nav = $("sommaire");
    nav.textContent = "";
    const titres = Array.from(zone().querySelectorAll("h1, h2, h3"));
    const comptes = titres.map(() => 0);
    for (const c of S.cartes) {
      if (!c.badge) continue;
      // Last heading preceding the annotation.
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
    // Review progress: one state per heading, kept by the server
    // in .glose/progression.json (never in the text).
    const etats = S.progression || {};
    const cles = GloseDonnees.clesDesTitres(titres);
    const cle = (h) => cles.get(h);
    let relues = 0, enCours = 0;
    for (const h of titres) {
      const e = etats[cle(h)];
      if (e && e.status === "relue") relues++;
      else if (e && e.status === "en-cours") enCours++;
    }
    const pourcent = titres.length ? Math.round(100 * relues / titres.length) : 0;
    $("avancement").querySelector("span").textContent = titres.length ? pourcent + " % relu" : "Avancement inconnu";
    $("avancement").querySelector("progress").value = pourcent;
    $("avancement").querySelector("progress").hidden = !titres.length;
    $("avancement").dataset.infobulle = relues + " / " + titres.length + " sections affichées relues";
    retenirTitres(titres, cle);
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
      ligne._titre = h;
      nav.append(ligne);
    });
  }

  // ---------- Column tabs (P4.1) ----------
  // The displayed tab's name, below the icon row, with the number
  // of pending annotations for the Annotations tab.
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
    if (nom === "annotations" && S.doc) chargerTraitees();
  }

  // Annotation filters: status, author, type, file, turn. Kept for
  // the session; a value that no longer matches anything is forgotten.
  const FILTRES = [
    ["statut", "Statut", [["", "Tous"], ["new", "De ce tour"], ["open", "Remises, en attente"]], (a) => a.status || ""],
    ["auteur", "Auteur", [["", "Tous"], ["author", "Vous"], ["agent", "L'agent"]], (a) => (a.by === "agent" ? "agent" : "author")],
    ["type", "Type", [["", "Tous"], ["discussion", "Commentaires"], ["proposition", "Propositions"]],
      (a) => (PROPOSITION[a.kind] ? "proposition" : "discussion")],
  ];

  function pastille(etat) {
    const p = el("span", "etat " + etat.ton);
    p.innerHTML = icone(etat.icone); // Fixed icon name, never document content.
    p.append(document.createTextNode(etat.libelle));
    return p;
  }

  // The annotations closed, read in the journal: the text no longer has them.
  const TRAITEES_MAX = 30;
  async function chargerTraitees() {
    const gen = (S.genTraitees = (S.genTraitees || 0) + 1);
    const r = await api("/api/log");
    if (gen !== S.genTraitees || !r.ok) return;
    S.journalTours = r.data.turns;
    listeTraitees();
  }

  function listeTraitees() {
    const zoneT = $("annotations-traitees");
    zoneT.textContent = "";
    const F = S.filtres || {};
    const fichier = F.fichier ? S.doc.files[+F.fichier] && S.doc.files[+F.fichier].path : "";
    const ouvertes = new Set(S.doc.annotations.map((a) => a.id));
    const retenues = F.statut ? [] : GloseDonnees.decisionsDuJournal(S.journalTours, ouvertes).filter(({ c, round }) =>
      (!fichier || c.file === fichier) && (!F.tour || String(round) === F.tour) &&
      (!F.auteur || (c.by === "agent" ? "agent" : "author") === F.auteur) &&
      (!F.type || (PROPOSITION[c.kind] ? "proposition" : "discussion") === F.type));
    if (!retenues.length) return;
    zoneT.append(el("h3", "", "Traitées (" + retenues.length + ")"));
    const bloc = el("div", "annotations");
    for (const { c, op, round, etat } of retenues.slice(0, TRAITEES_MAX)) {
      const a = el("a");
      a.href = "#";
      const message = (c.messages || [])[0];
      const resumeA = c.kind === "substitution" ? c.text + " → " + c.new : message ? GloseDonnees.texteSimple(message.text) : c.text;
      a.append(el("span", "type", NOMS[c.kind] || c.kind), pastille(etat), el("span", "extrait", court(resumeA, 70)));
      a.append(el("span", "meta " + (c.by === "agent" ? "agent" : "auteur"), (c.by === "agent" ? "Agent" : "Auteur") + " · t" + round + " · " + c.file));
      a.dataset.infobulle = "Ouvrir l'archive de cette annotation";
      a.addEventListener("click", (e) => { e.preventDefault(); GloseArchives.ouvrir(c, op); });
      bloc.append(a);
    }
    zoneT.append(bloc);
    if (retenues.length > TRAITEES_MAX) zoneT.append(el("p", "note", (retenues.length - TRAITEES_MAX) + " plus ancienne(s) : voir l'onglet Navette."));
  }

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
    const entetes = new Map();
    let fichier = -1;
    for (const { c, i } of retenues) {
      if (c.a.file !== fichier) {
        fichier = c.a.file;
        const tete = el("div", "fichier lot");
        tete.append(el("span", "", S.doc.files[fichier].path));
        bloc.append(tete);
        entetes.set(fichier, tete);
      }
      const a = el("a");
      a.href = "#";
      const resumeA = c.a.primary && c.a.note ? "Note : " + c.a.note : c.a.kind === "comment" ? c.a.text :
        c.a.kind === "substitution" ? c.a.text + " → " + c.a.new : (c.a.comments[0] || c.a.text);
      // The words of a proposal are Markdown of the text; those of a comment, a message.
      const extrait = PROPOSITION[c.a.kind] ? md("span", "extrait", court(resumeA, 70)) : el("span", "extrait", court(GloseDonnees.texteSimple(resumeA), 70));
      a.append(el("span", "num" + (PROPOSITION[c.a.kind] ? " prop" : ""), String(i + 1)), el("span", "type", NOMS[c.a.kind]), pastille(GloseDonnees.etatAnnotation(c.a)), extrait);
      const meta = el("span", "meta " + (c.a.by === "agent" ? "agent" : "auteur"), (c.a.by === "agent" ? (c.a.via || "Agent") : "Auteur") + (tour(c.a) ? " · t" + tour(c.a) : ""));
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
    listeTraitees();
    // Batch decisions, file by file (F12): on all
    // pending proposals in the file, filtered or not.
    const parFichier = new Map();
    for (const c of S.cartes) if (PROPOSITION[c.a.kind] && !c.cache) parFichier.set(c.a.file, (parFichier.get(c.a.file) || 0) + 1);
    for (const [f, n] of parFichier) {
      if (n < 2) continue;
      const lot = entetes.get(f) || el("div", "fichier lot");
      if (!entetes.has(f)) lot.append(el("span", "", S.doc.files[f].path));
      lot.dataset.infobulle = n + " propositions en attente dans ce fichier" + (S.filtreMission ? ", pour la mission affichée" : " (tous filtres confondus)");
      for (const [libelle, decision] of [["Tout accepter", "accept"], ["Tout rejeter", "reject"]]) {
        const b = el("button", decision === "accept" ? "accepter" : "", libelle);
        b.type = "button";
        b.setAttribute("aria-label", libelle);
        b.dataset.infobulle = libelle;
        b.textContent = "";
        b.innerHTML = icone(decision === "accept" ? "accepter" : "rejeter");
        b.addEventListener("click", () => deciderTout(f, decision, n, "", S.filtreMission || ""));
        lot.append(b);
      }
      if (!entetes.has(f)) corps.append(lot);
    }
  }

  // Shuttle history as a timeline ("Navette" tab): two lanes,
  // you and the agent; each turn is a node in the lane of whoever
  // held it, each handoff an arrow to the other lane. A node's
  // ring shows what was done during that turn (SEGMENTS); clicking
  // a node gives its details. Everything is built with createElementNS and
  // textContent: no journal text is interpreted as HTML.
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

    // Summary: turns, decided proposals.
    const tot = compter(parts.flatMap((p) => p.operations));
    const resume = el("div", "frise-resume");
    const tours = parts[parts.length - 1].round;
    resume.append(el("strong", "", tours + " tour" + (tours > 1 ? "s" : "")),
      el("span", "", tot.acceptees + " acceptée" + (tot.acceptees > 1 ? "s" : "") + " · " + tot.rejetees + " rejetée" + (tot.rejetees > 1 ? "s" : "") +
        " · " + tot.discussion + " message" + (tot.discussion > 1 ? "s" : "")));
    // While the agent holds the hand, its progress sentence (D-59 § 7) shows at
    // the top of the turns, whatever part is selected; navette.js refreshes it.
    {
      const a = S.doc.state.agent;
      const texte = a && a.fresh && a.taken && a.progress ? a.progress + ' · ' + new Date(a.progressAt).toLocaleTimeString('fr-FR', {hour:'2-digit',minute:'2-digit'}) : '';
      const progression = el('span', 'tour-avancement', texte); progression.hidden = !texte; resume.append(progression);
    }
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

    // The timeline.
    // Fixed width, scaled to the column (viewBox).
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
    // Alternating turn bands.
    const parTour = new Map();
    parts.forEach((p, i) => { if (!parTour.has(p.round)) parTour.set(p.round, []); parTour.get(p.round).push(i); });
    for (const [n, idx] of parTour) {
      const y1 = y(idx[0]) - GAP / 2 + 6, y2 = y(idx[idx.length - 1]) + GAP / 2 - 6;
      frise.append(svg("rect", { x: 2, y: y1, width: W - 4, height: y2 - y1, rx: 10, class: "bande" + (n % 2 ? "" : " paire") }));
      const ym = (y1 + y2) / 2; // vertically, in the band's left margin
      frise.append(svg("text", { x: 13, y: ym, class: "tour", "text-anchor": "middle", transform: "rotate(-90 13 " + ym + ")" }, "Tour " + n));
    }
    // Lanes.
    for (const [qui, nom] of [["author", "Vous"], ["agent", "Agent"]]) {
      frise.append(svg("line", { x1: X[qui], y1: 22, x2: X[qui], y2: H - 8, class: "couloir " + qui }));
      frise.append(svg("text", { x: X[qui], y: 14, class: "couloir-nom", "text-anchor": "middle" }, nom));
    }
    // Handoffs: a curved arrow from one lane to the other.
    parts.forEach((p, i) => {
      if (i === parts.length - 1) return;
      const a = { x: X[p.qui], y: y(i) }, b = { x: X[parts[i + 1].qui], y: y(i + 1) };
      const ra = rayon(p.n), rb = rayon(parts[i + 1].n);
      const dir = Math.sign(b.x - a.x) || 1;
      const d = "M" + (a.x + dir * (ra + 2)) + "," + a.y + " C" + (a.x + dir * 60) + "," + a.y + " " + (b.x - dir * 60) + "," + b.y + " " + (b.x - dir * (rb + 3)) + "," + b.y;
      const chemin = svg("path", { d, class: "remise" + (p.reclaimed ? " reprise" : ""), "marker-end": "url(#frise-fleche)" });
      frise.append(chemin);
      // Handoff time and note, in the middle of the arrow.
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
    // Nodes.
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
    // Legend.
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
    // Who held this part of the turn: the author for odd-numbered parts.
    const qui = p.number % 2 === 1 ? "author" : "agent";
    const d = el("div", "part " + qui);
    const badge = el("span", "qui");
    badge.innerHTML = icone(qui === "agent" ? "agent" : "vous");
    d.append(badge);
    const nom = qui === "agent" ? "L'agent" : "Vous";
    let ligne;
    if (!p.actor) ligne = nom + (qui === "agent" ? " a la main" : " avez la main") + " (en cours)";
    else if (p.reclaimed) ligne = "Main reprise par vous " + quand(new Date(p.time));
    else ligne = nom + (qui === "agent" ? " a remis son tour " : " avez remis votre tour ") + quand(new Date(p.time));
    const q = el("span", "quand", ligne);
    if (p.time) q.dataset.infobulle = new Date(p.time).toLocaleString("fr-FR");
    d.append(q);
    if (!p.actor && qui === 'agent') {
      const a = S.doc.state.agent;
      if (a && a.fresh && a.taken && a.progress) q.textContent = a.name + ' travaille';
      const texte = a && a.fresh && a.taken && a.progress ? a.progress + ' · ' + new Date(a.progressAt).toLocaleTimeString('fr-FR', {hour:'2-digit',minute:'2-digit'}) : '';
      const progression = el('span', 'tour-avancement', texte); progression.hidden = !texte; d.append(progression);
    }
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


    return { arbre, sommaire, choisirOnglet, listeAnnotations, historique };
  }
  return { creer };
})();
