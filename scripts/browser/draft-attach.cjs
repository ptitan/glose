// P22.5: a comment draft whose file has changed is attached to a new
// selection; nothing is written before « Enregistrer ».
const assert = require('node:assert/strict');
module.exports = async ({p,page}) => {
  // Required here: run.cjs loads this scenario before its helpers are exported.
  const { select, openComment, savedDraft, openDrafts, waitCards } = require('./run.cjs');
  const texte = 'Premier paragraphe à commenter.\n\nSecond paragraphe, la vraie cible.\n';
  await p.write(texte); await page.navigate(p.state.url);
  await openComment(page, 'Premier paragraphe');
  await page.fill('#saisie-texte', 'Remarque restée en brouillon.');
  await savedDraft(page, 'saisie-brouillon');
  await page.click('#saisie-fermer');
  await page.wait('panneau fermé', () => !document.getElementById('saisie').open);
  // The file changes meanwhile: « Reprendre » is no longer possible.
  const change = texte.replace('Premier', 'Tout premier');
  await p.write(change);
  await page.wait('texte rechargé', () => document.getElementById('texte').textContent.includes('Tout premier'));
  await select(page, 'vraie cible');
  await openDrafts(page);
  await page.wait('rattachement proposé', () => [...document.querySelectorAll('#brouillons-contenu button')].some((b) => b.textContent === 'Rattacher à la sélection' && !b.hidden) &&
    [...document.querySelectorAll('#brouillons-contenu button')].find((b) => b.textContent === 'Reprendre').disabled);
  assert.equal((await p.read()).toString(), change, 'rien d’écrit avant Enregistrer');
  await page.evaluate(() => [...document.querySelectorAll('#brouillons-contenu button')].find((b) => b.textContent === 'Rattacher à la sélection').click());
  await page.wait('panneau rouvert', () => document.getElementById('saisie').open && document.getElementById('saisie-texte').value === 'Remarque restée en brouillon.' &&
    /rattaché à cette sélection/.test(document.getElementById('saisie-message').textContent));
  await page.click('#saisie-ok');
  await waitCards(page, 1);
  assert.match((await p.read()).toString(), /\{==vraie cible==\}\{>>Remarque restée en brouillon\.<<\}/);
  await openDrafts(page);
  await page.wait('brouillon consommé', () => /Aucun brouillon/.test(document.getElementById('brouillons-contenu').textContent));
};
