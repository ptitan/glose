// Glose : fonctions pures de l'interface, sans DOM ni état (P5.7). Elles
// lisent les données du serveur (positions, annotations, journal) et en
// tirent des clés, des comptes et des libellés. Testées sans navigateur :
// node --test scripts/donnees.test.cjs
"use strict";
(() => {
  // data-g="fichier:début,fin[,t]" : la place d'un mot en octets de la source.
  function lireG(g) {
    const m = /^(\d+):(\d+),(\d+)(,t)?$/.exec(g || "");
    return m ? { f: +m[1], s: +m[2], e: +m[3], t: !!m[4] } : null;
  }

  // Texte ramené à une ligne, coupé à n caractères au plus.
  function court(t, n) {
    t = t.replace(/\s+/g, " ").trim();
    return t.length > n ? t.slice(0, n - 1) + "…" : t;
  }

  // Signature d'un rendu : la génération de la liste des fichiers, leurs
  // chemins et empreintes, et le dernier événement du journal. Un rendu gardé
  // ne sert que si elle n'a pas changé.
  function signature(d) {
    return (d.filesGen || 0) + "|" + (d.part || "") + "|" + d.files.map((f) => f.path + ":" + f.hash).join(",") + "|" + d.seq;
  }

  // L'identité d'une annotation pour l'interface : son identifiant, ou à
  // défaut sa place, son type et son texte, pour qu'une voisine arrivée à la
  // même place ne passe pas pour elle.
  function cleAnnotation(a) {
    return a.id ? "id:" + a.id : a.file + ":" + a.start + ":" + a.kind + ":" + a.text + ":" + (a.new || "");
  }

  // Premier mot de la liste (triée par s) dont s >= o.
  function premierApres(mots, o) {
    let lo = 0, hi = mots.length;
    while (lo < hi) { const m = (lo + hi) >> 1; if (mots[m].s >= o) hi = m; else lo = m + 1; }
    return lo;
  }

  // Plage de la source dont les mots sont visibles dans la vue, pour une
  // annotation : le passage commenté ; l'ancien texte d'une proposition
  // (Relecture, Actuel) ou son nouveau texte (Proposé).
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

  // Ce qui a été fait pendant une main, par catégorie de l'anneau de la frise.
  const SEGMENTS = [
    ["discussion", "commentaires et réponses"],
    ["propositions", "suggestions"],
    ["acceptees", "acceptées"],
    ["rejetees", "rejetées"],
    ["resolues", "discussions résolues"],
  ];

  function compter(ops) {
    const n = { discussion: 0, propositions: 0, acceptees: 0, rejetees: 0, resolues: 0 };
    for (const o of ops) {
      if (o.kind === "comment" || o.kind === "reply") n.discussion++;
      // Une suggestion, une correction, une remise convertie de l'agent :
      // un geste chacune, quel que soit le nombre de marques qu'il a créées.
      else if (o.kind === "propose" || o.kind === "revise" || o.kind === "track") n.propositions++;
      for (const c of o.closed || []) {
        if (c.kind === "message") continue;
        if (c.decision === "accept") n.acceptees++;
        else if (c.decision === "reject") n.rejetees++;
        else if (c.decision === "resolve") n.resolues++;
      }
    }
    n.total = SEGMENTS.reduce((t, [k]) => t + n[k], 0);
    return n;
  }

  // Rayon d'un nœud de la frise selon ce qui a été fait.
  function rayon(n) {
    return 11 + Math.min(7, Math.sqrt(n.total) * 1.8);
  }

  // « 2 commentaires, 1 proposition ; décisions : 3 acceptées, 1 rejetée »
  function bilan(ops) {
    const n = {};
    const plus = (k, v) => { n[k] = (n[k] || 0) + (v === undefined ? 1 : v); };
    for (const o of ops) {
      if (o.kind === "track") n.converties = true;
      else if (o.kind !== "decide" && o.kind !== "resolve" && o.kind !== "identify") plus(o.kind);
      for (const c of o.closed || []) if (c.kind !== "message") plus("d-" + c.decision);
    }
    const mots = (k, un, plusieurs) => (n[k] ? n[k] + " " + (n[k] > 1 ? plusieurs : un) : "");
    const faits = [mots("comment", "commentaire", "commentaires"), mots("reply", "réponse", "réponses"),
      mots("propose", "suggestion", "suggestions"), n.converties ? "modifications du texte converties en propositions" : "",
      mots("revise", "correction", "corrections"), mots("withdraw", "retrait", "retraits"), mots("undo", "décision annulée", "décisions annulées")].filter(Boolean);
    const decisions = [mots("d-accept", "acceptée", "acceptées"), mots("d-reject", "rejetée", "rejetées"), mots("d-resolve", "résolue", "résolues")].filter(Boolean);
    return [faits.join(", "), decisions.length ? "décisions : " + decisions.join(", ") : ""].filter(Boolean).join(" ; ");
  }

  // « 21:04 » aujourd'hui, « 06/11 21:04 » un autre jour.
  function heure(t, maintenant = new Date()) {
    const d = new Date(t);
    const jour = d.toDateString() === maintenant.toDateString() ? "" : d.toLocaleDateString("fr-FR", { day: "2-digit", month: "2-digit" }) + " ";
    return jour + d.toLocaleTimeString("fr-FR", { hour: "2-digit", minute: "2-digit" });
  }

  // « il y a 12 min », « le 06/11 à 21:04 »
  function quand(d, maintenant = Date.now()) {
    const min = Math.round((maintenant - d.getTime()) / 60000);
    if (min < 1) return "à l'instant";
    if (min < 60) return "il y a " + min + " min";
    if (min < 24 * 60) return "il y a " + Math.round(min / 60) + " h";
    return "le " + d.toLocaleDateString("fr-FR", { day: "2-digit", month: "2-digit" }) + " à " + d.toLocaleTimeString("fr-FR", { hour: "2-digit", minute: "2-digit" });
  }

  globalThis.GloseDonnees = { lireG, court, signature, cleAnnotation, premierApres, plageVisible, SEGMENTS, compter, rayon, bilan, heure, quand };
})();
