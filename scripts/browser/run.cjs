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
  constructor(dir) {
    this.dir = dir;
    // Rien de la session de l'hôte : ni nom d'agent, ni modèle, ni session OpenCode.
    this.env = { ...process.env, GLOSE_CONFIG_DIR: path.join(dir, 'reglages') };
    for (const k of ['GLOSE_AGENT', 'GLOSE_MODEL', 'GLOSE_PASS', 'OPENCODE_SESSION_ID']) delete this.env[k];
  }
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
// P20: search is a button (in « Plus » in narrower windows) that becomes the field.
async function openSearch(page) {
  if (await page.evaluate(() => document.getElementById('ouvrir-recherche').getClientRects().length > 0)) await page.click('#ouvrir-recherche');
  else { await page.click('#plus-bouton'); await page.click('#plus-menu [data-relais="ouvrir-recherche"]'); }
  await page.wait('champ de recherche', () => document.activeElement.id === 'recherche-texte');
}
async function openDrafts(page) { await page.click('#bouton-brouillons'); await page.wait('liste des brouillons', () => document.getElementById('brouillons').open); }
async function focusText(page) { await page.evaluate(() => document.getElementById('texte').focus()); }
async function rejectAll(page) {
  while (await page.evaluate(() => !!document.querySelector('#marge button[aria-label="Rejeter"]'))) {
    const n = await page.evaluate(() => document.querySelectorAll('#marge .carte').length);
    await page.click('#marge button[aria-label="Rejeter"]'); await waitCards(page, n - 1);
  }
}

// Scenarios whose fake agents are shell scripts: skipped under Windows,
// where they cannot run (said in the report, never passed silently).
const shellAgents = new Set(['outils requis et essai d’un faux agent (P10.4)', 'Glose lance l’agent à la remise (P10.5)',
  'destinataires : Réponse par, @Nom, deux agents à la suite (P10.6)']);

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
  ['modifier son commentaire et sa réponse, annuler, rétablir, puis remettre', async ({ p, page }) => {
    const carte = () => page.evaluate(() => document.querySelector('#marge .carte')?.textContent || '');
    await openComment(page, 'Le chat dort.'); await page.fill('#saisie-texte', 'Premier jet.'); await page.click('#saisie-ok');
    await page.wait('commentaire enregistré', () => !document.getElementById('saisie').open); await waitCards(page, 1);
    await page.click('#marge button[aria-label="Modifier"]');
    await page.wait('panneau Modifier prérempli', () => document.getElementById('saisie').open && document.getElementById('saisie-texte').value === 'Premier jet.');
    await page.fill('#saisie-texte', 'Version revue.'); await page.click('#saisie-ok');
    await page.wait('commentaire modifié', () => !document.getElementById('saisie').open && /Version revue\./.test(document.querySelector('#marge .carte')?.textContent || ''));
    assert.match((await p.read()).toString(), /\{==Le chat dort\.==\}\{>>Version revue\.<<\}\{>>glose:v1 id=\w+ by=author<<\}/);
    await page.click('#marge button[aria-label="Répondre"]'); await page.wait('panneau Répondre', () => document.getElementById('saisie').open);
    await page.fill('#saisie-texte', 'Ma réponse.'); await page.click('#saisie-ok');
    await page.wait('réponse enregistrée', () => !document.getElementById('saisie').open && /Ma réponse\./.test(document.querySelector('#marge .carte')?.textContent || ''));
    await page.wait('capsule : 1 commentaire et 1 réponse à remettre', () => document.querySelector('#puces .comments b')?.textContent === '1' && document.querySelector('#puces .replies b')?.textContent === '1' && !document.querySelector('#puces .proposals'));
    await page.click('#marge button[aria-label="Modifier votre réponse"]');
    await page.wait('réponse préremplie', () => document.getElementById('saisie').open && document.getElementById('saisie-texte').value === 'Ma réponse.');
    await page.fill('#saisie-texte', 'Réponse revue.'); await page.click('#saisie-ok');
    await page.wait('réponse modifiée', () => !document.getElementById('saisie').open && /Réponse revue\./.test(document.querySelector('#marge .carte')?.textContent || ''));
    const revue = await p.read();
    assert.match(revue.toString(), /\{>>Version revue\.<<\}\{>>glose:v1 [^<]+<<\}\{>>Réponse revue\.<<\}\{>>glose:v1 [^<]+<<\}/);
    await focusText(page); await page.key('z', 2);
    await page.wait('modification annulée', () => /Ma réponse\./.test(document.querySelector('#marge .carte')?.textContent || ''));
    assert.equal((await p.read()).toString(), revue.toString().replace('Réponse revue.', 'Ma réponse.'), 'annuler rend le texte précédent, rien d’autre');
    await focusText(page); await page.key('y', 2);
    await page.wait('modification rétablie', () => /Réponse revue\./.test(document.querySelector('#marge .carte')?.textContent || ''));
    assert.deepEqual(await p.read(), revue);
    await page.click('#marge button[aria-label="Retirer votre réponse"]'); await page.wait('confirmation du retrait', () => document.getElementById('boite').open);
    await page.click('#boite-ok'); await page.wait('réponse retirée', () => !/Réponse revue\./.test(document.querySelector('#marge .carte')?.textContent || ''));
    assert.ok(!(await p.read()).toString().includes('Réponse revue.'));
    const log = JSON.parse(p.cli('log', '--json'));
    const versions = log.turns.flatMap((t) => t.operations || []).filter((o) => o.kind === 'amend').flatMap((o) => o.revised || []).map((r) => r.text);
    assert.deepEqual(versions, ['Premier jet.', 'Ma réponse.', 'Réponse revue.', 'Ma réponse.'], 'versions précédentes au journal');
    // Remis puis repris : le commentaire appartient à la discussion, il ne se modifie plus.
    await page.click('#remettre'); await page.wait('remise ouverte', () => document.getElementById('remise').open); await page.click('#remise-ok');
    await page.wait('main à l’agent', () => document.body.classList.contains('main-agent'));
    await page.click('#remettre'); await page.wait('reprise ouverte', () => document.getElementById('remise').open); await page.click('#remise-ok');
    await page.wait('main reprise', () => !document.body.classList.contains('main-agent')); await waitCards(page, 1);
    assert.ok(!(await page.evaluate(() => !!document.querySelector('#marge button[aria-label="Modifier"]'))), 'plus de Modifier après la remise');
    assert.match(await carte(), /Version revue\./);
  }],
  ['barre du haut : colonne repliée, rail, recherche au clavier, pied de page', async ({ p, page }) => {
    const visible = (id) => page.evaluate((id) => !document.getElementById(id).hidden && getComputedStyle(document.getElementById(id)).display !== 'none', id);
    assert.match(await page.evaluate(() => document.getElementById('etat').textContent), /1 fichier · 0 annotation · rendu en \d+ ms/);
    assert.ok(await page.evaluate(() => { const r = document.getElementById('pied').getBoundingClientRect(); return Math.abs(r.bottom - innerHeight) < 2; }), 'pied de page en bas de la fenêtre');
    await page.click('#bouton-sommaire');
    await page.wait('colonne repliée', () => document.getElementById('cote').hidden && !document.getElementById('rail').hidden);
    await page.navigate(p.state.url); await waitCards(page, 0);
    assert.ok(await visible('rail') && !(await visible('cote')), 'colonne repliée retenue');
    await page.click('#rail button[data-rail="annotations"]');
    await page.wait('onglet Annotations depuis le rail', () => !document.getElementById('cote').hidden && document.getElementById('onglet-annotations').getAttribute('aria-selected') === 'true');
    await focusText(page); await page.key('0', 1);
    await page.wait('Alt+0 replie', () => document.getElementById('cote').hidden);
    await focusText(page); await page.key('0', 1);
    await page.wait('Alt+0 rouvre', () => !document.getElementById('cote').hidden);
    // La barre reste en haut quand le texte défile.
    await page.evaluate(() => { document.getElementById('texte').style.minHeight = '3000px'; window.scrollTo(0, 800); });
    await page.wait('barre collante', () => Math.abs(document.getElementById('barre').getBoundingClientRect().top) < 1 && document.body.classList.contains('defile'));
    await page.evaluate(() => { document.getElementById('texte').style.minHeight = ''; window.scrollTo(0, 0); });
    // Recherche : la saisie reste dans le champ ; flèches, Entrée, Échap.
    assert.equal(await page.evaluate(() => getComputedStyle(document.querySelector('.ancre-recherche')).display), 'none', 'le champ n’apparaît qu’à la demande');
    await openSearch(page); await page.fill('#recherche-texte', 'chat');
    await page.wait('résultats sous le champ', () => !document.getElementById('recherche').hidden && document.querySelectorAll('.resultat-recherche').length === 1);
    assert.equal(await page.evaluate(() => document.activeElement.id), 'recherche-texte', 'la saisie reste dans le champ');
    await page.key('ArrowDown');
    assert.ok(await page.evaluate(() => document.activeElement.classList.contains('resultat-recherche')), 'flèche vers le premier résultat');
    await page.key('Escape');
    await page.wait('Échap replie', () => document.getElementById('recherche').hidden && document.activeElement.id === 'recherche-texte');
    await page.key('Enter');
    await page.wait('Entrée relance', () => !document.getElementById('recherche').hidden && document.querySelectorAll('.resultat-recherche').length === 1);
    await page.key('ArrowDown'); await page.key('Enter');
    await page.wait('résultat ouvert', () => document.getElementById('recherche').hidden && document.querySelector('#texte .g-recherche')?.textContent === 'chat');
    // Two Escapes: the list, then the field, which gives way to the button again.
    await openSearch(page); await page.key('Escape'); await page.key('Escape');
    await page.wait('champ refermé', () => !document.body.classList.contains('recherche-ouverte'));
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
  ['modification directe : sans proposition, annulée puis rétablie (D-57)', async ({ p, page }) => {
    const original = Buffer.from('﻿' + ordinary.replaceAll('\n', '\r\n'));
    await p.write(original); await page.navigate(p.state.url);
    await openEdit(page, 'Le chat dort.'); await page.click('#edition-source');
    await page.fill('#edition-texte', 'Le chien dort bien.');
    // A message for the agent asks for an answer: no direct change with it.
    await page.fill('#edition-note', 'Pourquoi ?');
    await page.wait('bouton désactivé par le message', () => document.getElementById('edition-direct').disabled);
    await page.fill('#edition-note', '');
    await page.wait('bouton rétabli', () => !document.getElementById('edition-direct').disabled);
    await page.click('#edition-direct');
    await page.wait('modification appliquée', () => !document.getElementById('edition').open); await waitCards(page, 0);
    const expected = Buffer.from(original.toString().replace('Le chat dort.', 'Le chien dort bien.'));
    assert.deepEqual(await p.read(), expected, 'texte modifié, aucune marque, autres octets intacts');
    await page.click('#onglet-annotations');
    await page.wait('« Imposé » dans Traitées', () => document.getElementById('annotations-traitees').textContent.includes('Imposé'));
    await focusText(page); await page.key('z', 2);
    await page.wait('annulée', () => document.querySelector('#texte').textContent.includes('Le chat dort.'));
    assert.deepEqual(await p.read(), original, 'annuler rend les octets d’origine');
    await page.wait('sortie de Traitées', () => !document.getElementById('annotations-traitees').textContent.includes('Imposé'));
    await page.key('y', 2);
    await page.wait('rétablie', () => document.querySelector('#texte').textContent.includes('Le chien dort bien.'));
    assert.deepEqual(await p.read(), expected);
    assert.match(p.cli('status').toString(), /Modifications imposées par l'auteur : 2/);
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
  ['présence de l’agent dans la capsule (P10.1)', async ({ p, page }) => {
    const original = await p.read();
    const presence = (activity, ago) => p.write(JSON.stringify({ name: 'OpenCode', since: new Date(Date.now() - ago).toISOString(),
      seen: new Date(Date.now() - ago).toISOString(), activity, turn: 0 }), '.glose/agent.json');
    const capsule = () => page.evaluate(() => ({ text: document.getElementById('navette').textContent,
      absent: document.getElementById('carte-tour').classList.contains('absent') }));
    await presence('wait', 0);
    await page.wait('agent qui attend', () => /OpenCode attend/.test(document.getElementById('navette').textContent));
    assert.equal((await capsule()).absent, false);
    await presence('wait', 120000);
    await page.wait('plus personne', () => /Aucun agent (à l’écoute|à l'écoute|configuré)/.test(document.getElementById('navette').textContent));
    assert.ok(await page.evaluate(() => document.querySelector('#navette small.absent') !== null), 'mention orange');
    await page.click('#remettre'); await page.wait('remise ouverte', () => document.getElementById('remise').open);
    assert.ok(await page.evaluate(() => !document.getElementById('remise-alerte').hidden), 'avertissement de remise');
    await page.click('#remise-ok');
    await page.wait('main à personne', () => document.body.classList.contains('main-agent') && document.getElementById('carte-tour').classList.contains('absent'));
    p.cli('wait', '--agent', 'OpenCode', '--timeout', '5s');
    await page.wait('main prise', () => /OpenCode a pris la main/.test(document.getElementById('navette').textContent));
    assert.equal((await capsule()).absent, false);
    p.cli('comment', '--file', 'a.md', '--after', 'Dernier paragraphe.', '--text', 'Une remarque.', '--agent', 'OpenCode');
    await page.wait('agent qui écrit', () => /OpenCode travaille · 1 commentaire/.test(document.getElementById('navette').textContent));
    p.cli('done', '--agent', 'OpenCode');
    await page.wait('main rendue', () => !document.body.classList.contains('main-agent')); await waitCards(page, 1);
    // Seul le commentaire de l'agent s'ajoute au manuscrit.
    assert.equal((await p.read()).toString().replace(/\{>>[^]*?<<\}/g, ''), original.toString());
  }],
  ['lier une session d’agent par un code (P10.2)', async ({ p, page }) => {
    const original = await p.read();
    await page.click('#menu-navette-bouton'); await page.wait('menu de la capsule', () => !document.getElementById('menu-navette').hidden);
    await page.click('#m-lier');
    await page.wait('consigne prête', () => document.getElementById('lien').open && /glose join [A-Z2-9]{4}-[A-Z2-9]{2}/.test(document.getElementById('lien-consigne').value));
    const consigne = await page.evaluate(() => document.getElementById('lien-consigne').value);
    assert.ok(consigne.includes(p.dir), 'dossier du projet dans la consigne');
    await page.click('#lien-copier');
    const code = consigne.match(/glose join ([A-Z2-9]{4}-[A-Z2-9]{2})/)[1];
    // Un autre agent qui attend n'est pas l'agent lié par ce code (revue de Codex).
    spawnSync(binary, ['wait', '--agent', 'Autre', '--timeout', '100ms'], { cwd: p.dir, env: p.env });
    await page.wait('autre agent vu', () => /Autre attend/.test(document.getElementById('navette').textContent));
    assert.doesNotMatch(await page.evaluate(() => document.getElementById('lien-etat').textContent), /lié/);
    // P10.10 : prête à coller ; P10.12 : join attend la remise, puis « délai écoulé » (code 3).
    assert.ok(consigne.includes('--agent "agent"') && !consigne.includes('TON NOM') && /relance-la telle quelle/.test(consigne), 'consigne prête à coller');
    const joint = spawnSync(binary, ['join', code, '--agent', 'OpenCode', '--timeout', '1s'], { cwd: p.dir, env: p.env, encoding: 'utf8' });
    assert.equal(joint.status, 3, joint.stdout + joint.stderr);
    await page.wait('agent lié', () => /OpenCode est lié/.test(document.getElementById('lien-etat').textContent));
    await page.wait('capsule', () => /OpenCode attend/.test(document.getElementById('navette').textContent));
    // Un code sert une fois.
    const again = spawnSync(binary, ['join', code], { cwd: p.dir, env: p.env, encoding: 'utf8' });
    assert.equal(again.status, 1, again.stderr);
    await page.click('#lien-fermer'); await page.wait('panneau fermé', () => !document.getElementById('lien').open);
    // Depuis la boîte de remise.
    await page.click('#remettre'); await page.wait('remise ouverte', () => document.getElementById('remise').open);
    await page.click('#remise-lier');
    await page.wait('nouveau code', () => document.getElementById('lien').open && !document.getElementById('remise').open && /glose join/.test(document.getElementById('lien-consigne').value));
    assert.ok(!(await page.evaluate(() => document.getElementById('lien-consigne').value)).includes(code), 'nouveau code');
    await page.click('#lien-fermer');
    assert.deepEqual(await p.read(), original);
  }],
  ['réglages des agents : glose set et « Aucun agent configuré » (P10.3)', async ({ p, page }) => {
    const original = await p.read();
    await page.wait('aucun agent configuré', () => /Aucun agent configuré/.test(document.getElementById('navette').textContent) &&
      !document.getElementById('configurer-agents').hidden);
    await page.navigate(`http://127.0.0.1:${p.state.port}/set#t=${p.state.token}`, '#ra-detectes li');
    await page.wait('page des agents', () => document.querySelectorAll('#ra-detectes li').length >= 3 && /Aucun agent configuré/.test(document.getElementById('ra-alias').textContent));
    await page.click('#ra-ajouter');
    await page.fill('.ra-ligne .ra-nom', 'Sol');
    await page.evaluate(() => { const s = document.querySelector('.ra-ligne .ra-agent'); s.value = 'codex'; s.dispatchEvent(new Event('change')); });
    await page.fill('.ra-ligne .ra-modele', 'gpt-essai');
    await page.click('#ra-enregistrer');
    await page.wait('enregistré', () => /Enregistré/.test(document.getElementById('ra-statut').textContent));
    const conf = JSON.parse(await fs.readFile(path.join(p.env.GLOSE_CONFIG_DIR, 'agents.json'), 'utf8'));
    assert.deepEqual([conf.default, conf.aliases], ['Sol', [{ name: 'Sol', agent: 'codex', model: 'gpt-essai' }]]);
    // Un nom court invalide est refusé, la saisie reste.
    await page.click('#ra-ajouter');
    await page.evaluate(() => { const n = document.querySelectorAll('.ra-ligne .ra-nom'); n[1].value = 'Sol Levant'; n[1].dispatchEvent(new Event('input')); });
    await page.click('#ra-enregistrer');
    await page.wait('refus', () => /nom court invalide/.test(document.getElementById('ra-statut').textContent));
    assert.equal(await page.evaluate(() => document.querySelectorAll('.ra-ligne').length), 2);
    await page.evaluate(() => document.querySelectorAll('.ra-ligne button[aria-label="Retirer ce nom court"]')[1].click());
    await page.click('#ra-enregistrer');
    await page.wait('réenregistré', () => /Enregistré/.test(document.getElementById('ra-statut').textContent));
    await page.navigate(p.state.url);
    await page.wait('agent configuré', () => /Aucun agent à l’écoute|Aucun agent à l'écoute/.test(document.getElementById('navette').textContent) &&
      document.getElementById('configurer-agents').hidden);
    assert.deepEqual(await p.read(), original);
  }],
  ['outils requis et essai d’un faux agent (P10.4)', async ({ p, page }) => {
    // Un faux codex : aucun vrai agent n'est lancé par les essais.
    const bin = path.join(p.dir, '..', path.basename(p.dir) + '-bin');
    await fs.mkdir(bin, { recursive: true });
    await fs.writeFile(path.join(bin, 'codex'), '#!/bin/sh\nif [ "$1" = mcp ]; then echo "lexharness  enabled"; exit 0; fi\necho "faux codex : $*"; cat\n', { mode: 0o755 });
    await p.stop();
    p.env = { ...p.env, PATH: bin + path.delimiter + process.env.PATH };
    await p.write(JSON.stringify({ agent: { alias: 'Sol', requires: ['mcp:lexharness', 'skill:inexistant'] } }), 'glose.json');
    await fs.mkdir(p.env.GLOSE_CONFIG_DIR, { recursive: true });
    await fs.writeFile(path.join(p.env.GLOSE_CONFIG_DIR, 'agents.json'), JSON.stringify({ version: 1, default: 'Sol', aliases: [{ name: 'Sol', agent: 'codex', model: 'gpt-essai' }] }));
    await p.start();
    await page.navigate(`http://127.0.0.1:${p.state.port}/set#t=${p.state.token}`, '#ra-detectes li');
    await page.wait('ce texte', () => !document.getElementById('ra-projet').hidden && /Sol/.test(document.getElementById('ra-projet-texte').textContent));
    await page.click('#ra-verifier');
    await page.wait('outils vérifiés', () => document.querySelectorAll('#ra-outils li.present').length === 1 && document.querySelectorAll('#ra-outils li.absent').length === 1);
    await page.click('.ra-ligne .ra-essayer');
    await page.wait('essai terminé', () => /faux codex : exec .*-s read-only.*-m gpt-essai/.test(document.getElementById('ra-essai-sortie').textContent) &&
      /glose status/.test(document.getElementById('ra-essai-sortie').textContent));
    assert.match(await page.evaluate(() => document.getElementById('ra-essai-etat').textContent), /Terminé/);
  }],
  ['Glose lance l’agent à la remise (P10.5)', async ({ p, page }) => {
    const original = (await p.read()).toString();
    const bin = path.join(p.dir, '..', path.basename(p.dir) + '-bin');
    const crash = path.join(bin, 'plante');
    await fs.mkdir(bin, { recursive: true });
    await fs.writeFile(path.join(bin, 'codex'), ['#!/bin/sh', 'cat > /dev/null', 'echo "faux codex : $*"',
      'if [ -f "$GLOSE_TEST_CRASH" ]; then echo plantage; exit 3; fi', 'sleep 2',
      'glose comment --file a.md --after "Dernier paragraphe." --text "Vu par $GLOSE_AGENT."', 'glose done --note "Fait."', ''].join('\n'), { mode: 0o755 });
    await p.stop();
    p.env = { ...p.env, PATH: bin + path.delimiter + process.env.PATH, GLOSE_TEST_CRASH: crash };
    await fs.mkdir(p.env.GLOSE_CONFIG_DIR, { recursive: true });
    await fs.writeFile(path.join(p.env.GLOSE_CONFIG_DIR, 'agents.json'), JSON.stringify({ version: 1, default: 'Sol', aliases: [{ name: 'Sol', agent: 'codex', model: 'gpt-essai' }] }));
    await p.start(); await page.navigate(p.state.url);
    await page.click('#remettre'); await page.wait('remise ouverte', () => document.getElementById('remise').open);
    assert.ok(await page.evaluate(() => !document.getElementById('remise-lancement').hidden && document.getElementById('remise-lancer').checked &&
      /Lancer Sol \(Codex, gpt-essai\)/.test(document.getElementById('remise-lancer-texte').textContent)), 'case Lancer cochée');
    assert.ok(await page.evaluate(() => document.getElementById('remise-alerte').hidden), 'pas d’avertissement quand l’agent est lancé');
    await page.click('#remise-ok');
    await page.wait('agent lancé', () => /Sol · gpt-essai travaille/.test(document.getElementById('navette').textContent));
    await page.click('#menu-navette-bouton'); await page.click('#m-sortie');
    await page.wait('sortie de l’agent', () => document.getElementById('sortie').open && /faux codex : exec/.test(document.getElementById('sortie-texte').textContent));
    await page.click('#sortie-fermer');
    await page.wait('main rendue par l’agent lancé', () => !document.body.classList.contains('main-agent')); await waitCards(page, 1);
    assert.match((await p.read()).toString(), /Vu par Sol\./);
    // Second tour : l'agent plante ; la capsule le dit, on le relance.
    await fs.writeFile(crash, '');
    await page.click('#remettre'); await page.wait('seconde remise', () => document.getElementById('remise').open); await page.click('#remise-ok');
    await page.wait('arrêté sans remettre', () => /Sol( · gpt-essai)? s’est arrêté sans remettre|Sol( · gpt-essai)? s'est arrêté sans remettre/.test(document.getElementById('navette').textContent) &&
      document.getElementById('carte-tour').classList.contains('absent'));
    await fs.rm(crash);
    await page.click('#menu-navette-bouton');
    await page.wait('relance proposée', () => !document.getElementById('m-lancer').hidden);
    await page.click('#m-lancer');
    await page.wait('main rendue après relance', () => !document.body.classList.contains('main-agent'));
    assert.equal((await p.read()).toString().replace(/\{>>[^]*?<<\}/g, ''), original);
  }],
  ['destinataires : Réponse par, @Nom, deux agents à la suite (P10.6)', async ({ p, page }) => {
    const bin = path.join(p.dir, '..', path.basename(p.dir) + '-bin');
    await fs.mkdir(bin, { recursive: true });
    // Deux faux agents : chacun commente après sa phrase, signé, puis clôt son passage.
    const faux = ['#!/bin/sh', 'cat > /dev/null', 'case "$GLOSE_AGENT" in Sol) apres="Le chat dort." ;; *) apres="Dernier paragraphe." ;; esac',
      'glose comment --file a.md --after "$apres" --text "Réponse de $GLOSE_AGENT."', 'glose done --note "fait par $GLOSE_AGENT"', ''].join('\n');
    for (const nom of ['codex', 'opencode']) await fs.writeFile(path.join(bin, nom), faux, { mode: 0o755 });
    await p.stop();
    p.env = { ...p.env, PATH: bin + path.delimiter + process.env.PATH };
    await fs.mkdir(p.env.GLOSE_CONFIG_DIR, { recursive: true });
    await fs.writeFile(path.join(p.env.GLOSE_CONFIG_DIR, 'agents.json'), JSON.stringify({ version: 1, default: 'Sol',
      aliases: [{ name: 'Sol', agent: 'codex' }, { name: 'Luna', agent: 'opencode', model: 'x/y' }] }));
    await p.start(); await page.navigate(p.state.url);
    await openComment(page, 'mot important');
    assert.ok(await page.evaluate(() => !document.getElementById('saisie-pour-ligne').hidden &&
      [...document.getElementById('saisie-pour').options].map((o) => o.textContent).join('|') === 'Par défaut (Sol)|Luna|Sol'), 'Réponse par');
    await page.fill('#saisie-texte', 'Pour le défaut.'); await page.click('#saisie-ok'); await waitCards(page, 1);
    await openComment(page, 'drapeau');
    await page.fill('#saisie-texte', '@luna Pour Luna.'); await page.click('#saisie-ok'); await waitCards(page, 2);
    const src = (await p.read()).toString();
    assert.match(src, /\{>>Pour Luna\.<<\}\{>>glose:v1 id=\w+ by=author to=Luna<<\}/, '@Nom retiré, to=Luna écrit');
    assert.doesNotMatch(src, /Pour le défaut\.<<\}\{>>glose:v1 id=\w+ by=author to=/);
    assert.ok(await page.evaluate(() => [...document.querySelectorAll('#marge .carte .pour')].map((e) => e.textContent).join() === 'Luna' && document.querySelector('#marge .carte .pour svg') !== null), 'destinataire sur la carte');
    await page.click('#remettre'); await page.wait('remise ouverte', () => document.getElementById('remise').open); await page.click('#remise-ok');
    await page.wait('main rendue après les deux passages', () => !document.body.classList.contains('main-agent')); await waitCards(page, 4);
    const fin = (await p.read()).toString();
    assert.ok(fin.indexOf('Réponse de Sol.') > 0 && fin.indexOf('Réponse de Luna.') > 0 && /via=Sol/.test(fin) && /via=Luna/.test(fin), 'deux passages signés');
    assert.deepEqual(await page.evaluate(() => [...document.querySelectorAll('#marge .carte > .meta .qui, #marge .carte .msg .qui')].map((e) => e.textContent).filter((t) => /^(Sol|Luna)/.test(t)).sort()), ['Luna · y', 'Sol'], 'signatures sur les cartes, avec le modèle');
    const log = JSON.parse(p.cli('log', '--json'));
    assert.match(log.turns.at(-1).note || log.turns.at(-2).note, /Sol : fait par Sol[^]*Luna : fait par Luna/);
  }],
  ['cartes longues repliées, dépliées par-dessus les autres (P9.4)', async ({ p, page }) => {
    const long = Array.from({ length: 30 }, (_, i) => 'Ligne ' + (i + 1) + ' d’une très longue remarque.').join(' ');
    await openComment(page, 'mot important'); await page.fill('#saisie-texte', long); await page.click('#saisie-ok'); await waitCards(page, 1);
    // Même phrase : les deux cartes s'empilent (audit de Codex).
    await openComment(page, 'phrase ordinaire'); await page.fill('#saisie-texte', 'Courte.'); await page.click('#saisie-ok'); await waitCards(page, 2);
    await page.evaluate(() => document.getElementById('texte').click());
    const mesure = () => page.evaluate(() => [...document.querySelectorAll('#marge .carte')].map((c) =>
      ({ h: c.offsetHeight, top: c.offsetTop, longue: c.classList.contains('longue'), active: c.classList.contains('active'), texte: c.textContent })));
    await page.wait('repliée', () => document.querySelector('#marge .carte.longue') !== null);
    const avant = await mesure();
    const iLong = avant.findIndex((c) => c.texte.includes('Ligne 30')), iCourt = avant.findIndex((c) => c.texte.includes('Courte.'));
    // Repliée, ses actions restent visibles, au-dessus du fondu.
    assert.ok(await page.evaluate(() => { const c = document.querySelector('#marge .carte.longue'), o = c.querySelector('.outils button'); const r = o.getBoundingClientRect(), b = c.getBoundingClientRect(); return r.bottom <= b.bottom && r.height > 0 && document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2)?.closest('.outils'); }), 'actions d’une carte repliée à portée de clic');
    assert.ok(avant[iLong].longue && avant[iLong].h <= 220 && !avant[iCourt].longue, 'longue repliée : ' + JSON.stringify(avant));
    // Le texte entier reste dans le document (lecteurs d'écran).
    assert.ok(avant[iLong].texte.includes('Ligne 30'));
    assert.ok(avant[iCourt].top >= avant[iLong].top + avant[iLong].h, 'cartes empilées');
    await page.evaluate((i) => document.querySelectorAll('#marge .carte')[i].click(), iLong);
    await page.wait('dépliée', (i) => document.querySelectorAll('#marge .carte')[i].offsetHeight > 200, iLong);
    await new Promise((r) => setTimeout(r, 300)); // la disposition suit d'une image
    const apres = await mesure();
    assert.equal(apres[iCourt].top, avant[iCourt].top, 'la voisine ne bouge pas, au pixel près');
    // Par-dessus : au point de la voisine recouverte, c'est la carte dépliée qui est visible.
    const dessus = await page.evaluate((i, j) => {
      const cs = document.querySelectorAll('#marge .carte'), r = cs[j].getBoundingClientRect(), longue = cs[i].getBoundingClientRect();
      if (longue.bottom <= r.top) return 'sans recouvrement';
      const el = document.elementFromPoint(r.left + 20, Math.min(r.top + 10, longue.bottom - 5));
      return cs[i].contains(el) ? 'dessus' : 'dessous';
    }, iLong, iCourt);
    assert.notEqual(dessus, 'dessous');
    // Une autre carte choisie : la longue se replie.
    await page.evaluate((j) => document.querySelectorAll('#marge .carte')[j].click(), iCourt);
    await page.wait('repliée de nouveau', (i) => document.querySelectorAll('#marge .carte')[i].offsetHeight <= 220, iLong);
    // Le focus clavier qui entre dans la carte longue la déplie.
    await page.evaluate((i) => document.querySelectorAll('#marge .carte')[i].focus(), iLong);
    await page.wait('dépliée au focus', (i) => document.querySelectorAll('#marge .carte')[i].classList.contains('active') &&
      document.querySelectorAll('#marge .carte')[i].offsetHeight > 200, iLong);
  }],
  ['carte longue : repliée dès que le focus la quitte, lecture gardée au clic', async ({ p, page }) => {
    await p.stop();
    await p.write('# Titre\n\n' + 'Un paragraphe ordinaire pour remplir la page assez longtemps.\n\n'.repeat(6) +
      'Voici le mot important du texte.\n\nEt une autre phrase ordinaire ensuite.\n\n' + 'Encore un paragraphe ordinaire pour la suite de la page.\n\n'.repeat(40));
    await p.start();
    await page.send('Emulation.setDeviceMetricsOverride', { width: 1366, height: 768, deviceScaleFactor: 1, mobile: false });
    await page.navigate(p.state.url);
    const long = Array.from({ length: 60 }, (_, i) => 'Ligne ' + (i + 1) + ' d’une très longue remarque.').join(' ');
    await openComment(page, 'mot important'); await page.fill('#saisie-texte', long); await page.click('#saisie-ok'); await waitCards(page, 1);
    await openComment(page, 'autre phrase'); await page.fill('#saisie-texte', 'Courte.'); await page.click('#saisie-ok'); await waitCards(page, 2);
    const souris = async (x, y) => {
      await page.send('Input.dispatchMouseEvent', { type: 'mouseMoved', x, y });
      await page.send('Input.dispatchMouseEvent', { type: 'mousePressed', button: 'left', clickCount: 1, x, y });
      await page.send('Input.dispatchMouseEvent', { type: 'mouseReleased', button: 'left', clickCount: 1, x, y });
    };
    const LONGUE = "[...document.querySelectorAll('#marge .carte')].find((c) => c.textContent.includes('Ligne 60'))";
    const dansLaCarte = () => page.evaluate((s) => { const r = eval(s).getBoundingClientRect(); const haut = document.getElementById('barre').getBoundingClientRect().bottom;
      return { x: r.left + r.width / 2, y: Math.max(r.top, haut) + 60 }; }, LONGUE);
    const surLeTexte = () => page.evaluate(() => { const b = document.getElementById('barre').getBoundingClientRect().bottom;
      const r = [...document.querySelectorAll('#texte .rendu:not(.garde) p')].map((x) => x.getBoundingClientRect()).find((r) => r.top > b + 20 && r.bottom < innerHeight - 80);
      return { x: r.left + 40, y: r.top + 8 }; });
    const etat = () => page.evaluate((s) => { const c = eval(s); return { active: c.classList.contains('active'), h: c.offsetHeight, y: scrollY, marques: document.querySelectorAll('#texte .g-actif').length }; }, LONGUE);
    const replie = async (quoi) => { await page.wait(quoi, (s) => { const c = eval(s); return !c.classList.contains('active') && c.offsetHeight <= 220 && !document.querySelector('.g-actif'); }, LONGUE); };
    const pause = () => new Promise((r) => setTimeout(r, 400));

    // Le texte défilé jusqu'à la carte : un premier clic la déplie.
    await page.evaluate((s) => { eval(s).scrollIntoView({ block: 'start', behavior: 'instant' }); window.scrollBy({ top: -220, behavior: 'instant' }); }, LONGUE);
    await pause();
    let t = await surLeTexte(); await souris(t.x, t.y); await replie('repliée après un clic sur le texte');
    let c = await dansLaCarte(); await souris(c.x, c.y);
    await page.wait('dépliée au clic', (s) => eval(s).classList.contains('active') && eval(s).offsetHeight > 220, LONGUE);
    await pause();

    // Déjà dépliée : la lire plus bas et cliquer dedans ne ramène pas à son début.
    for (const pas of [300, 250]) {
      await page.evaluate((pas) => window.scrollBy({ top: pas, behavior: 'instant' }), pas);
      await pause();
      const avant = await etat();
      c = await dansLaCarte(); await souris(c.x, c.y);
      await pause();
      const apres = await etat();
      assert.equal(apres.y, avant.y, 'la page reste où elle est');
      assert.ok(apres.active, 'la carte reste dépliée');
    }

    // Un clic sur la page, puis dans l'espace vide de la marge : repliée.
    t = await surLeTexte(); await souris(t.x, t.y); await replie('repliée après un clic sur la page');
    await page.key('j');
    await page.wait('J reprend à la carte suivante', () => /Courte\./.test(document.querySelector('#marge .carte.active')?.textContent || ''));
    await page.evaluate((s) => eval(s).scrollIntoView({ block: 'center', behavior: 'instant' }), LONGUE);
    await page.evaluate((s) => eval(s).focus(), LONGUE);
    await page.wait('dépliée au focus', (s) => eval(s).classList.contains('active'), LONGUE);
    const vide = await page.evaluate(() => { const m = document.getElementById('marge').getBoundingClientRect(), b = document.getElementById('barre').getBoundingClientRect().bottom;
      for (let y = innerHeight - 60; y > b; y -= 20) if (!document.elementFromPoint(m.right - 4, y)?.closest('.carte')) return { x: m.right - 4, y };
      return null; });
    if (vide) { await souris(vide.x, vide.y); await replie('repliée après un clic dans la marge'); }

    // Au clavier : la carte se replie quand le focus part ailleurs, pas vers un panneau.
    await page.evaluate((s) => eval(s).focus(), LONGUE);
    await page.wait('dépliée au focus de nouveau', (s) => eval(s).classList.contains('active'), LONGUE);
    await page.evaluate(() => document.getElementById('bouton-reglages').focus());
    await replie('repliée quand le focus quitte la carte');

    // Répondre : le panneau appartient à la carte, qui reste dépliée pendant la saisie.
    await page.evaluate((s) => eval(s).focus(), LONGUE);
    await page.wait('dépliée pour répondre', (s) => eval(s).classList.contains('active'), LONGUE);
    await page.evaluate((s) => [...eval(s).querySelectorAll('.outils button')].find((b) => /Répondre/.test(b.textContent + b.getAttribute('aria-label'))).click(), LONGUE);
    await page.wait('panneau de réponse', () => document.getElementById('saisie').open);
    t = await surLeTexte(); await souris(t.x, t.y);
    await pause();
    assert.ok((await etat()).active, 'dépliée tant que le panneau est ouvert');
    await page.key('Escape');
    await page.wait('panneau fermé', () => !document.getElementById('saisie').open);
    await page.send('Emulation.clearDeviceMetricsOverride');
  }],
  ['impression en thème sombre : encre foncée, aucun message temporaire', async ({ page }) => {
    await page.evaluate(() => { document.documentElement.dataset.theme = 'dark'; document.getElementById('avis').textContent = 'Vue « Proposé ».'; });
    const encre = () => page.evaluate(() => { const [r, v, b] = getComputedStyle(document.querySelector('#texte .rendu:not(.garde) p')).color.match(/[\d.]+/g).map(Number); return (r + v + b) / 3; });
    assert.ok(await encre() > 128, 'à l’écran, le thème sombre écrit clair');
    await page.send('Emulation.setEmulatedMedia', { media: 'print' });
    assert.ok(await encre() < 128, 'sur le papier, l’encre est foncée');
    assert.equal(await page.evaluate(() => getComputedStyle(document.getElementById('avis')).display), 'none');
    await page.send('Emulation.setEmulatedMedia', { media: '' });
  }],
  ['Libertinus Serif choisie : embarquée et chargée', async ({ page }) => {
    await page.evaluate(() => { const s = document.getElementById('r-police'); s.value = 'libertinus'; s.dispatchEvent(new Event('input', { bubbles: true })); });
    await page.wait('police appliquée', () => getComputedStyle(document.querySelector('#texte .rendu:not(.garde) p')).fontFamily.includes('Libertinus'));
    await page.wait('police chargée', async () => { await document.fonts.load('17px "Libertinus Serif"'); return document.fonts.check('17px "Libertinus Serif"') && [...document.fonts].some((f) => f.family.includes('Libertinus') && f.status === 'loaded'); });
    await page.evaluate(() => { const s = document.getElementById('r-police'); s.value = 'source'; s.dispatchEvent(new Event('input', { bubbles: true })); });
  }],
  ['export en PDF : trois versions, commentaires en marge, écran rendu tel quel (P13)', async ({ p, page }) => {
    await p.stop();
    const source = "# Titre\n\nPremier {==passage==}{>>Commentaire A.<<} ordinaire.\n\nDeuxième {~~ancien~>nouveau~~}{>>Mot plus juste.<<} et {--retiré--} puis {++ajouté++}.\n\n" +
      "- élément {==de liste==}{>>Commentaire B.<<}\n- autre élément\n\n> Citation {==citée==}{>>Commentaire C.<<}\n\n| a | b |\n|---|---|\n| cellule {==tableau==}{>>Commentaire D.<<} | x |\n\n" +
      'Un paragraphe ordinaire pour remplir la page assez longtemps.\n\n'.repeat(40);
    await p.write(source);
    await p.start();
    await page.send('Emulation.setDeviceMetricsOverride', { width: 1366, height: 768, deviceScaleFactor: 1, mobile: false });
    await page.navigate(p.state.url);
    await page.evaluate(() => { window.__imp = 0; window.print = () => { window.dispatchEvent(new Event('beforeprint')); window.__imp++; }; });
    const avant = await p.read();
    const exporter = async (version, commentaires = true) => {
      const n = await page.evaluate(() => window.__imp);
      await page.click('#exporter');
      await page.wait('panneau d’export', () => !document.getElementById('export').hidden);
      await page.evaluate((version, commentaires) => {
        document.querySelector('input[name="export-version"][value="' + version + '"]').click();
        document.getElementById('export-commentaires').checked = commentaires;
      }, version, commentaires);
      await page.click('#export-ok');
      await page.wait('impression lancée', (n) => window.__imp > n, n);
    };
    const finir = async () => {
      await page.evaluate(() => window.dispatchEvent(new Event('afterprint')));
      await page.wait('écran rendu', () => document.body.dataset.vue === 'relecture' && !document.querySelector('.note-export, .export-copie') && !document.body.className.includes('export-') && !/—\s*(Modifications apparentes|Actuel|Proposé)$/.test(document.title));
    };
    const texte = () => page.evaluate(() => document.querySelector('#texte .rendu:not(.garde)').innerText);
    const papier = async (fn, ...args) => {
      await page.send('Emulation.setEmulatedMedia', { media: 'print' });
      const r = await page.evaluate(fn, ...args);
      await page.send('Emulation.setEmulatedMedia', { media: '' });
      return r;
    };

    // Le panneau part de la vue affichée ; la case ne vaut que pour la relecture ; Échap rend le focus.
    await page.click('#exporter');
    assert.equal(await page.evaluate(() => document.querySelector('input[name="export-version"]:checked').value), 'relecture');
    assert.equal(await page.evaluate(() => document.getElementById('export-commentaires-ligne').hidden), false);
    await page.evaluate(() => document.querySelector('input[name="export-version"][value="actuel"]').click());
    assert.equal(await page.evaluate(() => document.getElementById('export-commentaires-ligne').hidden), true);
    await page.key('Escape');
    await page.wait('panneau refermé', () => document.getElementById('export').hidden && document.activeElement.id === 'exporter');

    // Relecture : une note par annotation, dans l'ordre, en marge, sans recouvrir le texte.
    await exporter('relecture');
    assert.match(await page.evaluate(() => document.title), /— Modifications apparentes$/);
    const notes = await papier(() => {
      const t = document.getElementById('texte').getBoundingClientRect();
      return { mots: document.querySelectorAll('.export-copie span[data-g]:not([class]):not([data-a])').length, origine: getComputedStyle(document.querySelector('#texte .rendu:not(.export-copie):not(.garde)')).display, nb: document.querySelectorAll('.note-export').length, ancres: [...document.querySelectorAll('#texte .export-copie .g-ancre')].filter((x) => getComputedStyle(x).display !== 'none').length,
        numeros: [...document.querySelectorAll('.note-export .num')].map((x) => x.textContent),
        droite: t.right, largeur: t.width, gauches: [...document.querySelectorAll('.note-export')].map((x) => x.getBoundingClientRect().left),
        marges: ['marge', 'barre', 'cote', 'pied'].map((id) => getComputedStyle(document.getElementById(id)).display),
        boutons: document.querySelectorAll('.note-export button').length, liste: document.querySelector('#texte ul > :not(li)') === null,
        texteNote: document.querySelector('.note-export').textContent };
    });
    assert.equal(notes.mots, 0, 'le papier n’a plus un élément par mot');
    assert.equal(notes.origine, 'none', 'la page de l’écran ne s’imprime pas en plus');
    assert.deepEqual(notes.numeros, Array.from({ length: notes.nb }, (_, i) => String(i + 1)), 'dans l’ordre : ' + JSON.stringify(notes));
    assert.ok(notes.nb >= 7 && notes.ancres === notes.nb, JSON.stringify(notes));
    assert.ok(notes.gauches.every((g) => g >= notes.droite), 'en marge, hors du texte : ' + JSON.stringify(notes));
    assert.ok(new Set(notes.gauches.map(Math.round)).size === 1, 'notes alignées : ' + JSON.stringify(notes.gauches));
    assert.deepEqual([...new Set(notes.marges)], ['none'], 'l’écran ne s’imprime pas');
    assert.ok(notes.liste, 'une liste ne contient que ses éléments');
    assert.equal(notes.boutons, 0);
    assert.match(notes.texteNote, /Commentaire A\./);
    // Un vrai PDF, en A4 : Chrome imprime ce que les notes annoncent.
    const pdf = await page.send('Page.printToPDF', { preferCSSPageSize: true });
    const octets = Buffer.from(pdf.data, 'base64');
    assert.equal(octets.subarray(0, 5).toString(), '%PDF-');
    assert.match(octets.toString('latin1'), /MediaBox\s*\[\s*0 0 59[0-9](\.\d+)? 84[0-9](\.\d+)?\s*\]/, 'format A4');
    await finir();

    // Relecture sans commentaires : les marques restent, les renvois partent.
    await exporter('relecture', false);
    const sans = await papier(() => ({ notes: document.querySelectorAll('.note-export').length, ancre: getComputedStyle(document.querySelector('#texte .g-ancre')).display,
      barre: document.querySelector('.export-copie .g-barre') !== null, hl: getComputedStyle(document.querySelector('.export-copie .g-hl')).backgroundColor }));
    assert.equal(sans.notes, 0); assert.equal(sans.ancre, 'none'); assert.ok(sans.barre, 'suppressions gardées');
    assert.equal(sans.hl, 'rgba(0, 0, 0, 0)', 'surlignage retiré');
    await finir();

    // Actuel, puis Proposé : le texte seul, et la vue d'origine revient.
    await exporter('actuel');
    assert.equal(await page.evaluate(() => document.body.dataset.vue), 'actuel');
    let t = await texte();
    assert.ok(t.includes('ancien') && !t.includes('nouveau') && !t.includes('ajouté'), t.slice(0, 200));
    const actuel = await papier(() => ({ notes: document.querySelectorAll('.note-export').length, ancre: getComputedStyle(document.querySelector('.export-copie .g-ancre')).display }));
    assert.deepEqual(actuel, { notes: 0, ancre: 'none' });
    assert.match(await page.evaluate(() => document.title), /— Actuel$/);
    await finir();
    await exporter('propose');
    await page.wait('vue Proposé', () => document.body.dataset.vue === 'propose');
    t = await texte();
    assert.ok(t.includes('nouveau') && t.includes('ajouté') && !t.includes('ancien') && !t.includes('retiré'), t.slice(0, 200));
    assert.equal(await papier(() => getComputedStyle(document.querySelector('.export-copie .g-prop')).textDecorationLine), 'none', 'aucune marque de proposition');
    await finir();

    // Ctrl+P : la vue affichée, avec ses commentaires.
    await page.evaluate(() => window.print());
    assert.ok(await page.evaluate(() => document.querySelectorAll('.note-export').length >= 7));
    await finir();

    // Pendant une saisie, rien ne part : le panneau de saisie ne doit pas être perdu.
    await openComment(page, 'Deuxième');
    await page.evaluate(() => document.getElementById('exporter').click());
    await page.click('#export-ok');
    assert.match(await page.evaluate(() => document.getElementById('avis').textContent), /panneau de saisie/);
    assert.equal(await page.evaluate(() => document.body.classList.contains('export-impression')), false);
    await page.key('Escape');
    await page.wait('saisie fermée', () => !document.getElementById('saisie').open);

    // Exporter n'écrit rien.
    assert.ok(avant.equals(await p.read()), 'source inchangée');
    await page.send('Emulation.clearDeviceMetricsOverride');
  }],
  ['qui parle, et ce qu’est devenue une annotation : auteur et IA distincts, états, décisions', async ({ p, page }) => {
    await p.stop();
    await p.write('# Titre\n\nPremier {==passage==}{>>Une **question** à *préciser*.<<}{>>glose:v1 id=aaaaaa by=author<<}{>>Oui, **voir** ceci.<<}{>>glose:v1 id=aaaaab by=agent via=Sol model=gpt-5 reply=aaaaaa<<} ordinaire.\n\n' +
      'En {~~ancien~>nouveau~~}{>>glose:v1 id=bbbbbb by=agent via=Sol<<} mot.\n\nEt {++ajouté++}{>>glose:v1 id=cccccc by=agent via=Sol<<} ici.\n\nPuis {==autre==}{>>Seul.<<}{>>glose:v1 id=dddddd by=author<<} passage.\n');
    await p.start();
    await page.navigate(p.state.url);
    await waitCards(page, 4);

    // Sur la carte : une bulle par personne, jamais du même style, chacune nommée, avec son icône.
    const fil = await page.evaluate(() => {
      const c = [...document.querySelectorAll('#marge .carte')].find((x) => x.textContent.includes('voir'));
      return [...c.querySelectorAll('.msg')].map((m) => ({ classe: m.className, qui: m.querySelector('.qui').textContent, icone: !!m.querySelector('.qui svg'),
        fond: getComputedStyle(m).backgroundColor, bord: getComputedStyle(m).borderLeftColor, html: m.querySelector('.msg-texte').innerHTML }));
    });
    assert.equal(fil.length, 2);
    assert.match(fil[0].classe, /auteur/); assert.equal(fil[0].qui, 'Auteur');
    assert.match(fil[1].classe, /agent/); assert.match(fil[1].qui, /^Sol · /);
    assert.ok(fil[0].icone && fil[1].icone, 'icône de chacun');
    assert.notEqual(fil[0].fond, fil[1].fond, 'fonds distincts'); assert.notEqual(fil[0].bord, fil[1].bord, 'filets distincts');
    // Gras et italique, sans marques visibles ; la source garde les siennes.
    assert.equal(fil[0].html, 'Une <strong>question</strong> à <em>préciser</em>.');
    assert.equal(fil[1].html, 'Oui, <strong>voir</strong> ceci.');
    assert.match((await p.read()).toString(), /Une \*\*question\*\* à \*préciser\*\./);
    // Une proposition nomme son auteur en tête de carte.
    assert.match(await page.evaluate(() => [...document.querySelectorAll('#marge .carte.prop')][0].querySelector('.meta .qui.agent').textContent), /^Sol/);

    // Dans l'onglet : ce qui attend, et ce qui a reçu une réponse.
    await page.evaluate(() => document.getElementById('onglet-annotations').click());
    const etats = () => page.evaluate(() => [...document.querySelectorAll('#annotations-liste .etat')].map((e) => e.textContent));
    await page.wait('états de la liste', () => document.querySelectorAll('#annotations-liste .etat').length === 4);
    assert.deepEqual(await etats(), ['Répondu par Sol', 'À décider', 'À décider', 'À remettre']);
    assert.ok(!(await page.evaluate(() => document.getElementById('annotations-liste').textContent)).includes('**'), 'aucune marque dans les résumés');
    assert.equal(await page.evaluate(() => document.getElementById('annotations-traitees').textContent), '');

    // Accepter, rejeter : la décision reste visible dans la liste, avec son mot.
    const decider = async (mot, classe, restant) => {
      await page.evaluate((mot, classe) => [...document.querySelectorAll('#marge .carte.prop')].find((c) => c.textContent.includes(mot)).querySelector('.outils button.' + classe).click(), mot, classe);
      await waitCards(page, restant);
    };
    await decider('nouveau', 'accepter', 3);
    await page.wait('Accepté dans la liste', () => document.querySelector('#annotations-traitees .etat.accepte')?.textContent === 'Accepté');
    await decider('ajouté', 'rejeter', 2);
    await page.wait('Rejeté dans la liste', () => document.querySelector('#annotations-traitees .etat.rejete')?.textContent === 'Rejeté');
    assert.deepEqual(await page.evaluate(() => [...document.querySelectorAll('#annotations-traitees .etat')].map((e) => e.textContent)), ['Rejeté', 'Accepté'], 'le plus récent d’abord');
    assert.deepEqual(await etats(), ['Répondu par Sol', 'À remettre']);
    assert.match(await page.evaluate(() => document.querySelector('#annotations-traitees h3').textContent), /Traitées \(2\)/);

    // Un clic ouvre l'archive ; rien n'y est décidable.
    await page.evaluate(() => document.querySelector('#annotations-traitees a').click());
    await page.wait('archive ouverte', () => document.getElementById('archive').open);
    assert.match(await page.evaluate(() => document.getElementById('archive-contenu').textContent), /ajouté/);
    assert.equal(await page.evaluate(() => document.querySelectorAll('#archive-contenu button').length), 0);
    await page.evaluate(() => document.getElementById('archive').close());

    // Le filtre « de ce tour » ne garde que ce qui est en attente.
    await page.evaluate(() => { const f = document.getElementById('filtre-statut'); f.value = 'new'; f.dispatchEvent(new Event('change', { bubbles: true })); });
    await page.wait('filtre appliqué', () => document.getElementById('annotations-traitees').textContent === '');
    await page.evaluate(() => { const f = document.getElementById('filtre-statut'); f.value = ''; f.dispatchEvent(new Event('change', { bubbles: true })); });

    // Annuler une décision : elle quitte « Traitées » et l'annotation revient.
    await focusText(page); await page.key('z', 2); await waitCards(page, 3);
    await page.wait('décision annulée', () => { const t = [...document.querySelectorAll('#annotations-traitees .etat')].map((e) => e.textContent); return t.length === 1 && t[0] === 'Accepté'; });

    // Sur le PDF : le même gras, et chacun nommé.
    await page.evaluate(() => { window.print = () => { window.dispatchEvent(new Event('beforeprint')); window.__imp = 1; }; document.getElementById('exporter').click(); document.getElementById('export-ok').click(); });
    await page.wait('impression', () => window.__imp === 1);
    const note = await page.evaluate(() => [...document.querySelectorAll('.note-export')].find((n) => n.textContent.includes('voir')).innerHTML);
    assert.match(note, /<strong>question<\/strong>/); assert.match(note, /class="message auteur"/); assert.match(note, /class="message agent"/); assert.match(note, /Auteur : /); assert.match(note, /Sol : /);
    await page.evaluate(() => window.dispatchEvent(new Event('afterprint')));
    await page.wait('écran rendu', () => !document.querySelector('.export-copie'));
  }],
  ['retour au texte depuis Vos projets : position gardée, menu complet, thèmes', async ({ p, page }) => {
    await p.stop();
    await p.write('# Mon texte\n\n' + 'Un paragraphe ordinaire pour remplir la page assez longtemps.\n\n'.repeat(90));
    await p.start();
    await page.send('Emulation.setDeviceMetricsOverride', { width: 1366, height: 768, deviceScaleFactor: 1, mobile: false });
    await page.navigate(p.state.url);
    await page.evaluate(() => window.scrollTo({ top: 2000, behavior: 'instant' }));
    await new Promise((r) => setTimeout(r, 300));
    const repere = () => page.evaluate(() => { const b = document.getElementById('barre').getBoundingClientRect().bottom;
      const i = [...document.querySelectorAll('#texte .rendu:not(.garde) p')].findIndex((x) => x.getBoundingClientRect().top >= b); return { i, y: scrollY }; });
    const avant = await repere();
    assert.ok(avant.y > 1500 && avant.i > 5, JSON.stringify(avant));
    const proche = (nom, r) => assert.ok(Math.abs(r.i - avant.i) <= 1, nom + ' : ' + JSON.stringify([avant, r]));
    const accueil = async () => {
      await page.evaluate(() => document.getElementById('retour-accueil').click());
      await page.wait('accueil', () => location.pathname === '/home' && document.getElementById('acc-texte') !== null);
    };
    const texte = () => page.wait('texte revenu', () => location.pathname === '/' && document.querySelectorAll('#texte .rendu:not(.garde) p').length > 80 && scrollY > 1000);

    // L'accueil propose le retour, avec le titre du texte ; le lien retrouve la place.
    await accueil();
    assert.match(await page.evaluate(() => document.getElementById('acc-texte').textContent), /Revenir au texte/);
    assert.match(await page.evaluate(() => document.getElementById('acc-texte').textContent), /projet-|Mon texte/);
    await page.evaluate(() => document.getElementById('acc-texte').click());
    await texte();
    proche('lien', await repere());
    // Le bouton Précédent du navigateur aussi.
    await accueil();
    await page.evaluate(() => history.back());
    await texte();
    proche('précédent', await repere());
    // Recharger la page aussi.
    await page.evaluate(() => location.reload());
    await new Promise((r) => setTimeout(r, 500));
    await texte();
    proche('rechargement', await repere());

    // Les mêmes entrées partout : Agents ne fait pas disparaître Réglages.
    await accueil();
    const entrees = () => page.evaluate(() => [...document.querySelectorAll('.acc-nav nav a:not(.acc-revenir), .acc-nav nav button')].map((e) => e.textContent.trim()));
    assert.deepEqual(await entrees(), ['Accueil', 'Agents', 'Réglages']);
    await page.evaluate(() => document.getElementById('acc-agents').click());
    await page.wait('page Agents', () => location.pathname === '/set' && document.getElementById('acc-reglages') !== null);
    assert.deepEqual(await entrees(), ['Accueil', 'Agents', 'Réglages']);
    assert.ok(await page.evaluate(() => document.getElementById('acc-texte') !== null), 'retour au texte depuis Agents');

    // Réglages depuis Agents : choisir un thème l'applique et l'enregistre pour tous les projets.
    await page.evaluate(() => document.getElementById('acc-reglages').click());
    await page.wait('réglages ouverts', () => document.getElementById('espace-reglages').open && document.querySelectorAll('.theme-carte').length >= 18);
    const noms = await page.evaluate(() => [...document.querySelectorAll('.theme-nom')].map((e) => e.textContent));
    for (const n of ['Glose Default (clair)', 'Glose Default (sombre)', 'Nord', 'Tokyo Night']) assert.ok(noms.includes(n), n);
    await page.evaluate(() => document.querySelector('.theme-carte input[value="tokyo-night"]').click());
    assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), 'tokyo-night');
    await page.wait('thème enregistré', async () => (await (await fetch('/api/settings', { headers: { 'X-Glose-Token': sessionStorage.getItem('glose-jeton') } })).json()).theme === 'tokyo-night');
    const reglages = (await p.api('/api/settings')).theme;
    assert.equal(reglages, 'tokyo-night');

    // Le lecteur le reprend ; son panneau Affichage en change ; le papier reste clair.
    await page.navigate(p.state.url);
    const peinture = () => page.evaluate(() => ({ theme: document.documentElement.dataset.theme, schema: getComputedStyle(document.documentElement).colorScheme, papier: getComputedStyle(document.getElementById('texte')).backgroundColor, encre: getComputedStyle(document.querySelector('#texte .rendu:not(.garde) p')).color }));
    await page.wait('thème repris', () => document.documentElement.dataset.theme === 'tokyo-night');
    const nuit = await peinture();
    assert.match(nuit.schema, /dark/); assert.equal(nuit.papier, 'rgb(26, 27, 38)');
    assert.equal(await page.evaluate(() => document.querySelectorAll('#r-theme optgroup').length), 2);
    await page.evaluate(() => { const s = document.getElementById('r-theme'); s.value = 'rose-pine-dawn'; s.dispatchEvent(new Event('input', { bubbles: true })); });
    const aube = await peinture();
    assert.match(aube.schema, /light/); assert.notEqual(aube.papier, nuit.papier);
    await page.evaluate(() => { const s = document.getElementById('r-theme'); s.value = 'tokyo-night'; s.dispatchEvent(new Event('input', { bubbles: true })); });
    await page.send('Emulation.setEmulatedMedia', { media: 'print' });
    const papier = await peinture();
    await page.send('Emulation.setEmulatedMedia', { media: '' });
    const [r, v, b] = papier.encre.match(/\d+/g).map(Number);
    assert.ok((r + v + b) / 3 < 128, 'encre foncée sur le papier : ' + papier.encre);
    await page.evaluate(() => { const s = document.getElementById('r-theme'); s.value = 'system'; s.dispatchEvent(new Event('input', { bubbles: true })); });
    await new Promise((r) => setTimeout(r, 500));
    await page.send('Emulation.clearDeviceMetricsOverride');
  }],
  ['marge dense : 40 cartes aux trois largeurs (P12.4)', async ({ p, page }) => {
    const source = '# Marge dense\n\n' + Array.from({ length: 40 }, (_, i) => {
      const passage = i % 2 ? '{~~formulation ancienne~>formulation proposée~~}' : '{==passage commenté==}{>>Préciser ce passage.<<}';
      return `Paragraphe ${i + 1} : ${passage}. Une phrase ordinaire pour conserver le contexte.\n\n`;
    }).join('');
    await p.write(source); await page.navigate(p.state.url); await waitCards(page, 40);
    for (const largeur of ['16rem', '18rem', '20rem']) {
      await page.evaluate((v) => {
        document.documentElement.style.setProperty('--marge', v);
        window.dispatchEvent(new Event('resize'));
      }, largeur);
      await page.wait('40 cartes disposées sans recouvrement à ' + largeur, () => {
        const cartes = [...document.querySelectorAll('#marge .carte')];
        const badges = [...document.querySelectorAll('#texte .g-ancre')];
        const rects = cartes.map((c) => c.getBoundingClientRect());
        return cartes.length === 40 && badges.length === 40 &&
          Math.abs(rects[0].top - (badges[0].getBoundingClientRect().top - 6)) <= 2 &&
          rects.every((r, i) => !i || r.top >= rects[i - 1].bottom + 5) &&
          cartes.every((c) => c.scrollWidth <= c.clientWidth + 1);
      });
      await page.evaluate(() => document.querySelector('#marge .carte').focus());
      await page.wait('actions accessibles au focus', () => document.querySelector('#marge .carte').classList.contains('active'));
      const boutons = await page.evaluate(() => [...document.querySelectorAll('#marge .carte.active .outils button')].map((b) =>
        ({ label: b.getAttribute('aria-label'), texte: b.textContent, h: b.getBoundingClientRect().height })));
      for (const b of boutons) { assert.equal(b.texte, b.label); assert.ok(b.h >= 28 && b.h <= 32); }
      await page.evaluate(() => document.getElementById('texte').click());
    }
    assert.equal((await p.read()).toString(), source, 'la consultation ne change aucun octet');
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
  ['barre du lecteur P20 : trois bandes, « Plus » aux largeurs moyennes, options utiles seulement', async ({ p, page }) => {
    try {
      for (const w of [1440, 1366, 1100, 960]) {
        await page.send('Emulation.setDeviceMetricsOverride', { width: w, height: 800, deviceScaleFactor: 1, mobile: false });
        await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
        const m = await page.evaluate(() => {
          const vu = (id) => document.getElementById(id).getClientRects().length > 0;
          const barre = document.getElementById('barre');
          const dehors = [...barre.querySelectorAll('button, select, a, h1')].filter((e) => e.getClientRects().length && e.getBoundingClientRect().right > innerWidth + 0.5).map((e) => e.id || e.textContent);
          return { dehors, large: document.documentElement.scrollWidth - document.documentElement.clientWidth, annuler: vu('annuler'), plus: vu('plus-bouton'),
            mission: vu('mission-choix'), typo: vu('typo-choix'), sortie: vu('voir-sortie'), portee: document.getElementById('portee').textContent };
        });
        assert.deepEqual([m.dehors, m.large], [[], 0], w + ' : rien ne déborde');
        assert.equal(m.annuler, w > 1280, w + ' : Annuler dans la barre au-delà de 1280 px');
        assert.equal(m.plus, w <= 1280, w + ' : « Plus » à 1280 px et moins');
        assert.deepEqual([m.mission, m.typo, m.sortie, m.portee], [false, false, false, 'a.md'], w + ' : options masquées quand elles ne servent pas');
      }
      // « Plus » relaie Annuler et en recopie l'état.
      await page.send('Emulation.setDeviceMetricsOverride', { width: 1100, height: 800, deviceScaleFactor: 1, mobile: false });
      await page.click('#plus-bouton');
      await page.wait('menu « Plus » ouvert', () => !document.getElementById('plus-menu').hidden);
      assert.equal(await page.evaluate(() => document.querySelector('#plus-menu [data-relais="annuler"]').disabled), true, 'Annuler désactivé comme l’original');
      await page.key('Escape');
      await page.wait('menu refermé, focus rendu', () => document.getElementById('plus-menu').hidden && document.activeElement.id === 'plus-bouton');
      await openComment(page, 'Le chat dort.'); await page.fill('#saisie-texte', 'Une remarque.'); await page.click('#saisie-ok'); await waitCards(page, 1);
      await page.click('#plus-bouton');
      await page.click('#plus-menu [data-relais="annuler"]');
      await waitCards(page, 0);
      assert.equal((await p.read()).toString(), ordinary, 'annulé par « Plus » : octets d’origine');
    } finally {
      await page.send('Emulation.clearDeviceMetricsOverride');
    }
  }],
  ['consignes P20 : interrupteurs, segments et aides des accès, enregistrés puis relus', async ({ p, page }) => {
    const set = new URL('/set#t=' + p.state.token, p.state.url).href;
    await page.navigate(set, '#ra-statut');
    await page.wait('accès affichés', () => document.querySelectorAll('#ra-acces [role=switch]').length === 3);
    const champDossiers = () => page.evaluate(() => document.querySelector('#ra-acces textarea').closest('.ra-champ').hidden);
    assert.equal(await champDossiers(), true, 'liste des dossiers masquée sans dossier choisi');
    await page.evaluate(() => {
      document.querySelector('#ra-acces [role=switch]').click();
      [...document.querySelectorAll('#ra-acces .ra-segments button')].find((b) => b.textContent === 'Python').click();
      [...document.querySelectorAll('#ra-acces .ra-segments button')].find((b) => b.textContent === 'Dossiers choisis').click();
    });
    assert.equal(await champDossiers(), false, 'liste des dossiers montrée');
    await page.fill('#ra-acces textarea', 'docs');
    await page.fill('#ra-acces input', 'zotero, juridique');
    assert.match(await page.evaluate(() => document.getElementById('ra-statut').textContent), /attendent votre enregistrement/);
    // Une aide se lit au focus, sans clic.
    await page.evaluate(() => document.querySelector('#ra-acces .ra-info').focus());
    await page.key('Tab', 0); await page.evaluate(() => document.querySelector('#ra-acces .ra-info').focus());
    assert.equal(await page.evaluate(() => getComputedStyle(document.querySelector('#ra-acces .ra-bulle')).visibility), 'visible');
    await page.click('#ra-enregistrer');
    await page.wait('enregistré', () => /^Enregistré/.test(document.getElementById('ra-statut').textContent));
    const acces = await page.evaluate(async (t) => (await (await fetch('/api/agents', { headers: { 'X-Glose-Token': t } })).json()).config.access, p.state.token);
    assert.deepEqual([acces.web, !!acces.subagents, acces.scripts, acces.externalDirs, acces.dirs, acces.mcp], [true, false, 'python', 'list', ['docs'], ['zotero', 'juridique']]);
    await page.navigate(set, '#ra-statut');
    await page.wait('accès relus', () => document.querySelector('#ra-acces [role=switch]')?.getAttribute('aria-checked') === 'true' &&
      document.querySelector('#ra-acces .ra-segments button[aria-pressed=true]')?.textContent === 'Python');
  }],
  ['réduction des barres à 1366 × 768 : hauteur de texte, sans saut (P12.3)', async ({ p, page }) => {
    await p.stop();
    await p.write('# Titre\n\n' + 'Un paragraphe ordinaire, pour faire défiler le texte de cette page assez longtemps.\n\n'.repeat(80));
    await p.start();
    await page.send('Emulation.setDeviceMetricsOverride', { width: 1366, height: 768, deviceScaleFactor: 1, mobile: false });
    try {
      await page.navigate(p.state.url);
      const mesure = () => page.evaluate(() => {
        const barre = document.getElementById('barre').getBoundingClientRect();
        const pied = document.getElementById('pied').getBoundingClientRect();
        const p = [...document.querySelectorAll('#texte .rendu:not(.garde) p')][30].getBoundingClientRect();
        return { barre: barre.bottom, utile: pied.top - barre.bottom, repere: p.top + scrollY, reduite: document.body.classList.contains('barres-reduites'), large: document.documentElement.scrollWidth - document.documentElement.clientWidth };
      });
      // Polices et réglages posés : ils changent les coupures de ligne, donc le repère.
      await page.evaluate(async () => { await document.fonts.ready; await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))); });
      const avant = await mesure();
      assert.equal(avant.large, 0, 'pas de débordement');
      await page.evaluate(() => window.scrollTo(0, 900));
      await page.wait('barres réduites', () => document.body.classList.contains('barres-reduites'));
      await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
      const apres = await mesure();
      assert.ok(apres.barre <= 48, 'une seule ligne, 48 px au plus : ' + apres.barre);
      assert.ok(apres.utile >= 768 - 56 - 32, 'au moins la hauteur de texte d’avant la refonte : ' + apres.utile);
      assert.ok(Math.abs(apres.repere - avant.repere) <= 2, 'le texte ne saute pas : ' + avant.repere + ' / ' + apres.repere);
      // Rien n'est perdu dans la ligne compacte.
      assert.ok(await page.evaluate(() => document.getElementById('retablir').getBoundingClientRect().right <= document.getElementById('exporter').getBoundingClientRect().left), 'export après Rétablir');
      for (const id of ['remettre', 'annuler', 'retablir', 'exporter', 'vues', 'bouton-reglages', 'ouvrir-recherche', 'retour-accueil']) {
        assert.ok(await page.evaluate((id) => { const r = document.getElementById(id).getBoundingClientRect(); return r.width > 0 && r.top >= 0 && r.bottom <= 48; }, id), id + ' dans la ligne compacte');
      }
      await page.click('#ouvrir-recherche');
      await page.wait('recherche rouverte', () => document.activeElement.id === 'recherche-texte' && document.getElementById('recherche-texte').getBoundingClientRect().width > 0);
      await page.key('Escape');
      await page.key('Escape');
      assert.ok(!(await p.read()).toString().includes('{'), 'rien d’écrit');
    } finally {
      await page.send('Emulation.clearDeviceMetricsOverride');
    }
  }],
  ['panneau Commenter en bas de page : entier, sans défilement, passage visible', async ({ p, page }) => {
    await p.stop();
    await p.write('# Titre\n\n' + 'Un paragraphe ordinaire pour remplir la page assez longtemps.\n\n'.repeat(60));
    await p.start();
    for (const [w, h] of [[1600, 1000], [1366, 768]]) {
      await page.send('Emulation.setDeviceMetricsOverride', { width: w, height: h, deviceScaleFactor: 1, mobile: false });
      await page.navigate(p.state.url);
      await page.evaluate((h) => {
        const ps = [...document.querySelectorAll('#texte .rendu:not(.garde) p')];
        const q = ps.find((x) => x.getBoundingClientRect().top > h - 160);
        const sp = q.querySelectorAll('span[data-g]'), r = document.createRange();
        r.setStartBefore(sp[2]); r.setEndAfter(sp[8]);
        const s = getSelection(); s.removeAllRanges(); s.addRange(r);
        document.getElementById('bouton-commenter').click();
      }, h);
      await page.wait('panneau ouvert', () => document.getElementById('saisie').open);
      await page.evaluate(() => new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r))));
      const m = await page.evaluate(() => {
        const d = document.getElementById('saisie'), c = d.querySelector('.corps'), ok = document.getElementById('saisie-ok').getBoundingClientRect();
        const b = d.getBoundingClientRect(), bq = document.getElementById('saisie-extrait').getBoundingClientRect(), pied = document.getElementById('pied').getBoundingClientRect();
        return { defile: c.scrollHeight - c.clientHeight, bas: b.bottom, pied: pied.top, okBas: ok.bottom, extrait: bq.height };
      });
      assert.ok(m.defile <= 1, w + ' : le corps du panneau ne défile pas : ' + JSON.stringify(m));
      assert.ok(m.bas <= m.pied && m.okBas <= m.pied, w + ' : le panneau finit au-dessus du pied : ' + JSON.stringify(m));
      assert.ok(m.extrait > 20, w + ' : extrait lisible : ' + JSON.stringify(m));
      await page.key('Escape');
      await page.wait('fermé', () => !document.getElementById('saisie').open);
    }
    await page.send('Emulation.clearDeviceMetricsOverride');
  }],
  ['panneau de saisie ouvert : ni décision, ni remise, ni second panneau (P12.7)', async ({ p, page }) => {
    await p.stop();
    await p.write('Un premier {++ajout++}{>>glose:v1 id=aaaaaa by=agent<<} dans le texte.\n\nUn second paragraphe assez simple.\n');
    await p.start(); await page.navigate(p.state.url);
    await waitCards(page, 1);
    await page.evaluate(() => {
      const q = [...document.querySelectorAll('#texte .rendu:not(.garde) p')].find((x) => x.textContent.includes('second'));
      const sp = q.querySelectorAll('span[data-g]'), r = document.createRange();
      r.setStartBefore(sp[0]); r.setEndAfter(sp[2]);
      const s = getSelection(); s.removeAllRanges(); s.addRange(r);
    });
    await page.evaluate(() => document.getElementById('bouton-commenter').click());
    await page.wait('panneau ouvert', () => document.getElementById('saisie').open);
    await page.fill('#saisie-texte', 'Ma remarque en cours.');
    const avant = (await p.read()).toString();
    await page.evaluate(() => document.querySelector('#marge button[aria-label="Accepter"]').click());
    await page.evaluate(() => document.getElementById('remettre').click());
    await page.evaluate(() => new Promise((r) => setTimeout(r, 300)));
    assert.equal(await page.evaluate(() => document.getElementById('remise').open), false, 'pas de remise');
    assert.equal(await page.evaluate(() => document.getElementById('saisie-texte').value), 'Ma remarque en cours.', 'saisie gardée');
    assert.equal((await p.read()).toString(), avant, 'aucune décision écrite');
    await page.key('Escape');
    await page.wait('panneau refermé', () => !document.getElementById('saisie').open);
  }],
  ['poignée « + » d’un paragraphe et son menu', async ({ p, page }) => {
    await p.stop();
    await p.write('# Titre\n\nPremier paragraphe assez simple.\n\nSecond paragraphe assez simple.\n\n- un élément\n- un autre\n');
    await p.start(); await page.navigate(p.state.url);
    await page.wait('texte', () => document.querySelector('#texte .rendu:not(.garde) p'));
    const survol = async (texte) => {
      const pos = await page.evaluate((texte) => {
        const b = [...document.querySelectorAll('#texte .rendu:not(.garde) p, #texte .rendu:not(.garde) li')].find((x) => x.textContent.includes(texte));
        b.scrollIntoView({ block: 'center', behavior: 'instant' });
        const r = b.getBoundingClientRect();
        return { x: r.x + 20, y: r.y + r.height / 2 };
      }, texte);
      await page.send('Input.dispatchMouseEvent', { type: 'mouseMoved', ...pos });
    };
    const aligne = (texte) => page.wait('poignée à gauche de « ' + texte + ' »', (texte) => {
      const o = document.getElementById('outils-bloc');
      if (o.hidden) return false;
      const b = [...document.querySelectorAll('#texte .rendu:not(.garde) p, #texte .rendu:not(.garde) li')].find((x) => x.textContent.includes(texte)).getBoundingClientRect();
      const r = document.getElementById('poignee').getBoundingClientRect();
      return r.width > 0 && r.right <= b.left && r.left >= 0 && r.bottom > b.top && r.top < b.top + 40
        && document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2)?.closest('#poignee');
    }, texte);
    await survol('Premier'); await aligne('Premier');
    await survol('Second'); await aligne('Second');
    await survol('un autre'); await page.wait('poignée sur un élément de liste', () => !document.getElementById('outils-bloc').hidden);
    await survol('Second'); await aligne('Second');
    await page.click('#poignee');
    await page.wait('menu ouvert', () => document.getElementById('outils-bloc').classList.contains('ouvert'));
    const items = await page.evaluate(() => [...document.querySelectorAll('#menu-bloc button')].map((b) => [b.id, b.getBoundingClientRect().width > 0]));
    assert.deepEqual(items, [['menu-avant', true], ['menu-commenter', true], ['menu-suggerer', true], ['menu-apres', true]], 'quatre gestes visibles, dans cet ordre');
    await page.key('Escape');
    await page.wait('menu refermé', () => !document.getElementById('outils-bloc').classList.contains('ouvert'));
    for (const [id, panneau, titre] of [['menu-commenter', 'saisie', 'Second'], ['menu-suggerer', 'edition', 'Second'], ['menu-avant', 'edition', ''], ['menu-apres', 'edition', '']]) {
      await survol('Premier'); await survol('Second'); await aligne('Second');
      await page.click('#poignee');
      await page.click('#' + id);
      await page.wait('panneau ' + panneau + ' depuis ' + id, (panneau) => document.getElementById(panneau).open, panneau);
      if (id === 'menu-avant' || id === 'menu-apres') {
        const types = await page.evaluate(() => { const t = document.getElementById('edition-types'); return !t.hidden && [...t.querySelectorAll('button')].map((b) => b.dataset.type); });
        assert.deepEqual(types, ['paragraphe', 'titre', 'liste', 'tableau', 'encadre', 'image', 'code'], 'types proposés');
      }
      await page.key('Escape');
      await page.wait('panneau fermé', (panneau) => !document.getElementById(panneau).open, panneau);
      // Le menu d'où il venait est refermé : le focus revient au texte, et non nulle part.
      await page.wait('focus rendu au texte', () => document.activeElement.id === 'texte');
    }
    // Un événement « close » en retard ne reprend pas un focus déjà placé ailleurs.
    await survol('Premier'); await survol('Second'); await aligne('Second');
    await page.click('#poignee');
    await page.click('#menu-apres');
    await page.wait('panneau rouvert', () => document.getElementById('edition').open);
    await page.evaluate(() => { const d = document.getElementById('edition'); d.close(); document.getElementById('bouton-reglages').focus(); });
    await page.evaluate(() => new Promise((r) => setTimeout(r, 50)));
    assert.equal(await page.evaluate(() => document.activeElement.id), 'bouton-reglages');
    // Au clavier : la touche + ouvre le menu, les flèches le parcourent, Entrée agit.
    await focusText(page);
    await page.evaluate(() => document.dispatchEvent(new KeyboardEvent('keydown', { key: '+', bubbles: true, cancelable: true })));
    await page.wait('menu ouvert au clavier', () => document.getElementById('outils-bloc').classList.contains('ouvert') && document.activeElement.id === 'menu-commenter');
    assert.ok(await page.evaluate(() => { const r = document.getElementById('poignee').getBoundingClientRect(); return r.width > 0 && r.top >= 0 && r.bottom <= innerHeight; }), 'poignée visible à l’écran');
    await page.key('ArrowDown');
    assert.equal(await page.evaluate(() => document.activeElement.id), 'menu-suggerer');
    await page.key('Escape');
    await page.wait('menu refermé au clavier', () => !document.getElementById('outils-bloc').classList.contains('ouvert') && document.activeElement.id === 'poignee');
    assert.ok(!(await p.read()).toString().includes('{'), 'aucune marque écrite sans saisie');
    // Pendant le tour de l'agent : pas de poignée, l'auteur lit.
    await page.evaluate(() => document.getElementById('remettre').click());
    await page.wait('boîte de remise', () => document.getElementById('remise').open);
    await page.click('#remise-ok');
    await page.wait('main à l’agent', () => document.body.classList.contains('main-agent'));
    await survol('Premier');
    await page.wait('poignée masquée', () => {
      const o = document.getElementById('outils-bloc');
      return o.hidden || getComputedStyle(o).display === 'none' || getComputedStyle(o).visibility === 'hidden';
    });
    await page.evaluate(() => document.dispatchEvent(new KeyboardEvent('keydown', { key: '+', bubbles: true, cancelable: true })));
    assert.equal(await page.evaluate(() => document.getElementById('outils-bloc').classList.contains('ouvert')), false, 'pas de menu au clavier pendant le tour de l’agent');
  }],
  ['accueil : trois projets dont un introuvable, ouvrir, modifier, créer (P11.5, P11.9)', async ({ p, page, root }) => {
    await p.stop();
    const base = path.join(p.dir, 'mes-projets');
    // Le dossier temporaire du système n'entre pas au registre : il est ailleurs le temps de l'essai.
    // The system temporary directory elsewhere: TMPDIR under Unix, TMP and
    // TEMP under Windows.
    p.env.TMPDIR = p.env.TMP = p.env.TEMP = path.join(p.dir, 'systeme-tmp'); await fs.mkdir(p.env.TMPDIR);
    const projet = async (nom, profil) => {
      const d = path.join(base, nom); await fs.mkdir(d, { recursive: true });
      await fs.writeFile(path.join(d, 'a.md'), '# Titre\n\nUn {>>mot<<} texte.\n');
      await fs.writeFile(path.join(d, 'glose.json'), profil);
      return d;
    };
    const livre = await projet('livre', '{\r\n  "title": "Livre <b>gras</b>",\r\n  "project": {\r\n    "type": "ouvrage",\r\n    "status": "review",\r\n    "deadline": "2020-01-01"\r\n  }\r\n}\r\n');
    const note = await projet('note', '{"title":"Une note"}');
    const disparu = path.join(base, 'disparu');
    await fs.mkdir(p.env.GLOSE_CONFIG_DIR, { recursive: true });
    await fs.writeFile(path.join(p.env.GLOSE_CONFIG_DIR, 'projets.json'), JSON.stringify({ version: 1, projects: [livre, note, disparu].map((root) => ({ root, opened: '2026-11-01T10:00:00Z' })) }));
    await fs.mkdir(path.join(p.env.GLOSE_CONFIG_DIR, 'gabarits/article'), { recursive: true });
    await fs.writeFile(path.join(p.env.GLOSE_CONFIG_DIR, 'gabarits/article/plan.md'), '# Plan\n');
    await p.start();
    await page.navigate(`http://127.0.0.1:${p.state.port}/home#t=${p.state.token}`, '#acc-lignes tr');
    assert.equal(await page.evaluate(() => location.hash), '', 'jeton retiré de la barre d’adresse');
    const lignes = () => page.evaluate(() => [...document.querySelectorAll('#acc-lignes tr')].map((tr) => tr.textContent));
    await page.wait('trois lignes', () => document.querySelectorAll('#acc-lignes tr').length === 3);
    const texte = await lignes();
    assert.match(texte[0], /Livre <b>gras<\/b>/, 'titre laissé en texte');
    assert.match(texte[0], /Dépassée/);
    assert.match(texte.join(' '), /Dossier introuvable/);
    assert.equal(await page.evaluate(() => !!document.querySelector('#acc-lignes b')), false, 'aucun HTML venu du projet');
    assert.match(await page.evaluate(() => document.getElementById('acc-stats').textContent), /Projets\s*3/);
    // Recherche et filtre.
    await page.fill('#acc-recherche', 'note');
    await page.wait('recherche', () => document.querySelectorAll('#acc-lignes tr').length === 1 && document.querySelector('#acc-lignes tr').textContent.includes('Une note'));
    await page.fill('#acc-recherche', '');
    await page.wait('recherche effacée', () => document.querySelectorAll('#acc-lignes tr').length === 3);
    // Modifier : seule la valeur change ; une empreinte périmée garde la saisie.
    await page.evaluate(() => [...document.querySelectorAll('#acc-lignes tr')].find((t) => t.textContent.includes('Livre')).querySelector('button[aria-label*="Modifier"], button[title*="Modifier"]').click());
    await page.wait('formulaire', () => document.getElementById('acc-modifier').open);
    await fs.writeFile(path.join(livre, 'glose.json'), (await fs.readFile(path.join(livre, 'glose.json'), 'utf8')).replace('Livre', 'Livre revu'));
    await page.evaluate(() => { const f = document.getElementById('acc-form-modifier'); f.elements.nextAction.value = 'Relire la partie 2'; });
    await page.evaluate(() => document.getElementById('acc-form-modifier').requestSubmit());
    await page.wait('conflit dit', () => /a changé/.test(document.getElementById('acc-modifier').textContent) && document.getElementById('acc-modifier').open);
    assert.equal(await page.evaluate(() => document.getElementById('acc-form-modifier').elements.nextAction.value), 'Relire la partie 2', 'saisie gardée');
    assert.ok(!(await fs.readFile(path.join(livre, 'glose.json'), 'utf8')).includes('Relire'), 'rien d’écrit');
    await page.evaluate(() => document.getElementById('acc-modifier').close());
    await page.evaluate(() => document.dispatchEvent(new Event('visibilitychange')));
    await page.wait('liste relue', () => [...document.querySelectorAll('#acc-lignes tr')].some((t) => t.textContent.includes('Livre revu')));
    await page.evaluate(() => [...document.querySelectorAll('#acc-lignes tr')].find((t) => t.textContent.includes('Livre')).querySelector('button[aria-label*="Modifier"], button[title*="Modifier"]').click());
    await page.wait('formulaire de nouveau', () => document.getElementById('acc-modifier').open);
    await page.evaluate(() => { const f = document.getElementById('acc-form-modifier'); f.elements.nextAction.value = 'Relire la partie 2'; f.requestSubmit(); });
    await page.wait('ligne à jour', () => [...document.querySelectorAll('#acc-lignes tr')].some((t) => t.textContent.includes('Relire la partie 2')));
    assert.equal(await fs.readFile(path.join(livre, 'glose.json'), 'utf8'), '{\r\n  "title": "Livre revu <b>gras</b>",\r\n  "project": {\r\n    "type": "ouvrage",\r\n    "status": "review",\r\n    "deadline": "2020-01-01",\r\n    "nextAction": "Relire la partie 2"\r\n  }\r\n}\r\n', 'seule la valeur ajoutée');
    // Nouveau projet : erreurs, puis création sans écraser.
    const neuf = path.join(base, 'neuf');
    await page.evaluate(() => [...document.querySelectorAll('button')].find((b) => /Nouveau projet/.test(b.textContent)).click());
    await page.wait('formulaire nouveau', () => document.getElementById('acc-nouveau').open);
    assert.deepEqual(await page.evaluate(() => [...document.getElementById('acc-form-nouveau').elements.type.options].map((o) => o.value)), ['', 'article']);
    const soumettre = (chemin, type) => page.evaluate((chemin, type) => { const f = document.getElementById('acc-form-nouveau'); f.elements.path.value = chemin; f.elements.type.value = type; f.requestSubmit(); }, chemin, type);
    await soumettre(neuf, 'article');
    await page.wait('dossier absent dit', () => /n'existe pas/.test(document.getElementById('acc-nouveau').textContent));
    await fs.mkdir(neuf); await fs.writeFile(path.join(neuf, 'plan.md'), '# À moi\n');
    await soumettre(livre, 'article');
    await page.wait('projet existant dit', () => /déjà un projet/.test(document.getElementById('acc-nouveau').textContent));
    await soumettre(neuf, 'article');
    await page.wait('créé', () => !document.getElementById('acc-nouveau').open && document.querySelectorAll('#acc-lignes tr').length === 4);
    assert.equal(await fs.readFile(path.join(neuf, 'plan.md'), 'utf8'), '# À moi\n', 'rien d’écrasé');
    // Retirer : le dossier n'est pas touché.
    await page.evaluate(() => [...document.querySelectorAll('#acc-lignes tr')].find((t) => t.textContent.includes('Dossier introuvable')).querySelector('button').click());
    await page.wait('confirmation', () => document.getElementById('acc-retirer').open);
    await page.evaluate(() => document.getElementById('acc-form-retirer').requestSubmit());
    await page.wait('retiré', () => document.querySelectorAll('#acc-lignes tr').length === 3);
    // Ouvrir : un autre serveur, l'adresse à jeton n'arrive que par ce clic ; et retour.
    await page.evaluate(() => [...document.querySelectorAll('#acc-lignes tr')].find((t) => t.textContent.includes('Une note')).click());
    await page.wait('projet ouvert', () => location.pathname === '/' && document.querySelector('#texte .rendu:not(.garde) p'));
    const ouvert = await page.evaluate(() => document.getElementById('titre').textContent);
    assert.match(ouvert, /Une note/);
    const lien = await page.evaluate(() => document.getElementById('retour-accueil')?.getAttribute('href'));
    assert.equal(lien, '/home', 'retour vers l’accueil depuis un projet');
    await page.evaluate(() => document.getElementById('retour-accueil').click());
    await page.wait('accueil depuis le projet', () => location.pathname === '/home' && document.querySelectorAll('#acc-lignes tr').length === 3);
    assert.match(await page.evaluate(() => [...document.querySelectorAll('#acc-lignes tr')].find((t) => t.textContent.includes('Une note')).textContent), /serveur en marche/i);
    p.cli('stop', note);
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
    // P11.3 : le navigateur retient les titres de chaque fichier affiché, et
    // leur clé est celle des sections marquées.
    await until('titres retenus pour l\'accueil', async () => {
      const lu = JSON.parse(await fs.readFile(path.join(p.dir, '.glose/progression.json'), 'utf8'));
      return lu.headings?.['a.md']?.includes('gl-introduction') && lu.headings['chapitre/b espace.md']?.length === 2 && lu.sections['a.md#gl-introduction'];
    });
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
    await openSearch(page);
    await page.fill('#recherche-texte', 'CIBLE INTROUVABLE');
    await page.wait('résultat global', () => document.querySelectorAll('.resultat-recherche').length === 1);
    if (process.env.GLOSE_KEEP_BROWSER_TESTS === '1') await fs.writeFile(path.join(p.dir, 'recherche.png'), Buffer.from((await page.send('Page.captureScreenshot')).data, 'base64'));
    // Every notice is kept: the poll of the disk may announce the change
    // just after the refusal, in the same place (Windows, under load).
    await page.evaluate(() => {
      window.__avisVus = [];
      new MutationObserver(() => window.__avisVus.push(document.getElementById('avis').textContent))
        .observe(document.getElementById('avis'), { childList: true, characterData: true, subtree: true });
    });
    await p.write(b + '\n', 'chapitre/b espace.md');
    await page.click('.resultat-recherche');
    await page.wait('résultat périmé refusé', () => window.__avisVus.some((t) => t.includes('relancez la recherche')));
    assert.equal(await page.evaluate(() => !!document.querySelector('#texte .g-recherche')), false);
    await p.write(b, 'chapitre/b espace.md');
    await focusText(page); await page.key('f', 10);
    await page.wait('raccourci de recherche globale', () => !document.getElementById('recherche').hidden && document.activeElement.id === 'recherche-texte');
    await page.wait('résultat actualisé', () => document.querySelectorAll('.resultat-recherche').length === 1);
    await page.click('.resultat-recherche');
    await page.wait('résultat ouvert et surligné', () => document.getElementById('recherche').hidden && document.querySelector('#texte .g-recherche')?.textContent === 'Cible');
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
    const skip = process.platform === 'win32' && shellAgents.has(name) && 'faux agent en script shell, sans équivalent sous Windows';
    await t.test(name, { timeout: 45000, skip }, async () => {
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
        if (page) { await page.evaluate(() => document.fonts.ready.then(() => true)).catch(() => {}); await page.close().catch(() => {}); }
        await p.stop();
      }
    });
  }
});

module.exports = { Project, select, openComment, openEdit, savedDraft, waitCards, openDrafts, focusText, rejectAll };
