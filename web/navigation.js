// Navigation locale, isolée de l'édition. Le serveur ne résout que des cibles
// du projet ; aucun texte n'est modifié et aucun chemin disque n'est accepté.
"use strict";
window.GloseNavigation = (() => {
  function creer({ api, courant, afficherPartie, avis, amener }) {
    let generation = 0;
    const fichier = (path) => [...courant().zone.querySelectorAll('.glose-file')].find((f) => f.dataset.path === path);
    async function ouvrir(dst, hash) {
      const gen = ++generation;
      if (hash) {
        const doc = courant().doc, f = doc.files.find((x) => x.path === dst.path);
        const r = await api('/api/hashes');
        if (gen !== generation) return false;
        if (!r.ok || !f || r.data.filesGen !== doc.filesGen || r.data.files.find((x) => x.index === f.index)?.hash !== hash) {
          avis('Le texte a changé ou ne peut pas être vérifié : relancez la recherche.', true); return false;
        }
      }
      let f = fichier(dst.path);
      if (!f) {
        if (!await afficherPartie(dst.path) || gen !== generation) return false;
        f = fichier(dst.path);
      }
      if (!f) { avis("Ce fichier ne peut pas être affiché.", true); return false; }
      if (hash && courant().doc.files.find((x) => x.path === dst.path)?.hash !== hash) {
        avis("Le texte a changé depuis la recherche : relancez-la.", true); return false;
      }
      let cible = dst.anchor ? f.querySelector('#' + CSS.escape(dst.anchor)) : f;
      if (dst.start !== undefined) {
        for (const n of courant().zone.querySelectorAll('.g-recherche')) n.classList.remove('g-recherche');
        const spans = [...f.querySelectorAll('span[data-g]')];
        const plages = spans.map((n) => ({ n, p: /^\d+:(\d+),(\d+)/.exec(n.dataset.g) })).filter((x) => x.p);
        const touches = plages.filter((x) => +x.p[1] < dst.end && +x.p[2] > dst.start);
        for (const x of touches) x.n.classList.add('g-recherche');
        cible = touches[0]?.n || plages.find((x) => +x.p[1] >= dst.start)?.n || f;
      }
      if (!cible) { avis("La cible de ce renvoi n'est plus dans la vue affichée.", true); return false; }
      amener(cible);
      return true;
    }
    async function suivre(a) {
      const { doc, zone } = courant();
      const href = a.getAttribute('href');
      const from = a.closest('.glose-file')?.dataset.path;
      if (!from || !href || /^(?:[a-z][a-z0-9+.-]*:|\/\/)/i.test(href)) return false;
      if (!href.startsWith('#') && !/\.md(?:#|$)/i.test(href)) return false;
      if (href.startsWith('#')) {
        let id;
        try { id = decodeURIComponent(href.slice(1)); } catch { id = href.slice(1); }
        const local = id ? fichier(from)?.querySelector('#' + CSS.escape(id)) : null;
        if (local) { generation++; amener(local); return true; }
      }
      const gen = ++generation;
      const r = await api('/api/resolve?from=' + encodeURIComponent(from) + '&href=' + encodeURIComponent(href) + '&view=' + (zone.dataset.vue || 'current'));
      if (gen !== generation || doc.filesGen !== courant().doc.filesGen) return true;
      if (!r.ok) { avis(r.data.message || "Le renvoi ne peut pas être suivi.", true); return true; }
      await ouvrir(r.data);
      return true;
    }
    return { ouvrir, suivre };
  }
  return { creer };
})();
