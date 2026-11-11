// Shared P15 scenario for Chromium and Firefox. No real agent is launched.
const assert = require('node:assert/strict');
module.exports = async ({ p, page }) => {
  const original = await p.read();
  await page.navigate(new URL('/set#t=' + p.state.token, p.state.url).href, '#ra-statut');
  await page.wait('réglages chargés', () => document.getElementById('ra-statut').textContent !== '');
  // Configure an alias via the authenticated API; permissions and task editing
  // still go through the actual form below.
  await page.evaluate(async (token) => {
    const headers = {'X-Glose-Token': token, 'Content-Type':'application/json'};
    const d = await (await fetch('/api/agents', {headers})).json();
    const r = await fetch('/api/agents', {method:'POST',headers,body:JSON.stringify({...d.config,aliases:[{name:'Essai',agent:'codex'}],default:'Essai',hash:d.hash})});
    if (!r.ok) throw new Error(await r.text());
  }, p.state.token);
  await page.navigate(new URL('/set#t=' + p.state.token, p.state.url).href, '#ra-statut');
  await page.wait('agent chargé', () => /Essai/.test(document.getElementById('ra-statut').textContent));
  await page.click('#ra-ajouter-consigne');
  for (const [key,value] of [['name','Réécrire'],['prompt','Relis tout le texte.']]) {
    await page.fill('[data-task-field="' + key + '"]', value);
  }
  await page.evaluate(() => {
    const b = document.querySelector('.ra-consigne');
    b.querySelector('[data-task-field=bar]').checked = true;
  });
  await page.click('#ra-enregistrer');
  await page.wait('consigne enregistrée', () => /^Enregistré/.test(document.getElementById('ra-statut').textContent));
  assert.equal(await page.evaluate(() => document.querySelector('.ra-consigne').open), false);
  await page.click('.ra-consigne > summary');
  assert.equal(await page.evaluate(() => document.querySelector('.ra-options').open), false);
  await page.click('.ra-choix-icone > summary');
  assert.ok(await page.evaluate(() => [...document.querySelectorAll('.ra-icone')].every(b => b.querySelector('svg'))));
  await page.click('[data-task-icon=spell-check]');
  await page.click('#ra-enregistrer');
  await page.wait('icône enregistrée', () => /^Enregistré/.test(document.getElementById('ra-statut').textContent));
  await page.navigate(p.state.url);
  await page.wait('icône de consigne', () => !!document.querySelector('#consignes-barre button[data-task="reecrire"]'));
  assert.equal(await page.evaluate(() => !!document.querySelector('.barre-outils #consignes-barre') && !document.querySelector('#carte-tour #consignes-barre')), true);
  await page.click('#consignes-barre button[data-task="reecrire"]');
  await page.wait('accès rappelés', () => document.getElementById('remise').open && /Codex/.test(document.getElementById('remise-acces').textContent));
  assert.equal(await page.evaluate(() => document.getElementById('remise-ok').disabled), false);
  assert.equal(await page.evaluate(() => !!document.getElementById('remise-confirmer')), false);
  assert.equal(await page.evaluate(() => document.getElementById('remise-ok').textContent), 'Lancer');
  // P20.4: the launch panel lists each access with its answer.
  assert.deepEqual(await page.evaluate(() => [document.getElementById('remise').classList.contains('lancement'), document.querySelectorAll('#remise-acces .remise-droits li').length,
    document.querySelectorAll('#remise-acces .remise-droits li.accorde').length]), [true, 6, 1]);
  assert.equal(await page.evaluate(() => document.querySelector('#remise-acces details').open), false);
  assert.match(await page.evaluate(() => document.getElementById('remise-acces').textContent), /non applicables/);
  assert.equal(await page.evaluate(() => document.getElementById('remise-ok').disabled), false);
  await page.click('#remise-fermer');
  await page.click('#remettre');
  await page.wait('remise sans consigne', () => document.getElementById('remise').open && document.getElementById('remise-consigne').value === '');
  await page.click('#remise-fermer');
  assert.deepEqual(await p.read(), original, 'configuration et aperçu : manuscrit identique');
  await page.navigate(new URL('/set#t=' + p.state.token, p.state.url).href, '#ra-statut');
  await page.wait('consigne relue', () => !!document.querySelector('.ra-consigne'));
  await page.evaluate(() => { [...document.querySelectorAll('.ra-consigne button')].find(b => b.textContent === 'Retirer').click(); });
  await page.click('#ra-enregistrer');
  await page.wait('retrait enregistré', () => /^Enregistré/.test(document.getElementById('ra-statut').textContent));
  await page.navigate(p.state.url);
  await page.wait('icône retirée', () => !!document.getElementById('texte')?.textContent && !document.querySelector('#consignes-barre button'));
  // Legacy place values remain readable; every task now appears at handover.
  await page.evaluate(async token => {
    const headers = {'X-Glose-Token':token,'Content-Type':'application/json'};
    const d = await (await fetch('/api/agents',{headers})).json();
    const tasks = ['bar','handover'].map(place => ({id:'ancien-'+place,name:'Ancien '+place,icon:'suggerer',prompt:'Relis.',place:[place]}));
    const r = await fetch('/api/agents',{method:'POST',headers,body:JSON.stringify({...d.config,tasks,hash:d.hash})});
    if (!r.ok) throw Error(await r.text());
  },p.state.token);
  await page.navigate(new URL('/set#t='+p.state.token,p.state.url).href,'#ra-statut');
  await page.wait('anciennes consignes chargées',()=>document.querySelectorAll('.ra-consigne').length===2);
  assert.deepEqual(await page.evaluate(()=>[...document.querySelectorAll('.ra-consigne')].map(b=>({open:b.open,bar:b.querySelector('[data-task-field=bar]').checked}))),[{open:false,bar:true},{open:false,bar:false}]);
  await page.click('.ra-consigne > summary');
  await page.fill('.ra-consigne [data-task-field=name]','Nom modifié');
  await page.click('#ra-enregistrer');
  await page.wait('ancien identifiant conservé',()=>/^Enregistré/.test(document.getElementById('ra-statut').textContent));
  await page.navigate(p.state.url);
  await page.wait('bouton ancien relu',()=>!!document.querySelector('[data-task="ancien-bar"]'));
  assert.equal(await page.evaluate(()=>!!document.querySelector('[data-task="ancien-handover"]')),false);
  await page.click('#remettre');
  await page.wait('toutes les consignes dans la remise',()=>[...document.querySelectorAll('#remise-consigne option')].some(o=>o.value==='ancien-bar') && [...document.querySelectorAll('#remise-consigne option')].some(o=>o.value==='ancien-handover'));
  await page.click('#remise-fermer');
  assert.deepEqual(await p.read(), original);

};
