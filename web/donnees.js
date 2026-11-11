// Glose: pure interface functions, without DOM or state (P5.7). They
// read server data (positions, annotations, journal) and derive
// keys, counts and labels. Tested without a browser:
// node --test scripts/donnees.test.cjs
"use strict";
(() => {
  // data-g="fichier:début,fin[,t]": a word's position in source bytes.
  function lireG(g) {
    const m = /^(\d+):(\d+),(\d+)(,t)?$/.exec(g || "");
    return m ? { f: +m[1], s: +m[2], e: +m[3], t: !!m[4] } : null;
  }

  // Text reduced to one line, truncated to at most n characters.
  function court(t, n) {
    t = t.replace(/\s+/g, " ").trim();
    return t.length > n ? t.slice(0, n - 1) + "…" : t;
  }

  // Rendering signature: the file list generation, file paths
  // and hashes, and the last journal event. A retained rendering
  // is only useful if this signature has not changed.
  function signature(d) {
    return (d.filesGen || 0) + "|" + (d.part || "") + "|" + d.files.map((f) => f.path + ":" + f.hash).join(",") + "|" + d.seq;
  }

  // An annotation's identity for the interface: its identifier, or,
  // failing that, its position, type and text, so a neighbour arriving
  // at the same position is not mistaken for it.
  function cleAnnotation(a) {
    return a.id ? "id:" + a.id : a.file + ":" + a.start + ":" + a.kind + ":" + a.text + ":" + (a.new || "");
  }

  // First word in the list (sorted by s) whose s >= o.
  function premierApres(mots, o) {
    let lo = 0, hi = mots.length;
    while (lo < hi) { const m = (lo + hi) >> 1; if (mots[m].s >= o) hi = m; else lo = m + 1; }
    return lo;
  }

  // Source range whose words are visible in the view, for an
  // annotation: the commented passage; a proposal's old text
  // (Relecture, Actuel) or its new text (Proposé).
  function plageVisible(a, vue) {
    const propose = vue === "propose";
    switch (a.kind) {
      case "highlight": return [a.textStart, a.textEnd];
      case "deletion": return propose ? null : [a.textStart, a.textEnd];
      case "substitution": return propose ? [a.newStart, a.newEnd] : [a.textStart, a.textEnd];
      case "addition": return propose ? [a.textStart, a.textEnd] : null;
    }
    return null;
  }

  // What was done during a turn, by timeline ring category.
  const SEGMENTS = [
    ["discussion", "commentaires et réponses"],
    ["propositions", "suggestions"],
    ["acceptees", "acceptées"],
    ["rejetees", "rejetées"],
    ["resolues", "discussions résolues"],
    ["directes", "modifications directes"],
  ];

  function compter(ops) {
    const n = { discussion: 0, propositions: 0, acceptees: 0, rejetees: 0, resolues: 0, directes: 0 };
    for (const o of ops) {
      if (o.kind === "comment" || o.kind === "reply") n.discussion++;
      // A suggestion, correction, or converted agent handoff:
      // one action each, regardless of the number of marks created.
      else if (o.kind === "propose" || o.kind === "revise" || o.kind === "track") n.propositions++;
      // A direct change of the author (D-57): one action, whatever its words.
      else if (o.kind === "direct") n.directes++;
      for (const c of o.closed || []) {
        if (c.kind === "message" || c.decision === "direct") continue;
        if (c.decision === "accept") n.acceptees++;
        else if (c.decision === "reject") n.rejetees++;
        else if (c.decision === "resolve") n.resolues++;
      }
    }
    n.total = SEGMENTS.reduce((t, [k]) => t + n[k], 0);
    return n;
  }

  // Timeline node radius according to what was done.
  function rayon(n) {
    return 11 + Math.min(7, Math.sqrt(n.total) * 1.8);
  }

  // "2 comments, 1 proposal; decisions: 3 accepted, 1 rejected"
  function bilan(ops) {
    const n = {};
    const plus = (k, v) => { n[k] = (n[k] || 0) + (v === undefined ? 1 : v); };
    for (const o of ops) {
      if (o.kind === "track") n.converties = true;
      else if (o.kind !== "decide" && o.kind !== "resolve" && o.kind !== "identify") plus(o.kind);
      for (const c of o.closed || []) if (c.kind !== "message" && c.decision !== "direct") plus("d-" + c.decision);
    }
    const mots = (k, un, plusieurs) => (n[k] ? n[k] + " " + (n[k] > 1 ? plusieurs : un) : "");
    const faits = [mots("comment", "commentaire", "commentaires"), mots("reply", "réponse", "réponses"),
      mots("propose", "suggestion", "suggestions"), n.converties ? "modifications du texte converties en propositions" : "",
      mots("direct", "modification directe", "modifications directes"), mots("revise", "correction", "corrections"), mots("amend", "message modifié", "messages modifiés"), mots("withdraw", "retrait", "retraits"), mots("undo", "décision annulée", "décisions annulées")].filter(Boolean);
    const decisions = [mots("d-accept", "acceptée", "acceptées"), mots("d-reject", "rejetée", "rejetées"), mots("d-resolve", "résolue", "résolues")].filter(Boolean);
    return [faits.join(", "), decisions.length ? "décisions : " + decisions.join(", ") : ""].filter(Boolean).join(" ; ");
  }

  // "21:04" today, "06/11 21:04" on another day.
  function heure(t, maintenant = new Date()) {
    const d = new Date(t);
    const jour = d.toDateString() === maintenant.toDateString() ? "" : d.toLocaleDateString("fr-FR", { day: "2-digit", month: "2-digit" }) + " ";
    return jour + d.toLocaleTimeString("fr-FR", { hour: "2-digit", minute: "2-digit" });
  }

  // "12 min ago", "on 06/11 at 21:04"
  function quand(d, maintenant = Date.now()) {
    const min = Math.round((maintenant - d.getTime()) / 60000);
    if (min < 1) return "à l'instant";
    if (min < 60) return "il y a " + min + " min";
    if (min < 24 * 60) return "il y a " + Math.round(min / 60) + " h";
    return "le " + d.toLocaleDateString("fr-FR", { day: "2-digit", month: "2-digit" }) + " à " + d.toLocaleTimeString("fr-FR", { hour: "2-digit", minute: "2-digit" });
  }

  // The key of each heading of the review (F2 quinquies): its file and its
  // identifier, without the rendering prefix, which depends on the rank of the
  // file; its text if it has no identifier. Two headings of one file that come
  // to the same key would share one mark: from the second on, a rank is added.
  // The first keeps its key, so that the marks already made stay valid.
  function clesDesTitres(titres) {
    const cles = new Map(), vus = new Map();
    for (const h of titres) {
      const f = h.closest(".glose-file");
      const base = ((f && f.dataset.path) || "") + "#" + (h.id.replace(/^gl-f\d+-a-/, "") || h.textContent.trim());
      const n = (vus.get(base) || 0) + 1;
      vus.set(base, n);
      cles.set(h, n === 1 ? base : base + "~" + n);
    }
    return cles;
  }

  // Name of a model without its provider nor its variant.
  function modeleCourt(m) { return m ? String(m).split("/").pop().split("#")[0] : ""; }

  // Send headings of each displayed file to the server (P11.3). What is
  // sent says nothing about review progress, and can safely be repeated: a refusal
  // or lost response leaves the file unknown, sent again on the next call.
  // One send at a time, the next starting from what is wanted then.
  // envoyer(titres) returns a promise of { ok, data } (data.headings: the
  // recorded state, if supplied). The server refuses requests over 1 MiB:
  // files are batched, and a file that would not fit alone remains
  // unknown to the home page, without being resent on every display.
  function creerEnvoiTitres(envoyer, maxOctets = 900000) {
    let connus = {}, voulus = {}, enCours = false;
    const taille = (chemin, liste) => new TextEncoder().encode(JSON.stringify([chemin, liste])).length;
    const memes = (a, b) => !!a && !!b && a.length === b.length && a.every((x, i) => x === b[i]);
    async function pousser() {
      if (enCours) return;
      for (;;) {
        const changes = {};
        let octets = 0;
        for (const [chemin, liste] of Object.entries(voulus)) {
          if (memes(connus[chemin], liste)) { delete voulus[chemin]; continue; }
          const t = taille(chemin, liste);
          if (t > maxOctets) { connus[chemin] = liste; delete voulus[chemin]; continue; }
          if (octets + t > maxOctets) break;
          octets += t;
          changes[chemin] = liste;
          delete voulus[chemin];
        }
        if (!Object.keys(changes).length) return;
        enCours = true;
        let r = { ok: false };
        try { r = await envoyer(changes); } catch (e) { /* sent again on the next call */ }
        enCours = false;
        if (!r.ok) {
          for (const chemin of Object.keys(changes)) delete connus[chemin];
          voulus = {};
          return;
        }
        connus = { ...connus, ...changes, ...((r.data && r.data.headings) || {}) };
      }
    }
    return {
      connaitre(t) { connus = { ...(t || {}) }; },
      vouloir(t) { voulus = { ...voulus, ...t }; return pousser(); },
    };
  }

  // The bold and italics of a message, read as Markdown: a tree of strings and
  // { t: "strong" | "em" | "code", c: [...] }, never HTML. Only that: every other
  // character stays as it was written, and an unclosed mark stays a character.
  function emphase(texte) {
    const alnum = /[\p{L}\p{N}]/u;
    const blanc = (c) => c === undefined || /\s/.test(c);
    function fermeture(s, debut, lettre, long) {
      for (let j = debut; j < s.length; j++) {
        if (s[j] === "\\") { j++; continue; }
        if (s[j] === "\n" && s[j + 1] === "\n") return -1;
        if (s[j] === "`") {
          let n = 1;
          while (s[j + n] === "`") n++;
          const f = s.indexOf("`".repeat(n), j + n);
          if (f > 0) j = f + n - 1;
          continue;
        }
        if (s[j] !== lettre) continue;
        let n = 1;
        while (s[j + n] === lettre) n++;
        const juste = long === 1 ? n === 1 : n >= long;
        const apres = s[j + long];
        if (juste && !blanc(s[j - 1]) && j > debut && (lettre !== "_" && long !== 1 || apres === undefined || !alnum.test(apres))) return j + (n - long);
        j += n - 1;
      }
      return -1;
    }
    function lire(s) {
      const sortie = [];
      let texteCourant = "";
      const noeud = (t, c) => {
        if (texteCourant) sortie.push(texteCourant);
        texteCourant = "";
        sortie.push({ t, c });
      };
      for (let i = 0; i < s.length; i++) {
        const c = s[i];
        if (c === "\\" && /[*_`\\]/.test(s[i + 1] || "")) { texteCourant += s[++i]; continue; }
        if (c === "`") {
          let n = 1;
          while (s[i + n] === "`") n++;
          const f = s.indexOf("`".repeat(n), i + n);
          if (f > i + n - 1 && !s.slice(i, f).includes("\n\n")) { noeud("code", [s.slice(i + n, f)]); i = f + n - 1; continue; }
          texteCourant += s.slice(i, i + n); i += n - 1; continue;
        }
        if (c !== "*" && c !== "_") { texteCourant += c; continue; }
        let n = 1;
        while (s[i + n] === c) n++;
        const long = Math.min(n, 3);
        const avant = s[i - 1], apres = s[i + n];
        const ouvre = !blanc(apres) && ((c === "*" && long > 1) || avant === undefined || !alnum.test(avant));
        const f = ouvre ? fermeture(s, i + long, c, long) : -1;
        if (f < 0) { texteCourant += s.slice(i, i + n); i += n - 1; continue; }
        const interieur = lire(s.slice(i + long, f));
        if (long === 3) noeud("strong", [{ t: "em", c: interieur }]);
        else noeud(long === 2 ? "strong" : "em", interieur);
        i = f + long - 1;
      }
      if (texteCourant) sortie.push(texteCourant);
      return sortie;
    }
    return lire(String(texte || ""));
  }

  // The same text without its marks, for a short summary.
  function texteSimple(texte) {
    const plat = (ms) => ms.map((m) => (typeof m === "string" ? m : plat(m.c))).join("");
    return plat(emphase(texte));
  }

  // Who is expected to do what on a pending annotation, as the list shows it.
  // Replied: a message by the other party, whatever its flags; the last one speaks.
  function etatAnnotation(a) {
    const messages = a.messages || [];
    const dernier = messages[messages.length - 1];
    const repondu = messages.some((m) => m.by !== a.by);
    if (repondu && dernier) {
      const nom = dernier.by === "author" ? "vous" : dernier.via || "l'agent";
      const ton = dernier.by === "author" ? "auteur" : "agent";
      if (dernier.fix) return { libelle: "Corrigé par " + nom, ton, icone: "corriger" };
      return { libelle: "Répondu par " + nom, ton, icone: "repondre" };
    }
    if (a.by === "agent") {
      const proposition = ["substitution", "addition", "deletion"].includes(a.kind);
      return { libelle: proposition ? "À décider" : "À traiter", ton: "attente", icone: "horloge" };
    }
    if (a.status === "new") return { libelle: "À remettre", ton: "attente", icone: "remettre" };
    return { libelle: "Sans réponse", ton: "attente", icone: "horloge" };
  }

  // A decision of the journal (D-14: the author's), as the list shows it.
  const ETATS_DECISION = {
    accept: { libelle: "Accepté", ton: "accepte", icone: "accepter" },
    reject: { libelle: "Rejeté", ton: "rejete", icone: "rejeter" },
    resolve: { libelle: "Résolu", ton: "neutre", icone: "resoudre" },
    withdraw: { libelle: "Retiré", ton: "neutre", icone: "retirer" },
    direct: { libelle: "Imposé", ton: "auteur", icone: "corriger" },
  };

  // The annotations closed, most recent first, from what /api/log returns. The
  // journal never forgets: a decision undone stays in it, so an annotation still
  // in the text is not closed, and one decided twice counts by its last decision.
  function decisionsDuJournal(tours, ouvertes) {
    const liste = [];
    // The direct changes undone since (D-57) no longer count.
    const annulees = new Set();
    for (const p of tours || []) for (const op of p.operations || []) for (const id of op.reverted || []) annulees.add(id);
    for (const p of tours || []) {
      for (const op of p.operations || []) {
        for (const c of op.closed || []) {
          if (ETATS_DECISION[c.decision] && !(c.decision === "direct" && annulees.has(c.id))) liste.push({ c, op, round: p.round, etat: ETATS_DECISION[c.decision] });
        }
      }
    }
    const vues = new Set();
    return liste.reverse().filter(({ c }) => {
      const cle = c.file + "#" + (c.id || c.kind + ":" + c.text);
      if (vues.has(cle) || (c.id && ouvertes && ouvertes.has(c.id))) return false;
      vues.add(cle);
      return true;
    });
  }

  globalThis.GloseDonnees = { emphase, texteSimple, etatAnnotation, decisionsDuJournal, clesDesTitres, modeleCourt, creerEnvoiTitres, lireG, court, signature, cleAnnotation, premierApres, plageVisible, SEGMENTS, compter, rayon, bilan, heure, quand };
})();
