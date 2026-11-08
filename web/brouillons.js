// Private drafts: one write queue per input, conditional versions
// on the server. The browser mirror only contains unconfirmed sends;
// the project disk is authoritative after confirmation, even if its port changes.
"use strict";
(() => {
  const copie = (v) => JSON.parse(JSON.stringify(v));
  const egal = (a, b) => JSON.stringify(a) === JSON.stringify(b);
  function creer({ api, stockage, identifiant, changement = () => {}, delai = 350 }) {
    const entrees = new Map();
    const session = identifiant(); // each tab owns its emergency mirrors
    let prefixe = "", pret = false, erreurStockage = "", problemes = [];
    const notifier = () => changement(Array.from(entrees.values()), erreurStockage);
    function miroir(e) {
      if (!prefixe || !stockage) return;
      try {
        const cle = prefixe + session + ":" + e.id;
        if (e.pending || e.busy) stockage.setItem(cle, JSON.stringify({ id: e.id, hash: e.hash, draft: e.draft }));
        else stockage.removeItem(cle);
      } catch (_) { erreurStockage = "Copie de secours du navigateur indisponible ; attendez la confirmation de sauvegarde."; }
    }
    function entree(id, hash, draft) {
      return { id, hash, draft: copie(draft), pending: false, busy: null, timer: 0, error: "", closed: false };
    }
    async function charger() {
      const p = await api("/api/ping");
      if (!p.ok) return false;
      prefixe = "glose-brouillon:" + encodeURIComponent(p.data.root) + ":";
      const r = await api("/api/drafts");
      if (!r.ok) return false;
      problemes = r.data.problems || [];
      // Do not replace objects held by an open panel or an ongoing save.
      const presents = new Set();
      for (const v of r.data.drafts) {
        presents.add(v.id);
        if (!entrees.has(v.id)) entrees.set(v.id, entree(v.id, v.hash, v.draft));
        else {
          const e = entrees.get(v.id);
          if (!e.pending && !e.busy && !e.active) { e.hash = v.hash; e.draft = copie(v.draft); }
        }
      }
      for (const [id, e] of entrees) if (!presents.has(id) && !e.pending && !e.busy && !e.active) entrees.delete(id);
      if (!pret && stockage) {
        try {
          const cles = Array.from({ length: stockage.length }, (_, i) => stockage.key(i));
          for (const k of cles) {
            if (!k || !k.startsWith(prefixe)) continue;
            let v;
            try { v = JSON.parse(stockage.getItem(k)); } catch (_) { continue; }
            if (!v || !/^[0-9a-f]{32}$/.test(v.id) || !v.draft || !v.draft.path) continue;
            const disque = entrees.get(v.id);
            if (disque && egal(disque.draft, v.draft)) { stockage.removeItem(k); continue; }
            // A mirror is recovered as a SEPARATE draft: it may be newer, or
            // have been submitted/deleted in another tab. Never replay it.
            const e = entree(identifiant(), "", v.draft);
            e.pending = true;
            // If there is no disk counterpart, a submission may have succeeded
            // and its draft been removed before this stale mirror was cleared.
            e.draft.sending = !!e.draft.sending;
            entrees.set(e.id, e);
            miroir(e);
            stockage.removeItem(k);
          }
        } catch (_) { erreurStockage = "Copie de secours du navigateur illisible ; les brouillons du projet restent accessibles."; }
      }
      pret = true;
      notifier();
      return true;
    }
    function nouveau(draft) {
      const e = entree(identifiant(), "", draft);
      entrees.set(e.id, e);
      modifier(e, draft);
      return e;
    }
    function modifier(e, draft) {
      if (e.closed || egal(e.draft, draft) && (e.pending || e.hash)) return;
      e.draft = copie(draft);
      e.pending = true;
      e.error = "";
      miroir(e);
      clearTimeout(e.timer);
      e.timer = setTimeout(() => sauver(e), delai);
      notifier();
    }
    async function sauver(e) {
      clearTimeout(e.timer);
      if (e.closed) return true;
      if (e.busy) { await e.busy; return e.pending && !e.error ? sauver(e) : !e.error; }
      if (!e.pending) return !e.error;
      const draft = copie(e.draft);
      e.pending = false;
      e.busy = (async () => {
        let r;
        try { r = await api("/api/drafts", { id: e.id, hash: e.hash, draft }); }
        catch (_) { r = { ok: false, data: { message: "Serveur injoignable : brouillon non confirmé." } }; }
        if (r.ok) { e.hash = r.data.hash; e.error = ""; }
        else { e.pending = true; e.error = r.data.message || "Brouillon non confirmé."; }
      })();
      miroir(e);
      notifier();
      await e.busy;
      e.busy = null;
      miroir(e);
      notifier();
      if (e.pending && !e.error) return sauver(e);
      return !e.error;
    }
    async function retirer(e) {
      if (e.closed) return true;
      if (!(await sauver(e))) return false;
      const r = await api("/api/drafts", { id: e.id, hash: e.hash, remove: true, draft: null });
      if (!r.ok) { e.error = r.data.message || "Le brouillon n'a pas pu être retiré."; notifier(); return false; }
      e.closed = true;
      clearTimeout(e.timer);
      entrees.delete(e.id);
      e.pending = false;
      miroir(e);
      notifier();
      return true;
    }
    return { charger, nouveau, modifier, sauver, retirer, entrees,
      get pret() { return pret; }, get problemes() { return problemes; } };
  }
  globalThis.GloseBrouillons = { creer };
})();
