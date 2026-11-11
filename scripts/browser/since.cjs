const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
module.exports = async ({p,page}) => {
  const handoff = async () => {
    assert.equal(await page.evaluate(async token => (await fetch('/api/handoff', {method:'POST',headers:{'X-Glose-Token':token,'Content-Type':'application/json'},body:JSON.stringify({launch:false})})).status,p.state.token),200);
    p.cli('done','--agent','Essai');
  };
  const restart = async () => {
    await page.evaluate(() => document.fonts.ready.then(() => true));
    await p.stop(); await p.start(); await page.navigate(p.state.url);
  };
  await p.write('Un chat dort.\n');
  await p.write('Fichier ancien.\n','disparu.md');
  await restart(); // The profile-less inventory is read on server startup.
  await handoff();
  await p.write('Un chien dort.\n');
  await p.write('Fichier nouveau.\n','apparu.md');
  await fs.unlink(path.join(p.dir,'disparu.md'));
  await restart();
  const before = await p.read();
  await page.click('[data-vue="depuis"]');
  await page.wait('différence depuis la remise', () => !!document.querySelector('.g-depuis') && document.querySelector('.depuis-bilan')?.textContent.includes('chat'));
  assert.equal(await page.evaluate(() => document.getElementById('depuis-reference').value), '1');
  assert.match(await page.evaluate(() => document.querySelector('.depuis-bilan').textContent), /fichier apparu/);
  assert.match(await page.evaluate(() => document.querySelector('.depuis-bilan').textContent), /fichier disparu/);
  assert.equal(await page.evaluate(() => document.querySelectorAll('#marge .carte').length), 0);
  await page.key('c');
  assert.equal(await page.evaluate(() => document.getElementById('saisie').open), false);
  await page.click('[data-vue="actuel"]');
  await page.wait('couleurs retirées', () => !document.querySelector('.rendu:not(.garde) .g-depuis'));
  await handoff();
  await page.navigate(p.state.url); await page.click('[data-vue="depuis"]');
  await page.wait('dernière remise choisie', () => document.getElementById('depuis-reference').value === '3');
  await page.evaluate(() => { const s=document.getElementById('depuis-reference');s.value='1';s.dispatchEvent(new Event('change')); });
  await page.wait('ancienne remise choisie', () => !!document.querySelector('.g-depuis') && document.getElementById('depuis-reference').value === '1');
  assert.deepEqual(await p.read(), before);
};
