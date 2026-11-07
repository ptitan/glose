// Tests de bout en bout : vrai exécutable, vrai pandoc, vrai navigateur.
// Aucun serveur ni manuscrit personnel n'est utilisé. Voir docs/TESTS-NAVIGATEUR.md.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs/promises');
const path = require('node:path');
const os = require('node:os');
const net = require('node:net');
const { spawn, spawnSync } = require('node:child_process');
const { launchChrome, until } = require('./cdp.cjs');

const binary = path.resolve(process.env.GLOSE_BIN || path.join(__dirname, '../../bin', process.platform === 'win32' ? 'glose.exe' : 'glose'));
const ordinary = '# Essai\n\nLe **mot important** dans une phrase ordinaire.\n\nLe chat dort.\n\nUn café e\u0301, emoji 👩🏽‍💻 et drapeau 🇫🇷.\n\nDernier paragraphe.\n';

class Project {
  constructor(dir) { this.dir = dir; this.env = { ...process.env, GLOSE_CONFIG_DIR: path.join(dir, 'reglages') }; }
  async start() {
    this.log = ''; this.child = spawn(binary, ['serve', this.dir], { env: this.env, stdio: ['ignore', 'ignore', 'pipe'] });
    this.child.stderr.on('data', (b) => { this.log += b; });
    this.child.on('error', (e) => { this.log += e.stack; });
    this.state = await until('démarrage du serveur Glose de test', async () => {
      const s = JSON.parse(await fs.readFile(path.join(this.dir, '.glose/server.json'), 'utf8'));
      const response = await fetch(`http://127.0.0.1:${s.port}/api/ping`, { headers: { 'X-Glose-Token': s.token }, signal: AbortSignal.timeout(1000) });
      return response.ok && s;
    });
    return this;
  }
  async api(route) {
    const response = await fetch(`http://127.0.0.1:${this.state.port}${route}`, { headers: { 'X-Glose-Token': this.state.token }, signal: AbortSignal.timeout(10000) });
    assert.equal(response.status, 200, `GET ${route}`); return response.json();
  }
  cli(...args) {
    const r = spawnSync(binary, args, { cwd: this.dir, env: this.env, encoding: 'utf8', timeout: 20000 });
    assert.equal(r.status, 0, `${args.join(' ')} : ${r.stderr || r.error}`); return r.stdout;
  }
  read(name = 'a.md') { return fs.readFile(path.join(this.dir, name)); }
  write(text, name = 'a.md') { return fs.writeFile(path.join(this.dir, name), text); }
  async stop() {
    if (!this.child || this.child.exitCode !== null) return;
    if (!this.state) { this.child.kill(); return; }
    try {
      await fetch(`http://127.0.0.1:${this.state.port}/api/stop`, { method: 'POST', headers: { 'X-Glose-Token': this.state.token, 'Content-Type': 'application/json' }, signal: AbortSignal.timeout(3000) });
      await until('arrêt du serveur de test', () => this.child.exitCode !== null, 7000);
    } finally { if (this.child.exitCode === null) this.child.kill(); }
  }
}

async function select(page, text) {
  await page.evaluate((text) => {
    const ps = [...document.querySelectorAll('#texte .rendu:not(.garde) p')].filter((p) => p.textContent.includes(text));
    if (ps.length !== 1) throw new Error('Passage absent ou ambigu dans le rendu : ' + text);
    const p = ps[0], start = p.textContent.indexOf(text), end = start + text.length;
    const walker = document.createTreeWalker(p, NodeFilter.SHOW_TEXT);
    const range = document.createRange(); let offset = 0, started = false;
    while (walker.nextNode()) {
      const n = walker.currentNode, next = offset + n.length;
      if (!started && start < next) { range.setStart(n, start - offset); started = true; }
      if (started && end <= next) { range.setEnd(n, end - offset); break; }
      offset = next;
    }
    document.getElementById('texte').focus();
    const selection = getSelection(); selection.removeAllRanges(); selection.addRange(range);
  }, text);
}
async function openComment(page, text) { await select(page, text); await page.key('c'); await page.wait('panneau Commenter', (id) => document.getElementById(id).open, 'saisie'); }
async function openEdit(page, text) { await select(page, text); await page.key('s'); await page.wait('panneau Suggérer', (id) => document.getElementById(id).open, 'edition'); }
async function savedDraft(page, id) {
  await page.wait('brouillon confirmé', (id) => document.getElementById(id).textContent.startsWith('Brouillon sauvegardé') && !document.getElementById(id).classList.contains('erreur'), id);
}
async function waitCards(page, count) { await page.wait(`${count} cartes`, (n) => document.querySelectorAll('#marge .carte').length === n && document.getElementById('texte').getAttribute('aria-busy') === 'false', count); }
async function openDrafts(page) { await page.click('#bouton-brouillons'); await page.wait('liste des brouillons', () => document.getElementById('brouillons').open); }
async function focusText(page) { await page.evaluate(() => document.getElementById('texte').focus()); }
async function rejectAll(page) {
  while (await page.evaluate(() => !!document.querySelector('#marge button[aria-label="Rejeter"]'))) {
    const n = await page.evaluate(() => document.querySelectorAll('#marge .carte').length);
    await page.click('#marge button[aria-label="Rejeter"]'); await waitCards(page, n - 1);
  }
}

const scenarios = [
  ['sélection, élargissement et octets originaux', async ({ p, page }) => {
    // BOM, CRLF and multibyte characters: never compare a normalized document.
    const original = Buffer.from('\ufeff' + ordinary.replaceAll('\n', '\r\n'));
    await p.write(original); await page.navigate(p.state.url);
    await openComment(page, 'important dans');
    await page.fill('#saisie-texte', 'Préciser cette expression.');
    await page.click('#saisie-ok');
    await page.wait('confirmation de l’élargissement', () => !document.getElementById('saisie-autre').hidden);
    assert.deepEqual(await p.read(), original, 'pas d’élargissement silencieux');
    await page.click('#saisie-autre');
    await page.wait('commentaire enregistré', () => !document.getElementById('saisie').open); await waitCards(page, 1);
    assert.match((await p.read()).toString(), /\{==\*\*mot important\*\* dans==\}/);
    await page.click('#marge button[aria-label="Retirer"]'); await page.wait('confirmation du retrait', () => document.getElementById('boite').open);
    await page.click('#boite-ok'); await waitCards(page, 0);
    assert.deepEqual(await p.read(), original, 'retrait = tous les octets originaux');
    await openComment(page, '👩🏽‍💻'); await page.fill('#saisie-texte', 'Emoji entier.'); await page.click('#saisie-ok');
    await page.wait('annotation emoji enregistrée', () => !document.getElementById('saisie').open); await waitCards(page, 1);
    assert.match((await p.read()).toString(), /\{==👩🏽‍💻==\}/);
    await focusText(page); await page.key('z', 2); await waitCards(page, 0);
    assert.deepEqual(await p.read(), original);
  }],
  ['édition visuelle, source, aperçu, décisions et annulation', async ({ p, page }) => {
    const original = await p.read();
    await openEdit(page, 'Le chat dort.');
    await page.click('#edition-mef');
    await page.fill('#edition-visuel', 'Le chien _littéral_ dort 👩🏽‍💻.');
    await page.fill('#edition-note', 'Réécriture d’essai.');
    await page.click('#edition-source');
    const source = await page.evaluate(() => document.getElementById('edition-texte').value);
    assert.equal(source, 'Le chien \\_littéral\\_ dort 👩🏽‍💻.');
    await page.click('#edition-mef'); await page.click('#edition-source');
    assert.equal(await page.evaluate(() => document.getElementById('edition-texte').value), source, 'pas de double échappement');
    await page.click('#edition-tenant');
    // Freeze just the 500 ms auto-preview timer and hold the next response.
    // The explicit Preview gesture must cancel that obsolete timer instead
    // of letting it invalidate the result requested by the author.
    await page.evaluate(() => {
      const set = window.setTimeout, clear = window.clearTimeout, fetch = window.fetch;
      window.__autoPreviewTimers = new Set();
      window.setTimeout = (fn, delay, ...args) => {
        const id = set(fn, delay === 500 ? 60000 : delay, ...args);
        if (delay === 500) window.__autoPreviewTimers.add(id);
        return id;
      };
      window.clearTimeout = (id) => { window.__autoPreviewTimers.delete(id); clear(id); };
      let hold = true;
      window.fetch = async (...args) => {
        const r = await fetch(...args);
        if (args[0] === '/api/preview' && hold) {
          hold = false;
          await new Promise((resolve) => { window.__releasePreview = resolve; });
        }
        return r;
      };
      window.__restorePreviewSchedule = () => { window.setTimeout = set; window.clearTimeout = clear; window.fetch = fetch; };
    });
    await page.fill('#edition-texte', source);
    await page.click('#edition-voir');
    await page.wait('réponse d’aperçu retenue', () => typeof window.__releasePreview === 'function');
    assert.equal(await page.evaluate(() => window.__autoPreviewTimers.size), 0, 'le bouton Aperçu annule la mise à jour automatique périmée');
    await page.evaluate(() => window.__releasePreview());
    await page.wait('aperçu calculé', () => !document.getElementById('edition-apercu').hidden && document.getElementById('edition-rendu').textContent.includes('_littéral_'));
    await page.evaluate(() => window.__restorePreviewSchedule());
    assert.deepEqual(await p.read(), original, 'l’aperçu n’écrit rien');
    await page.click('#edition-ok'); await page.wait('réécriture enregistrée', () => !document.getElementById('edition').open); await waitCards(page, 1);
    await focusText(page); await page.key('j'); await page.key('a'); await waitCards(page, 0);
    const expected = Buffer.from(original.toString().replace('Le chat dort.', source));
    assert.deepEqual(await p.read(), expected);
    await focusText(page); await page.key('z', 2); await waitCards(page, 1);
    await page.key('y', 2); await waitCards(page, 0); assert.deepEqual(await p.read(), expected);
    await page.key('z', 2); await waitCards(page, 1);
    await focusText(page); await page.key('j'); await page.key('x'); await waitCards(page, 0);
    assert.deepEqual(await p.read(), original, 'rejeter restitue les octets d’origine');
  }],
  ['changement externe pendant une saisie et conflit conservé', async ({ p, page }) => {
    await openEdit(page, 'Le chat dort.'); await page.click('#edition-source');
    await page.fill('#edition-texte', 'Une réécriture à ne pas perdre.');
    await page.fill('#edition-note', 'Message à garder également.'); await savedDraft(page, 'edition-brouillon');
    const external = Buffer.from((await p.read()).toString().replace('Dernier paragraphe.', 'Changement externe observé.'));
    await p.write(external);
    await page.wait('suivi automatique du disque', () => document.querySelector('#texte').textContent.includes('Changement externe observé.'));
    assert.equal(await page.evaluate(() => document.getElementById('edition-texte').value), 'Une réécriture à ne pas perdre.');
    await page.click('#edition-ok'); await page.wait('conflit affiché', () => document.getElementById('edition-message').textContent.includes('changé'));
    assert.equal(await page.evaluate(() => document.getElementById('edition').open), true);
    assert.deepEqual(await p.read(), external, 'le conflit n’écrase pas l’extérieur');
    await page.click('#edition-fermer'); await openDrafts(page);
    assert.equal(await page.evaluate(() => document.querySelector('#brouillons .actions button').disabled), true);
    assert.deepEqual(await page.evaluate(() => [...document.querySelectorAll('#brouillons textarea')].map((e) => e.value)), ['Une réécriture à ne pas perdre.', 'Message à garder également.']);
  }],
  ['raccourcis inertes pendant la saisie et composition IME', async ({ p, page }) => {
    const original = await p.read();
    await openEdit(page, 'Le chat dort.'); await page.click('#edition-source');
    await page.fill('#edition-texte', '');
    for (const key of ['a', 'x', 'r', 'e', 'j', 'k', '1', '2', '3']) await page.key(key);
    assert.equal(await page.evaluate(() => document.getElementById('edition-texte').value), 'axrejk123');
    assert.deepEqual(await p.read(), original, 'aucun raccourci de décision pendant la frappe');
    await page.click('#edition-mef'); await page.fill('#edition-visuel', '');
    // Real browser composition events; shortcut guard also checked with an
    // explicit composing key because CDP does not emulate a physical OS IME.
    await page.send('Input.imeSetComposition', { text: 'に', selectionStart: 1, selectionEnd: 1 });
    await page.evaluate(() => document.getElementById('edition-visuel').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true, isComposing: true, keyCode: 229, bubbles: true })));
    assert.equal(await page.evaluate(() => document.getElementById('edition').open), true);
    await page.send('Input.insertText', { text: '日本語 👩🏽‍💻' });
    await page.click('#edition-source');
    assert.match(await page.evaluate(() => document.getElementById('edition-texte').value), /日本語 👩🏽‍💻/);
    await savedDraft(page, 'edition-brouillon'); assert.deepEqual(await p.read(), original);
  }],
  ['navette, réponse de l’agent et reprise de main', async ({ p, page }) => {
    const original = await p.read();
    await page.click('#remettre'); await page.wait('remise ouverte', () => document.getElementById('remise').open);
    await page.fill('#remise-note', 'Corrige la phrase.'); await page.click('#remise-ok');
    await page.wait('main à l’agent', () => document.body.classList.contains('main-agent'));
    await p.write(original.toString().replace('Le chat dort.', 'Le chien dort.'));
    p.cli('done', '--note', 'Correction proposée.');
    await page.wait('main rendue automatiquement', () => !document.body.classList.contains('main-agent')); await waitCards(page, 1);
    await rejectAll(page); assert.deepEqual(await p.read(), original);
    await page.click('#remettre'); await page.wait('seconde remise ouverte', () => document.getElementById('remise').open); await page.click('#remise-ok');
    await page.wait('seconde main à l’agent', () => document.body.classList.contains('main-agent'));
    await p.write(original.toString().replace('Le chat dort.', 'Le chat veille.'));
    await page.click('#remettre'); await page.wait('reprise ouverte', () => document.getElementById('remise').open); await page.click('#remise-ok');
    await page.wait('main reprise', () => !document.body.classList.contains('main-agent')); await waitCards(page, 1);
    await rejectAll(page); assert.deepEqual(await p.read(), original);
    const log = JSON.parse(p.cli('log', '--json')); assert.ok(log);
    // La frise (onglet Navette) se construit avec les fonctions de donnees.js :
    // une main par nœud, des remises datées, le détail d'une main au clic.
    await page.click('#onglet-navette');
    await page.wait('frise affichée', () => document.querySelectorAll('#historique svg.frise g.noeud').length >= 4);
    assert.ok(await page.evaluate(() => document.querySelectorAll('#historique .remise-heure').length >= 3), 'remises datées');
    await page.click('#historique g.noeud.agent');
    await page.wait('détail d’une main', () => /suggestion|converties|Rien d'écrit/.test(document.querySelector('#historique .frise-detail')?.textContent || ''));
  }],
  ['brouillons : rechargement, nouveau port et ordre des fichiers', async ({ p, page }) => {
    const original = await p.read();
    await openEdit(page, 'Le chat dort.'); await page.click('#edition-source');
    await page.fill('#edition-texte', ''); // an empty rewrite is a proposed deletion
    await page.fill('#edition-note', 'Suppression à reprendre.'); await savedDraft(page, 'edition-brouillon');
    await page.navigate(p.state.url); await openDrafts(page);
    assert.deepEqual(await page.evaluate(() => [...document.querySelectorAll('#brouillons textarea')].map((e) => e.value)), ['', 'Suppression à reprendre.']);
    await p.stop();
    await p.write('# Autre fichier\n\nAutre texte.\n', 'b.md');
    await p.write(JSON.stringify({ files: ['b.md', 'a.md'] }), 'glose.json');
    const oldPort = p.state.port;
    // Reserve the old port so the test genuinely changes origin, even if the
    // OS would otherwise happen to allocate the same ephemeral port again.
    const reservation = net.createServer((socket) => socket.destroy());
    await new Promise((resolve, reject) => { reservation.once('error', reject); reservation.listen(oldPort, '127.0.0.1', resolve); });
    try { await p.start(); } finally { await new Promise((resolve) => reservation.close(resolve)); }
    // The disk must suffice: clear the old origin's emergency storage.
    await page.evaluate(() => localStorage.clear());
    await page.navigate(p.state.url); await openDrafts(page);
    await page.click('#brouillons .actions button'); await page.wait('brouillon repris', () => document.getElementById('edition').open);
    await page.click('#edition-source');
    assert.equal(await page.evaluate(() => document.getElementById('edition-texte').value), '');
    assert.equal(await page.evaluate(() => document.getElementById('edition-note').value), 'Suppression à reprendre.');
    assert.deepEqual(await p.read(), original);
    assert.notEqual(p.state.port, oldPort, 'nouvelle origine de serveur');
  }],
  ['réponse réseau perdue après écriture : aucun double envoi', async ({ p, page }) => {
    await openComment(page, 'Le chat dort.'); await page.fill('#saisie-texte', 'Commentaire unique.');
    await page.evaluate(() => {
      const original = window.fetch;
      window.fetch = async (...args) => { const result = await original(...args); if (args[0] === '/api/comment') throw new TypeError('Réponse perdue après écriture'); return result; };
    });
    await page.click('#saisie-ok'); await page.wait('erreur réseau affichée', () => !document.getElementById('saisie-ok').disabled && document.getElementById('saisie-message').textContent.includes('ne répond pas'));
    await page.click('#saisie-ok'); await page.wait('second envoi refusé', () => document.getElementById('saisie-message').textContent.includes('déjà') || document.getElementById('saisie-message').textContent.includes('a été tenté'));
    assert.equal(JSON.parse(p.cli('list', '--json')).annotations.length, 1);
    await page.click('#saisie-fermer'); await openDrafts(page);
    assert.equal(await page.evaluate(() => document.querySelector('#brouillons .actions button').disabled), true);
    assert.match(await page.evaluate(() => document.querySelector('#brouillons').textContent), /enregistrement a été tenté/);
  }],
  ['onglet Fichiers : partie affichée et annotation d’un autre fichier', async ({ p, page }) => {
    await p.write('# Autre fichier\n\nTexte de b avec une {>>remarque dans b<<} ici.\n', 'b.md');
    await page.navigate(p.state.url);
    await page.wait('deux fichiers affichés', () => document.querySelectorAll('.glose-file').length === 2);
    // Rangée d'icônes : une seule ligne, rien ne déborde, le titre suit
    // l'onglet ; Alt+3 ouvre les annotations.
    const rangee = await page.evaluate(() => {
      const b = [...document.querySelectorAll('#onglets [role=tab]')].map((x) => x.getBoundingClientRect());
      const o = document.getElementById('onglets');
      return { tops: new Set(b.map((r) => Math.round(r.top))).size, haut: Math.max(...b.map((r) => r.height)), deborde: o.scrollWidth > o.clientWidth };
    });
    assert.deepEqual([rangee.tops, rangee.haut <= 40, rangee.deborde], [1, true, false], 'onglets sur une ligne : ' + JSON.stringify(rangee));
    await page.key('3', 1);
    await page.wait('Alt+3 : annotations', () => document.getElementById('onglet-annotations').getAttribute('aria-selected') === 'true' &&
      /^Annotations · 1$/.test(document.getElementById('onglet-titre').textContent));
    // Colonne redimensionnable : flèche droite (avec Maj) puis double-clic.
    const largeur = () => page.evaluate(() => Math.round(document.getElementById('cote').getBoundingClientRect().width));
    const avant = await largeur();
    await page.evaluate(() => { const p = document.getElementById('poignee-colonne'); p.focus();
      p.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', shiftKey: true, bubbles: true })); });
    await page.wait('colonne élargie', (a) => Math.round(document.getElementById('cote').getBoundingClientRect().width) > a + 20, avant);
    // Glisser à la souris (vrais événements du navigateur).
    const pg = await page.evaluate(() => { const r = document.getElementById('poignee-colonne').getBoundingClientRect(); return { x: r.x + r.width / 2, y: r.y + 200 }; });
    const souris = (type, x) => page.send('Input.dispatchMouseEvent', { type, x, y: pg.y, button: 'left', buttons: type === 'mouseReleased' ? 0 : 1, clickCount: 1 });
    await souris('mouseMoved', pg.x); await souris('mousePressed', pg.x);
    for (let dx = 20; dx <= 120; dx += 20) await souris('mouseMoved', pg.x + dx);
    await souris('mouseReleased', pg.x + 120);
    await page.wait('colonne glissée et retenue', (a) => Math.round(document.getElementById('cote').getBoundingClientRect().width) > a + 60 &&
      Number(localStorage.getItem('glose-colonne')) > 0, avant);
    // Un simple clic sur le bord gauche de la poignée (6 px avant la fin de la
    // colonne) ne change pas la largeur.
    const glissee = await page.evaluate(() => [document.getElementById('cote').getBoundingClientRect().width, localStorage.getItem('glose-colonne')]);
    const bord = await page.evaluate(() => { const r = document.getElementById('poignee-colonne').getBoundingClientRect(); return r.left + 0.5; });
    await souris('mouseMoved', bord); await souris('mousePressed', bord); await souris('mouseReleased', bord);
    await page.wait('clic sans effet, guide retiré', (g) => document.getElementById('cote').getBoundingClientRect().width === g[0] &&
      localStorage.getItem('glose-colonne') === g[1] && !document.querySelector('.guide-colonne'), glissee);
    await page.evaluate(() => document.getElementById('poignee-colonne').dispatchEvent(new MouseEvent('dblclick', { bubbles: true })));
    await page.wait('largeur par défaut', (a) => Math.round(document.getElementById('cote').getBoundingClientRect().width) === a, avant);
    await page.click('#onglet-fichiers');
    await page.wait('titre Fichiers', () => document.getElementById('onglet-titre').textContent === 'Fichiers');
    await page.wait('arbre', () => document.querySelectorAll('#fichiers ul[role=tree] li.doc a.partie').length === 2);
    await page.evaluate(() => [...document.querySelectorAll('#fichiers ul[role=tree] li.doc a.partie')].find((a) => a.textContent.startsWith('a')).click());
    await page.wait('seul a.md affiché', () => document.querySelectorAll('.glose-file').length === 1 && document.querySelector('.glose-file').dataset.path === 'a.md');
    await page.click('#onglet-annotations');
    await page.wait('annotation de b.md listée ailleurs', () => document.querySelectorAll('#annotations-liste a.ailleurs').length === 1);
    await page.click('#annotations-liste a.ailleurs');
    await page.wait('b.md affiché, carte active', () => document.querySelector('.glose-file')?.dataset.path === 'b.md' && /remarque dans b/.test(document.querySelector('.carte.active')?.textContent || ''));
    await page.click('#onglet-fichiers');
    await page.click('#fichiers ul.tete a.partie');
    await page.wait('tout le projet', () => document.querySelectorAll('.glose-file').length === 2);
  }],
  ['navigation, recherche globale et archives', async ({ p, page }) => {
    await p.stop();
    const a = '# Introduction\n\nVoir [le chapitre](chapitre/b%20espace.md#suite).\n\nVoir [le renvoi caché](#suite).\n';
    const b = '# Introduction\n\n' + 'Un paragraphe ordinaire pour éloigner la cible.\n\n'.repeat(35) + '## Suite {#suite}\n\nCible introuvable ailleurs café.\n\n![Figure](img/figure.png)\n\nVoir [mon début](#introduction).\n\n' + 'Un dernier paragraphe après la cible.\n\n'.repeat(15);
    await p.write(a);
    await fs.mkdir(path.join(p.dir, 'chapitre/img'), { recursive: true });
    await p.write(b, 'chapitre/b espace.md');
    await fs.writeFile(path.join(p.dir, 'chapitre/img/figure.png'), Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aWQAAAABJRU5ErkJggg==', 'base64'));
    await p.write(JSON.stringify({ version: 1, sections: { 'a.md#gl-introduction': { status: 'relue', title: 'Introduction' } } }), '.glose/progression.json');
    await p.start(); await page.navigate(p.state.url);
    await page.wait('progression antérieure conservée', () => document.querySelector('#sommaire .ligne.relue'));
    await page.click('#onglet-fichiers');
    await page.evaluate(() => [...document.querySelectorAll('#fichiers a.partie')].find((a) => a.textContent === 'a').click());
    await page.wait('a seul', () => document.querySelectorAll('#texte .rendu:not(.garde) .glose-file').length === 1 && document.querySelector('#texte .rendu:not(.garde) .glose-file').dataset.path === 'a.md');
    await page.click('#texte a[href*="b%20espace.md"]');
    await page.wait('chapitre caché ouvert', () => document.querySelector('#texte .rendu:not(.garde) .glose-file')?.dataset.path === 'chapitre/b espace.md');
    await page.wait('cible du lien sous la barre', () => { const r = document.querySelector('#texte .rendu:not(.garde) h2')?.getBoundingClientRect(); return r && r.top >= 70 && r.top < 250; });
    await page.wait('image relative chargée', () => { const i = document.querySelector('#texte .rendu:not(.garde) img'); return i?.complete && i.naturalWidth === 1 && i.src.includes('/chapitre/img/figure.png?t='); });
    await page.click('#texte .rendu:not(.garde) a[href*="introduction"]');
    await page.wait('renvoi au titre du bon fichier', () => { const r = document.querySelector('#texte .rendu:not(.garde) h1')?.getBoundingClientRect(); return r && r.top >= 70 && r.top < 250; });
    await page.click('#onglet-fichiers');
    await page.click('#fichiers ul.tete a.partie');
    await page.wait('ancres uniques', () => { const h = [...document.querySelectorAll('#texte .rendu:not(.garde) h1')]; return h.length === 2 && new Set(h.map((x) => x.id)).size === 2; });
    await page.evaluate(() => [...document.querySelectorAll('#fichiers a.partie')].find((a) => a.textContent === 'a').click());
    await page.wait('retour a', () => document.getElementById('texte').getAttribute('aria-busy') === 'false' && document.querySelectorAll('#texte .rendu:not(.garde) .glose-file').length === 1 && document.querySelector('#texte .rendu:not(.garde) .glose-file')?.dataset.path === 'a.md');
    await page.click('#texte a[href*="suite"]:not([href*=".md"])');
    await page.wait('fragment caché suivi', () => document.querySelector('#texte .rendu:not(.garde) .glose-file')?.dataset.path === 'chapitre/b espace.md');
    await page.click('#onglet-fichiers');
    await page.evaluate(() => [...document.querySelectorAll('#fichiers a.partie')].find((a) => a.textContent === 'a').click());
    await page.wait('recherche depuis a', () => document.querySelector('#texte .rendu:not(.garde) .glose-file')?.dataset.path === 'a.md');
    await page.click('#bouton-recherche');
    await page.fill('#recherche-texte', 'CIBLE INTROUVABLE');
    await page.wait('résultat global', () => document.querySelectorAll('.resultat-recherche').length === 1);
    if (process.env.GLOSE_KEEP_BROWSER_TESTS === '1') await fs.writeFile(path.join(p.dir, 'recherche.png'), Buffer.from((await page.send('Page.captureScreenshot')).data, 'base64'));
    await p.write(b + '\n', 'chapitre/b espace.md');
    await page.click('.resultat-recherche');
    await page.wait('résultat périmé refusé', () => document.getElementById('avis').textContent.includes('relancez la recherche'));
    assert.equal(await page.evaluate(() => !!document.querySelector('#texte .g-recherche')), false);
    await p.write(b, 'chapitre/b espace.md');
    await focusText(page); await page.key('f', 10);
    await page.wait('raccourci de recherche globale', () => document.getElementById('recherche').open);
    await page.wait('résultat actualisé', () => document.querySelectorAll('.resultat-recherche').length === 1);
    await page.click('.resultat-recherche');
    await page.wait('résultat ouvert et surligné', () => !document.getElementById('recherche').open && document.querySelector('#texte .g-recherche')?.textContent === 'Cible');
    assert.equal((await p.read()).toString(), a);
    assert.equal((await p.read('chapitre/b espace.md')).toString(), b);
    await openComment(page, 'Cible introuvable');
    await page.fill('#saisie-texte', 'Discussion archivée <script>jamais exécuté</script>.');
    await page.click('#saisie-ok'); await waitCards(page, 1);
    await page.click('#remettre'); await page.wait('remise avant résolution', () => document.getElementById('remise').open); await page.click('#remise-ok');
    await page.wait('discussion remise à l’agent', () => document.body.classList.contains('main-agent'));
    p.cli('done');
    await page.wait('discussion rendue à l’auteur', () => !document.body.classList.contains('main-agent'));
    await page.click('#marge button[aria-label="Résoudre"]');
    await waitCards(page, 0);
    await page.click('#onglet-navette');
    await page.wait('discussion archivée disponible', () => document.querySelector('.archive-ouvrir')?.textContent.includes('Discussion résolue'));
    await page.click('.archive-ouvrir');
    await page.wait('texte intégral de la discussion', () => document.getElementById('archive').open && document.querySelector('.archive-texte')?.textContent.includes('<script>jamais exécuté</script>') && !document.querySelector('.archive-texte script'));
    await page.key('a');
    assert.equal(await page.evaluate(() => document.getElementById('archive').open), true, 'pas de décision pendant la consultation');
    await page.evaluate(async () => { await Promise.all(document.getElementById('archive').getAnimations().map((a) => a.finished)); });
    if (process.env.GLOSE_KEEP_BROWSER_TESTS === '1') await fs.writeFile(path.join(p.dir, 'archives.png'), Buffer.from((await page.send('Page.captureScreenshot')).data, 'base64'));
    assert.equal((await p.read('chapitre/b espace.md')).toString(), b, 'résolution = source exacte');
  }],
];

if (require.main === module) test('Glose dans un navigateur isolé', { timeout: 240000 }, async (t) => {
  const root = await fs.mkdtemp(path.join(process.env.GLOSE_TEST_TMP || os.tmpdir(), 'glose-browser-tests-'));
  let browser, failed = false;
  t.after(async () => {
    if (browser) await browser.close();
    if (failed || process.env.GLOSE_KEEP_BROWSER_TESTS === '1') console.log('Données et diagnostics conservés : ' + root);
    else await fs.rm(root, { recursive: true, force: true });
  });
  browser = await launchChrome(root);
  console.log('Navigateur : ' + browser.version.product);
  const chosen = scenarios.filter(([name]) => !process.env.GLOSE_BROWSER_CASE || name.includes(process.env.GLOSE_BROWSER_CASE));
  assert.ok(chosen.length, 'aucun scénario correspondant à GLOSE_BROWSER_CASE');
  for (const [name, scenario] of chosen) {
    await t.test(name, { timeout: 45000 }, async () => {
      const dir = await fs.mkdtemp(path.join(root, 'projet-'));
      const p = new Project(dir); let page;
      try {
        await p.write(ordinary); await p.start(); page = await browser.page(); await page.navigate(p.state.url);
        await scenario({ p, page });
        assert.deepEqual(page.errors, [], 'aucune exception JavaScript ni boîte système');
        p.cli('check');
      } catch (err) {
        failed = true;
        if (page) {
          const state = await page.evaluate(() => ({ ...window.__gloseTest, dialogs: [...document.querySelectorAll('dialog')].map((d) => ({ id: d.id, open: d.open })), message: document.getElementById('saisie-message').textContent })).catch(() => null);
          await fs.writeFile(path.join(dir, 'echec.json'), JSON.stringify({ scenario: name, error: err.stack, exceptions: page.errors, server: p.log, state }, null, 2));
          try { const shot = await page.send('Page.captureScreenshot'); await fs.writeFile(path.join(dir, 'echec.png'), Buffer.from(shot.data, 'base64')); } catch (_) {}
        }
        throw err;
      } finally {
        if (page) await page.close().catch(() => {});
        await p.stop();
      }
    });
  }
});

module.exports = { Project, select, openComment, openEdit, savedDraft, waitCards, openDrafts, focusText, rejectAll };
