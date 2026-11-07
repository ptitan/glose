// Sans dépendance : node --test scripts/brouillons.test.cjs
const { test } = require("node:test");
const assert = require("node:assert/strict");
require("../web/brouillons.js");

function stockage() {
  const m = new Map();
  return { get length() { return m.size; }, key: (i) => [...m.keys()][i], getItem: (k) => m.get(k) ?? null,
    setItem: (k, v) => m.set(k, v), removeItem: (k) => m.delete(k) };
}
const clone = (v) => JSON.parse(JSON.stringify(v));
const initial = () => ({ kind: "edit", path: "chapitre.md", hash: "a".repeat(64), original: "Avant", text: "Après", note: "Avis", sending: false });
function banc(t) {
  const disque = new Map(), local = stockage(), requetes = [];
  let numero = 0, version = 0;
  const api = async (url, req) => {
    if (url === "/api/ping") return { ok: true, data: { root: "/projet" } };
    if (!req) return { ok: true, data: { drafts: [...disque.values()].filter((d) => !d.deleted), problems: [] } };
    requetes.push(clone(req));
    const prev = disque.get(req.id);
    if (prev && prev.deleted === !!req.remove && JSON.stringify(prev.draft) === JSON.stringify(req.draft)) return { ok: true, data: clone(prev) };
    if ((prev?.hash || "") !== req.hash || prev?.deleted) return { ok: false, status: 409, data: { message: "conflit" } };
    const rep = { id: req.id, hash: "v" + ++version, draft: req.remove ? null : clone(req.draft), deleted: !!req.remove };
    disque.set(req.id, rep);
    return { ok: true, data: clone(rep) };
  };
  function creer(transport = api, s = local) {
    const b = GloseBrouillons.creer({ api: transport, stockage: s, identifiant: () => (++numero).toString(16).padStart(32, "0"), delai: 60000 });
    t.after(() => { for (const e of b.entrees.values()) clearTimeout(e.timer); });
    return b;
  }
  return { disque, local, requetes, api, creer };
}

test("saisie, note et suppression proposées survivent à une nouvelle interface", async (t) => {
  const f = banc(t), b = f.creer(); await b.charger();
  const d = initial(); d.text = ""; // une suppression est une vraie saisie
  const e = b.nouveau(d);
  assert.equal(f.local.length, 1, "miroir synchrone avant le moindre appel réseau");
  assert.equal(await b.sauver(e), true);
  assert.equal(f.local.length, 0);
  const suite = f.creer(undefined, stockage()); await suite.charger();
  assert.deepEqual([...suite.entrees.values()][0].draft, d);
});

test("les frappes arrivées pendant une sauvegarde sont sérialisées, sans perte", async (t) => {
  const f = banc(t);
  let liberer;
  const b = f.creer(async (url, req) => {
    if (req && !liberer) await new Promise((r) => { liberer = r; });
    return f.api(url, req);
  });
  await b.charger(); const e = b.nouveau(initial());
  const sauvegarde = b.sauver(e);
  b.modifier(e, { ...initial(), text: "Dernière frappe", note: "Dernier message" });
  liberer();
  assert.equal(await sauvegarde, true);
  assert.equal(f.requetes.length, 2);
  assert.equal(f.requetes[1].hash, "v1");
  assert.equal(f.disque.get(e.id).draft.text, "Dernière frappe");
  assert.equal(f.local.length, 0);
});

test("une réponse de sauvegarde perdue se réessaie sans doublon", async (t) => {
  const f = banc(t); let perdre = true;
  const b = f.creer(async (url, req) => {
    const r = await f.api(url, req);
    if (req && perdre) { perdre = false; throw Error("réponse perdue"); }
    return r;
  });
  await b.charger(); const e = b.nouveau(initial());
  assert.equal(await b.sauver(e), false);
  assert.equal(f.local.length, 1);
  assert.equal(await b.sauver(e), true);
  assert.equal(f.disque.size, 1);
  assert.equal(f.local.length, 0);
});

test("un miroir non confirmé se récupère sans réappliquer de requête au manuscrit", async (t) => {
  const f = banc(t), b = f.creer(); await b.charger();
  const e = b.nouveau({ ...initial(), sending: true });
  clearTimeout(e.timer);
  const suite = f.creer(); await suite.charger();
  const recupere = [...suite.entrees.values()][0];
  assert.equal(recupere.draft.sending, true);
  assert.equal(recupere.pending, true);
  assert.notEqual(recupere.id, e.id);
  assert.equal(f.requetes.length, 0);
});

test("deux onglets : ni remplacement ni suppression silencieuse de la version d'autrui", async (t) => {
  const f = banc(t), a = f.creer(); await a.charger();
  const e = a.nouveau(initial()); await a.sauver(e);
  const b = f.creer(); await b.charger(); const autre = b.entrees.get(e.id);
  a.modifier(e, { ...initial(), text: "Onglet A" }); await a.sauver(e);
  b.modifier(autre, { ...initial(), text: "Onglet B" });
  assert.equal(await b.sauver(autre), false);
  assert.equal(await b.retirer(autre), false);
  assert.equal(f.disque.get(e.id).draft.text, "Onglet A");
  assert.equal(autre.draft.text, "Onglet B");
  assert.equal(f.local.length, 1);
});

test("la confirmation d'un onglet n'efface pas le secours non confirmé d'un autre", async (t) => {
  const f = banc(t), a = f.creer(); await a.charger();
  const e = a.nouveau(initial()); await a.sauver(e);
  const b = f.creer(); await b.charger(); const autre = b.entrees.get(e.id);
  b.modifier(autre, { ...initial(), text: "Saisie privée B" });
  a.modifier(e, { ...initial(), text: "Saisie privée A" });
  assert.equal(f.local.length, 2);
  await a.sauver(e);
  assert.equal(f.local.length, 1);
  assert.equal(JSON.parse(f.local.getItem(f.local.key(0))).draft.text, "Saisie privée B");
});

test("une suppression attend la sauvegarde, ne laisse pas de miroir et ne ressuscite pas", async (t) => {
  const f = banc(t), b = f.creer(); await b.charger();
  const e = b.nouveau(initial());
  assert.equal(await b.retirer(e), true);
  b.modifier(e, { ...initial(), text: "ancien événement tardif" });
  assert.equal(b.entrees.size, 0);
  assert.equal(f.local.length, 0);
  assert.equal(f.disque.get(e.id).deleted, true);
});

test("stockage navigateur interdit : la sauvegarde projet fonctionne encore", async (t) => {
  const f = banc(t), b = f.creer(undefined, { get length() { throw Error("interdit"); }, setItem() { throw Error("quota"); }, removeItem() { throw Error("interdit"); } });
  await b.charger(); const e = b.nouveau(initial());
  assert.equal(await b.sauver(e), true);
  assert.equal(f.disque.get(e.id).draft.text, "Après");
});
