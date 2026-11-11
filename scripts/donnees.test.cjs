// Sans dépendance : node --test scripts/donnees.test.cjs
const { test } = require("node:test");
const assert = require("node:assert/strict");
require("../web/donnees.js");
const D = globalThis.GloseDonnees;

test("lireG : place d'un mot, mot transformé, valeurs refusées", () => {
  assert.deepEqual(D.lireG("2:10,15"), { f: 2, s: 10, e: 15, t: false });
  assert.deepEqual(D.lireG("0:0,3,t"), { f: 0, s: 0, e: 3, t: true });
  for (const v of ["", null, undefined, "1:2", "a:1,2", "1:2,3,x", " 1:2,3"]) assert.equal(D.lireG(v), null);
});

test("court : une ligne, coupée avec points de suspension", () => {
  assert.equal(D.court("  un\n deux\ttrois  ", 50), "un deux trois");
  assert.equal(D.court("abcdefghij", 5), "abcd…");
  assert.equal(D.court("abcde", 5), "abcde");
});

test("signature : liste des fichiers, empreintes et dernier événement", () => {
  const d = { filesGen: 1, files: [{ path: "a.md", hash: "h" }, { path: "b.md", hash: "h" }], seq: 7 };
  assert.equal(D.signature(d), D.signature(JSON.parse(JSON.stringify(d))));
  assert.notEqual(D.signature(d), D.signature({ ...d, seq: 8 }));
  assert.notEqual(D.signature(d), D.signature({ ...d, filesGen: 2 }));
  // Deux fichiers identiques échangés : mêmes empreintes, chemins inversés.
  assert.notEqual(D.signature(d), D.signature({ ...d, files: [d.files[1], d.files[0]] }));
});

test("cleAnnotation : identifiant, sinon place, type et texte", () => {
  assert.equal(D.cleAnnotation({ id: "k3fz9a", file: 0, start: 4 }), "id:k3fz9a");
  const a = { file: 1, start: 4, kind: "comment", text: "x" };
  assert.notEqual(D.cleAnnotation(a), D.cleAnnotation({ ...a, text: "y" }));
  assert.notEqual(D.cleAnnotation({ ...a, kind: "substitution", new: "a" }), D.cleAnnotation({ ...a, kind: "substitution", new: "b" }));
});

test("premierApres : recherche dichotomique, bornes", () => {
  const mots = [0, 3, 3, 8, 12].map((s) => ({ s }));
  assert.equal(D.premierApres(mots, -1), 0);
  assert.equal(D.premierApres(mots, 3), 1);
  assert.equal(D.premierApres(mots, 4), 3);
  assert.equal(D.premierApres(mots, 12), 4);
  assert.equal(D.premierApres(mots, 13), 5);
  assert.equal(D.premierApres([], 0), 0);
  // Contre une recherche linéaire, sur des listes tirées au hasard (graine fixe).
  let x = 42;
  const hasard = (n) => (x = (x * 1103515245 + 12345) % 2147483648) % n;
  for (let k = 0; k < 500; k++) {
    const l = Array.from({ length: hasard(20) }, () => hasard(50)).sort((a, b) => a - b).map((s) => ({ s }));
    const o = hasard(55) - 2;
    const lin = l.findIndex((m) => m.s >= o);
    assert.equal(D.premierApres(l, o), lin < 0 ? l.length : lin);
  }
});

test("plageVisible : ancien texte en Relecture et Actuel, nouveau en Proposé", () => {
  const sub = { kind: "substitution", textStart: 1, textEnd: 2, newStart: 5, newEnd: 6 };
  assert.deepEqual(D.plageVisible(sub, "relecture"), [1, 2]);
  assert.deepEqual(D.plageVisible(sub, "propose"), [5, 6]);
  assert.equal(D.plageVisible({ kind: "deletion", textStart: 1, textEnd: 2 }, "propose"), null);
  assert.equal(D.plageVisible({ kind: "addition", textStart: 1, textEnd: 2 }, "actuel"), null);
  assert.deepEqual(D.plageVisible({ kind: "highlight", textStart: 1, textEnd: 2 }, "propose"), [1, 2]);
  assert.equal(D.plageVisible({ kind: "comment" }, "relecture"), null);
});

test("compter et rayon : un geste par suggestion, décisions par marque", () => {
  const ops = [
    { kind: "comment" }, { kind: "reply" }, { kind: "propose" }, { kind: "track" },
    { kind: "decide", closed: [{ decision: "accept" }, { decision: "accept" }, { decision: "reject" }, { kind: "message", decision: "accept" }] },
    { kind: "resolve", closed: [{ decision: "resolve" }] },
  ];
  const n = D.compter(ops);
  assert.deepEqual(n, { discussion: 2, propositions: 2, acceptees: 2, rejetees: 1, resolues: 1, directes: 0, total: 8 });
  // D-57 : une modification directe est un geste, quel que soit le nombre de ses mots.
  assert.deepEqual(D.compter([{ kind: "direct", closed: [{ decision: "direct" }, { decision: "direct" }] }]),
    { discussion: 0, propositions: 0, acceptees: 0, rejetees: 0, resolues: 0, directes: 1, total: 1 });
  assert.equal(D.bilan([{ kind: "direct", closed: [{ decision: "direct" }] }, { kind: "direct", closed: [{ decision: "direct" }] }]), "2 modifications directes");
  assert.equal(D.rayon({ total: 0 }), 11);
  assert.equal(D.rayon({ total: 1000 }), 18);
});

test("bilan : libellés au singulier et au pluriel", () => {
  assert.equal(D.bilan([]), "");
  assert.equal(D.bilan([{ kind: "comment" }, { kind: "comment" }, { kind: "propose" }]), "2 commentaires, 1 suggestion");
  assert.equal(D.bilan([{ kind: "track" }, { kind: "decide", closed: [{ decision: "accept" }, { decision: "reject" }, { decision: "reject" }] }]),
    "modifications du texte converties en propositions ; décisions : 1 acceptée, 2 rejetées");
  assert.equal(D.bilan([{ kind: "identify" }]), "");
  assert.equal(D.bilan([{ kind: "comment" }, { kind: "amend" }, { kind: "amend" }]), "1 commentaire, 2 messages modifiés");
});

test("quand et heure : relatifs à l'instant donné", () => {
  const t0 = new Date(2026, 10, 7, 21, 4).getTime();
  assert.equal(D.quand(new Date(t0), t0 + 20e3), "à l'instant");
  assert.equal(D.quand(new Date(t0), t0 + 12 * 60e3), "il y a 12 min");
  assert.equal(D.quand(new Date(t0), t0 + 3 * 3600e3), "il y a 3 h");
  assert.match(D.quand(new Date(t0), t0 + 3 * 86400e3), /^le 07\/11 à 21:04$/);
  assert.equal(D.heure(t0, new Date(t0 + 3600e3)), "21:04");
  assert.match(D.heure(t0, new Date(t0 + 2 * 86400e3)), /^07\/11 21:04$/);
});

const attendre = () => new Promise((r) => setImmediate(r));

test("creerEnvoiTitres : un envoi à la fois, la dernière liste voulue ensuite", async () => {
  const ouverts = [];
  const e = D.creerEnvoiTitres((changes) => new Promise((resolve) => ouverts.push({ changes, resolve })));
  e.connaitre({ "a.md": ["x"] });
  await e.vouloir({ "a.md": ["x"] });
  assert.equal(ouverts.length, 0);
  e.vouloir({ "a.md": ["x", "y"] });
  e.vouloir({ "a.md": ["x", "z"] });
  assert.equal(ouverts.length, 1);
  assert.deepEqual(ouverts[0].changes["a.md"], ["x", "y"]);
  ouverts[0].resolve({ ok: true, data: { headings: { "a.md": ["x", "y"] } } });
  await attendre();
  assert.equal(ouverts.length, 2);
  assert.deepEqual(ouverts[1].changes["a.md"], ["x", "z"]);
  ouverts[1].resolve({ ok: true, data: {} });
  await attendre();
  assert.equal(ouverts.length, 2);
});

test("creerEnvoiTitres : un refus se renvoie au prochain appel, sans boucle", async () => {
  const ouverts = [];
  const e = D.creerEnvoiTitres((changes) => new Promise((resolve) => ouverts.push({ changes, resolve })));
  e.vouloir({ "b.md": ["t"] });
  ouverts[0].resolve({ ok: false });
  await attendre();
  assert.equal(ouverts.length, 1);
  e.vouloir({ "b.md": ["t"] });
  assert.equal(ouverts.length, 2);
  ouverts[1].resolve({ ok: true });
  await attendre();
  e.vouloir({ "b.md": ["t"] });
  assert.equal(ouverts.length, 2);
});

test("creerEnvoiTitres : réponse perdue, puis retour à la valeur connue : renvoyé", async () => {
  const ouverts = [];
  const e = D.creerEnvoiTitres((changes) => new Promise((resolve) => ouverts.push({ changes, resolve })));
  e.connaitre({ "a.md": ["x"] });
  e.vouloir({ "a.md": ["x", "y"] });
  ouverts[0].resolve({ ok: false }); // le serveur a pu écrire : on ne sait pas
  await attendre();
  e.vouloir({ "a.md": ["x"] });
  assert.equal(ouverts.length, 2);
  assert.deepEqual(ouverts[1].changes["a.md"], ["x"]);
});

test("creerEnvoiTitres : un fichier en échec ne reste pas collé aux suivants", async () => {
  const ouverts = [];
  const e = D.creerEnvoiTitres((changes) => new Promise((resolve) => ouverts.push({ changes, resolve })));
  e.vouloir({ "retire.md": ["t"] });
  ouverts[0].resolve({ ok: false });
  await attendre();
  e.vouloir({ "b.md": ["u"] });
  assert.deepEqual(Object.keys(ouverts[1].changes), ["b.md"]);
});

test("creerEnvoiTitres : une erreur d'envoi ne casse rien", async () => {
  let n = 0;
  const e = D.creerEnvoiTitres(() => { n++; return Promise.reject(new Error("réseau")); });
  await e.vouloir({ "a.md": [] });
  await e.vouloir({ "a.md": [] });
  assert.equal(n, 2);
});

test("creerEnvoiTitres : par paquets sous la limite du serveur ; un fichier trop gros n'est pas renvoyé", async () => {
  const ouverts = [];
  const e = D.creerEnvoiTitres((changes) => new Promise((resolve) => ouverts.push({ changes, resolve })), 100);
  const liste = (n) => Array.from({ length: n }, (_, i) => "t" + i);
  e.vouloir({ "a.md": liste(10), "b.md": liste(10), "enorme.md": liste(200) });
  assert.deepEqual(Object.keys(ouverts[0].changes), ["a.md"]);
  ouverts[0].resolve({ ok: true });
  await attendre();
  assert.deepEqual(Object.keys(ouverts[1].changes), ["b.md"]);
  ouverts[1].resolve({ ok: true });
  await attendre();
  assert.equal(ouverts.length, 2);
  e.vouloir({ "enorme.md": liste(200) });
  assert.equal(ouverts.length, 2);
});

test("clesDesTitres : préfixe de rendu retiré, texte à défaut, deux titres gardent deux clés", () => {
  const fichier = (path) => ({ dataset: { path } });
  const titre = (id, texte, f) => ({ id, textContent: texte, closest: () => f });
  const a = fichier("a.md"), b = fichier("b.md");
  const t = [titre("gl-f3-a-intro", "Intro", a), titre("gl-x", "Premier", a), titre("gl-f3-a-gl-x", "Second", a),
    titre("", " Même texte ", a), titre("", "Même texte", a), titre("", "Même texte", a), titre("gl-x", "Ailleurs", b)];
  const c = D.clesDesTitres(t);
  assert.deepEqual(t.map((h) => c.get(h)), ["a.md#intro", "a.md#gl-x", "a.md#gl-x~2", "a.md#Même texte", "a.md#Même texte~2", "a.md#Même texte~3", "b.md#gl-x"]);
});

test("emphase : gras, italique, code ; le reste ne bouge pas", () => {
  const e = D.emphase;
  const strong = (...c) => ({ t: "strong", c }), em = (...c) => ({ t: "em", c });
  assert.deepEqual(e("du **gras** ici"), ["du ", strong("gras"), " ici"]);
  assert.deepEqual(e("de l'*italique* et de l'_autre_"), ["de l'", em("italique"), " et de l'", em("autre")]);
  assert.deepEqual(e("__gras__ puis ***les deux***"), [strong("gras"), " puis ", strong(em("les deux"))]);
  assert.deepEqual(e("**un *mot* dedans**"), [strong("un ", em("mot"), " dedans")]);
  assert.deepEqual(e("*un **mot** dedans*"), [em("un ", strong("mot"), " dedans")]);
  assert.deepEqual(e("`a*b*c` et *d*"), [{ t: "code", c: ["a*b*c"] }, " et ", em("d")]);
  assert.deepEqual(e("Deux lignes\n\n**puis** une autre"), ["Deux lignes\n\n", strong("puis"), " une autre"]);
  for (const brut of ["2*3*4 reste", "snake_case_name", "* une puce", "- a * b * c", "**pas fermé", "fin *", "a ** b ** c", "**par-dessus\n\nun blanc**"]) {
    assert.deepEqual(e(brut), [brut], brut);
  }
  assert.deepEqual(e("\\*pas italique\\*"), ["*pas italique*"]);
  assert.deepEqual(e("<b>rien</b> & co"), ["<b>rien</b> & co"], "le HTML reste du texte");
  assert.deepEqual(e(""), []);
  assert.equal(D.texteSimple("**Sur le fond**, *elle* est `exacte`."), "Sur le fond, elle est exacte.");
});

test("etatAnnotation : répondu si l'autre a parlé, le dernier message dit qui", () => {
  const moi = { by: "author", text: "?" }, sol = { by: "agent", via: "Sol", text: "!" };
  const et = (a) => D.etatAnnotation(a).libelle;
  assert.equal(et({ kind: "comment", by: "author", status: "new", messages: [moi] }), "À remettre");
  assert.equal(et({ kind: "comment", by: "author", status: "open", messages: [moi] }), "Sans réponse");
  assert.equal(et({ kind: "comment", by: "author", status: "open", messages: [moi, sol] }), "Répondu par Sol");
  assert.equal(et({ kind: "comment", by: "author", status: "open", messages: [moi, { by: "agent", text: "!" }] }), "Répondu par l'agent");
  assert.equal(et({ kind: "comment", by: "agent", via: "Sol", status: "new", messages: [sol, moi] }), "Répondu par vous");
  assert.equal(et({ kind: "comment", by: "agent", via: "Sol", status: "new", messages: [sol] }), "À traiter");
  assert.equal(et({ kind: "substitution", by: "agent", via: "Sol", status: "new", messages: [sol] }), "À décider", "son explication n'est pas une réponse");
  assert.equal(et({ kind: "substitution", by: "author", status: "open", messages: [{ by: "agent", via: "Sol", fix: true, text: "x" }] }), "Corrigé par Sol");
  assert.equal(D.etatAnnotation({ kind: "comment", by: "author", messages: [moi, sol] }).ton, "agent");
  assert.equal(D.etatAnnotation({ kind: "comment", by: "agent", messages: [sol, moi] }).ton, "auteur");
});

test("decisionsDuJournal : du plus récent au plus ancien, révisions et inconnus écartés", () => {
  const close = (id, decision) => ({ id, decision, file: "a.md", kind: "comment", text: id });
  const liste = D.decisionsDuJournal([
    { round: 1, operations: [{ closed: [close("a", "accept"), close("b", "reject")] }, { kind: "comment" }] },
    { round: 2, operations: [{ closed: [close("c", "resolve"), close("d", "withdraw"), close("e", "autre")], revised: [close("f", "revise")] }] },
  ]);
  assert.deepEqual(liste.map((x) => [x.c.id, x.etat.libelle, x.round]), [["d", "Retiré", 2], ["c", "Résolu", 2], ["b", "Rejeté", 1], ["a", "Accepté", 1]]);
  assert.deepEqual(D.decisionsDuJournal(undefined), []);
  // Annulée : l'annotation est de nouveau dans le texte. Décidée deux fois : la dernière.
  const tours = [{ round: 1, operations: [{ closed: [close("a", "accept")] }, { closed: [close("b", "reject")] }, { closed: [close("b", "accept")] }] }];
  assert.deepEqual(D.decisionsDuJournal(tours, new Set(["a"])).map((x) => [x.c.id, x.etat.libelle]), [["b", "Accepté"]]);
  // D-57 : une modification directe est « Imposé » ; annulée, elle sort de la liste.
  const directes = [{ round: 1, operations: [{ kind: "direct", closed: [close("x", "direct"), close("y", "direct")] }, { kind: "undo", reverted: ["x"] }] }];
  assert.deepEqual(D.decisionsDuJournal(directes).map((x) => [x.c.id, x.etat.libelle]), [["y", "Imposé"]]);
});

test("thèmes : chacun complet, et lisible pour un texte long", () => {
  const css = require("node:fs").readFileSync(require("node:path").join(__dirname, "..", "web", "themes.css"), "utf8");
  // Glose Default garde ses couleurs dans style.css ; ses aperçus les reprennent telles quelles.
  const blocs = [...css.matchAll(/data-theme="([a-z-]+)"\] \{ color-scheme: (light|dark); (--[^}]*)\}/g)].filter((b) => !b[3].includes("light-dark"));
  assert.ok(blocs.length >= 15, "thèmes lus : " + blocs.length);
  const canal = (v) => { v /= 255; return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; };
  const lum = (c) => { const n = parseInt(c.slice(1), 16); return 0.2126 * canal(n >> 16) + 0.7152 * canal((n >> 8) & 255) + 0.0722 * canal(n & 255); };
  const contraste = (a, b) => { const x = lum(a), y = lum(b); return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05); };
  const noms = new Set(blocs.map((b) => b[1]));
  const exigences = [["encre", "carte", 7], ["encre", "fond", 7], ["encre", "accent-doux", 4.5], ["encre", "positif-fond", 4.5], ["encre", "surligne", 4.5], ["encre-2", "carte", 5.5],
    ["discret", "carte", 4.5], ["discret", "fond", 4.5], ["discret", "accent-doux", 4.5], ["discret", "positif-fond", 4.5], ["accent", "carte", 4.5], ["accent", "accent-doux", 4.5], ["accent-texte", "accent", 4.5],
    ["ajout", "ajout-fond", 4.5], ["retrait", "retrait-fond", 4.5], ["suggestion", "suggestion-fond", 4.5], ["commentaire", "commentaire-fond", 4.5], ["positif", "positif-fond", 4.5],
    ["nav-encre", "nav", 7], ["nav-discret", "nav", 4.5]];
  for (const [, nom, teinte, corps] of blocs) {
    const v = Object.fromEntries([...corps.matchAll(/--([a-z0-9-]+): ([^;]+);/g)].map((m) => [m[1], m[2]]));
    for (const [a, b, min] of exigences) {
      assert.ok(v[a] && v[b], nom + " : --" + a + " ou --" + b + " manquante");
      assert.ok(contraste(v[a], v[b]) >= min, nom + " : " + a + " sur " + b + " = " + contraste(v[a], v[b]).toFixed(2));
    }
    assert.equal(lum(v.carte) < 0.2, teinte === "dark", nom + " : teinte annoncée");
  }
  const code = require("node:fs").readFileSync(require("node:path").join(__dirname, "..", "web", "themes.js"), "utf8");
  const ids = [...code.matchAll(/"id": "([a-z-]+)"/g)].map((m) => m[1]);
  for (const n of noms) assert.ok(ids.includes(n), n + " absent de la liste");
  assert.ok(code.includes("Glose Default (clair)") && code.includes("Glose Default (sombre)"));
});

