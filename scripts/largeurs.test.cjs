// Exercise the actual drawer and column preference handlers without a browser.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const vm = require('node:vm');
const source = fs.readFileSync('web/app.js', 'utf8');
class Element extends EventTarget {
  constructor() {
    super(); this.attrs = {}; this.isConnected = true; this.classes = new Set();
    this.classList = { contains: (c) => this.classes.has(c), toggle: (c, on) => on ? this.classes.add(c) : this.classes.delete(c) };
  }
  setAttribute(k, v) { this.attrs[k] = v; }
  focus() { this.focused = true; }
  append(node) { this.child = node; }
}
function drawer() {
  const narrow = new EventTarget(); narrow.matches = true;
  const document = new EventTarget(), toolbar = new Element(), original = new Element();
  Object.assign(document, { body: new Element(), activeElement: original, querySelector: (s) => s === '.barre-document' ? toolbar : null });
  const context = { document, matchMedia: () => narrow, el: () => new Element(), disposer() {} };
  const start = source.indexOf('  // The narrow drawer');
  const fin = 'fenetreEtroite.addEventListener("change", () => panneauAnnotations(false));';
  const end = source.indexOf(fin, start) + fin.length;
  vm.runInNewContext(source.slice(start, end) + '\nthis.drawer = { panneauAnnotations, annotationsOuvrir, annotationsFermer };', context);
  return { ...context.drawer, document, original, narrow, toolbar };
}
test('panneau étroit : ouverture, fermeture, Échap et retour du focus', () => {
  const m = drawer();
  assert.equal(m.toolbar.child, m.annotationsOuvrir);
  m.annotationsOuvrir.dispatchEvent(new Event('click'));
  assert.ok(m.document.body.classes.has('annotations-ouvertes'));
  assert.equal(m.annotationsOuvrir.attrs['aria-expanded'], 'true');
  assert.ok(m.annotationsFermer.focused);
  const key = new Event('keydown', { cancelable: true }); Object.assign(key, { key: 'Escape' });
  m.document.dispatchEvent(key);
  assert.equal(key.defaultPrevented, true);
  assert.equal(m.annotationsOuvrir.attrs['aria-expanded'], 'false');
  assert.ok(m.original.focused);
  m.panneauAnnotations(true); m.annotationsFermer.dispatchEvent(new Event('click'));
  assert.ok(!m.document.body.classes.has('annotations-ouvertes'));
});
test('panneau : composition IME respectée et fermeture au retour à une fenêtre large', () => {
  const m = drawer(); m.panneauAnnotations(true);
  const key = new Event('keydown', { cancelable: true }); Object.assign(key, { key: 'Escape', isComposing: true });
  m.document.dispatchEvent(key);
  assert.ok(m.document.body.classes.has('annotations-ouvertes'));
  m.narrow.matches = false; m.narrow.dispatchEvent(new Event('change'));
  assert.ok(!m.document.body.classes.has('annotations-ouvertes'));
  m.panneauAnnotations(true);
  assert.equal(m.annotationsOuvrir.attrs['aria-expanded'], 'false');
});
test('1024 : repli automatique sans écriture, choix explicite conservé', () => {
  for (const [saved, narrow, expected] of [[null, true, false], [null, false, true], ['0', true, true], ['1', false, false]]) {
    const query = new EventTarget(); query.matches = narrow;
    const calls = [];
    const context = { matchMedia: () => query, localStorage: { getItem: () => saved }, montrerColonne: (...args) => calls.push(args) };
    const start = source.indexOf('    const fenetreMoyenne =');
    const end = source.indexOf('    document.addEventListener', start);
    vm.runInNewContext(source.slice(start, end), context);
    assert.deepEqual(calls[0], [expected, true, false]);
    query.matches = !narrow; query.dispatchEvent(new Event('change'));
    assert.deepEqual(calls[1], [saved === null ? narrow : saved !== '1', true, false]);
  }
});
