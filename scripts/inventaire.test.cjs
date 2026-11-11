// Inventaire des gestes que la refonte doit garder (docs/etudes/refonte.md,
// § 17), sans navigateur : node --test scripts/inventaire.test.cjs. Un élément
// ne se retire d'ici que par une décision (docs/DECISIONS.md).
const { test } = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');

const web = (nom) => fs.readFileSync(path.join(__dirname, '..', 'web', nom), 'utf8');
const page = web('index.html');
const code = ['export.js', 'edition.js', 'saisie.js', 'historique.js', 'colonne.js', 'marge.js', 'reglages.js', 'interface.js', 'navette.js', 'app.js', 'visuel.js', 'agents.js', 'recherche.js', 'navigation.js', 'archives.js', 'brouillons.js'].map(web).join('\n');

const IDENTIFIANTS = {
  'poignée du paragraphe et son menu': ['outils-bloc', 'poignee', 'menu-bloc', 'menu-avant', 'menu-commenter', 'menu-suggerer', 'menu-apres'],
  'export en PDF': ['exporter', 'export', 'export-ok', 'export-commentaires'],
  'menu de sélection': ['menu-selection', 'bouton-commenter', 'bouton-suggerer'],
  'panneau Commenter': ['saisie', 'saisie-texte', 'saisie-pour-ligne', 'saisie-brouillon', 'saisie-message'],
  'panneau Suggérer': ['edition', 'edition-types', 'edition-elargir', 'edition-mef', 'edition-source', 'edition-voir', 'edition-outils',
    'edition-visuel', 'edition-texte', 'edition-rendu', 'edition-changements', 'edition-tenant-case', 'edition-note', 'edition-pour-ligne', 'edition-brouillon', 'edition-direct'],
  'colonne': ['cote', 'rail', 'rail-ouvrir', 'poignee-colonne', 'onglet-sommaire', 'onglet-fichiers', 'onglet-annotations', 'onglet-navette',
    'bouton-sommaire', 'sommaire', 'fichiers', 'annotations', 'filtres', 'annotations-liste', 'historique'],
  'barre': ['vues', 'annuler', 'retablir', 'bouton-reglages', 'reglages', 'r-police', 'r-taille', 'r-interligne', 'r-largeur', 'r-justifie', 'r-defaut',
    'recherche-texte', 'recherche', 'recherche-casse', 'recherche-resultats', 'ouvrir-recherche', 'exporter', 'plus-bouton', 'plus-menu', 'portee', 'avancement',
    'depuis-reference', 'mission-filtre', 'typo-fichier', 'typo-lancer', 'consignes-barre', 'voir-sortie'],
  'navette': ['remettre', 'configurer-agents', 'menu-navette-bouton', 'menu-navette', 'm-configurer', 'm-sortie', 'm-arreter', 'm-lancer', 'm-lier',
    'navette-annonce', 'remise', 'remise-lancer', 'remise-outils', 'remise-note', 'remise-force', 'remise-lier', 'remise-ok',
    'lien', 'lien-consigne', 'lien-nouveau', 'lien-copier', 'lien-etat', 'lien-reste', 'sortie', 'sortie-texte', 'sortie-arreter'],
  'pied et écrans à part': ['pied', 'etat', 'bouton-brouillons', 'brouillons', 'archive', 'bandeau', 'anomalies', 'boite', 'avis', 'marge', 'texte'],
};

for (const [groupe, ids] of Object.entries(IDENTIFIANTS)) {
  test('présent dans index.html : ' + groupe, () => {
    for (const id of ids) assert.ok(page.includes(`id="${id}"`), `#${id} a disparu`);
  });
}

test('types d’élément à ajouter, dans l’ordre', () => {
  const zone = page.slice(page.indexOf('id="edition-types"'), page.indexOf('</div>', page.indexOf('id="edition-types"')));
  assert.deepEqual([...zone.matchAll(/data-type="([a-z]+)"/g)].map((m) => m[1]), ['paragraphe', 'titre', 'liste', 'tableau', 'encadre', 'image', 'code']);
});

test('ordre du menu de la poignée', () => {
  const zone = page.slice(page.indexOf('id="menu-bloc"'), page.indexOf('</div>', page.indexOf('id="menu-bloc"')));
  assert.deepEqual([...zone.matchAll(/id="(menu-[a-z]+)"/g)].map((m) => m[1]).slice(1), ['menu-avant', 'menu-commenter', 'menu-suggerer', 'menu-apres']);
});

test('gestes des cartes et du sommaire, dans le code', () => {
  for (const texte of ['"Répondre"', '"Modifier"', '"Retirer"', '"Résoudre"', '"Accepter"', '"Rejeter"', 'Accepter cette version', '"Reprendre"',
    'Effacer les filtres', 'Abandonner', 'Réessayer la sauvegarde', 'Réécrire plutôt']) {
    assert.ok(code.includes(texte), texte + ' a disparu');
  }
});

test('barre d’outils de l’éditeur', () => {
  for (const cmd of ['bold', 'italic', 'lien', 'note', 'tableau', 'liste']) assert.ok(page.includes(`data-cmd="${cmd}"`), cmd);
});

// Audit A14: an exception during a save never leaves the panel frozen.
test('enregistrement : panneau et écritures libérés dans un finally', () => {
  for (const nom of ['edition.js', 'saisie.js']) {
    const src = web(nom);
    const i = src.indexOf('S.envoi = true;');
    const fin = src.indexOf('} finally {', i);
    assert.ok(i >= 0 && fin > i, nom + ' : pas de finally après S.envoi = true');
    const bloc = src.slice(fin, src.indexOf('}', fin + 12));
    assert.match(bloc, /S\.envoi = false;/, nom);
    assert.match(bloc, /\.inert = false;/, nom);
  }
});
