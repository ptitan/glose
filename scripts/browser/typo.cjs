const assert = require('node:assert/strict');
module.exports = async ({p,page}) => {
  const src = Buffer.from('\ufeffUn texte...\r\n\r\nVoici : art. 12 !\r\n\r\n`code...` et [lien...](cible).\r\n\r\n{~~ancien~>nouveau~~}{>>glose:v1 id=abcd by=agent via=Autre<<}\r\n');
  await p.write(src); await page.navigate(p.state.url);
  await page.click('#typo-lancer');
  await page.wait('propositions Typo', () => [...document.querySelectorAll('#marge .carte')].some(c => c.textContent.includes('Typo')));
  const marked = await p.read(); assert.ok(!marked.equals(src));
  await page.click('#typo-accepter');
  await page.wait('confirmation Typo', () => document.querySelector('dialog[open]')?.textContent.includes('(Typo)'));
  await page.click('#boite-ok');
  await page.wait('Typo accepté, autre proposition conservée', () => document.querySelectorAll('#marge .carte').length === 1 && document.querySelector('#marge .carte')?.textContent.includes('Autre'));
  const accepted = await p.read();
  assert.ok(accepted.includes(Buffer.from('Un texte…\r\n')));
  assert.ok(accepted.includes(Buffer.from('`code...` et [lien...](cible)')));
  assert.ok(accepted.includes(Buffer.from('{~~ancien~>nouveau~~}')));
};
