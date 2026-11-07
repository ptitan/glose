# Guide : mener une navette avec Glose

Ce guide s'adresse à l'auteur. Il dit comment ouvrir un texte, mener les allers-retours avec l'agent, sauvegarder, et reprendre quand quelque chose bloque. Les consignes de l'agent sont celles de la section « Navette Glose » que `glose init` écrit dans `AGENTS.md` (ou `CLAUDE.md`) ; `glose init --print` les affiche.

## 1. Préparer un texte

```bash
cd mon-texte/
glose init        # profil glose.json et section « Navette Glose » pour l'agent
glose doctor      # pandoc, Git, navigateur, profil, droits : chaque problème avec sa correction
glose open        # lance le serveur en arrière-plan et ouvre le navigateur
```

- **Quels fichiers ?** Sans profil, Glose prend les `.md` du dossier et de ses sous-dossiers, dans l'ordre d'une table des matières. Il saute les dossiers cachés et les consignes d'agent. `glose init` écrit un profil qui liste les fichiers de la racine, puis chaque sous-dossier : retirez de `files` ce qui n'est pas le texte (notes, consignes, sources). `exclude` ne filtre que ce que trouvent les dossiers et les motifs (un sous-dossier à écarter, des brouillons) ; un fichier nommé dans `files` reste pris. Tout est décrit par `glose help profile`.
- **Quel agent ?** OpenCode et Codex lisent `AGENTS.md`. Claude Code lit `CLAUDE.md` : `glose init` n'y écrit que si `AGENTS.md` n'existe pas ; avec Claude Code, lancez donc `glose init --to CLAUDE.md` (sauf si votre `CLAUDE.md` importe déjà `AGENTS.md` par une ligne `@AGENTS.md`). Lancez l'agent depuis le dossier du texte, après `glose init`, pour qu'il lise la section au démarrage ; sinon, demandez-lui de la lire.
- **Une partie d'un projet** : dans un projet qui a un `glose.json`, `glose open livre-1/` ou `glose open livre-1/01.md` ouvre le projet entier (même serveur, même navette), avec cette partie mise en avant ; `glose list livre-1/` ne liste que ses annotations. `glose init` se lance à la racine du projet, jamais dans un de ses sous-dossiers.
- **Un seul fichier**, hors de tout projet : `glose open notes.md`.
- `glose stop` arrête le serveur ; `glose status` dit s'il tourne et à quelle adresse.

## 2. Le déroulement d'une navette

Une navette est une suite de tours. La main est d'abord à l'auteur.

**Votre tour.** Dans le navigateur :
- sélectionnez un passage, puis **Commenter** (C) ou **Suggérer** (S) ; la poignée de la marge gauche agit sur un paragraphe entier, le menu « + » ajoute un élément avant ou après ;
- chaque geste est écrit aussitôt dans le fichier, en CriticMarkup, avec son identité ;
- Ctrl+Z et Ctrl+Y annulent et rétablissent vos derniers gestes ;
- J et K passent d'une annotation à l'autre ; A accepte, X rejette, R répond, E corrige votre proposition ;
- quand vous avez fini, **Remettre à l'agent**, avec un message si vous voulez.

**Le tour de l'agent.** Il attend votre remise (`glose wait`), lit les annotations (`glose list --json`), répond, propose ou modifie librement les fichiers, puis remet son tour (`glose done`). Ses modifications libres deviennent des propositions mot à mot ; seule une réécriture faite par `glose propose --whole` devient une proposition unique, à accepter ou rejeter en bloc. Pendant ce tour, vous lisez mais n'écrivez pas : le bandeau le rappelle.

**Décider.** Les propositions de l'agent apparaissent en marge. Vous les acceptez, les rejetez, ou répondez dans leur fil. **L'agent ne décide jamais** : aucune commande ne lui permet d'accepter, de rejeter ou de résoudre. La navette s'achève quand il ne reste plus d'annotation.

**Suivre.** La colonne de gauche a quatre onglets, en icônes (leur nom s'affiche dessous et au survol ; Alt+1 à Alt+4 au clavier) : Sommaire (avec l'avancement de la relecture, que vous marquez titre par titre), Fichiers (l'arbre du projet : un clic sur un fichier ou un dossier n'affiche que lui ; un gros projet s'ouvre sur son premier fichier), Annotations (filtrables ; celles d'un fichier non affiché sont en italique, un clic l'affiche) et Navette (la frise des tours). `glose log` donne la même histoire en ligne de commande.

## 3. Ce que Glose garde, et où

| Où | Quoi | Normatif ? |
|---|---|---|
| vos fichiers `.md` | le texte et les annotations en cours, en CriticMarkup | **oui** : c'est le présent |
| `.glose/journal.jsonl` | l'histoire : tours, opérations, décisions, copie des annotations closes | **oui** : c'est le passé |
| `.glose/snapshots/` | la copie des fichiers au moment de chaque remise de l'auteur, nommée par son empreinte | nécessaire pour convertir les modifications de l'agent pendant son tour |
| `.glose/brouillons/` | vos saisies non encore enregistrées (commentaires, réponses, réécritures) | non : privées, jamais dans le texte ni dans le journal |
| `.glose/progression.json` | l'avancement de la relecture, titre par titre | non |
| `.glose/cache/` | le rendu de chaque fichier, pour rouvrir un gros texte sans tout relire (512 Mo au plus) | non : effaçable à tout moment, il se reconstruit |
| `.glose/server.json`, `server.lock`, `server.log`, `lock` | serveur en cours, verrous, sortie du serveur | non |
| `glose/reglages.json` dans votre dossier de configuration (`~/.config` sous Linux, ou `$XDG_CONFIG_HOME` ; `%AppData%` sous Windows ; `GLOSE_CONFIG_DIR` le remplace) | vos réglages d'affichage (police, taille, thème…) | non |

## 4. Sauvegarder

**Avec Git (recommandé).** Suivez vos fichiers `.md`, le journal et les copies de remise ; ignorez le reste :

```gitignore
.glose/*
!.glose/journal.jsonl
!.glose/snapshots/
```

Ajoutez `!.glose/progression.json` si vous voulez garder l'avancement de la relecture. Les brouillons sont personnels : les suivre n'a d'intérêt que pour ne pas les perdre d'une machine à l'autre.

**Quand commiter ?** À la fin de votre tour ou après une série de décisions, quand `glose status` n'indique aucun problème. Évitez de commiter pendant que l'agent écrit.

**Sans Git**, copiez le dossier entier, `.glose/` compris, serveur arrêté (`glose stop`).

**Ce que Glose garantit lui-même** (détail dans `docs/ARCHITECTURE.md`, « Durabilité ») :
- si Glose ou le serveur s'arrête brutalement, chaque fichier contient son ancien ou son nouveau contenu, jamais un mélange, et une opération interrompue est reprise ou défaite à l'écriture suivante ;
- après une coupure de courant ou un gel de la machine, une écriture terminée sans erreur survit, dans la mesure où le système et le disque honorent la synchronisation (`fsync`) ; ce n'est pas vérifié sur chaque système ;
- les dernières frappes d'un brouillon pas encore confirmé par le serveur peuvent se perdre.

**Dossier Windows ou synchronisé (Dropbox, OneDrive).** Glose fonctionne dans un dossier Windows vu depuis WSL (`/mnt/c/…`) : écriture sûre, verrou, journal, navette et rendu y ont été essayés (le rendu y est environ 20 % plus lent). Deux limites :
- **Droits** : sans l'option `metadata` du montage, les fichiers de `.glose/` (journal, brouillons) y sont lisibles par tous les comptes de la machine ; `glose doctor` le signale et donne la correction.
- **Synchronisation** : elle n'a pas été essayée en direct. Glose refuse d'écrire un fichier qui a changé depuis sa lecture (votre saisie est conservée), ce qui protège d'une synchronisation qui arrive au mauvais moment. Mais une « copie en conflit » créée par Dropbox est un nouveau fichier `.md` : il entrerait dans le projet. Écartez-les dans le profil, par exemple `"exclude": ["**/*conflicted copy*", "**/*copie en conflit*"]`, et ne modifiez pas le même texte sur deux machines pendant une navette.

## 5. Reprendre quand quelque chose bloque

Commencez toujours par :

```bash
glose status      # à qui est la main, anomalies, problèmes du journal
glose check       # marques mal formées, identités, journal ; code 1 en cas d'erreur (les avertissements, comme une annotation disparue, laissent le code 0)
glose doctor      # installation et projet, avec la correction de chaque problème
```

| Situation | Que faire |
|---|---|
| L'agent ne remet pas son tour (session fermée, agent bloqué) | Dans le bandeau : **Reprendre la main**. Les modifications de l'agent sont converties en propositions, comme par `glose done`. |
| La reprise refuse de convertir (anomalie que l'agent n'a pas corrigée : marque modifiée, marque écrite à la main mêlée à une modification…) | Le panneau donne chaque anomalie, avec le fichier et la ligne. Corrigez le fichier, ou demandez à l'agent de le faire, puis recommencez. En dernier recours : **reprendre sans convertir** (voir ci-dessous). |
| `glose done` refuse (côté agent) | Il dit pourquoi et où. L'agent corrige le fichier puis relance ; `glose check` annonce d'avance ce que `glose done` refuserait. |
| Opération interrompue (plantage, coupure) | Rien à faire : elle est reprise ou défaite à l'écriture suivante. Si `glose status` la signale comme **partielle**, certains fichiers n'ont pas pu être remis dans leur état d'avant : regardez-les (`git diff`), puis corrigez à la main. |
| Dernière ligne du journal tronquée | Signalée, jamais ignorée ; l'écriture suivante la répare. |
| « Annotation disparue sans décision » | Une annotation connue du journal a été effacée du fichier sans être acceptée ni rejetée. Elle n'est **jamais** tenue pour acceptée. Retrouvez-la dans l'historique Git ou dans `glose log`, et remettez-la si besoin. |
| Un éditeur extérieur a modifié le fichier pendant une saisie | L'écriture est refusée et votre saisie conservée ; l'affichage se met à jour. |
| `glose stop` dit qu'aucun serveur ne tourne, mais l'adresse répond encore | Attendez quelques secondes : un serveur dont le dossier `.glose` a été effacé réécrit son état, puis `glose stop` le trouve. Sinon, arrêtez le processus `glose serve <dossier>` (gestionnaire de tâches, ou `kill`). |

**Reprendre sans convertir.** Les modifications de l'agent restent alors dans les fichiers **sans propositions** : vous ne les verrez pas en marge, et rien ne les distingue de votre texte. La reprise est notée au journal. Avant de l'utiliser, faites un `git diff` pour voir ce que l'agent a changé, ou revenez au dernier commit.

## 6. Retrouver ses brouillons

Un commentaire, une réponse ou une réécriture que vous n'avez pas encore enregistrés sont gardés dans `.glose/brouillons/`, même si vous fermez le panneau, rechargez la page, ou fermez et rouvrez Glose (sur un autre port). Le bouton **Brouillons** de la barre les liste, même pendant le tour de l'agent.

- Attendez l'indication « Brouillon sauvegardé dans le projet » : c'est elle qui garantit la récupération après un redémarrage du serveur. Avant, seule une copie de secours du navigateur existe, liée à l'adresse (donc au port).
- **Reprendre** rouvre le panneau, si la main est à vous et si le fichier n'a pas changé depuis.
- Si le fichier a changé, le brouillon n'est **jamais** réappliqué tout seul : son texte et son message restent à copier, pour les reporter vous-même au bon endroit.
- Un envoi dont la réponse s'est perdue est marqué « à vérifier » : regardez si l'annotation est déjà dans le texte avant de la refaire, pour ne pas la doubler.
- **Abandonner** retire un brouillon, après confirmation. Il n'y a pas d'expiration automatique.

## 7. Ce que Glose sait lire, et ses limites

**Navigation et recherche.** Les images relatives se résolvent par rapport au fichier Markdown qui les contient : `chapitre/texte.md` peut écrire `![Figure](img/figure.png)` pour `chapitre/img/figure.png`. Un lien `[suite](../chapitre-2/texte.md#section)` ouvre le fichier du projet et son ancre, même non affichés ; un simple `#section` cherche d'abord dans son propre fichier, puis dans les autres, seulement si la cible est unique. Une cible absente ou ambiguë est signalée, pas devinée. Le navigateur ne quitte pas Glose pour suivre un lien Markdown local. Avec un filtre, le contrat de provenance et les enveloppes de fichiers doivent rester respectés.

Le bouton **Rechercher** (Ctrl+Maj+F ; Ctrl+F reste celui du navigateur) cherche littéralement dans le Markdown projeté de tous les fichiers : vue Actuel en Relecture/Actuel, vue Proposé en Proposé. Les annotations et métadonnées CriticMarkup ne sont pas cherchées ; les signes Markdown, échappements et différences d'accents composés/décomposés restent pris en compte. Les majuscules sont ignorées, sauf option contraire. Le total est indiqué et les 200 premiers résultats contextualisés sont affichés. Un clic ouvre le chapitre et surligne les mots concernés ; si le fichier a changé, relancer la recherche. Pour un élément sans mots annotables (code, image), le déplacement se fait au voisinage du passage, sans inventer une sélection éditable.

**Discussions closes.** Dans l'onglet **Navette**, choisir une main puis ouvrir une archive pour retrouver, dans un panneau de lecture, le texte d'origine, le texte proposé, la décision et les messages avec leur auteur. Les versions précédentes d'une proposition corrigée y figurent aussi. Ce sont les événements de cette main, pas une affirmation sur l'état présent : une décision a pu être annulée ensuite. Cette consultation ne réapplique rien au texte ; Échap ou la croix ferme le panneau.

Glose lit le Markdown avec pandoc, au format `commonmark_x` : CommonMark, plus les extensions de pandoc qu'il active (tableaux à barres verticales, barré, indices et exposants, notes, listes de définitions, attributs, encadrés `:::`, formules, typographie automatique). Ce n'est pas le format `markdown` de pandoc : une syntaxe propre à ce dernier (tableaux en grille, par exemple) peut s'afficher autrement que dans votre chaîne de compilation. Il vérifie à chaque affichage que chaque mot affiché correspond exactement aux octets du fichier ; ce qu'il ne peut pas vérifier, il ne le devine pas.

- **Fichiers** : UTF-8 obligatoire (un autre codage est refusé) ; fins de ligne LF, CRLF ou mélangées et marque d'ordre des octets conservées telles quelles.
- **Annotable au mot près** : le texte courant, avec accents (même décomposés, comme dans les fichiers venus de macOS), emoji, japonais, coréen. Entités (`&amp;`), échappements (`\*`) et typographie automatique (`'` → `’`, `--` → `–`) se commentent, mais ne se modifient pas à l'intérieur.
- **Annotable au paragraphe seulement** : un passage dont la position ne peut être établie au caractère près (tabulations, caractères inconnus des tables Unicode 15 de Glose). Glose le dit et propose le paragraphe.
- **Non annotable** : ce qu'un filtre d'affichage ajoute (numéros, titres générés), le code en ligne et les liens vides (renvois), dont la source se retrouve par les mots voisins.
- **Structures** (tableaux, listes, formules) : elles se réécrivent par lignes entières, dans la source, depuis le panneau « Suggérer ».
- **CriticMarkup dans du code** : une marque écrite dans un bloc ou un code en ligne reste du texte, pour les documents qui expliquent la syntaxe. Le repérage est prudent ; `glose check` signale toute marque laissée littérale que pandoc ne voit pas comme du code.
- **HTML brut** : affiché comme du code, jamais exécuté. Images extérieures : remplacées par leur description (rien n'est chargé depuis l'extérieur).
- **Pandoc** : 3.1 ou plus récent ; seule la 3.1.3 est essayée de façon suivie.
- **Taille** : un ouvrage de 50 000 mots s'affiche en 1 à 1,5 s. Un gros ouvrage (243 000 mots) s'affiche partie par partie : le premier affichage d'un fichier prend une ou deux secondes, la réouverture moins d'une seconde grâce au cache, et une annotation ne refait que son fichier. Un très gros fichier (plus de 512 Ko, sans filtre) est rendu par sections, coupées devant ses titres : une annotation n'en refait qu'une (2 à 3 s pour un fichier de 240 000 mots) ; ses notes sont alors montrées à la fin de chaque section. Découper un tel fichier reste plus rapide.
- **Fichiers créés ou supprimés par l'agent** : un fichier que l'agent crée pendant son tour, dans un dossier du texte, devient une proposition d'ajout de tout son contenu (le rejeter le laisse vide) ; l'interface l'affiche sans redémarrage. Un fichier qu'il supprime fait refuser `glose done` : il doit le restaurer (la copie est indiquée), puis proposer d'en vider le texte. Si vous ajoutez au profil, pendant le tour de l'agent, un fichier qui existait déjà, la remise est refusée : remettez ce changement de profil à votre prochain tour. Une remise couvre toujours le projet entier, même ouvert depuis un seul fichier.
