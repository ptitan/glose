// Existing task= bytes remain authoritative even if the task is no longer configured.
const assert = require('node:assert/strict');
module.exports = async ({p,page}) => {
 const src = Buffer.from('Un {++texte++}{>>glose:v1 id=abcd by=agent task=rewrite<<}.\r\n\r\n{>>Question<<}{>>glose:v1 id=efgh by=author<<}{>>Réponse<<}{>>glose:v1 id=ijkl re=efgh by=agent task=proofread<<}\r\n');
 await p.write(src); await page.navigate(p.state.url);
 await page.wait('étiquettes sur les cartes', () => !!document.querySelector('.mission[data-task="rewrite"]') && !!document.querySelector('.mission[data-task="proofread"]'));
 await page.evaluate(() => {const s=document.getElementById('mission-filtre');s.value='rewrite';s.dispatchEvent(new Event('change'));});
 await page.wait('marge filtrée', () => document.querySelectorAll('#marge .carte:not([hidden])').length === 1);
 assert.equal(await page.evaluate(() => document.querySelector('#marge .mission').dataset.task),'rewrite');
 await page.evaluate(() => {const s=document.getElementById('mission-filtre');s.value='proofread';s.dispatchEvent(new Event('change'));});
 await page.wait('mission de réponse filtrée', () => document.querySelectorAll('#marge .carte:not([hidden])').length === 1 && !!document.querySelector('#marge .mission[data-task="proofread"]'));
 const list = JSON.parse(p.cli('list','--json','--task','proofread')); assert.equal(list.annotations?.length ?? list.items?.length, 1);
 assert.deepEqual(await p.read(),src);
};
