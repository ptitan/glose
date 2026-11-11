// Non-modal panel geometry, focus and IME guards without a browser.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

function setup({ large = false, bottom = 280 } = {}) {
  class Element extends EventTarget {
    constructor(rect = {}) {
      super(); this.rect = rect; this.style = {}; this.isConnected = true; this.scrollHeight = 500;
      this.classes = new Set(); this.classList = { add: (c) => this.classes.add(c), remove: (c) => this.classes.delete(c),
        toggle: (c, on) => on ? this.classes.add(c) : this.classes.delete(c) };
    }
    setAttribute() {}
    getBoundingClientRect() { return this.rect; }
    getClientRects() { return [this.rect]; }
    contains(x) { return x === this; }
    focus() { document.activeElement = this; }
    show() { this.open = true; }
    showModal() { assert.fail('Les éditeurs restent non modaux'); }
    close() { this.open = false; this.dispatchEvent(new Event('close')); }
  }
  const dialogue = new Element({ height: 500 }), retour = new Element();
  const ui = { pied: new Element({ height: 32 }), barre: new Element({ bottom: 150 }), texte: new Element({ left: 280, width: 600 }),
    marge: new Element({ left: 910, width: 320 }) };
  const document = new EventTarget();
  Object.assign(document, { body: new Element(), activeElement: retour, modal: null,
    createElement: () => new Element(), querySelector: (s) => s === 'dialog:modal' ? document.modal : dialogue.open ? dialogue : null });
  const window = new EventTarget(); Object.assign(window, { innerWidth: 1280, innerHeight: 1000 });
  const frames = [];
  const context = { window, document, Event, setTimeout, clearTimeout,
    requestAnimationFrame: (fn) => (frames.push(fn), frames.length), ResizeObserver: class { observe() {} } };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../web/interface.js'), 'utf8'), context);
  const cible = new Element({ top: 240, bottom });
  const panneau = window.GloseInterface.creer({ $: (id) => ui[id] }).panneau({ dialogue, cible: () => cible, large });
  const key = (props = {}) => {
    const event = new Event('keydown', { cancelable: true });
    Object.assign(event, { key: 'Escape', ...props }); document.dispatchEvent(event); return event;
  };
  return { panneau, dialogue, document, window, ui, retour, cible, key, frames };
}

test('commentaire en marge, barre compacte et retour du focus', () => {
  const m = setup(); m.panneau.ouvrir();
  assert.equal(m.dialogue.style.left, '910px'); assert.equal(m.dialogue.style.width, '320px');
  assert.ok(parseFloat(m.dialogue.style.left) >= m.ui.texte.rect.left + m.ui.texte.rect.width);
  assert.equal(m.dialogue.style.top, '240px');
  m.ui.barre.rect.bottom = 48; m.cible.rect.top = -40; m.panneau.placer();
  assert.equal(m.dialogue.style.top, '60px');
  m.document.activeElement = m.dialogue;
  assert.equal(m.key().defaultPrevented, true); assert.equal(m.dialogue.open, false);
  assert.equal(m.document.activeElement, m.retour);
});

test('Échap respecte la composition IME, les boîtes et le refus de fermeture pendant un envoi', () => {
  const m = setup(); m.panneau.ouvrir();
  m.key({ isComposing: true }); assert.equal(m.dialogue.open, true);
  m.dialogue.dispatchEvent(new Event('compositionstart')); m.key(); assert.equal(m.dialogue.open, true);
  m.dialogue.dispatchEvent(new Event('compositionend'));
  m.document.modal = {}; m.key(); assert.equal(m.dialogue.open, true); m.document.modal = null;
  const refuser = (e) => e.preventDefault(); m.dialogue.addEventListener('cancel', refuser);
  m.key(); assert.equal(m.dialogue.open, true);
  m.dialogue.removeEventListener('cancel', refuser); m.key(); assert.equal(m.dialogue.open, false);
});

test('suggestion sous le passage, repli latéral si la place manque', () => {
  const sous = setup({ large: true }); sous.panneau.ouvrir();
  assert.equal(sous.dialogue.style.top, '292px'); assert.equal(sous.dialogue.style.width, '600px');
  assert.ok(parseFloat(sous.dialogue.style.top) > sous.cible.rect.bottom);
  const cote = setup({ large: true, bottom: 800 }); cote.panneau.ouvrir();
  assert.ok(cote.document.body.classes.has('edition-laterale'));
  assert.ok(parseFloat(cote.dialogue.style.left) >= cote.ui.texte.rect.left + cote.ui.texte.rect.width);
});


test('une réduction de fenêtre garde les actions accessibles en panneau latéral', () => {
  const m = setup({ large: true }); m.panneau.ouvrir();
  m.window.innerHeight = 450; m.panneau.placer();
  assert.ok(m.document.body.classes.has('edition-laterale'));
  assert.ok(parseFloat(m.dialogue.style.top) + parseFloat(m.dialogue.style.maxHeight) <= m.window.innerHeight);
});

// Audit 11 novembre : un avis n'efface plus le précédent ; chacun garde son
// temps (3 s, 8 s pour une erreur), trois au plus, le même texte n'est pas
// répété.
test('avis empilés, chacun pour son temps', () => {
  class Node { constructor() { this.children = []; this.className = ''; this._text = ''; this.style = {}; this.classList = { add() {}, remove() {}, toggle() {} }; }
    setAttribute() {} append() {} addEventListener() {}
    replaceChildren(...c) { this.children = c; }
    get textContent() { return this.children.length ? this.children.map((c) => c.textContent).join('\n') : this._text; }
    set textContent(t) { this._text = t; this.children = []; } }
  const timers = []; let now = 0;
  const context = { window: {}, document: { addEventListener() {}, createElement: () => new Node(), body: new Node() }, Event,
    setTimeout: (fn, ms) => { const t = { fn, at: now + ms }; timers.push(t); return t; },
    clearTimeout: (t) => { const i = timers.indexOf(t); if (i >= 0) timers.splice(i, 1); }, requestAnimationFrame() {} };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../web/interface.js'), 'utf8'), context);
  const el = new Node();
  const { avis } = context.window.GloseInterface.creer({ $: () => el });
  const advance = (ms) => { now += ms; for (const t of timers.filter((t) => t.at <= now)) { timers.splice(timers.indexOf(t), 1); t.fn(); } };
  avis('Le texte a changé ou ne peut pas être vérifié : relancez la recherche.', true);
  avis('Le texte a changé sur le disque : affichage mis à jour.');
  assert.match(el.textContent, /relancez la recherche/);
  assert.match(el.textContent, /affichage mis à jour/);
  assert.match(el.className, /visible/); assert.match(el.className, /erreur/);
  advance(3000);
  assert.doesNotMatch(el.textContent, /affichage mis à jour/, 'information retirée après 3 s');
  assert.match(el.textContent, /relancez la recherche/, 'erreur encore là');
  advance(5000);
  assert.equal(el.className, '');
  for (const m of ['a', 'b', 'c', 'd', 'd']) avis(m);
  assert.equal(el.children.length, 3);
  assert.equal(el.textContent, 'b\nc\nd');
});
