// P22.1: beyond 200 results, the next ones on demand.
const assert = require('node:assert/strict');
module.exports = async ({p,page}) => {
  await p.write('Un mot. '.repeat(250) + '\n');
  await page.navigate(p.state.url);
  await page.click('#ouvrir-recherche');
  await page.wait('champ de recherche', () => document.activeElement.id === 'recherche-texte');
  await page.fill('#recherche-texte', 'mot');
  await page.wait('200 premiers', () => document.querySelectorAll('.resultat-recherche').length === 200 &&
    /250 résultats \(1 à 200 affichés\)/.test(document.getElementById('recherche-statut').textContent));
  assert.match(await page.evaluate(() => document.querySelector('.recherche-suite').textContent), /50 résultats suivants/);
  await page.click('.recherche-suite');
  await page.wait('les 50 suivants', () => document.querySelectorAll('.resultat-recherche').length === 250 && !document.querySelector('.recherche-suite'));
  assert.match(await page.evaluate(() => document.getElementById('recherche-statut').textContent), /^250 résultats$/);
};
