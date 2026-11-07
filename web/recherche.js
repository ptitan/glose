"use strict";
window.GloseRecherche = (() => {
  function creer({ api, courant, ouvrir, avis }) {
    const d = document.getElementById('recherche');
    const input = document.getElementById('recherche-texte');
    const casse = document.getElementById('recherche-casse');
    const liste = document.getElementById('recherche-resultats');
    const statut = document.getElementById('recherche-statut');
    let generation = 0, timer;
    const el = (tag, text) => { const n = document.createElement(tag); n.textContent = text; return n; };
    async function chercher() {
      clearTimeout(timer);
      const gen = ++generation, { doc, view } = courant();
      liste.replaceChildren();
      if (!doc) { statut.textContent = 'Attendez la fin du chargement du texte.'; return; }
      if (!input.value.trim()) { statut.textContent = 'Chercher dans tous les fichiers, même non affichés.'; return; }
      statut.textContent = 'Recherche…';
      const r = await api('/api/search?q=' + encodeURIComponent(input.value) + '&case=' + (casse.checked ? '1' : '0') + '&view=' + view);
      if (gen !== generation || !d.open) return;
      if (!r.ok) { statut.textContent = r.data.message || 'Recherche impossible.'; return; }
      if (r.data.filesGen !== courant().doc.filesGen) { statut.textContent = 'La liste des fichiers a changé : relancez la recherche.'; return; }
      statut.textContent = r.data.total + ' résultat' + (r.data.total > 1 ? 's' : '') + (r.data.total > 200 ? ' (les 200 premiers sont affichés)' : '') +
        (r.data.problems.length ? ' · ' + r.data.problems.join(' ; ') : '');
      for (const m of r.data.matches) {
        const b = el('button', ''); b.type = 'button'; b.className = 'resultat-recherche';
        b.append(el('strong', m.path));
        const extrait = el('span', '');
        extrait.append(document.createTextNode(m.before.replace(/\s+/g, ' ')), el('mark', m.text), document.createTextNode(m.after.replace(/\s+/g, ' ')));
        b.append(extrait);
        b.addEventListener('click', async () => {
          if (view !== courant().view) { avis('La vue a changé : relancez la recherche.', true); return; }
          d.close();
          await ouvrir(m);
        });
        liste.append(b);
      }
    }
    input.addEventListener('input', () => { generation++; clearTimeout(timer); timer = setTimeout(chercher, 220); });
    casse.addEventListener('change', chercher);
    document.getElementById('recherche-forme').addEventListener('submit', (e) => { e.preventDefault(); chercher(); });
    document.getElementById('recherche-fermer').addEventListener('click', () => d.close());
    d.addEventListener('close', () => { generation++; clearTimeout(timer); });
    function montrer() { if (!d.open) d.showModal(); input.focus(); input.select(); chercher(); }
    document.getElementById('bouton-recherche').addEventListener('click', montrer);
    return { montrer };
  }
  return { creer };
})();
