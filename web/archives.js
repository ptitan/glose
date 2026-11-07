// Lecture des copies du journal : pas de HTML provenant des messages, pas de
// geste de décision ni de réapplication au manuscrit.
"use strict";
window.GloseArchives = (() => {
  const DECISIONS = { accept: 'Acceptée', reject: 'Rejetée', resolve: 'Discussion résolue', withdraw: 'Retirée', revise: 'Version précédente' };
  const QUI = { author: 'Vous', agent: "L’agent", unknown: 'Auteur inconnu' };
  function el(tag, text, classe) {
    const n = document.createElement(tag); n.textContent = text;
    if (classe) n.className = classe;
    return n;
  }
  function afficher(operations) {
    const zone = el('div', '', 'archives');
    let n = 0;
    for (const op of operations || []) {
      for (const c of [...(op.closed || []), ...(op.revised || [])]) {
        n++;
        const bouton = el('button', '', 'archive-ouvrir'); bouton.type = 'button'; bouton.setAttribute('aria-haspopup', 'dialog');
        bouton.append(el('strong', DECISIONS[c.decision] || c.decision), el('span', c.file));
        const d = el('div', '', 'archive-texte');
        d.append(el('p', 'Événement historique : une annulation ultérieure peut avoir rouvert cette annotation.', 'aide'));
        d.append(el('p', c.file + (c.id ? ' · ' + c.id : ''), 'archive-meta'));
        d.append(el('p', (QUI[c.by] || 'Auteur non renseigné') + ' · ' + (op.time ? new Date(op.time).toLocaleString('fr-FR') : ''), 'archive-meta'));
        const morceau = (titre, texte) => { d.append(el('h4', titre), el('pre', texte || '∅')); };
        if (c.kind === 'substitution') { morceau('Texte d’origine', c.text); morceau('Texte proposé', c.new); }
        else if (c.kind === 'addition') morceau('Ajout proposé', c.text);
        else if (c.kind === 'deletion') morceau('Suppression proposée', c.text);
        else if (c.kind !== 'comment' || !(c.messages || []).length) morceau(c.kind === 'highlight' ? 'Passage commenté' : 'Message', c.text);
        if ((c.messages || []).length) {
          d.append(el('h4', 'Discussion'));
          for (const m of c.messages) {
            const b = el('blockquote', '');
            b.append(el('strong', QUI[m.by] || 'Auteur non renseigné'), el('p', m.text));
            d.append(b);
          }
        }
        bouton.addEventListener('click', () => {
          const panneau = document.getElementById('archive');
          document.getElementById('archive-titre').textContent = DECISIONS[c.decision] || 'Archive';
          document.getElementById('archive-contenu').replaceChildren(d);
          document.getElementById('archive-fermer').onclick = () => panneau.close();
          if (!panneau.open) panneau.showModal();
        });
        zone.append(bouton);
      }
    }
    if (n) zone.prepend(el('p', n + ' archive' + (n > 1 ? 's' : '') + ' à consulter', 'aide'));
    return zone;
  }
  return { afficher };
})();
