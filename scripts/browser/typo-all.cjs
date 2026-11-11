// P22.4: Typo on every file shown, then « Tout accepter (Typo) » once.
const assert = require('node:assert/strict');
module.exports = async ({p,page}) => {
  await p.stop();
  await p.write('# Un\n\nVoilà...\n'); await p.write('# Deux\n\nEnfin...\n', 'b.md'); await p.write('# Trois\n\nRien à corriger.\n', 'c.md');
  await p.start(); await page.navigate(p.state.url);
  await page.wait('choix « tous »', () => [...document.getElementById('typo-fichier').options].some((o) => o.value === 'tous'));
  await page.evaluate(() => { const s = document.getElementById('typo-fichier'); s.value = 'tous'; s.dispatchEvent(new Event('change')); });
  await page.click('#typo-lancer');
  await page.wait('bilan', () => /Typo : 2 corrections proposées dans 2 fichiers, 1 sans correction sûre\./.test(document.getElementById('avis').textContent));
  await page.wait('bouton', () => !document.getElementById('typo-accepter').hidden);
  await page.click('#typo-accepter');
  await page.wait('confirmation unique', () => /Accepter \(Typo\) les 2 propositions/.test(document.querySelector('dialog[open]')?.textContent || ''));
  await page.click('#boite-ok');
  await page.wait('acceptées', () => /2 propositions de Typo acceptées/.test(document.getElementById('avis').textContent));
  assert.equal((await p.read()).toString(), '# Un\n\nVoilà…\n');
  assert.equal((await p.read('b.md')).toString(), '# Deux\n\nEnfin…\n');
  assert.equal((await p.read('c.md')).toString(), '# Trois\n\nRien à corriger.\n');
};
