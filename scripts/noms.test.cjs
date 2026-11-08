// Sans navigateur : node --test scripts/noms.test.cjs. Le découpage de app.js a
// plusieurs fois laissé une fonction appelée mais déclarée ailleurs : l'erreur
// n'apparaît qu'à l'exécution. Ce contrôle, volontairement simple, cherche dans
// chaque script les fonctions appelées qu'il ne déclare nulle part (ni fonction,
// ni variable, ni paramètre). Il ne connaît pas les portées.
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const dossier = path.join(__dirname, '..', 'web');
const MOTS = new Set(`if for while switch catch return function typeof await async new super import
  void delete in of do else throw yield case`.split(/\s+/));
const NAVIGATEUR = new Set(`Array Boolean Date Error Map Math Number Object Promise RegExp Set String Symbol JSON parseInt parseFloat isNaN
  encodeURIComponent decodeURIComponent getComputedStyle getSelection requestAnimationFrame cancelAnimationFrame setTimeout clearTimeout
  setInterval clearInterval fetch confirm alert URL URLSearchParams TextEncoder TextDecoder ResizeObserver IntersectionObserver MutationObserver
  AbortController AbortSignal CSS Range Node Element Event MouseEvent KeyboardEvent WeakMap WeakSet Intl atob btoa structuredClone Blob Uint8Array
  AggregateError queueMicrotask matchMedia NodeFilter DOMParser`.split(/\s+/));

// Retire commentaires et chaînes, sans toucher aux expressions des gabarits.
function nettoyer(code) {
  let out = '', i = 0;
  const pile = [];
  while (i < code.length) {
    const c = code[i], d = code[i + 1];
    if (c === '/' && d === '/') { while (i < code.length && code[i] !== '\n') i++; continue; }
    if (c === '/' && d === '*') { i = code.indexOf('*/', i + 2); i = i < 0 ? code.length : i + 2; continue; }
    if (c === '"' || c === "'") {
      for (i++; i < code.length && code[i] !== c; i++) if (code[i] === '\\') i++;
      i++; out += '""'; continue;
    }
    if (c === '`' || (c === '}' && pile[pile.length - 1] === 0)) {
      if (c === '}') pile.pop();
      for (i++; i < code.length && code[i] !== '`'; i++) {
        if (code[i] === '\\') { i++; continue; }
        if (code[i] === '$' && code[i + 1] === '{') { pile.push(0); i += 2; out += ' ( '; break; }
      }
      if (code[i - 1] === '{' && pile[pile.length - 1] === 0 && code[i - 2] === '$') continue;
      i++; out += ' '; continue;
    }
    if (c === '{' && pile.length) pile.push(1);
    if (c === '}' && pile.length && pile[pile.length - 1] === 1) pile.pop();
    // Une expression régulière : après un signe qui ne peut pas finir une valeur.
    if (c === '/' && /[=(,:;!&|?{}\n[]\s*$/.test(out.slice(-3))) {
      let classe = false;
      for (i++; i < code.length && (code[i] !== '/' || classe); i++) {
        if (code[i] === '\\') { i++; continue; }
        if (code[i] === '[') classe = true; else if (code[i] === ']') classe = false;
      }
      i++; out += '""'; continue;
    }
    out += c; i++;
  }
  return out;
}

const NOM = '[A-Za-zÀ-ÿ_$][\\w$À-ÿ]*';
function declares(code) {
  const noms = new Set();
  const ajouter = (liste) => {
    for (const m of liste.matchAll(new RegExp(NOM, 'g'))) noms.add(m[0]);
  };
  for (const m of code.matchAll(new RegExp(`function\\s*\\*?\\s*(${NOM})?\\s*\\(([^)]*)\\)`, 'g'))) { if (m[1]) noms.add(m[1]); ajouter(m[2]); }
  for (const m of code.matchAll(new RegExp(`(?:const|let|var)\\s+(${NOM})`, 'g'))) noms.add(m[1]);
  for (const m of code.matchAll(/(?:const|let|var)\s+([{[][^=]*?[}\]])\s*=/g)) ajouter(m[1]);
  for (const m of code.matchAll(/,\s*([A-Za-zÀ-ÿ_$][\w$À-ÿ]*)\s*=(?!=)/g)) noms.add(m[1]);
  for (const m of code.matchAll(new RegExp(`\\(([^()]*)\\)\\s*=>`, 'g'))) ajouter(m[1]);
  for (const m of code.matchAll(new RegExp(`(?:^|[^.\\w$])(${NOM})\\s*=>`, 'g'))) noms.add(m[1]);
  for (const m of code.matchAll(new RegExp(`catch\\s*\\((${NOM})\\)`, 'g'))) noms.add(m[1]);
  // Méthodes d'objet et de classe : nom(…) { … }
  for (const m of code.matchAll(new RegExp(`(?:^|[,{\\s])(?:async\\s+)?(${NOM})\\s*\\([^()]*\\)\\s*\\{`, 'gm'))) noms.add(m[1]);
  return noms;
}

for (const nom of fs.readdirSync(dossier).filter((f) => f.endsWith('.js'))) {
  test('fonctions appelées et déclarées : ' + nom, () => {
    const code = nettoyer(fs.readFileSync(path.join(dossier, nom), 'utf8'));
    const connus = declares(code);
    const manquants = new Set();
    for (const m of code.matchAll(new RegExp(`(?<![.\\w$À-ÿ])(${NOM})\\s*\\(`, 'g'))) {
      const n = m[1];
      if (!MOTS.has(n) && !NAVIGATEUR.has(n) && !connus.has(n)) manquants.add(n);
    }
    assert.deepEqual([...manquants], [], nom + ' appelle des fonctions qu’il ne déclare pas');
  });
}

// `$` of app.js only knows the identifiers of its list: any other gives undefined,
// and the error shows only when the code runs. Every module that receives it counts.
test('identifiants lus par $ : tous dans la liste de app.js, et dans la page', () => {
  const lire = (nom) => fs.readFileSync(path.join(dossier, nom), 'utf8');
  const app = lire('app.js');
  const debut = app.indexOf('for (const id of ["bouton-sommaire"'), fin = app.indexOf(']) {', debut);
  assert.ok(debut > 0 && fin > debut, 'liste introuvable');
  const liste = new Set([...app.slice(debut, fin).matchAll(/"([a-z0-9-]+)"/g)].map((m) => m[1]));
  const page = lire('index.html');
  const modules = fs.readdirSync(dossier).filter((f) => f.endsWith('.js') && (f === 'app.js' || /creer\(\{[^}]*\$[,\s]/.test(lire(f))));
  assert.ok(modules.length >= 6, 'modules : ' + modules);
  const manquants = new Set();
  for (const f of modules) {
    for (const m of lire(f).matchAll(/(?<![\w.])\$\(["']([a-z0-9-]+)["']\)/g)) if (!liste.has(m[1])) manquants.add(f + ' : ' + m[1]);
  }
  assert.deepEqual([...manquants], []);
  for (const id of liste) assert.ok(page.includes(`id="${id}"`) || app.includes(`.id = "${id}"`), '#' + id + ' absent de la page');
});

