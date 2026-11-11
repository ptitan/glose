// Glose: historique panel, with explicit dependencies.
"use strict";
window.GloseHistorique = (() => {
  function creer({ $, S, api, avis, charger }) {
  function majBoutons() {
    $("annuler").disabled = S.vue === "depuis" || !S.annuler.length;
    $("retablir").disabled = S.vue === "depuis" || !S.retablir.length;
  }

  async function annulerDernier() {
    if (S.vue === "depuis") return;
    if (S.ecriture) { avis("Une écriture est en cours : réessayez dans un instant."); return; }
    const x = S.annuler.pop();
    if (!x) return;
    S.ecriture = true;
    S.occupe++;
    try {
      const r = x.type === "decision" || x.type === "directe"
        ? await api("/api/undecide", { file: x.file, hash: x.hashApres })
        : x.type === "edition"
        ? await api("/api/decide", { file: x.file, hash: x.hashApres, start: x.start, end: x.end, decision: "reject" })
        : x.type === "correction"
          ? await api("/api/revise", { file: x.file, hash: x.hashApres, start: x.start, text: x.avant, note: "" })
          : x.type === "modification"
            ? await api("/api/amend", { file: x.file, hash: x.hashApres, start: x.start, text: x.avant })
            : await api("/api/withdraw", { file: x.file, hash: x.hashApres, start: x.start });
      if (r.ok) {
        if (x.type === "correction" || x.type === "decision" || x.type === "modification" || x.type === "directe") x.hashAvant = r.data.hash;
        S.retablir.push(x);
        avis(x.type === "decision" ? "Décision annulée : la proposition ou la discussion est de retour." :
          x.type === "directe" ? "Modification directe annulée : le texte est revenu." :
          x.type === "edition" ? "Modification annulée." : x.type === "correction" ? "Modification de la proposition annulée." :
          x.type === "modification" ? (x.reponse ? "Modification de la réponse annulée." : "Modification du commentaire annulée.") :
          x.type === "reponse" ? "Réponse annulée." : "Commentaire annulé.");
      } else if (r.status === 0) {
        S.annuler.push(x); // transient error: retry will be possible
        avis("Impossible d'annuler : " + r.data.message, true);
      } else if (x.type === "correction" || x.type === "decision" || x.type === "modification" || x.type === "directe") {
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
    if (S.vue === "depuis") return;
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
        : x.type === "directe"
        ? await api("/api/edit", Object.assign({}, x.req, { hash: x.hashAvant }))
        : x.type === "reponse"
        ? await api("/api/reply", { file: x.req.file, hash: x.req.hash, start: x.req.start, text: x.req.text })
        : x.type === "correction"
          ? await api("/api/revise", { file: x.file, hash: x.hashAvant, start: x.start, text: x.apres, note: "" })
          : x.type === "modification"
            ? await api("/api/amend", { file: x.file, hash: x.hashAvant, start: x.start, text: x.apres })
            : await api(x.type === "edition" ? "/api/edit" : "/api/comment", x.req);
      if (r.ok) {
        S.annuler.push(x.type === "correction" || x.type === "decision" || x.type === "modification" || x.type === "directe" ? Object.assign({}, x, { start: x.type === "decision" ? x.start : r.data.start, hashApres: r.data.hash })
          : { type: x.type, file: x.file, start: r.data.start, end: r.data.end, hashApres: r.data.hash, req: x.req });
        avis(x.type === "decision" ? "Décision rétablie." : x.type === "directe" ? "Modification directe rétablie." : x.type === "edition" ? "Modification rétablie." :
          x.type === "correction" ? "Modification de la proposition rétablie." :
          x.type === "modification" ? (x.reponse ? "Modification de la réponse rétablie." : "Modification du commentaire rétablie.") : "Commentaire rétabli.");
      } else if (r.status === 0) {
        S.retablir.push(x);
        avis("Impossible de rétablir : " + r.data.message, true);
      } else if (x.type === "edition" || x.type === "correction" || x.type === "decision" || x.type === "modification" || x.type === "directe") {
        avis("Impossible de rétablir : " + (r.data.message || "le fichier a changé depuis."), true);
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


    return { majBoutons, annulerDernier, retablirDernier };
  }
  return { creer };
})();
