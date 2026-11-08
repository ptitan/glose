# Essais navigateur (P5.3)

## Lancer

```bash
./scripts/test-browser.sh
```

La commande reconstruit Glose puis exécute onze scénarios avec **le vrai serveur, pandoc et un vrai Chrome/Chromium sans interface**. Elle ne dépend ni des outils d'un agent IA ni d'un paquet npm. Aucune installation ni aucun téléchargement automatique.

Prérequis de développement, non requis pour utiliser Glose :
- Go et pandoc, comme pour les autres tests ;
- Node.js **22 ou plus récent** (WebSocket et moteur de test intégrés) ;
- Chrome ou Chromium installé.

Le navigateur est cherché dans le `PATH`, puis aux emplacements usuels de Chrome sur Windows et macOS. Pour en choisir un :

```bash
GLOSE_BROWSER=/chemin/vers/chromium ./scripts/test-browser.sh
```

**Sous WSL**, le pilote sait utiliser Chrome Windows. Son port de débogage reste local à Windows : un petit relais par l'entrée/sortie standard utilise Node.js Windows 22+, cherché sous `%PROGRAMFILES%\nodejs\node.exe` (`GLOSE_WINDOWS_NODE` pour un autre chemin, accessible depuis WSL). Le profil est créé dans le dossier temporaire Windows. Les deux connexions HTTP de Glose et CDP restent sur `127.0.0.1` ; aucun port n'est exposé sur le réseau.

Les prérequis absents font échouer la commande avec un conseil ; ils ne font pas passer silencieusement les tests.

## Ce qui est vérifié

1. **Sélection** : un passage coupe un gras, l'élargissement est proposé puis confirmé ; commentaire d'un emoji entier ; retrait et annulation restituent exactement les octets, BOM et CRLF compris.
2. **Édition** : saisie visuelle littérale, aller-retour Source/Mis en forme, message, proposition d'un tenant, aperçu sans écriture, accepter/rejeter au clavier, annuler/rétablir. Une réponse d'aperçu est retenue et le minuteur automatique contrôlé pour reproduire la course qui annulait une demande explicite d'aperçu.
3. **Disque** : modification externe pendant la saisie, rafraîchissement automatique, conflit explicite, texte et message conservés dans un brouillon non réappliqué.
4. **Clavier et IME** : les lettres de raccourci s'insèrent dans le champ au lieu de commander l'application ; composition japonaise et emoji par CDP, Ctrl+Entrée composé n'enregistre pas.
5. **Navette** : remise par l'interface, modification et `done` par la ligne de commande, retour automatique de la main, puis reprise d'une main non rendue ; rejeter restitue le texte original.
6. **Brouillons** : une réécriture vide et son message survivent à un vrai rechargement, puis à un redémarrage avec un nouveau port et un nouvel ordre des fichiers ; la récupération ne dépend pas du stockage de l'ancienne origine.
7. **Réponse perdue** : l'opération est réellement écrite, puis sa réponse est perdue ; une seconde tentative n'écrit pas une seconde annotation et le brouillon reste à vérifier.
8. **Fichiers** : arborescence, partie affichée, annotation d'un fichier non affiché, largeur de colonne et clic sans redimensionnement.
10. **Modifier ses messages (P9.1)** : commentaire et réponse modifiés depuis la carte, panneau prérempli, octets attendus (identités comprises), annuler et rétablir, retrait d'une réponse, versions précédentes au journal ; après remise puis reprise, plus de « Modifier ».
11. **Barre du haut (P9.2)** : état du rendu au pied de page, colonne repliée et retenue après rechargement, rail vers un onglet, Alt+0, barre collante au défilement ; recherche dont la saisie reste dans le champ, flèches, Échap, Entrée et ouverture du passage.
9. **Consultation (P7)** : titres homonymes et ancres uniques, progression antérieure conservée, lien vers un chapitre caché avec espace dans son nom, défilement vers son titre, fragment vers une partie cachée, image relative réellement chargée, recherche globale insensible à la casse, passage ouvert/surligné, discussion résolue puis relue dans la frise (texte contenant du HTML rendu littéralement). Sources comparées avant/après.

Le fichier est comparé en octets aux moments utiles ; `glose check` valide chaque projet à la fin. Les exceptions JavaScript non interceptées et les boîtes système inattendues font échouer le scénario.

Les touches et la saisie utilisent les événements du navigateur, les clics leurs coordonnées après stabilisation des animations. La sélection est préparée avec un `Range` du DOM, sans fabriquer de positions source. Les attentes vérifient des états avec une échéance, pas un délai supposé suffisant. Une action de l'auteur ou une assertion n'est jamais réessayée pour masquer un échec. Seul l'accès initial au nouveau port local peut attendre son transfert WSL → Windows.

**Limites assumées** : moteur Chromium seulement ; pas de glisser de souris physique, de presse-papiers système ni de clavier IME installé dans l'OS. La composition est commandée par CDP, et la garde Ctrl+Entrée est aussi exercée par un événement explicitement composé. Ces essais ne remplacent pas une vérification manuelle d'accessibilité, de rendu visuel ou d'IME natif. Les corpus complexes restent couverts par les tests Go et les essais sur copie de textes réels.

## Isolation et diagnostics

Chaque scénario crée un projet neuf ; les réglages utilisent un dossier dédié. Le navigateur a un **profil temporaire propre**, sans les comptes, extensions et onglets personnels. Les serveurs, pages et navigateur de test sont fermés même en cas d'échec. Les seuls dossiers supprimés automatiquement sont ceux créés par le pilote.

En cas d'échec, le chemin des données conservées est affiché : `echec.json` (état, requêtes, événements, exceptions, journal du serveur) et `echec.png`. Ils ne contiennent que le texte du corpus d'essai, mais les fichiers `.glose/` peuvent porter le jeton du serveur d'essai : ne pas les publier tels quels.

Options utiles :

```bash
# Un scénario pendant le diagnostic (aucun scénario correspondant = erreur)
GLOSE_BROWSER_CASE='édition visuelle' ./scripts/test-browser.sh
# Choisir le dossier temporaire et conserver aussi les succès
GLOSE_TEST_TMP=/dossier/temporaire GLOSE_KEEP_BROWSER_TESTS=1 ./scripts/test-browser.sh
# Inclure les essais navigateur dans le contrôle général
GLOSE_BROWSER_TESTS=1 ./scripts/check.sh
```

Sans cette dernière option, `check.sh` garde ses contrôles rapides habituels (tests JavaScript des brouillons inclus si Node.js est présent). Aucun test navigateur n'est exécuté par `go test ./...` ni embarqué dans l'exécutable. Pour lancer directement le pilote sur un exécutable construit : `GLOSE_BIN=/chemin/glose node scripts/browser/run.cjs`.

Pilote : `scripts/browser/cdp.cjs`, relais WSL : `scripts/browser/relay.cjs`, scénarios : `scripts/browser/run.cjs`. Référence du protocole : <https://chromedevtools.github.io/devtools-protocol/>.
