// Scoped refusal, shared by both browser drivers. No agent call.
const assert = require('node:assert/strict');
module.exports = async ({p, page}) => {
  const original = Buffer.from('\ufeffTexte ordinaire.\r\n');
  await p.write(original,'a.md');
  await p.write('Autre fichier {>>À trancher<<}.\r\n','b.md');
  // The project lists its files when the server starts: restart it.
  await page.evaluate(() => document.fonts.ready.then(() => true));
  await p.stop(); await p.start(); await page.navigate(p.state.url);
  await page.evaluate(async token => {
    const headers = {'X-Glose-Token':token, 'Content-Type':'application/json'};
    const d = await (await fetch('/api/agents',{headers})).json();
    const config = {...d.config, aliases:[{name:'Essai',agent:'codex'}],default:'Essai',tasks:[{id:'scope',name:'Portée',icon:'suggerer',prompt:'Relis.',place:['bar'],scope:'files'}],hash:d.hash};
    const r = await fetch('/api/agents',{method:'POST',headers,body:JSON.stringify(config)});
    if (!r.ok) throw Error(await r.text());
  },p.state.token);
  await page.navigate(p.state.url);
  await page.wait('consigne chargée',()=>!!document.querySelector('[data-task="scope"]'));
  await page.click('[data-task="scope"]');
  await page.evaluate(()=>{const a=document.querySelector('#remise-fichiers input[value="a.md"]');a.checked=true;for(const x of document.querySelectorAll('#remise-fichiers input')) if(x!==a) x.checked=false;a.dispatchEvent(new Event('change'));});
  await page.wait('fichier sans annotation permis',()=>!document.getElementById('remise-ok').disabled);
  assert.equal(await page.evaluate(()=>document.getElementById('remise-blocages').textContent),'');
  await page.evaluate(()=>{const b=document.querySelector('#remise-fichiers input[value="b.md"]');b.checked=true;b.dispatchEvent(new Event('change'));});
  await page.wait('refus avant le clic',()=>document.getElementById('remise-blocages').textContent.includes('Ce fichier (« b.md ») a 1 annotation en cours : tranchez-la avant de lancer cette consigne'));
  assert.ok(await page.evaluate(()=>document.getElementById('remise-ok').disabled));
  const result = await page.evaluate(async token => {
    const headers={'X-Glose-Token':token,'Content-Type':'application/json'};
    const d=await(await fetch('/api/tasks/preview?id=scope&file=b.md',{headers})).json();
    const r=await fetch('/api/handoff',{method:'POST',headers,body:JSON.stringify({task:'scope',files:['b.md'],taskHash:d.hash,confirmAccess:true})});
    return {code:r.status,message:(await r.json()).message,expected:d.blocks[0].message};
  },p.state.token);
  assert.notEqual(result.code,200); assert.equal(result.message,result.expected);
  await page.click('#remise-blocages button');
  await page.wait('première annotation affichée',()=>!document.getElementById('remise').open && !!document.querySelector('.carte.active'));
  assert.deepEqual(await p.read('a.md'),original);
  assert.equal((await p.read('b.md')).toString(),'Autre fichier {>>À trancher<<}.\r\n');
};
