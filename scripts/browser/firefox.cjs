// End-to-end tests in Firefox (P8.1): real executable, real pandoc, real
// Firefox driven through WebDriver BiDi. No personal server or manuscript is
// used. See docs/TESTS-NAVIGATEUR.md.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const os = require('node:os');
const { launchFirefox } = require('./bidi.cjs');
const { Project } = require('./run.cjs');

// Two files: the reader is in the second one, so the first one sits above
// the viewport. Firefox gives a file only its estimated height
// (content-visibility, 3000px) until the frame after a new rendering, while
// the cards are placed at once; this first file is much shorter than that.
const paragraph = (n) => `Paragraphe ${n} de l’avant-propos, assez long pour occuper quelques lignes dans la colonne de texte et donner au fichier une hauteur réelle bien différente de son estimation.\n\n`;
const preface = '# Avant-propos\n\n' + Array.from({ length: 12 }, (_, i) => paragraph(i + 1)).join('');
const body = (n) => `Texte ${n} du chapitre, qui suit la section et remplit l’écran pour que les annotations s’y trouvent à des hauteurs bien distinctes.\n\n`;
const chapter = '# Chapitre\n\n## Première section\n\n' + Array.from({ length: 40 }, (_, i) => body(i + 1)).join('') + '## Seconde section\n\nFin.\n';

const scenarios = [
  ["missions : cartes et filtre de marge", require("./missions.cjs")],
  ["missions : « Tout accepter » limité à la mission (D-65)", require("./mission-batch.cjs")],
  ["filtre : exécuté seulement une fois approuvé (D-64)", require("./approval.cjs")],
  ["Typo : propositions et acceptation filtrée", require("./typo.cjs")],
  ["vue Depuis en lecture seule", require("./since.cjs")],
  ['consignes : création, accès, confirmation et retrait', require('./tasks.cjs')],
  ['consignes : refus limité à la portée', require('./task-blocks.cjs')],
  ['avancement de l’agent par la commande progress', require('./agent-progress.cjs')],
  ['réglages et projets : colonne en fenêtre étroite', require('./space-width.cjs')],
  ['cartes de la marge en face de leur annotation après chaque écriture', async ({ p, page }) => {
    const A = Buffer.from(chapter).indexOf('## Première section') + Buffer.byteLength('## Première section');
    const B = Buffer.from(chapter).indexOf('\n\n', Buffer.from(chapter).indexOf('Texte 8 ')); // end of a paragraph further down
    await page.evaluate(() => {
      [...document.querySelectorAll('#texte h2')].find((h) => h.textContent.includes('Première section')).scrollIntoView({ block: 'center' });
    });
    // Writes go through the server as the buttons do; the interface sees
    // them on its next poll and renders again, the path of the defect.
    const write = (route, pos) => page.evaluate(async (token, route, pos) => {
      const d = await (await fetch('/api/doc?part=', { headers: { 'X-Glose-Token': token } })).json();
      const f = d.files.findIndex((x) => x.path === 'b.md');
      const body = route === '/api/comment'
        ? { file: f, hash: d.files[f].hash, mode: 'point', start: pos, end: pos, text: 'Essai' }
        : { file: f, hash: d.files[f].hash, start: d.annotations.find((a) => a.file === f).start };
      const r = await fetch(route, { method: 'POST', headers: { 'X-Glose-Token': token, 'Content-Type': 'application/json', 'X-Glose-Files': String(d.filesGen) }, body: JSON.stringify(body) });
      return r.status;
    }, p.state.token, route, pos);
    const aligned = async (what) => {
      await page.wait('badge et carte de ' + what, () => !!document.querySelector('.g-ancre') && !!document.querySelector('#marge .carte:not([hidden])'));
      // The card is placed 6px above its badge. A wait, not a nap: the
      // defect left the card about 1000px away for good.
      await page.wait('carte en face de son badge (' + what + ')', () => {
        const b = document.querySelector('.g-ancre').getBoundingClientRect();
        const c = document.querySelector('#marge .carte:not([hidden])').getBoundingClientRect();
        return Math.abs(c.top - (b.top - 6)) <= 2;
      });
    };
    const gone = () => page.wait('annotation retirée de l’affichage', () => !document.querySelector('.g-ancre'));
    assert.equal(await write('/api/comment', A), 200); await aligned('A');
    assert.equal(await write('/api/withdraw'), 200); await gone();
    assert.equal(await write('/api/comment', B), 200); await aligned('B');
    assert.equal(await write('/api/withdraw'), 200); await gone();
    assert.equal(await write('/api/comment', A), 200); await aligned('A, de nouveau');
    assert.equal((await p.read('a.md')).toString(), preface, 'premier fichier intact');
  }],
  ['export en PDF : commentaires en marge, écran rendu tel quel (P13)', async ({ p, page }) => {
    const A = Buffer.from(chapter).indexOf('## Première section') + Buffer.byteLength('## Première section');
    const avant = await p.read('b.md');
    assert.equal(await page.evaluate(async (token, pos) => {
      const d = await (await fetch('/api/doc?part=', { headers: { 'X-Glose-Token': token } })).json();
      const f = d.files.findIndex((x) => x.path === 'b.md');
      const r = await fetch('/api/comment', { method: 'POST', headers: { 'X-Glose-Token': token, 'Content-Type': 'application/json', 'X-Glose-Files': String(d.filesGen) },
        body: JSON.stringify({ file: f, hash: d.files[f].hash, mode: 'point', start: pos, end: pos, text: 'Note d’essai' }) });
      return r.status;
    }, p.state.token, A), 200);
    await page.wait('carte affichée', () => !!document.querySelector('.g-ancre'));
    const avecNote = await p.read('b.md');
    assert.ok(!avant.equals(avecNote));
    await page.evaluate(() => {
      window.__imp = 0;
      window.print = () => { window.dispatchEvent(new Event('beforeprint')); window.__imp++; };
      document.getElementById('exporter').click();
      document.getElementById('export-ok').click();
    });
    await page.wait('impression lancée', () => window.__imp > 0);
    assert.ok(await page.evaluate(() => document.querySelectorAll('.export-copie .note-export').length === 1));
    const pdf = await page.send('browsingContext.print', { context: page.context, page: { width: 21, height: 29.7 } });
    assert.equal(Buffer.from(pdf.data, 'base64').subarray(0, 5).toString(), '%PDF-');
    await page.evaluate(() => window.dispatchEvent(new Event('afterprint')));
    await page.wait('écran rendu', () => !document.querySelector('.export-copie') && !document.body.className.includes('export-'));
    assert.ok(avecNote.equals(await p.read('b.md')), 'exporter n’écrit rien');
  }],
];

if (require.main === module) test('Glose dans Firefox isolé', { timeout: 180000 }, async (t) => {
  if (process.env.GLOSE_FIREFOX === '0') { t.skip('GLOSE_FIREFOX=0 : essais Firefox sautés à la demande'); return; }
  // Resolved, as Glose names its projects: the short names of Windows
  // (RUNNER~1) and links would otherwise make two forms of one path.
  const root = await fs.realpath(await fs.mkdtemp(path.join(process.env.GLOSE_TEST_TMP || os.tmpdir(), 'glose-firefox-tests-')));
  let browser, failed = false;
  t.after(async () => {
    if (browser) await browser.close();
    if (failed || process.env.GLOSE_KEEP_BROWSER_TESTS === '1') console.log('Données et diagnostics conservés : ' + root);
    else await fs.rm(root, { recursive: true, force: true });
  });
  browser = await launchFirefox(root);
  console.log('Navigateur : ' + browser.version.product);
  const chosen = scenarios.filter(([name]) => !process.env.GLOSE_BROWSER_CASE || name.includes(process.env.GLOSE_BROWSER_CASE));
  for (const [name, scenario] of chosen) {
    await t.test(name, { timeout: 60000 }, async () => {
      const dir = await fs.mkdtemp(path.join(root, 'projet-'));
      const p = new Project(dir); let page;
      try {
        await p.write(preface, 'a.md'); await p.write(chapter, 'b.md');
        await p.start(); page = await browser.page(); await page.navigate(p.state.url);
        await scenario({ p, page });
        assert.deepEqual(page.errors, [], 'aucune exception JavaScript ni boîte système');
        p.cli('check');
      } catch (err) {
        failed = true;
        await fs.writeFile(path.join(dir, 'echec.json'), JSON.stringify({ scenario: name, error: err.stack, exceptions: page && page.errors, server: p.log }, null, 2)).catch(() => {});
        throw err;
      } finally {
        if (page) { await page.evaluate(() => document.fonts.ready.then(() => true)).catch(() => {}); await page.close().catch(() => {}); }
        await p.stop();
      }
    });
  }
});
