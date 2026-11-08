"use strict";
// Recherche dans tout le projet (P7.3), depuis le champ de la barre du haut
// (P9.2) : les résultats s'affichent dans une liste déroulante sous le champ.
// Clavier : ↓ et ↑ parcourent les résultats, Entrée ouvre, Échap ferme.
window.GloseRecherche = (() => {
  function creer({ api, courant, ouvrir, avis }) {
    const d = document.getElementById('recherche');
    const input = document.getElementById('recherche-texte');
    const casse = document.getElementById('recherche-casse');
    const liste = document.getElementById('recherche-resultats');
    const statut = document.getElementById('recherche-statut');
    const ancre = input.closest('.ancre-recherche');
    let generation = 0, timer;
    const el = (tag, text) => { const n = document.createElement(tag); n.textContent = text; return n; };
    const resultats = () => [...liste.querySelectorAll('.resultat-recherche')];
    // Un bloc masquable, pas un <dialog> : show() donnerait le focus à la
    // première case du déroulant, et la saisie quitterait le champ.
    function deplier() {
      d.hidden = false;
      input.setAttribute('aria-expanded', 'true');
    }
    // Le focus rendu au champ ne doit pas rouvrir la liste (écouteur « focus »).
    let retour = false;
    function replier(rendreFocus) {
      generation++; clearTimeout(timer);
      d.hidden = true;
      input.setAttribute('aria-expanded', 'false');
      if (rendreFocus) { retour = true; input.focus(); retour = false; }
    }
    async function chercher() {
      clearTimeout(timer);
      const gen = ++generation, { doc, view } = courant();
      liste.replaceChildren();
      if (!doc) { statut.textContent = 'Attendez la fin du chargement du texte.'; return; }
      if (!input.value.trim()) { statut.textContent = 'Chercher dans tous les fichiers, même non affichés.'; return; }
      statut.textContent = 'Recherche…';
      const r = await api('/api/search?q=' + encodeURIComponent(input.value) + '&case=' + (casse.checked ? '1' : '0') + '&view=' + view);
      if (gen !== generation || d.hidden) return;
      if (!r.ok) { statut.textContent = r.data.message || 'Recherche impossible.'; return; }
      if (r.data.filesGen !== courant().doc.filesGen) { statut.textContent = 'La liste des fichiers a changé : relancez la recherche.'; return; }
      statut.textContent = (r.data.total ? r.data.total + ' résultat' + (r.data.total > 1 ? 's' : '') : 'Aucun résultat') +
        (r.data.total > 200 ? ' (les 200 premiers sont affichés)' : '') + (r.data.problems.length ? ' · ' + r.data.problems.join(' ; ') : '');
      for (const m of r.data.matches) {
        const b = el('button', ''); b.type = 'button'; b.className = 'resultat-recherche';
        b.append(el('strong', m.path));
        const extrait = el('span', '');
        extrait.append(document.createTextNode(m.before.replace(/\s+/g, ' ')), el('mark', m.text), document.createTextNode(m.after.replace(/\s+/g, ' ')));
        b.append(extrait);
        b.addEventListener('click', async () => {
          if (view !== courant().view) { avis('La vue a changé : relancez la recherche.', true); return; }
          replier(false);
          await ouvrir(m);
        });
        liste.append(b);
      }
    }
    input.addEventListener('input', () => {
      deplier();
      generation++; clearTimeout(timer); timer = setTimeout(chercher, 220);
    });
    input.addEventListener('focus', () => { if (!retour && input.value.trim() && d.hidden) { deplier(); chercher(); } });
    input.addEventListener('keydown', (e) => {
      if (e.key === 'Enter') { e.preventDefault(); deplier(); chercher(); }
      else if (e.key === 'Escape') { e.preventDefault(); if (!d.hidden) replier(true); else input.blur(); }
      else if (e.key === 'ArrowDown' && !d.hidden) { const r = resultats(); if (r.length) { e.preventDefault(); r[0].focus(); } }
    });
    d.addEventListener('keydown', (e) => {
      const r = resultats(), i = r.indexOf(document.activeElement);
      if (e.key === 'Escape') { e.preventDefault(); replier(true); }
      else if (e.key === 'Enter' && i >= 0) { e.preventDefault(); r[i].click(); }
      else if (e.key === 'ArrowDown' && i >= 0) { e.preventDefault(); (r[i + 1] || r[i]).focus(); }
      else if (e.key === 'ArrowUp' && i >= 0) { e.preventDefault(); if (i === 0) input.focus(); else r[i - 1].focus(); }
    });
    casse.addEventListener('change', chercher);
    // Un clic ailleurs replie la liste.
    document.addEventListener('pointerdown', (e) => { if (!d.hidden && !ancre.contains(e.target)) replier(false); });
    function montrer() { deplier(); input.focus(); input.select(); chercher(); }
    return { montrer, replier };
  }
  return { creer };
})();
