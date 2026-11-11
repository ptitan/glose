"use strict";
// Project-wide search (P7.3), from the field in the top bar
// (P9.2): results appear in a dropdown below the field.
// Keyboard: ↓ and ↑ browse results, Enter opens, Escape closes.
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
    // A hideable block, not a <dialog>: show() would focus the
    // first box in the dropdown, taking input away from the field.
    function deplier() {
      d.hidden = false;
      input.setAttribute('aria-expanded', 'true');
    }
    // Returning focus to the field must not reopen the list ("focus" listener).
    let retour = false;
    function replier(rendreFocus) {
      generation++; clearTimeout(timer);
      if (!rendreFocus && !ancre.contains(document.activeElement)) document.body.classList.remove('recherche-ouverte');
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
    // Clicking elsewhere collapses the list.
    document.addEventListener('pointerdown', (e) => { if (!d.hidden && !ancre.contains(e.target)) replier(false); });
    // P20: the field stands in for the "Rechercher" button while in use.
    function montrer() { document.body.classList.add('recherche-ouverte'); deplier(); input.focus(); input.select(); chercher(); }
    ancre.addEventListener('focusout', (e) => {
      if (!ancre.contains(e.relatedTarget) && d.hidden) document.body.classList.remove('recherche-ouverte');
    });
    input.addEventListener('keydown', (e) => {
      // Before the handler above (capture): a first Escape closes the list, a second the field.
      if (e.key === 'Escape' && d.hidden) { document.body.classList.remove('recherche-ouverte'); document.getElementById('ouvrir-recherche').focus(); }
    }, true);
    return { montrer, replier };
  }
  return { creer };
})();
