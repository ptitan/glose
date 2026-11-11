// D-64: a filter runs only once the author approves it (glose approve, in a
// terminal: the interface has no button for it), and waits again once
// changed (by an agent, say).
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const crypto = require('node:crypto');
const marker = 'function Pandoc(doc)\n  local f = assert(io.open("ran.txt", "wb"))\n  f:write("x")\n  f:close()\n  return doc\nend\n';
const exists = (f) => fs.stat(f).then(() => true, () => false);
// What glose approve writes (internal/trust), outside the project.
async function approve(p) {
  const root = await fs.realpath(p.dir);
  const hash = crypto.createHash('sha256').update(await fs.readFile(path.join(p.dir, 'f.lua'))).digest('hex');
  await fs.mkdir(p.env.GLOSE_CONFIG_DIR, { recursive: true });
  await fs.writeFile(path.join(p.env.GLOSE_CONFIG_DIR, 'approbations.json'),
    JSON.stringify({ version: 1, projects: { [root]: { filter: 'f.lua', filterHash: hash, at: new Date().toISOString() } } }));
}
module.exports = async ({p,page}) => {
  const ran = path.join(p.dir, 'ran.txt');
  await p.write(marker, 'f.lua'); await p.write('{"filter":"f.lua"}', 'glose.json');
  await p.stop(); await p.start(); await page.navigate(p.state.url);
  await page.wait('approbation demandée', () => !document.getElementById('approbation').hidden &&
    document.getElementById('approbation').textContent.includes('f.lua') && document.querySelector('#approbation code')?.textContent === 'glose approve');
  assert.equal(await page.evaluate(() => !!document.querySelector('#approbation button')), false, 'pas de bouton');
  assert.equal(await exists(ran), false, 'filtre exécuté sans accord');
  await approve(p);
  await page.wait('approuvé, vu par l’interface', () => document.getElementById('approbation').hidden);
  assert.equal(await exists(ran), true, 'filtre approuvé non exécuté');
  await fs.rm(ran);
  await p.write(marker + '-- changé par un agent\n', 'f.lua');
  await page.wait('approbation demandée de nouveau', () => !document.getElementById('approbation').hidden);
  assert.equal(await exists(ran), false, 'filtre changé exécuté');
};
