// Real CLI and polling, shared by Chrome and Firefox.
const assert = require('node:assert/strict');
module.exports = async ({p,page}) => {
 const original = await p.read();
 const code = await page.evaluate(async token => (await fetch('/api/handoff', {method:'POST',headers:{'X-Glose-Token':token,'Content-Type':'application/json'},body:JSON.stringify({launch:false})})).status,p.state.token);
 assert.equal(code,200);
 p.cli('progress','--agent','Essai','--text','Je relis <b>les références</b>.');
 await page.navigate(p.state.url);
 await page.wait('phrase dans la capsule',()=>document.querySelector('#navette .agent-avancement')?.textContent.includes('Je relis <b>les références</b>.'));
 assert.equal(await page.evaluate(()=>!!document.querySelector('#navette .agent-avancement b')),false);
 await page.click('#onglet-navette');
 await page.wait('phrase dans les tours',()=>[...document.querySelectorAll('.tour-avancement')].some(e=>e.textContent.includes('Je relis')));
 p.cli('progress','--agent','Essai','--text','Je prépare les propositions.');
 await page.wait('phrase mise à jour dans les tours',()=>[...document.querySelectorAll('.tour-avancement')].some(e=>e.textContent.includes('Je prépare')));
 p.cli('done','--agent','Essai');
 await page.wait('main rendue',()=>!document.body.classList.contains('main-agent'));
 assert.equal(await page.evaluate(()=>!!document.querySelector('#navette .agent-avancement')),false);
 assert.deepEqual(await p.read(),original);
};
