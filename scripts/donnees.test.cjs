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
  assert.deepEqual(n, { discussion: 2, propositions: 2, acceptees: 2, rejetees: 1, resolues: 1, total: 8 });
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
