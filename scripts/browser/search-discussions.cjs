// P22.3: the search finds the discussions, in progress and closed.
const assert = require('node:assert/strict');
module.exports = async ({p,page}) => {
  await p.write('Un {~~mot~>terme~~}{>>glose:v1 id=aaaa by=agent via=Sol<<}{>>Formulation plus précise.<<}{>>glose:v1 id=aaab re=aaaa by=agent via=Sol<<}' +
    ' et un {==passage==}{>>Cette formulation me gêne.<<}{>>glose:v1 id=bbbb by=author<<}.\n');
  await page.navigate(p.state.url);
  await page.wait('deux cartes', () => document.querySelectorAll('#marge .carte:not([hidden])').length === 2);
  await page.click('#marge button[aria-label="Accepter"]');
  await page.wait('proposition acceptée', () => document.querySelectorAll('#marge .carte:not([hidden])').length === 1);
  await page.click('#ouvrir-recherche');
  await page.wait('champ de recherche', () => document.activeElement.id === 'recherche-texte');
  await page.fill('#recherche-texte', 'formulation');
  await page.wait('deux discussions', () => document.querySelectorAll('.resultat-discussion').length === 2 &&
    /Dans les discussions : 2/.test(document.querySelector('.recherche-discussions h4')?.textContent));
  const titres = await page.evaluate(() => [...document.querySelectorAll('.resultat-discussion strong')].map((s) => s.textContent));
  assert.deepEqual(titres, ['En cours · a.md', 'Close · a.md']);
  await page.evaluate(() => [...document.querySelectorAll('.resultat-discussion')].find((b) => b.textContent.startsWith('Close')).click());
  await page.wait('archive ouverte', () => document.getElementById('archive').open && /Formulation plus précise/.test(document.getElementById('archive').textContent) &&
    /Sol/.test(document.getElementById('archive').textContent));
};
