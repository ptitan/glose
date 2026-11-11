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

## 1 bis. Retrouver ses projets

`glose open` et `glose init` inscrivent un projet à profil dans un registre personnel (`projets.json`, à côté des réglages des agents : le chemin et la date, rien d'autre ; le perdre ne perd rien). Ce qui vit dans le dossier temporaire du système n'y entre pas.

`glose home` ouvre la page de vos projets, relus à chaque fois dans leur dossier sans rien y écrire : à qui est la main, annotations, avancement, échéance, serveur en marche, prochaine action. Par défaut, ce qui attend votre main vient d'abord, puis l'échéance la plus proche. Filtrez par type et par statut, cherchez par titre. Un clic ouvre le projet, en démarrant son serveur s'il le faut ; depuis un projet, le logo et le lien « Projets » reviennent ici. **Nouveau projet** prépare un dossier existant (son chemin, et un gabarit s'il y en a) sans rien écraser. La page ne remet ni ne décide rien : on décide dans le texte, après l'avoir lu.

L'avancement vient des titres que vous avez affichés : un gros projet dont un seul fichier a été ouvert dit « partiel », et un projet jamais affiché dit « Inconnu ». Un dossier disparu reste listé comme introuvable (disque ou dossier synchronisé absent) ; **Retirer du registre**, ou `glose home --forget CHEMIN`, le retire sans toucher au dossier. `glose home --list` donne la même vue dans le terminal. WSL et Windows ont chacun leur registre.

**Dans OpenCode**, une commande `/projets` donne la même vue, sans quitter la session. Elle vit dans votre configuration, pas dans Glose : `~/.config/opencode/commands/projets.md` :

```markdown
---
description: Vue d'ensemble des projets Glose (qui a la main, annotations, échéances)
---

Résultat de `glose home --json` :

!`glose home --json 2>&1; echo "code de sortie : $?"`

Présente ces projets en un tableau court, dans l'ordre reçu : titre, type, statut,
à qui est la main, annotations, avancement, échéance, prochaine action. Signale à part
ceux dont `state` n'est pas `ok`, avec leur `message`. Termine par une ligne sur `totals`.
N'exécute aucune autre commande : cette vue ne décide rien et ne remet aucun tour.
```

Cette sortie ne contient jamais de jeton : elle ne permet pas d'ouvrir une session à votre place. `glose home` est refusé si `GLOSE_AGENT` est posé : un agent lancé par Glose travaille sur son projet, sans voir les autres.

**Ce que vous dites d'un projet.** La clé `project` de `glose.json` garde son type, son statut, son échéance et la prochaine action : `glose project --status review --deadline 2026-12-31 --next-action "Relire la partie 2"`. Seules ces valeurs changent dans le fichier, le reste garde ses octets. Ce fichier est suivi dans le dépôt du texte : ces valeurs s'y voient si le dépôt est public, et chaque changement y fait une différence à commiter.

**Partir d'un gabarit.** `glose init --type article` recopie d'abord le dossier `gabarits/article/` de votre configuration (à côté de `agents.json`) : son arborescence, son `glose.json` (clés `project` et `agent` comprises), ses consignes. Rien n'est écrasé, et ce qui est sauté est dit. Sans gabarit de ce nom, la commande liste ceux qui existent. Glose n'en fournit aucun : ils sont à vous.

## 1 quater. Préparer une consigne réutilisable

Ouvrez `glose set`, puis **Consignes → Ajouter une consigne**. Par exemple :

- nom **Réécrire**, icône **Plume** choisie dans la grille d’icônes nommées ;
- consigne : « /rewrite relis tout le texte et propose les modifications nécessaires » ;
- agent : votre nom court Claude Code ou OpenCode ; sans choix, l'agent du texte, sinon celui par défaut ;
- portée : **Fichier affiché**, **Fichiers choisis** ou **Projet entier** ;
- sous **Options**, cochez **Bouton dans la barre des vues** pour un raccourci ; la consigne figure toujours dans le panneau de remise ;
- annotations en cours : **Refuser le lancement** pour commencer sur les fichiers choisis sans annotation, ou **Les traiter aussi**.

Chaque consigne enregistrée est repliée sur une ligne (icône, nom, agent). Cliquez pour la déplier : Nom, Icône, Consigne et Agent sont visibles, le reste se trouve dans **Options** (portée, accès et limites). L’identifiant se déduit du nom à la création puis reste stable. Chaque aide se lit dans une bulle, au survol ou au focus du bouton (i).

`/rewrite` est du texte remis à l'agent : Glose ne crée ni commande slash ni skill. Pour une méthode particulière, indiquez son nom et sa manière d'être chargée dans le prompt. Les skills et les serveurs MCP doivent déjà exister dans la configuration de l'agent.

Le dossier de lancement vide désigne la racine du projet ; la durée vide reprend le réglage général (30 minutes en l'absence de réglage). Les chemins relatifs des réglages globaux sont interprétés depuis le projet courant. Les dossiers doivent exister. Si vous lancez depuis un autre dossier, la consigne rappelle à l'agent de passer explicitement le chemin du texte aux commandes `glose`.

**Accès.** La section **Accès par défaut des consignes** vaut pour toutes les consignes. Chaque consigne peut les élargir, jamais les réduire. Les oui-non sont des interrupteurs, les choix à trois valeurs des segments. Choisissez **Dossiers choisis** pour lire des dossiers supplémentaires : leur liste, un par ligne, apparaît alors ; n’activez l'écriture extérieure que si elle est nécessaire. **Tous** est déconseillé. Les outils MCP, séparés par des virgules, s'ajoutent à ceux requis par le texte. Python ou le shell peuvent agir avec les droits de votre compte : les cases dossiers et web ne les isolent pas. Lisez les limites affichées pour chaque agent ; une case non applicable ne constitue pas une protection. [Tableau et versions vérifiées](etudes/p15-acces.md).

**Lancer.** Cliquez sur l'icône de la consigne dans la barre ou choisissez-la dans le panneau de remise. Vérifiez la sélection des fichiers (pour un fichier affiché, le premier fichier visible est présélectionné), l'agent, le dossier, la durée et le rappel des accès. Ajoutez une note si nécessaire, puis cliquez sur **Lancer** ; **Annuler** ferme le panneau. Sous l’agent et les fichiers, **Accès accordés** donne chaque accès sur sa ligne, coché s’il est accordé, barré sinon ; ouvrez **Détails** pour les limites de l’agent, le dossier et la durée. Aucune case de confirmation. Rien ne se lance au clic sur l'icône. Pour changer les accès, retournez dans `glose set`. Si les réglages changent entre aperçu et confirmation, la remise est refusée ; votre note reste dans le panneau. Si les fichiers choisis ont des annotations à trancher, l’encart **Lancement en attente** le dit, **Lancer** reste désactivé et **Aller à la première annotation** y mène. Les annotations dans les autres fichiers ne bloquent pas. **Aucune** conserve la remise habituelle. Une consigne lance son agent choisi, même si une autre session attend déjà.

Au retour, les changements libres deviennent des propositions, comme dans toute navette. L'auteur les accepte ou les rejette. La capsule montre la consigne pendant le travail ; `glose status` et `glose log` la donnent aussi. Après un échec, **Relancer** reprend la consigne inscrite à la remise, même si ses réglages ont changé depuis.

Pendant son tour, l’agent peut lancer `glose progress --text "Je vérifie les références."` (120 caractères au plus). La phrase et son heure apparaissent sous son activité dans la capsule et le panneau des tours. Elles ne vont ni dans le manuscrit ni dans le journal et disparaissent après la remise. **Afficher la sortie** reste disponible pour le détail.

Un projet peut proposer sa propre consigne, en modifiant à la main `glose.json` :

```json
{
  "tasks": [
    {
      "id": "rewrite",
      "name": "Réécrire",
      "icon": "suggerer",
      "description": "Relecture complète du chapitre",
      "prompt": "/rewrite relis tout le texte et propose les modifications nécessaires.",
      "scope": "file",
      "place": ["bar", "handover"],
      "whenAnnotations": "block",
      "maxMinutes": 30,
      "access": {
        "externalDirs": "list",
        "dirs": ["references"],
        "web": true,
        "scripts": "none"
      }
    }
  ]
}
```

Créez le dossier `references` pour cet exemple. Tous les dossiers de la consigne du projet, y compris `workdir`, sont relatifs au projet ; aucun chemin absolu ni secret. À identifiant égal, l'entrée du projet remplace toute la consigne globale. Les réglages globaux d'accès continuent de s'ajouter. `glose check` signale les consignes invalides. La page des consignes modifie seulement les entrées globales ; les entrées du projet restent dans son profil.

**Validation réelle à faire par l'autre agent (P15.5).** Sur une copie propre d'un chapitre, et aussi un Markdown ordinaire sans profil, lancer **Réécrire** avec Claude Code puis OpenCode. Vérifier le chargement de la méthode/skill hors du projet, le fichier annoté au retour, le journal `task`, le rejet qui rend les octets originaux et l'absence de changement hors de la copie. Cet essai réel n'est pas réalisé par la présente implémentation.

## 2. Le déroulement d'une navette

Une navette est une suite de tours. La main est d'abord à l'auteur.

**L'écran.** En haut, **glose** ramène aux projets ; à côté, le titre de l’ouvrage, et dessous ce qui est affiché et le pourcentage de sections relues ; à droite, **Annuler**, **Rétablir** et **Réglages** (l’affichage). Dessous, le **bandeau de la navette** montre la main (vert pour vous, ardoise pour l’agent), l’état de l’agent et les commentaires, suggestions et réponses de ce tour ; **Remettre à l’agent** ou **Reprendre la main** ouvre la boîte correspondante, et **Afficher la sortie** montre ce que fait un agent lancé par Glose. Le bandeau devient ambre quand personne n’écoute. La troisième bande réunit les vues **Modifications**, **Actuel**, **Proposé** et **Depuis** (touches 1 à 3), puis à droite **Rechercher** (Ctrl+Maj+F), **Exporter en PDF**, vos consignes et **Typo**. Dans une fenêtre de 1280 pixels ou moins, Rechercher, Annuler et Rétablir passent dans **Plus**. Après 80 pixels de défilement, ces trois bandes deviennent une ligne compacte de 48 pixels ; la loupe rouvre la recherche. À gauche, la colonne à onglets ; au centre, le texte ; à droite, les annotations en marge. Le pied de page donne l’état du rendu et le bouton **Brouillons**.

**Votre tour.** Dans le navigateur :
- sélectionnez un passage, puis **Commenter** (C) ou **Suggérer** (S) ; la poignée de la marge gauche agit sur un paragraphe entier, le menu « + » ajoute un élément avant ou après ;
- chaque geste est écrit aussitôt dans le fichier, en CriticMarkup, avec son identité ;
- un long commentaire se déplie quand vous cliquez dessus et se replie dès que vous cliquez ailleurs ; cliquer dedans pendant que vous le lisez ne fait pas bouger la page ;
- sur une carte, chaque message porte le nom et la couleur de qui l'a écrit : vert pour vous, bleu pour l'agent ; le gras et l'italique des messages y sont affichés ;
- dans l'onglet **Annotations**, une pastille dit où en est chacune (À remettre, À décider, Sans réponse, Répondu par…), et **Traitées**, sous la liste, garde ce que vous avez accepté, rejeté, résolu ou retiré ; un clic en ouvre l'archive ;
- tant que vous n'avez pas remis votre tour, vos commentaires et vos réponses se modifient (**Modifier** sur la carte, ou au bout de la réponse) ou se retirent ; vos propositions se modifient tant qu'elles sont en attente ; une fois remis, on répond dans le fil ;
- Ctrl+Z et Ctrl+Y annulent et rétablissent vos derniers gestes ;
- la touche + ouvre le menu de la poignée, sur le paragraphe sélectionné ou le premier de l'écran : Ajouter avant, Commenter, Suggérer, Ajouter après, aux flèches ;
- J et K passent d'une annotation à l'autre ; A accepte, X rejette, R répond, E modifie votre proposition ou votre commentaire pas encore remis ;
- quand vous avez fini, **Remettre à l'agent**, avec un message si vous voulez.

**Commenter et suggérer.** Le panneau **Commenter** s'ouvre en marge droite à hauteur du passage : le texte reste lisible et défilable. **Ajouter** (Ctrl+Entrée) enregistre la remarque ; **Annuler**, la croix ou Échap ferment le panneau en conservant le brouillon. Le focus revient au point de départ, ou au texte. Une saisie en cours doit être fermée avant d'en ouvrir une autre.

Le panneau **Suggérer** se place sous le passage quand la place suffit, sinon dans une marge droite élargie. Les onglets **Mis en forme**, **Source** et **Aperçu**, la barre d'outils (Ctrl+B, Ctrl+I), les propositions qui seront créées, **Une seule proposition**, le message et **Réponse par** restent disponibles. Le contenu défile dans le panneau ; **Annuler**, **Appliquer directement** et **Ajouter la suggestion** restent en bas. Les touches d'une composition IME appartiennent à la méthode de saisie, y compris Échap et Ctrl+Entrée.

**Modifier directement.** Quand une correction n'a pas à être discutée, **Appliquer directement** (Ctrl+Maj+Entrée) l'écrit tout de suite dans le texte, sans proposition. L'agent la verra comme une modification imposée et ne la discutera pas. Le bouton est désactivé tant que vous écrivez un message pour l'agent : un message demande une réponse, il va donc avec une suggestion. Ctrl+Z annule la modification tant que le fichier n'a pas changé ; elle figure dans **Traitées** comme « Imposé ».

**Exporter en PDF.** Le bouton après Annuler et Rétablir ouvre un petit panneau : **Modifications apparentes** (avec les commentaires en marge, si la case reste cochée), **Actuel** ou **Proposé**. Glose exporte ce qui est affiché : pour une seule partie d'un gros projet, choisissez-la d'abord dans le sommaire. **Exporter** ouvre la fenêtre d'impression : choisissez « Enregistrer au format PDF ». Sous Chrome et Edge, décochez une fois « En-têtes et pieds de page », qui ajouterait l'adresse de la page ; les numéros de page restent. Sous Firefox, la page est numérotée par sa propre fenêtre d'impression. Ctrl+P donne la vue affichée avec ses commentaires. L'écran revient ensuite tel qu'il était, et rien n'est écrit dans vos fichiers.

**Réglages**, en haut à droite du texte, ouvre un petit panneau sous son bouton : police, taille, interligne, largeur, justification, thème et retour aux réglages par défaut. **Réglages**, dans le menu de l'accueil et de la page Agents, offre les mêmes choix, avec un aperçu de chaque thème. **Revenir au texte**, en haut de ce menu, vous ramène à l'endroit où vous étiez. La page `/set` reprend la navigation et les champs de l'accueil ; **Vos projets** ramène à la bibliothèque.

**Le tour de l'agent.** Il attend votre remise (`glose wait`), lit les annotations (`glose list --json`), répond, propose ou modifie librement les fichiers, puis remet son tour (`glose done`). Ses modifications libres deviennent des propositions mot à mot ; seule une réécriture faite par `glose propose --whole` devient une proposition unique, à accepter ou rejeter en bloc. Pendant ce tour, vous lisez mais n'écrivez pas : la barre du haut le rappelle.

**Décider.** Les propositions de l'agent apparaissent en marge. Vous les acceptez, les rejetez, ou répondez dans leur fil. **L'agent ne décide jamais** : aucune commande ne lui permet d'accepter, de rejeter ou de résoudre. La navette s'achève quand il ne reste plus d'annotation.

**Suivre.** La colonne de gauche a quatre onglets, en icônes (leur nom s'affiche dessous et au survol ; Alt+1 à Alt+4 au clavier) : Sommaire (avec l'avancement de la relecture, que vous marquez titre par titre), Fichiers (l'arbre du projet : un clic sur un fichier ou un dossier n'affiche que lui ; un gros projet s'ouvre sur son premier fichier), Annotations (filtrables ; celles d'un fichier non affiché sont en italique, un clic l'affiche) et Navette (la frise des tours, en deux couloirs **Vous** et **Agent**, avec la première main à l’auteur, les anneaux, les remises et les archives). Quand une partie seule est affichée, l’onglet Fichiers rappelle son nombre de fichiers et son avancement ; la recherche et les comptes d’annotations portent toujours sur le projet. Le bouton en haut de la colonne (ou Alt+0) la replie : un rail étroit reste à gauche pour la rouvrir ou aller droit à un onglet. `glose log` donne la même histoire en ligne de commande.

## 2 bis. Les agents : qui répond, et comment le lancer

Glose ne parle à aucun modèle lui-même (D-47) : il travaille avec un **agent en ligne de commande** (Claude Code, Codex, OpenCode), qui garde sa propre connexion et ses outils (MCP, skills). Deux façons de mener le tour de l'agent :

- **Glose lance l'agent à la remise** (D-46). Lancez une fois `glose set` : une page s'ouvre (réglages **généraux**, valables pour tous vos projets). Elle montre les agents installés et vous fait choisir des **noms courts** : un nom pour un couple agent + modèle (« Sol » : Codex avec tel modèle ; « Lex » : OpenCode avec tel autre), et celui **par défaut**. Les modèles proposés viennent de l'agent quand il en donne la liste (`codex debug models`, `opencode models`) ; sinon, saisie libre. **Essayer** lance l'agent à blanc (il liste ses outils et lit `glose status`, sans rien écrire ; cela consomme des jetons). Tant que rien n'est réglé, le bandeau de la navette et `glose open`, `status`, `doctor` disent « Aucun agent configuré » et mènent à `glose set`. Ensuite, la boîte de remise propose **Lancer Sol à la remise** (cochée quand aucun agent n'écoute) : l'agent travaille seul, dans le dossier du projet, avec des permissions réduites (lancer `glose`, lire et écrire les fichiers du texte), puis remet la main par `glose done`. Le bandeau de la navette dit « Sol travaille » ; son menu (la flèche à côté) permet de **voir ce qu'il fait** (sa sortie, gardée dans `.glose/agent/tour-N.log`), de l'**arrêter**, ou de le **relancer** s'il s'est arrêté sans remettre. Un tour dure 30 minutes au plus (réglable dans `glose set`).
- **Vous menez déjà une session d'agent** (Claude Code ou OpenCode ouvert à côté). Menu du bandeau de la navette, **Lier un agent** : copiez la consigne et collez-la telle quelle dans la session. Elle contient un code court (dix minutes pour lier) et la commande `glose join CODE --agent "Sol"`, au nom de l'agent du texte ou par défaut. Cette commande attend votre remise ; au bout de 90 s, elle se termine par « délai écoulé » et l'agent la relance ; quand la main vient, elle lui donne les consignes du tour, puis il la relance pour le tour suivant. Le bandeau de la navette montre « Sol attend », avec le modèle quand il est connu (lu chez OpenCode, déclaré par `--model` sinon). Si la session a cessé de relancer la commande, le bandeau de la navette dit « Sol est lié mais n'attend pas » : demandez-lui de la relancer. Pendant qu'un agent lancé par Glose travaille, une session liée attend la remise suivante.

**Qui écoute ?** Le bandeau de la navette le dit : « Sol attend » (un agent est en `glose wait`), « Sol a pris la main », « Sol travaille · 2 réponses », ou, en orange, « Aucun agent à l'écoute » (personne : la main restera à l'agent jusqu'à ce qu'un agent la prenne ; vous pouvez la reprendre). Les agents donnent leur nom par `--agent NOM` ou la variable `GLOSE_AGENT` (la section « Navette Glose » le leur demande).

**Choisir l'agent par annotation.** Dans les panneaux Commenter et Suggérer, **Réponse par** choisit le nom court qui répondra (affiché dès que vous en avez deux) ; écrire « @Luna » en tête du texte fait de même. La carte montre « → Luna ». À la remise, Glose fait **un passage par destinataire**, l'un après l'autre : chacun ne voit que ses annotations ; après le dernier, Glose rend lui-même la main, avec une note qui réunit celles de chacun. Les réponses et propositions portent le nom de l'agent qui les a écrites. Une session liée répond aux annotations sans destinataire et à celles adressées à son nom, pendant que Glose lance les autres.

**Par texte**, dans `glose.json`, la clé `agent` choisit un nom court pour ce texte, ajoute des consignes propres (« vérifie chaque référence avec LexHarness ») et déclare des outils requis (`mcp:lexharness`, `skill:…`) ; Glose vérifie que l'agent les déclare et prévient sinon (voir `glose help profile`). Jamais de chemin ni de clé dans ce fichier.

**Limites.** Un agent installé seulement côté Windows n'est pas lancé depuis WSL. Codex ne montre pas ses connecteurs dans `codex mcp list` : un outil requis peut y être dit absent alors qu'il l'a. Le bac à sable de Codex borne ses écritures au projet mais ne restreint pas les programmes qu'il lance. Le jeton du serveur n'est jamais montré à un agent qui se nomme, et Claude Code et OpenCode lancés par Glose ne lisent pas `.glose/` ; il protège d'abord des autres pages web.

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
| `.glose/agent.json` | le dernier signe de vie de l'agent (son nom, ce qu'il fait, quand) : le bandeau de la navette dit qui écoute | non : il se perd sans dommage |
| `.glose/agent/` | la sortie des agents lancés par Glose (`tour-N.log`) | non |
| `.glose/passes.json`, `.glose/owner.json` | les passages d'un tour à plusieurs destinataires ; qui mène le tour de l'agent en cours (agent lancé ou session liée) | non |
| `.glose/link.json` | le code de liaison en cours (dix minutes) | non |
| `glose/agents.json` dans votre dossier de configuration | vos agents : noms courts, agent par défaut, durée maximale d'un tour (`glose set`) | non : aucune clé ni secret |
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
| L'agent ne remet pas son tour (session fermée, agent bloqué) | Dans la barre du haut : **Reprendre la main**. Les modifications de l'agent sont converties en propositions, comme par `glose done`. |
| La reprise refuse de convertir (anomalie que l'agent n'a pas corrigée : marque modifiée, marque écrite à la main mêlée à une modification…) | Le panneau donne chaque anomalie, avec le fichier et la ligne. Corrigez le fichier, ou demandez à l'agent de le faire, puis recommencez. En dernier recours : **reprendre sans convertir** (voir ci-dessous). |
| `glose done` refuse (côté agent) | Il dit pourquoi et où. L'agent corrige le fichier puis relance ; `glose check` annonce d'avance ce que `glose done` refuserait. |
| Opération interrompue (plantage, coupure) | Rien à faire : elle est reprise ou défaite à l'écriture suivante. Si `glose status` la signale comme **partielle**, certains fichiers n'ont pas pu être remis dans leur état d'avant : regardez-les (`git diff`), puis corrigez à la main. |
| Dernière ligne du journal tronquée | Signalée, jamais ignorée ; l'écriture suivante la répare. |
| « Annotation disparue sans décision » | Une annotation connue du journal a été effacée du fichier sans être acceptée ni rejetée. Elle n'est **jamais** tenue pour acceptée. Retrouvez-la dans l'historique Git ou dans `glose log`, et remettez-la si besoin. |
| Un éditeur extérieur a modifié le fichier pendant une saisie | L'écriture est refusée et votre saisie conservée ; l'affichage se met à jour. |
| `glose stop` dit qu'aucun serveur ne tourne, mais l'adresse répond encore | Attendez quelques secondes : un serveur dont le dossier `.glose` a été effacé réécrit son état, puis `glose stop` le trouve. Sinon, arrêtez le processus `glose serve <dossier>` (gestionnaire de tâches, ou `kill`). |

**Reprendre sans convertir.** Les modifications de l'agent restent alors dans les fichiers **sans propositions** : vous ne les verrez pas en marge, et rien ne les distingue de votre texte. La reprise est notée au journal. Avant de l'utiliser, faites un `git diff` pour voir ce que l'agent a changé, ou revenez au dernier commit.

## 6. Retrouver ses brouillons

Un commentaire, une réponse ou une réécriture que vous n'avez pas encore enregistrés sont gardés dans `.glose/brouillons/`, même si vous fermez le panneau, rechargez la page, ou fermez et rouvrez Glose (sur un autre port). Le bouton **Brouillons** du pied de page ouvre leur panneau à droite, même pendant le tour de l'agent. Chaque carte permet de reprendre, abandonner, réessayer la sauvegarde ou copier le texte et son message.

- Attendez l'indication « Brouillon sauvegardé dans le projet » : c'est elle qui garantit la récupération après un redémarrage du serveur. Avant, seule une copie de secours du navigateur existe, liée à l'adresse (donc au port).
- **Reprendre** rouvre le panneau, si la main est à vous et si le fichier n'a pas changé depuis.
- Si le fichier a changé, le brouillon n'est **jamais** réappliqué tout seul : son texte et son message restent à copier, pour les reporter vous-même au bon endroit.
- Un envoi dont la réponse s'est perdue est marqué « à vérifier » : regardez si l'annotation est déjà dans le texte avant de la refaire, pour ne pas la doubler.
- **Abandonner** retire un brouillon, après confirmation. Il n'y a pas d'expiration automatique.

## 7. Ce que Glose sait lire, et ses limites

**Navigation et recherche.** Les images relatives se résolvent par rapport au fichier Markdown qui les contient : `chapitre/texte.md` peut écrire `![Figure](img/figure.png)` pour `chapitre/img/figure.png`. Un lien `[suite](../chapitre-2/texte.md#section)` ouvre le fichier du projet et son ancre, même non affichés ; un simple `#section` cherche d'abord dans son propre fichier, puis dans les autres, seulement si la cible est unique. Une cible absente ou ambiguë est signalée, pas devinée. Le navigateur ne quitte pas Glose pour suivre un lien Markdown local. Avec un filtre, le contrat de provenance et les enveloppes de fichiers doivent rester respectés.

Le bouton **Rechercher** de la barre du haut, qui devient un champ le temps de la recherche (Échap deux fois le referme), (Ctrl+Maj+F ; Ctrl+F reste celui du navigateur) cherche littéralement dans le Markdown projeté de tous les fichiers : vue Actuel en Relecture/Actuel, vue Proposé en Proposé. Les annotations et métadonnées CriticMarkup ne sont pas cherchées ; les signes Markdown, échappements et différences d'accents composés/décomposés restent pris en compte. Les majuscules sont ignorées, sauf option contraire. Le total est indiqué et les 200 premiers résultats contextualisés sont affichés. Les résultats s'affichent sous le champ (↑ ↓ pour les parcourir, Entrée pour ouvrir, Échap pour fermer). Un clic ouvre le chapitre et surligne les mots concernés ; si le fichier a changé, relancer la recherche. Pour un élément sans mots annotables (code, image), le déplacement se fait au voisinage du passage, sans inventer une sélection éditable.

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


## Vue Depuis

La quatrième vue compare le texte Actuel à la dernière remise de l’auteur. La liste Référence permet de choisir une remise plus ancienne dont toutes les copies sont disponibles. Les changements sont colorés au mot près ; le bilan par fichier montre aussi les suppressions et signale les fichiers apparus ou disparus. Cette vue est en lecture seule. Aucun fichier ni journal n’est écrit par la comparaison.


## Règles typographiques

`typo.json`, dans le dossier de configuration de Glose, est créé à la première utilisation. Il contient une liste de règles (`id`, `description`, `pattern`, `replacement`, `disabled`). Les motifs suivent la syntaxe des expressions régulières de Go ; les remplacements emploient `${1}` pour les groupes. Une règle invalide refuse le lancement, sans réécriture du fichier.

Les règles par défaut corrigent les espaces avant `; : ! ? »` et après `«`, les guillemets droits sûrement appariés, l’espace avant un appel de note, les espaces après « art. » et « n° », et les trois points. Les plages doivent être certifiées. Code, citations, liens, macros, frontmatter, attributs et CriticMarkup sont exclus. En cas de doute, notamment de guillemets droits non appariés, aucune proposition n’est produite. Ce relecteur local déterministe n’appelle aucun modèle ni réseau.


Le bouton **Typo** lance la relecture du fichier affiché choisi dans « Fichier pour Typo », seulement quand l’auteur a la main. Les corrections deviennent des propositions signées `via=Typo`, sans changer le texte Actuel. **Tout accepter (Typo)** demande confirmation et accepte uniquement ces propositions dans ce fichier ; les autres restent en attente. L’opération et chaque décision sont gardées dans le journal ; l’acceptation se défait avec Annuler. Le bouton est désactivé dans Depuis.


## Mission des annotations

Pendant le tour lancé par une consigne, les nouvelles propositions, commentaires, réponses et modifications libres converties portent `task=ID` dans leur commentaire technique. Les annotations antérieures restent inchangées ; une réponse peut porter une autre mission que la tête du fil. La carte affiche l’icône et le nom de la consigne conservés lors de la remise, ou son identifiant si la consigne est inconnue. Le menu Mission filtre la marge, y compris par mission d’une réponse. `glose list --task ID` donne les fils concernés ; `glose list --json` expose `task` sur les annotations et messages. Les décisions archivées conservent cette étiquette.
