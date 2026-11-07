// P5.5 : navettes répétées sur des COPIES de trois documents réellement utilisés.
// Ceci mesure une session pilotée, pas plusieurs jours d'usage humain.
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const os = require('node:os');
const crypto = require('node:crypto');
const { spawnSync } = require('node:child_process');
const { launchChrome } = require('./cdp.cjs');
const { Project, openComment, waitCards, focusText } = require('./run.cjs');

const repo = path.resolve(__dirname, '../..');
const cycles = Number(process.env.GLOSE_ENDURANCE_TURNS || 8);
assert.ok(Number.isInteger(cycles) && cycles >= 4 && cycles <= 100 && cycles % 4 === 0, 'GLOSE_ENDURANCE_TURNS : multiple de 4, entre 4 et 100');
const hash = (bytes) => crypto.createHash('sha256').update(bytes).digest('hex');
const cases = [
  ['README.md', 'Glose affiche un texte écrit en Markdown dans le navigateur, mis en forme.', 'Glose affiche dans le navigateur un texte Markdown mis en forme.'],
  ['docs/TESTS-NAVIGATEUR.md', 'Aucune installation ni aucun téléchargement automatique.', 'Aucune installation ni aucun téléchargement ne sont lancés automatiquement.'],
  ['docs/GUIDE.md', 'Ce guide s\'adresse à l\'auteur.', 'Ce guide est écrit pour l\'auteur.'],
];
const summary = (samples) => {
  const sorted = [...samples].sort((a, b) => a - b);
  return { min: sorted[0], median: sorted[Math.floor(sorted.length / 2)], max: sorted.at(-1) };
};

async function main() {
  const root = await fs.mkdtemp(path.join(process.env.GLOSE_TEST_TMP || os.tmpdir(), 'glose-endurance-'));
  const started = Date.now();
  const version = spawnSync('git', ['rev-parse', '--short', 'HEAD'], { cwd: repo, encoding: 'utf8' }).stdout.trim();
  const report = { version, started: new Date().toISOString(), cyclesPerDocument: cycles, documents: [], completed: false };
  let browser, p, page;
  try {
    browser = await launchChrome(root); report.browser = browser.version.product;
    const chosen = cases.filter(([file]) => !process.env.GLOSE_ENDURANCE_CASE || file.includes(process.env.GLOSE_ENDURANCE_CASE));
    assert.ok(chosen.length, 'aucun document correspondant à GLOSE_ENDURANCE_CASE');
    for (const [file, before, after] of chosen) {
      const original = await fs.readFile(path.join(repo, file));
      assert.equal(original.toString().split(before).length, 2, 'passage source unique : ' + file);
      const title = original.toString().split('\n')[0].replace(/\r$/, '');
      const dir = await fs.mkdtemp(path.join(root, 'projet-'));
      p = new Project(dir); await p.write(original); await p.start();
      page = await browser.page(); await page.navigate(p.state.url);
      const measurements = { file, bytes: original.length, sha256Before: hash(original), cycles: 0, savesMs: [], decisionsMs: [], rendersMs: [], restarts: 0 };
      report.documents.push(measurements);
      let expected = Buffer.from(original), current = before;
      for (let i = 0; i < cycles; i++) {
        const next = current === before ? after : before;
        await openComment(page, current);
        await page.fill('#saisie-texte', 'Rends cette formulation plus directe, sans changer le sens.');
        const saveStart = Date.now(); await page.click('#saisie-ok');
        await page.wait('commentaire enregistré', () => !document.getElementById('saisie').open); await waitCards(page, 1);
        measurements.savesMs.push(Date.now() - saveStart);
        const id = JSON.parse(p.cli('list', '--json')).annotations[0].id;
        await page.click('#remettre'); await page.wait('remise ouverte', () => document.getElementById('remise').open);
        await page.fill('#remise-note', `Navette ${i + 1} : reformulation et point secondaire.`); await page.click('#remise-ok');
        await page.wait('main à l’agent', () => document.body.classList.contains('main-agent'));
        p.cli('reply', '--id', id, '--text', 'Voici une formulation plus directe ; le fond est conservé.');
        p.cli('propose', '--id', id, '--new', next, '--note', 'Reformulation demandée.');
        p.cli('propose', '--file', 'a.md', '--find', title, '--new', title + ` (essai ${i + 1})`, '--whole', '--note', 'Point secondaire à rejeter.');
        p.cli('done', '--note', 'Les deux propositions attendent votre décision.');
        await page.wait('main rendue', () => !document.body.classList.contains('main-agent')); await waitCards(page, 2);
        assert.match(await page.evaluate(() => document.querySelector('#marge').textContent), /le fond est conservé/);
        // Decide the secondary title proposal first, then the requested rewrite.
        const secondary = await page.evaluate(() => [...document.querySelectorAll('#marge .carte')].findIndex((c) => c.textContent.includes('Point secondaire à rejeter.')));
        assert.ok(secondary >= 0);
        await page.click(`#marge .carte:nth-child(${secondary + 1}) button[aria-label="Rejeter"]`); await waitCards(page, 1);
        const accept = i % 2 === 0, decisionStart = Date.now();
        await page.click(`#marge button[aria-label="${accept ? 'Accepter' : 'Rejeter'}"]`); await waitCards(page, 0);
        measurements.decisionsMs.push(Date.now() - decisionStart);
        if (accept) { expected = Buffer.from(expected.toString().replace(current, next)); current = next; }
        assert.deepEqual(await p.read(), expected, 'octets après décision : ' + file + ', cycle ' + i);
        if (i % 3 === 0) {
          await focusText(page); await page.key('z', 2); await waitCards(page, 1);
          await page.key('y', 2); await waitCards(page, 0);
          assert.deepEqual(await p.read(), expected, 'octets après annulation/rétablissement');
        }
        p.cli('check');
        const state = await p.api('/api/state');
        assert.equal(state.annotations, 0); assert.equal(state.issues, 0); assert.deepEqual(state.problems, []);
        measurements.rendersMs.push((await p.api('/api/doc')).millis);
        measurements.cycles++;
        if (i + 1 === cycles / 2) {
          await p.stop(); await p.start(); await page.navigate(p.state.url); measurements.restarts++;
          assert.deepEqual(await p.read(), expected, 'redémarrage en cours de navette');
        }
      }
      assert.deepEqual(await p.read(), original, 'retour final à tous les octets d’origine');
      measurements.sha256After = hash(await p.read());
      const events = (await fs.readFile(path.join(dir, '.glose/journal.jsonl'), 'utf8')).trim().split('\n').map(JSON.parse);
      measurements.events = events.length;
      measurements.handovers = events.filter((e) => e.type === 'turn').length;
      assert.equal(measurements.handovers, cycles * 2);
      assert.equal(events.some((e) => e.partial), false);
      for (const key of ['savesMs', 'decisionsMs', 'rendersMs']) measurements[key] = summary(measurements[key]);
      assert.deepEqual(page.errors, [], 'aucune exception JavaScript');
      await page.close(); page = null; await p.stop(); p = null;
      console.log(`${file} : ${cycles} navettes, ${measurements.events} événements, octets et journal vérifiés.`);
    }
    report.completed = true;
  } finally {
    if (!report.completed && page) {
      report.failureState = await page.evaluate(() => ({ click: window.__gloseClick, trace: window.__gloseTest, cards: [...document.querySelectorAll('#marge .carte')].map((c) => c.textContent), dialogs: [...document.querySelectorAll('dialog')].map((d) => ({ id: d.id, open: d.open })) })).catch(() => null);
      try { const shot = await page.send('Page.captureScreenshot'); await fs.writeFile(path.join(root, 'echec.png'), Buffer.from(shot.data, 'base64')); } catch (_) {}
    }
    if (page) await page.close().catch(() => {});
    if (p) await p.stop();
    if (browser) await browser.close();
    report.elapsedMs = Date.now() - started;
    await fs.writeFile(path.join(root, 'bilan.json'), JSON.stringify(report, null, 2));
    console.log('Mesures et copies conservées : ' + root);
  }
}
main().catch((err) => { console.error(err); process.exitCode = 1; });
