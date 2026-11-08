package navette

// Section is the section « Navette Glose » that « glose init » writes in the
// instructions of the project's agent (SPEC F21), and that Glose gives to
// an agent it launches (P10.5). It is enough for an
// agent with no other instruction to lead a turn.
const Section = `## Navette Glose

Ce texte est relu avec Glose. L'auteur le lit mis en forme dans son navigateur, le commente et propose des modifications. Vous répondez et proposez les vôtres. **L'auteur seul décide.** Tout s'écrit dans les fichiers Markdown, en CriticMarkup ; l'histoire est gardée par Glose.

Tant qu'une navette est en cours (` + "`glose status`" + ` indique un tour), ces règles l'emportent sur toute autre consigne de ce fichier relative aux annotations : vous ne les appliquez pas, vous ne les retirez pas.

### Mener votre tour

Donnez votre nom à Glose : lancez ses commandes avec ` + "`--agent \"<votre nom>\"`" + ` (par exemple ` + "`--agent \"Claude Code\"`" + `), ou posez une fois la variable ` + "`GLOSE_AGENT`" + `. L'auteur voit ainsi qui l'écoute, qui a pris la main et ce que vous écrivez ; tant que ` + "`glose wait`" + ` tourne, il sait que vous attendez.

1. ` + "`glose status`" + ` dit à qui est la main. Si elle est à l'auteur, attendez : ` + "`glose wait`" + ` rend la main quand l'auteur vous remet son tour (avec ` + "`--timeout 10m`" + ` si votre environnement interrompt les commandes longues ; relancez-la tant qu'elle se termine par « délai écoulé »).
2. ` + "`glose list --json`" + ` donne les annotations. Chacune a un ` + "`id`" + `, un ` + "`kind`" + ` (` + "`comment`" + `, ` + "`highlight`" + `, ` + "`addition`" + `, ` + "`deletion`" + `, ` + "`substitution`" + `), le passage visé (` + "`text`" + `, et ` + "`new`" + ` pour un remplacement), son auteur (` + "`by`" + `), son fil (` + "`messages`" + `) et le paragraphe où elle se trouve, tel qu'il se lit (` + "`context`" + `). Les annotations de l'auteur sont votre travail.
3. Pour chaque annotation, selon ce qu'elle demande :
   - **répondre** dans son fil : ` + "`glose reply --id ID --text \"…\"`" + ` ;
   - **modifier le texte** : éditez directement le fichier avec vos outils habituels, comme si vous écriviez le texte final, hors des marques. Quand vous remettez votre tour, Glose convertit vos modifications en propositions mot à mot, que l'auteur acceptera ou rejettera. Pour ajouter du texte à l'endroit d'un commentaire, écrivez-le juste après le commentaire et son fil ;
   - **corriger un passage commenté** par l'auteur (` + "`{==passage==}{>>remarque<<}`" + `) : réécrivez le texte à l'intérieur de ` + "`{== ==}`" + `, et seulement lui, ou lancez ` + "`glose propose --id ID --new \"nouveau passage\" --note \"…\"`" + `. À la remise, le surlignage devient une proposition qui reprend le fil de l'auteur ;
   - **corriger une proposition de l'auteur** (` + "`{++…++}`" + `, ` + "`{--…--}`" + `, ` + "`{~~…~>…~~}`" + `, quand il vous demande d'en revoir le texte, ou qu'elle contient une erreur) : réécrivez seulement ce qu'elle propose (le texte d'un ajout, ce qui suit ` + "`~>`" + ` dans un remplacement), ou lancez ` + "`glose propose --id ID --new \"texte corrigé\" --note \"pourquoi\"`" + `. Sa proposition reste telle qu'elle est ; votre correction s'ajoute à son fil, et l'auteur peut l'accepter d'un clic. Vos propres propositions encore en attente, corrigez-les de la même façon : elles changent sur place ;
   - **proposer une modification précise avec son explication** : ` + "`glose propose --file F --find \"texte exact\" --new \"nouveau texte\" --note \"pourquoi\"`" + ` (` + "`--new \"\"`" + ` pour supprimer ; ` + "`--after \"texte exact\" --new \"…\"`" + ` pour ajouter après ce texte). Sans autre option, la réécriture devient des propositions mot à mot, que l'auteur décide une à une : c'est le bon choix pour des corrections ponctuelles et indépendantes. Pour une réécriture d'ensemble (une phrase, un paragraphe) qui n'a de sens qu'entière, ajoutez ` + "`--whole`" + ` : elle devient une seule proposition, que l'auteur accepte ou rejette en bloc ;
   - **commenter** un passage : ` + "`glose comment --file F --find \"passage\" --text \"…\"`" + `.
   - **notes de bas de page** : ajoutez l'appel ` + "`[^n]`" + ` et sa définition ` + "`[^n]: …`" + ` (en fin de fichier), ou écrivez ` + "`^[texte de la note]`" + ` dans le ` + "`--new`" + ` de ` + "`glose propose`" + `. Pour supprimer une note, effacez son appel : sa définition suit. L'auteur décide l'appel et la définition d'un seul geste.

   Le texte cité par ` + "`--find`" + ` ou ` + "`--after`" + ` doit figurer une seule fois dans le fichier, hors des annotations, tel qu'il y est écrit.

   Un texte long, sur plusieurs lignes ou avec des guillemets, passe sans échappement par l'entrée standard : donnez ` + "`-`" + ` comme valeur de l'option (une seule par commande ; le dernier saut de ligne est retiré), par exemple ` + "`glose reply --id ID --text - <<'FIN'`" + `, le texte, puis ` + "`FIN`" + `. Avec ` + "`--json`" + `, ` + "`reply`" + `, ` + "`propose`" + `, ` + "`comment`" + ` et ` + "`done`" + ` répondent en JSON (identifiants créés ; pour ` + "`done`" + `, le curseur ` + "`seq`" + ` de ` + "`glose wait --after`" + `).
4. Vérifiez avec ` + "`glose check`" + ` (il annonce ce que ` + "`glose done`" + ` refuserait), puis remettez votre tour : ` + "`glose done --note \"ce que vous avez fait, en une ou deux phrases\"`" + `. Si ` + "`glose done`" + ` refuse, il dit pourquoi et où : corrigez le fichier, puis relancez-le.

### Ce qui est interdit

- Accepter, rejeter ou résoudre une annotation : c'est la décision de l'auteur. Ne supprimez jamais une marque (` + "`{++ ++}`" + `, ` + "`{-- --}`" + `, ` + "`{~~ ~> ~~}`" + `, ` + "`{== ==}`" + `, ` + "`{>> <<}`" + `) et ne modifiez pas le texte à l'intérieur d'une marque existante, sauf le passage d'un surlignage ` + "`{== ==}`" + ` et ce que propose une proposition (voir plus haut).
- Écrire ou modifier un commentaire technique ` + "`{>>glose:v1 …<<}`" + ` : Glose les écrit.
- Écrire dans les fichiers quand la main est à l'auteur.
- Mêler au même endroit une modification libre et une marque écrite à la main : préférez les modifications libres, ou ` + "`glose propose`" + `.
- Modifier un mot collé contre un commentaire existant ` + "`{>>…<<}`" + ` (le commentaire deviendrait une réponse à votre proposition) : laissez au moins un mot inchangé entre les deux. Un ajout, lui, peut s'écrire juste après le commentaire et son fil.
`
