// Exercise the extracted module without a server or a browser.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const path = require('node:path');

class Element {
  constructor(tag, classes = '', text = '') {
    this.tag = tag; this.children = []; this.dataset = {}; this.style = {}; this.attrs = {}; this.events = {};
    this.text = text; this.classes = new Set(classes.split(' ').filter(Boolean));
    this.classList = { add: (c) => this.classes.add(c), contains: (c) => this.classes.has(c),
      toggle: (c, on) => on ? this.classes.add(c) : this.classes.delete(c) };
  }
  append(...nodes) { this.children.push(...nodes); }
  querySelector(selector) {
    const classe = /^:scope > button\.([\w-]+)$/.exec(selector);
    assert.ok(classe, 'sélecteur du double DOM pris en charge : ' + selector);
    return this.children.find((c) => c.tag === 'button' && c.classes.has(classe[1])) || null;
  }
  prepend(node) { this.children = [node, ...this.children.filter((c) => c !== node)]; }
  get childNodes() { return this.children; }
  get lastChild() { return this.children.at(-1); }
  get textContent() { return this.text + this.children.map((c) => c.textContent).join(''); }
  set textContent(text) { this.text = text; this.children = []; }
  setAttribute(k, v) { this.attrs[k] = v; }
  addEventListener(k, v) { this.events[k] = v; }
  getBoundingClientRect() { return { top: this.top || 0 }; }
}
function setup(position = () => "absolute") {
  const frame = [];
  const context = { window: {}, document: { createTextNode: (t) => new Element('#text', '', t) },
    getComputedStyle: () => ({ position: position() }), requestAnimationFrame: (fn) => (frame.push(fn), frame.length) };
  vm.runInNewContext(fs.readFileSync(path.join(__dirname, '../web/marge.js'), 'utf8'), context);
  const marge = new Element('aside'), S = { doc: { files: [{ path: 'notes.md' }] }, cartes: [], actif: -1 };
  const actions = [];
  const el = (tag, cls, txt) => new Element(tag, cls, txt);
  const deps = { $: (id) => { assert.equal(id, 'marge'); return marge; }, S, el, md: el, riche: el,
    icone: (nom) => `<svg data-icone="${nom}"></svg>`, court: (t, n) => t.slice(0, n), modeleCourt: (t) => t.split('/').at(-1),
    NOMS: { comment: 'Commentaire', highlight: 'Passage commenté', substitution: 'Remplacement proposé', addition: 'Ajout proposé', deletion: 'Suppression proposée' },
    PROPOSITION: { addition: true, deletion: true, substitution: true }, QUI: { author: 'Auteur', agent: 'Agent' },
    modifiable: (m) => m.status === 'new', teteModifiable: () => null, corrigeable: () => true,
    outilsMessage: () => el('span', 'msg-outils') };
  for (const key of ['activer', 'modifier', 'retirer', 'resoudre', 'corriger', 'repondre', 'decider', 'appliquerCorrection']) deps[key] = (...args) => actions.push([key, ...args]);
  return { ...context.window.GloseMarge.creer(deps), S, marge, actions, flush: () => frame.shift()() };
}
const annotation = (extra = {}) => ({ file: 0, line: 5, kind: 'substitution', text: 'Avant', new: 'Après',
  by: 'agent', via: 'Sol', model: 'openai/gpt-5', status: 'open', turn: 5, messages: [], ...extra });
function descendants(node) { return [node, ...node.children.flatMap(descendants)]; }

test('cartes : dépendances injectées, signatures, actions et corrections', () => {
  const m = setup();
  for (const kind of ['comment', 'highlight', 'addition', 'deletion', 'substitution']) {
    const a = annotation({ kind, to: 'Luna', messages: [{ by: 'agent', via: 'Sol', model: 'openai/gpt-5', text: 'Avis', turn: 5 }] });
    const c = m.fabriquerCarte(a, 0), nodes = descendants(c);
    assert.match(c.textContent, /Sol · gpt-5/);
    assert.match(c.textContent, /remis au tour 3/);
    const dest = nodes.find((n) => n.classes.has('pour'));
    assert.equal(dest.textContent, 'Luna'); assert.match(dest.innerHTML, /fleche/);
    const buttons = nodes.filter((n) => n.tag === 'button');
    assert.equal(buttons.some((n) => n.attrs['aria-label'] === 'Accepter'), !['comment', 'highlight'].includes(kind));
    for (const b of buttons) { assert.equal(b.textContent, b.attrs['aria-label']); assert.ok(b.dataset.infobulle); }
    if (kind === 'substitution') {
      const accepter = buttons.find((b) => b.attrs['aria-label'] === 'Accepter');
      assert.match(accepter.dataset.infobulle, /touche A/);
      accepter.events.click({ stopPropagation() {} }); assert.deepEqual(m.actions.at(-1), ['decider', a, 'accept']);
    }
    c.events.focusin(); assert.deepEqual(m.actions.at(-1), ['activer', 0, false]);
  }
  const c = m.fabriquerCarte(annotation({ by: 'author', status: 'new', messages: [{ fix: true, text: '∅' }] }), 1);
  assert.match(c.textContent, /de ce tour/); assert.match(c.textContent, /Accepter cette version/); assert.match(c.textContent, /Reprendre/);
});

test('40 cartes : alignement à −6 px, absence de recouvrement et dépliage sans déplacement', () => {
  const m = setup(); m.marge.top = 100;
  m.S.cartes = Array.from({ length: 40 }, (_, i) => {
    const carte = new Element('div', 'carte'), badge = new Element('span');
    badge.top = 140 + i * 20; carte.scrollHeight = i % 2 ? 90 : 600; carte.offsetHeight = 216; carte.clientHeight = 214;
    return { carte, badge };
  });
  m.disposer(); m.flush();
  const tops = m.S.cartes.map((c) => parseFloat(c.carte.style.top));
  assert.equal(tops[0], 34);
  for (let i = 1; i < tops.length; i++) assert.ok(tops[i] >= tops[i - 1] + (i % 2 ? 216 : 92) + 6);
  m.S.cartes[0].carte.classList.add('active'); m.S.cartes[0].carte.offsetHeight = 602; m.S.cartes[0].carte.clientHeight = 600;
  m.disposer(); m.flush();
  assert.deepEqual(m.S.cartes.map((c) => parseFloat(c.carte.style.top)), tops);
  assert.ok(m.S.cartes[0].carte.classList.contains('longue'));
});

test('panneau étroit : mêmes cartes en flux, puis retour à la marge alignée', () => {
  let position = 'absolute';
  const m = setup(() => position);
  const carte = new Element('div', 'carte'), badge = new Element('span');
  carte.scrollHeight = 600; carte.offsetHeight = 216; carte.clientHeight = 214; badge.top = 140;
  m.S.cartes = [{ carte, badge }];
  m.disposerMaintenant(); assert.equal(carte.style.top, '134px');
  position = 'relative'; m.disposerMaintenant();
  assert.equal(m.marge.style.minHeight, '');
  assert.equal(m.S.cartes[0].carte, carte);
  position = 'absolute'; badge.top = 240; m.disposerMaintenant();
  assert.equal(carte.style.top, '234px');
});
