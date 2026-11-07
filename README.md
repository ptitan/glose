# Glose

La navette entre un auteur et son agent IA, dans les marges du texte.

Glose affiche un texte écrit en Markdown dans le navigateur, mis en forme. L'auteur y commente et y propose des modifications ; l'agent IA (Claude Code, Codex, OpenCode…) répond et propose les siennes ; l'auteur accepte ou rejette d'un clic. Tout s'écrit dans le fichier Markdown, en [CriticMarkup](https://github.com/CriticMarkup/CriticMarkup-toolkit), à l'endroit visé et nulle part ailleurs. L'histoire de la navette est gardée dans un journal.

> Une *glose* est une note écrite en marge d'un texte pour l'expliquer ou le discuter ; les glossateurs de Bologne annotaient ainsi le *Digeste*.

**État** : expérimental (version 0.x). Lire, commenter, proposer et corriger à l'écran, mener la navette et la suivre, conserver ses brouillons ; écritures durables ; gros ouvrages affichés partie par partie. Essayé sous Linux et WSL ; Windows natif compile mais n'a pas encore été essayé en conditions réelles.

**Pour commencer** : `docs/GUIDE.md` (préparer un texte, mener une navette, sauvegarder, reprendre quand quelque chose bloque, ce que Glose sait lire).

## Principes

- **Le fichier n'est jamais régénéré** : Glose ne touche qu'aux plages annotées ou décidées.
- **L'auteur décide** : l'agent propose, il n'applique jamais.
- **Tout reste local** : serveur sur `127.0.0.1`, aucune requête vers l'extérieur.
- **Un seul programme** (`glose`, écrit en Go), plus pandoc.

## Installation

Prérequis : Go 1.27 (pour construire) et pandoc 3.1 ou plus récent.

```bash
go install github.com/ptitan/glose/cmd/glose@latest
```

ou, depuis une copie du dépôt :

```bash
./scripts/build.sh          # produit bin/glose
cp bin/glose ~/.local/bin/  # ou tout dossier du PATH
```

## Usage

```bash
glose open                  # le dossier courant : lance le serveur en arrière-plan, ouvre le navigateur
glose open notes.md         # un seul fichier
glose help profile          # le profil glose.json : fichiers, dossiers, motifs, filtre d'affichage
glose init                  # profil glose.json et section « Navette Glose » pour l'agent (AGENTS.md ou CLAUDE.md)
glose status                # à qui est la main, combien d'annotations, quels problèmes
glose list                  # les annotations, fichier:ligne:colonne ; --json pour un agent
glose log                   # l'histoire : tours, opérations, décisions archivées
glose doctor                # installation (pandoc, Git, navigateur) et projet (filtre, droits, serveur), avec la correction de chaque problème
glose check                 # profil, pandoc, marques, identités, journal (code 1 en cas d'erreur ; les avertissements laissent 0)
glose render > page.html    # le HTML affiché, pour contrôler un filtre
glose stop                  # arrête le serveur
```

Dans le navigateur : sélectionner un passage, puis « Commenter » (C) ou « Suggérer » (S) ; la poignée de la marge gauche agit sur un paragraphe entier. Chaque annotation est écrite aussitôt dans le fichier, en CriticMarkup, avec son identité. Les cartes de la marge montrent le fil (auteur et agent) et les décisions : accepter, rejeter, résoudre, ou tout accepter dans un fichier. Annuler et Rétablir (Ctrl+Z, Ctrl+Y) portent sur vos dernières actions. Au clavier : J et K passent d'une annotation à l'autre, A accepte, X rejette, R répond, E corrige votre proposition (sur la carte active) ; 1, 2, 3 changent de vue. La colonne de gauche a quatre onglets, en icônes (Alt+1 à Alt+4) : Sommaire (avec l'avancement de la relecture), Fichiers (choisir la partie affichée), Annotations (filtrables), Navette (la frise des tours).

**La navette.** Quand l'auteur a fini, il clique sur « Remettre à l'agent ». L'agent, guidé par la section écrite par `glose init` :

```bash
glose wait                  # attend la remise de l'auteur (rend la main aussitôt si c'est déjà le cas)
glose list --json           # les annotations à traiter
glose reply --id ID --text "…"                                        # répondre dans un fil
glose propose --file a.md --find "texte" --new "autre texte" --note "…"   # proposer, avec une explication (--whole : en un bloc)
# … ou modifier le fichier librement : ses modifications deviendront des propositions
glose done --note "…"       # remettre le tour ; les modifications libres deviennent des propositions
```

L'auteur accepte ou rejette ; la navette s'achève quand il ne reste plus d'annotation. L'agent ne décide jamais. Si l'agent ne remet pas son tour, l'auteur peut reprendre la main depuis le bandeau (`docs/GUIDE.md`, « Reprendre »).

Sans profil, Glose prend les `.md` du dossier et de ses sous-dossiers, dans l'ordre d'une table des matières (sauf dossiers cachés et consignes d'agent). Un fichier `glose.json` peut choisir les fichiers, dossiers ou motifs du texte, en écarter d'autres, fixer un titre, et adapter l'affichage par un filtre Lua et une feuille de style propres au texte (`glose help profile`).

**Brouillons.** Les commentaires, réponses et réécritures se sauvegardent pendant la saisie, séparément du manuscrit, dans `.glose/brouillons/`. Fermer un panneau conserve sa saisie ; le bouton **Brouillons** permet de la reprendre ou de l'abandonner. Attendez « Brouillon sauvegardé dans le projet » pour garantir sa récupération après redémarrage du serveur. Si le fichier a changé ou qu'un enregistrement a eu une réponse incertaine, le brouillon reste à copier et à vérifier, sans réapplication automatique. Ces fichiers contiennent vos saisies privées : sauvegardez-les si nécessaire, mais ne les publiez pas par défaut.

**Retrouver un passage.** **Rechercher** (Ctrl+Maj+F) cherche dans tous les fichiers, même non affichés, dans la vue Actuel ou Proposé. Un résultat ouvre son chapitre et surligne le passage ; un fichier changé depuis la recherche impose de la relancer. Les liens Markdown entre chapitres ouvrent aussi la partie visée. Dans **Navette**, choisir une main puis ouvrir une archive permet de relire la proposition, la décision et toute sa discussion dans un panneau de lecture.

**Sauvegarder.** Suivez avec Git vos fichiers `.md`, `.glose/journal.jsonl` et `.glose/snapshots/` ; le reste de `.glose/` est local (`docs/GUIDE.md`, « Sauvegarder »).

**Limites.** UTF-8 obligatoire ; ce que Glose ne peut pas situer au caractère près s'annote au paragraphe, jamais au hasard ; pandoc 3.1.3 est la seule version suivie ; un très gros fichier se refait section par section (2 à 3 s pour 240 000 mots d'un seul tenant). Détail : `docs/GUIDE.md`, « Ce que Glose sait lire ».

## Documentation

- `docs/GUIDE.md` : préparer un texte, mener une navette, sauvegarder, reprendre, ce que Glose sait lire.
- `docs/TESTS-NAVIGATEUR.md` : essais reproductibles dans Chrome/Chromium (`./scripts/test-browser.sh`), sans paquet npm.
- `docs/UNICODE.md` : entretien des tables Unicode.

Vérifications de développement : `./scripts/check.sh` (format, analyse statique, tests, compilation Windows de contrôle).

## Licence

Glose est un logiciel libre : vous pouvez le redistribuer et le modifier selon les termes de la licence publique générale GNU (GPL), version 3 ou toute version ultérieure. Voir `LICENSE`.

Les icônes viennent de [Lucide](https://lucide.dev) (licence ISC, avis reproduit dans `web/icones.js`).
