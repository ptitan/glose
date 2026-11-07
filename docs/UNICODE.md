# Entretien des règles Unicode

Glose écrit lui-même deux mécanismes Unicode, sans dépendance. Ce document dit d'où viennent leurs données, quand et comment les mettre à jour, et ce qu'ils ne font pas.

## Les deux mécanismes

| | Normalisation NFC (`internal/sourcepos`) | Graphèmes (`internal/grapheme`) |
|---|---|---|
| Rôle | Reproduire la composition que pandoc applique au texte, pour que ses colonnes retombent sur les bons octets (D-29) | Ne jamais couper ce que le lecteur voit comme un seul caractère (sélection, propositions) |
| Données | `nfc_tables.go`, **généré** par `tools/gen-nfc/gen.py` depuis le module `unicodedata` de Python | les tables du paquet `unicode` de Go, plus quelques plages écrites dans le code |
| Version d'Unicode | **15.0.0** (`nfcUnicodeVersion`) | celle de Go : **17.0.0** avec Go 1.27 (`unicode.Version`) |
| Si un caractère est inconnu | la suite de la ligne est **inexacte** : annotable au paragraphe seulement, jamais devinée | `unicode` le classe selon sa propre version |
| Erreurs possibles | aucune position fausse : un doute rend la position inexacte | dans les deux sens : des frontières refusées à tort (après U+200D), et quelques coupures acceptées à tort à l'intérieur d'un graphème (voir plus bas) |

Les deux versions diffèrent. Pour les positions, c'est sans danger : un caractère ajouté après Unicode 15 rend sa ligne inexacte, jamais fausse (test `TestCharacterNewerThanTheTablesIsNotGuessed`). Pour les graphèmes, le découpage n'est pas garanti dans les écritures qu'il traite mal (ci-dessous) : une sélection ou une proposition peut y couper un caractère composé. Le texte n'est pas abîmé pour autant (l'octet coupé reste celui du fichier, et le serveur refuse une borne au milieu d'un caractère UTF-8), mais la proposition peut être mal placée à l'œil.

## Ce que la normalisation NFC ne garantit pas

- **La version d'Unicode de pandoc n'est pas connue de Glose.** Les tables suivent celle de Python. Le garde-fou est `TestCorpusAgainstPandoc` (`internal/sourcepos/corpus_test.go`), qui compare les positions de Glose à celles de pandoc sur le corpus de `testdata/`. Seule pandoc 3.1.3 a été essayée.
- Les caractères non attribués dans les tables coupent la ligne en inexact, même si pandoc les connaît.

## Ce que le découpage en graphèmes ne fait pas

Règles de UAX #29 simplifiées (`internal/grapheme/grapheme.go`) :
- **non traités** (coupures acceptées à tort) : les marques préposées de quelques écritures (GB9b), les conjointes indiennes (GB9c, Unicode 15.1) ; une coupure entre virama et consonne peut donc être acceptée ;
- **plus strict que la norme** (frontières refusées à tort) : aucune coupure après U+200D (la norme ne l'interdit qu'entre pictogrammes) ;
- traités : accents et marques combinantes, sélecteurs de variante, modificateurs de couleur de peau, étiquettes (drapeaux de subdivisions), paires d'indicateurs régionaux (drapeaux), syllabes hangeul en jamos, CR LF.

## Quand mettre à jour

| Événement | À faire |
|---|---|
| Nouvelle version de pandoc | `go test ./internal/sourcepos -run Corpus -count=1` ; si des positions changent, étudier avant tout (`go run ./tools/sourcepos-study`) |
| Pandoc passe à un Unicode qui ajoute des compositions ou des caractères courants dans les textes relus | régénérer les tables NFC (ci-dessous) avec un Python de même version d'Unicode |
| Nouvelle version de Go | rien d'obligatoire ; `unicode.Version` change, les graphèmes suivent. Relancer `go test ./...` |
| Modification de `gen.py` | régénérer, puis `TestNFCTablesMatchTheGenerator` doit passer |

Ne pas régénérer les tables pour la seule raison qu'une version plus récente de Python existe : ce sont les compositions de **pandoc** qu'il faut reproduire.

## Régénérer les tables NFC

```bash
python3 -c "import unicodedata; print(unicodedata.unidata_version)"   # la version visée
python3 -I tools/gen-nfc/gen.py | gofmt > internal/sourcepos/nfc_tables.go
python3 -I tools/gen-nfc/gen.py --vectors > internal/sourcepos/testdata/nfc.txt
go test ./internal/sourcepos -count=1      # vecteurs, découpe, corpus contre pandoc
go test ./internal/sourcepos -run '^$' -fuzz FuzzNFCPieces -fuzztime 2m   # la découpe par morceaux
```

Puis, sur un ouvrage réel et sur un Markdown ordinaire : `glose render` et vérifier que la numérotation et les positions n'ont pas bougé. Consigner la nouvelle version dans D-29.

## Tests qui protègent ces règles

- `TestNFCAgainstPython` : 3 000 vecteurs générés par Python.
- `TestNFCBoundary` et `FuzzNFCPieces` : découper puis normaliser par morceaux donne la même chose que normaliser d'un coup.
- `TestNFCTablesMatchTheGenerator` : les fichiers commités sont exactement la sortie du générateur (sauté si le Python de la machine a une autre version d'Unicode que les tables, ou s'il est absent).
- `TestCharacterNewerThanTheTablesIsNotGuessed` : un caractère plus récent que les tables n'est pas deviné.
- `TestCorpusAgainstPandoc` : positions de Glose contre celles de pandoc.
- `TestClusters` et `FuzzBoundary` (`internal/grapheme`) : cas de graphèmes, y compris emoji, drapeaux, jamos.
