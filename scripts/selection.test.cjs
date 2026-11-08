// Sans navigateur : node --test scripts/selection.test.cjs
const { test } = require('node:test');
const assert = require('node:assert/strict');
require('../web/donnees.js');
require('../web/selection.js');
const { lireG, premierApres, court } = globalThis.GloseDonnees;
const { creer, borneDansMot, ajusterGrapheme } = globalThis.GloseSelection;

const octets = (t) => Buffer.byteLength(t);

// Des mots contigus, comme les donne le rendu : { s, e, el }.
function motsDe(textes, depart = 0, f = 0) {
  let o = depart;
  return textes.map((t) => {
    const s = o; o += octets(t);
    return { s, e: o, el: { textContent: t, dataset: { g: `${f}:${s},${o}` } } };
  });
}
const ajuster = (mots, o, debut) => ajusterGrapheme(mots, o, debut, premierApres);

test('borneDansMot : bords, milieu, caractères de plusieurs octets', () => {
  const g = { f: 2, s: 100, e: 106, t: false };
  assert.deepEqual(borneDansMot(g, 'été', 0, true), { f: 2, o: 100, ajuste: false });
  assert.deepEqual(borneDansMot(g, 'été', 3, false), { f: 2, o: 106, ajuste: false });
  assert.deepEqual(borneDansMot(g, 'été', 1, true), { f: 2, o: 102, ajuste: false }, 'é fait deux octets');
  assert.deepEqual(borneDansMot(g, 'été', 2, false), { f: 2, o: 103, ajuste: false });
});

test('borneDansMot : un mot transformé ne se coupe pas', () => {
  const g = { f: 0, s: 10, e: 15, t: true };
  assert.deepEqual(borneDansMot(g, '&', 0, true), { f: 0, o: 10, ajuste: false });
  assert.deepEqual(borneDansMot(g, 'abc', 1, true), { f: 0, o: 10, ajuste: true });
  assert.deepEqual(borneDansMot(g, 'abc', 1, false), { f: 0, o: 15, ajuste: true });
});

test('borneDansMot : le milieu d’une paire de substitution va vers l’extérieur', () => {
  const t = 'a😀b', g = { f: 0, s: 0, e: octets(t), t: false };
  assert.deepEqual(borneDansMot(g, t, 2, true), { f: 0, o: 1, ajuste: true });
  assert.deepEqual(borneDansMot(g, t, 2, false), { f: 0, o: 5, ajuste: true });
  assert.deepEqual(borneDansMot(g, t, 3, true), { f: 0, o: 5, ajuste: false }, 'au bord : rien ne bouge');
});

test('ajusterGrapheme : lettre et accent combinant coupés entre deux mots', () => {
  const mots = motsDe(['e', '́', 'te']);
  assert.equal(ajuster(mots, 1, true), 0, 'début : avant la lettre');
  assert.equal(ajuster(mots, 1, false), 3, 'fin : après l’accent');
  assert.equal(ajuster(mots, 3, false), 3, 'au bord : rien ne bouge');
});

test('ajusterGrapheme : emoji avec modificateur, séquence liée, drapeau', () => {
  for (const [nom, a, b] of [['modificateur', '👍', '🏽'], ['drapeau', '🇫', '🇷'], ['séquence', '👩‍', '💻']]) {
    const mots = motsDe(['x', a, b, 'y']);
    const milieu = 1 + octets(a);
    assert.equal(ajuster(mots, milieu, true), 1, nom + ' : début');
    assert.equal(ajuster(mots, milieu, false), milieu + octets(b), nom + ' : fin');
  }
});

test('ajusterGrapheme : un mot transformé arrête tout, un trou sépare', () => {
  const mots = motsDe(['e', '́']);
  mots[1].el.dataset.g += ',t';
  assert.equal(ajuster(mots, 1, true), 1);
  const separes = [...motsDe(['e']), ...motsDe(['́'], 5)];
  assert.equal(ajuster(separes, 5, true), 5, 'non contigus : pas de lien');
});

test('ajusterGrapheme : une borne hors des mots n’est pas déplacée', () => {
  const mots = motsDe(['abc'], 10);
  assert.equal(ajuster(mots, 99, true), 99);
  assert.equal(ajuster([], 3, false), 3);
});

// Un document réduit, à la place du navigateur.
function fauxTexte(g, data, parentBloc) {
  const parent = { dataset: { g }, closest: () => parentBloc };
  return { nodeType: 3, parentElement: parent, data };
}
function monde({ spans = [], mots = () => [], selection, comparePoint }) {
  const avis = [];
  const s = creer({
    zone: () => ({ contains: () => true }), spans: () => spans, mots, blocs: 'p', lireG, premierApres, court,
    texteSource: () => 'source', avis: (m) => avis.push(m), selection: () => selection,
    creerRange: () => ({ setStart() {}, comparePoint }),
  });
  return { s, avis };
}
const plage = (startContainer, startOffset, endContainer, endOffset, texte = 'choisi') =>
  ({ rangeCount: 1, isCollapsed: false, getRangeAt: () => ({ startContainer, startOffset, endContainer, endOffset, commonAncestorContainer: startContainer, toString: () => texte }) });

test('borne : hors d’un mot, le mot connu le plus proche vers l’intérieur', () => {
  const sp = (g) => ({ dataset: { g }, childNodes: [0] });
  const spans = [sp('0:0,3'), sp('0:4,7'), sp('0:8,9')];
  // Le point est entre le premier et le deuxième mot.
  const comparePoint = (s, off) => (s === spans[0] ? -1 : 1) * (off >= 0 ? 1 : 1);
  const { s } = monde({ spans, comparePoint });
  const hors = { nodeType: 3, parentElement: { dataset: {} }, data: '1.' };
  assert.deepEqual({ ...s.borne(hors, 0, true), span: undefined }, { f: 0, o: 4, ajuste: true, span: undefined });
  assert.deepEqual({ ...s.borne(hors, 0, false), span: undefined }, { f: 0, o: 3, ajuste: true, span: undefined });
});

test('borne : rien de connu dans ce sens', () => {
  const sp = { dataset: { g: '0:0,3' }, childNodes: [0] };
  const avant = monde({ spans: [sp], comparePoint: () => -1 }).s;
  const apres = monde({ spans: [sp], comparePoint: () => 1 }).s;
  const hors = { nodeType: 3, parentElement: { dataset: {} }, data: '' };
  assert.equal(avant.borne(hors, 0, true), null);
  assert.equal(apres.borne(hors, 0, false), null);
});

test('lireSelection : octets, extrait du navigateur si rien n’a bougé', () => {
  const bloc = { querySelectorAll: () => [] };
  const a = fauxTexte('1:10,15', 'alpha', bloc), b = fauxTexte('1:16,20', 'beta', bloc);
  const { s } = monde({ selection: plage(a, 1, b, 2) });
  assert.deepEqual(s.lireSelection(), { f: 1, start: 11, end: 18, extrait: 'choisi', ajuste: false });
});

test('lireSelection : une borne ajustée prend l’extrait de la source', () => {
  const bloc = { querySelectorAll: () => [] };
  const a = fauxTexte('0:0,5,t', 'abcde', bloc);
  const { s } = monde({ selection: plage(a, 1, a, 3) });
  assert.deepEqual(s.lireSelection(), { f: 0, start: 0, end: 5, extrait: 'source', ajuste: true });
});

test('lireSelection : deux fichiers, sélection vide, aucun texte : avis et null', () => {
  const bloc = { querySelectorAll: () => [] };
  const a = fauxTexte('0:0,5', 'alpha', bloc), b = fauxTexte('1:0,5', 'alpha', bloc);
  let w = monde({ selection: plage(a, 1, b, 2) });
  assert.equal(w.s.lireSelection(), null);
  assert.match(w.avis[0], /deux fichiers/);
  w = monde({ selection: plage(a, 3, a, 3) });
  assert.equal(w.s.lireSelection(), null);
  assert.match(w.avis[0], /poignée/);
  assert.equal(monde({ selection: { rangeCount: 1, isCollapsed: true } }).s.lireSelection(), null);
  assert.equal(monde({ selection: null }).s.lireSelection(), null);
});

test('lireSelection : triple clic, ramené à la fin du bloc de départ', () => {
  const mots = [{ dataset: { g: '0:0,5' } }, { dataset: { g: '0:6,12' } }];
  const bloc = { querySelectorAll: () => mots }, suivant = { querySelectorAll: () => [] };
  const a = fauxTexte('0:0,5', 'alpha', bloc), b = fauxTexte('0:20,25', 'gamma', suivant);
  const { s } = monde({ selection: plage(a, 0, b, 0) });
  assert.deepEqual(s.lireSelection(), { f: 0, start: 0, end: 12, extrait: 'source', ajuste: true });
});

test('lireSelection : un caractère coupé est élargi, et cela se dit', () => {
  const mots = motsDe(['e', '́', 'te']);
  const bloc = { querySelectorAll: () => [] };
  const a = fauxTexte('0:1,3', '́', bloc);
  const { s } = monde({ mots: () => mots, selection: plage(a, 0, a, 1) });
  const x = s.lireSelection();
  assert.equal(x.start, 0);
  assert.equal(x.end, 3);
  assert.equal(x.ajuste, true);
});
