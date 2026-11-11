// D-65: « Tout accepter » under a mission filter decides what it announces.
const assert = require('node:assert/strict');
module.exports = async ({p,page}) => {
  const src = Buffer.from('Un{++ premier++}{>>glose:v1 id=aaaa by=agent task=rewrite<<} et{++ second++}{>>glose:v1 id=bbbb by=agent task=rewrite<<}' +
    ' puis{++ autre++}{>>glose:v1 id=cccc by=agent task=proofread<<}.\r\n');
  await p.write(src); await page.navigate(p.state.url);
  await page.wait('cartes', () => document.querySelectorAll('#marge .carte:not([hidden])').length === 3);
  await page.evaluate(() => {const s=document.getElementById('mission-filtre');s.value='rewrite';s.dispatchEvent(new Event('change'));});
  await page.wait('marge filtrée', () => document.querySelectorAll('#marge .carte:not([hidden])').length === 2);
  await page.click('#onglet-annotations');
  await page.wait('lot du fichier', () => !!document.querySelector('#annotations-liste .lot button.accepter'));
  await page.click('#annotations-liste .lot button.accepter');
  await page.wait('confirmation de la mission', () => /les 2 propositions/.test(document.querySelector('dialog[open]')?.textContent || ''));
  await page.click('#boite-ok');
  await page.wait('deux décidées, l’autre mission gardée', () => document.querySelectorAll('#marge .carte').length === 1);
  const after = (await p.read()).toString();
  assert.match(after, /^Un premier et second puis\{\+\+ autre\+\+\}/);
};
