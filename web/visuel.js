// Glose : éditeur mis en forme du dialogue « Suggérer ».
//
// L'auteur écrit dans le texte mis en forme ; le résultat redevient du
// Markdown, que le serveur transforme en propositions (comme en mode Source).
// Pour ne jamais abîmer le Markdown :
//   - seuls des paragraphes s'éditent ainsi (pas de titre, liste, tableau,
//     bloc de code…) ;
//   - ce que l'éditeur ne sait pas réécrire à l'identique devient un « bloc
//     intouchable » (formule, image, code, appel de note, étiquette {#…},
//     échappement, entité, HTML…) qui garde sa source exacte ;
//   - à l'ouverture, la source relue puis réécrite doit être identique à
//     l'originale, octet pour octet ; sinon, le mode n'est pas proposé.
"use strict";
window.GloseVisuel = (() => {
  // ---------- Lecture : Markdown → éléments ----------
  // Un paragraphe est une suite d'éléments : texte, gras, italique, lien, et
  // blocs intouchables. Renvoie null si le texte n'est pas fait de simples
  // paragraphes.
  // Début de ligne qui ouvre un bloc que l'éditeur ne traite pas.
  const STRUCTURE = /^(#{1,6}(\s|$)|[-*+]\s|\d{1,9}[.)]\s|\||```|~~~|:::|\[\^[^\]\s]+\]:|-{3,}\s*$|\*{3,}\s*$|_{3,}\s*$|={2,}\s*$)/;
  // Préfixe de la première ligne d'un paragraphe : marques d'encadré, puis
  // marqueur d'élément de liste ; il devient un bloc intouchable.
  const PREFIXE = /^ {0,3}(?:> ?)*(?: {0,3}(?:[-*+]|\d{1,9}[.)])[ \t]+)?/;

  function blocs(md) {
    // Paragraphes et séparateurs (lignes vides), gardés tels quels.
    const parties = md.split(/(\r?\n[ \t>]*\r?\n(?:[ \t>]*\r?\n)*)/);
    const out = [];
    for (let i = 0; i < parties.length; i++) {
      if (i % 2 === 1) { out.push({ sep: parties[i] }); continue; }
      const p = parties[i];
      const lignes = p.split(/\r?\n/);
      const prefixe = PREFIXE.exec(lignes[0])[0];
      if (STRUCTURE.test(lignes[0].slice(prefixe.length)) || /^ {4}|^\t/.test(lignes[0].slice(prefixe.length))) return null;
      for (const l of lignes.slice(1)) if (STRUCTURE.test(l.replace(/^[ \t>]*/, ""))) return null;
      out.push({ prefixe, texte: p.slice(prefixe.length) });
    }
    return out;
  }

  function atome(md, affichage, classe) {
    const s = document.createElement("span");
    s.className = "v-atome" + (classe ? " " + classe : "");
    s.contentEditable = "false";
    s.dataset.md = md;
    s.textContent = affichage;
    s.dataset.infobulle = md;
    return s;
  }

  // Cherche la fermeture d'une emphase délim (« ** », « * », « __ », « _ »)
  // dans t à partir de i, au même niveau de crochets et hors code.
  function fermeture(t, i, delim) {
    for (let j = i; j < t.length; j++) {
      const c = t[j];
      if (c === "\\") { j++; continue; }
      if (c === "`") {
        const n = /^`+/.exec(t.slice(j))[0].length;
        const k = t.indexOf("`".repeat(n), j + n);
        if (k < 0) return -1;
        j = k + n - 1;
        continue;
      }
      if (t.startsWith(delim, j) && j > i && !/\s/.test(t[j - 1]) &&
        (delim.length === 2 || t[j + 1] !== delim[0]) && (delim.length === 1 || t[j + 2] !== delim[0])) {
        if (delim[0] === "_" && /[\p{L}\p{N}]/u.test(t[j + delim.length] || "")) continue;
        return j;
      }
    }
    return -1;
  }

  // Crochet fermant qui correspond à celui ouvert en i.
  function crochet(t, i) {
    let d = 0;
    for (let j = i; j < t.length; j++) {
      if (t[j] === "\\") { j++; continue; }
      if (t[j] === "[") d++;
      else if (t[j] === "]" && --d === 0) return j;
    }
    return -1;
  }

  // Les textes lus par l'appel de lire en cours, gardés sur sa racine.
  let textesLus = null;

  function enLigne(t, parent) {
    let texte = "";
    // Chaque texte garde la source dont il vient : réécrit, ce qui n'a pas
    // changé reprend ces octets tels quels (voir ecrireTexte).
    const vider = () => {
      if (!texte) return;
      const n = document.createTextNode(texte);
      n.__glose_src = texte;
      if (textesLus) textesLus.push(texte);
      parent.append(n);
      texte = "";
    };
    const ajouter = (n) => { vider(); parent.append(n); };
    for (let i = 0; i < t.length;) {
      const reste = t.slice(i);
      let m;
      if ((m = /^\\(\r?\n)/.exec(reste))) { ajouter(atome(m[0], "↵", "v-saut")); i += m[0].length; continue; }
      if ((m = /^\\[!-/:-@[-`{-~]/.exec(reste))) { ajouter(atome(m[0], m[0][1], "v-echappe")); i += 2; continue; }
      if ((m = /^ {2,}\r?\n/.exec(reste))) { ajouter(atome(m[0], "↵", "v-saut")); i += m[0].length; continue; }
      if ((m = /^\r?\n[ \t>]*/.exec(reste))) { ajouter(atome(m[0], " ", "v-ligne")); i += m[0].length; continue; }
      if (reste[0] === "`") {
        const n = /^`+/.exec(reste)[0].length;
        const k = reste.indexOf("`".repeat(n), n);
        if (k > 0 && reste[k + n] !== "`") {
          const md = reste.slice(0, k + n);
          ajouter(atome(md, md.slice(n, -n).trim(), "v-code"));
          i += md.length;
          continue;
        }
      }
      if ((m = /^\$\$[\s\S]+?\$\$|^\$[^$\s][^$]*?\$/.exec(reste))) { ajouter(atome(m[0], m[0], "v-formule")); i += m[0].length; continue; }
      if ((m = /^!\[[^\]]*\]\([^)]*\)(\{[^}]*\})?/.exec(reste))) { ajouter(atome(m[0], "🖼", "v-image")); i += m[0].length; continue; }
      if ((m = /^\[\^[^\]\s]+\]/.exec(reste))) { ajouter(atome(m[0], m[0].slice(2, -1), "v-appel")); i += m[0].length; continue; }
      if (reste.startsWith("^[")) {
        const k = crochet(reste, 1);
        if (k > 0) { ajouter(atome(reste.slice(0, k + 1), "note", "v-note")); i += k + 1; continue; }
      }
      if ((m = /^\[\]\(#[^)]*\)/.exec(reste))) { ajouter(atome(m[0], "n° …", "v-renvoi")); i += m[0].length; continue; }
      if (reste[0] === "[") {
        const k = crochet(reste, 0);
        const lien = k > 0 && /^\(([^()\s]+)\)/.exec(reste.slice(k + 1));
        if (lien && reste[k + 1 + lien[0].length] !== "{") {
          const a = document.createElement("a");
          a.dataset.href = lien[1];
          a.dataset.infobulle = lien[1];
          enLigne(reste.slice(1, k), a);
          ajouter(a);
          i += k + 1 + lien[0].length;
          continue;
        }
      }
      if ((m = /^\{[^{}\n]*\}/.exec(reste))) { ajouter(atome(m[0], "¶", "v-attribut")); i += m[0].length; continue; }
      if ((m = /^<[^>\n]+>/.exec(reste))) { ajouter(atome(m[0], m[0], "v-html")); i += m[0].length; continue; }
      if ((m = /^&(#\d+|#x[0-9a-fA-F]+|[a-zA-Z]+);/.exec(reste))) {
        const tmp = document.createElement("textarea");
        tmp.innerHTML = m[0];
        ajouter(atome(m[0], tmp.value, "v-entite"));
        i += m[0].length;
        continue;
      }
      let fait = false;
      for (const [delim, tag] of [["**", "strong"], ["__", "strong"], ["*", "em"], ["_", "em"]]) {
        if (!reste.startsWith(delim) || /\s/.test(reste[delim.length] || " ")) continue;
        if (delim[0] === "_" && i > 0 && /[\p{L}\p{N}]/u.test(t[i - 1])) continue;
        const k = fermeture(reste, delim.length, delim);
        if (k < 0) continue;
        const e = document.createElement(tag);
        e.dataset.delim = delim;
        enLigne(reste.slice(delim.length, k), e);
        ajouter(e);
        i += k + delim.length;
        fait = true;
        break;
      }
      if (fait) continue;
      texte += t[i];
      i++;
    }
    vider();
  }

  // Lit md dans racine (vidée). Renvoie false si le texte ne s'y prête pas.
  function lire(md, racine) {
    racine.textContent = "";
    textesLus = racine.__glose_textes = [];
    const bs = blocs(md);
    if (!bs) return false;
    for (const b of bs) {
      if (b.sep !== undefined) { racine.lastChild && (racine.lastChild.dataset.sep = b.sep); continue; }
      const p = document.createElement("p");
      if (b.prefixe) {
        const marque = b.prefixe.replace(/[> \t]/g, "");
        p.append(atome(b.prefixe, marque ? (/\d/.test(marque) ? marque : "•") + " " : "▌ ", "v-prefixe"));
      }
      enLigne(b.texte, p);
      racine.append(p);
    }
    if (!racine.firstChild) racine.append(document.createElement("p"));
    return true;
  }

  // ---------- Écriture : éléments → Markdown ----------
  // Le texte tapé reste du texte (F7) : tout signe qui pourrait former une
  // construction Markdown y est échappé (pandoc lit « \x » comme le signe x).
  // Le texte venu de la source, lui, est repris tel quel : seule la partie
  // changée d'un texte est échappée, ce qui garde l'aller-retour identique.
  function echapperNouveau(s, debutDeLigne) {
    let out = "";
    for (let i = 0; i < s.length; i++) {
      const c = s[i], reste = s.slice(i), avant = i ? s[i - 1] : "";
      let e = "\\`*_[]{}<>~^$|".includes(c);
      if (c === "&") e = /^&(#\d+|#x[0-9a-fA-F]+|[a-zA-Z]+);/.test(reste);
      else if (c === ":") e = /^:[\w+-]+:/.test(reste); // émoji
      else if (c === "@") e = /^@\w/.test(reste) && !/[\p{L}\p{N}]/u.test(avant); // citation
      out += (e ? "\\" : "") + c;
    }
    // Début de ligne : titre, liste, encadré, définition, soulignement.
    if (debutDeLigne) {
      out = out.replace(/^( {0,3})([#+\-=:])/, "$1\\$2").replace(/^( {0,3}\d{1,9})([.)])(?=\s|$)/, "$1\\$2");
    }
    return out;
  }

  // Le Markdown d'un nœud texte de l'éditeur : la partie qui diffère de sa
  // source (tout, pour un texte nouveau) est échappée, le reste repris tel
  // quel. La partie changée s'étend aux mots qu'elle touche : un signe tapé
  // contre un texte gardé (« [a] » puis « (b) ») ne forme pas de construction.
  // Un nœud que le navigateur a recréé (mot mis en gras, coupure) a perdu sa
  // source : on la retrouve parmi les textes lus à l'ouverture.
  function ecrireTexte(c, opt, debutDeLigne) {
    let s = c.nodeValue;
    if (!opt.nbsp) s = s.replace(/\u00a0/g, " ");
    let src = c.__glose_src;
    // Un nœud recréé, en début de ligne, dont le début formerait un titre ou
    // une liste : son premier mot est toujours traité comme saisi.
    const debutRisque = src === undefined && debutDeLigne && echapperNouveau(s, true) !== echapperNouveau(s, false);
    if (src === undefined && s) {
      const textes = opt.textes || [];
      // Un morceau de la source, inchangé, repris tel quel.
      // Un morceau de la source est du texte littéral là où il était ; ce
      // qui pourrait changer, c'est le début de ligne (debutRisque) et ses
      // voisins, tapés, donc échappés. Vaut aussi pour un paragraphe coupé
      // par Entrée, dont la seconde moitié est un nœud recréé.
      // Ses deux bords doivent être des caractères ordinaires : un signe au
      // bord (« _bar_ » pris dans « foo_bar_baz ») formerait une construction
      // avec ses nouveaux voisins ; à l'intérieur, chaque signe garde les
      // siens.
      const ordinaire = (c) => echapperNouveau(c, false) === c;
      if (!debutRisque && ordinaire(s[0]) && ordinaire(s[s.length - 1]) && textes.some((t) => t.includes(s))) return s;
      let mieux = 0;
      for (const t of textes) {
        const n = communs(s, t);
        if (n[0] + n[1] > mieux) { mieux = n[0] + n[1]; src = t; }
      }
    }
    if (src === undefined) return echapperNouveau(s, debutDeLigne);
    if (s === src && !debutRisque) return s;
    let [p, q] = communs(s, src);
    if (debutRisque) p = 0;
    if (p + q > s.length) q = s.length - p;
    // Jusqu'aux espaces qui bordent la partie changée.
    while (p > 0 && !/\s/.test(s[p - 1])) p--;
    while (q > 0 && !/\s/.test(s[s.length - q])) q--;
    return s.slice(0, p) + echapperNouveau(s.slice(p, s.length - q), debutDeLigne && p === 0) + s.slice(s.length - q);
  }

  // Longueurs du début et de la fin communs à s et t (sans chevauchement).
  function communs(s, t) {
    let p = 0;
    while (p < s.length && p < t.length && s[p] === t[p]) p++;
    let q = 0;
    while (q < s.length - p && q < t.length - p && s[s.length - 1 - q] === t[t.length - 1 - q]) q++;
    return [p, q];
  }

  // Un texte commence une ligne s'il ouvre le paragraphe (après son préfixe)
  // ou suit un saut de ligne.
  function debutDeLigne(c) {
    let x = c.previousSibling;
    while (x && x.nodeType === Node.TEXT_NODE && x.nodeValue === "") x = x.previousSibling;
    if (!x) return true;
    return x.nodeType === Node.ELEMENT_NODE && (x.tagName === "P" || x.tagName === "DIV" ||
      x.classList.contains("v-prefixe") || x.classList.contains("v-ligne") || x.classList.contains("v-saut"));
  }

  function ecrireEnLigne(n, opt, dedans) {
    let out = "";
    for (const c of n.childNodes) {
      if (c.nodeType === Node.TEXT_NODE) {
        out += ecrireTexte(c, opt, !dedans && debutDeLigne(c));
      } else if (c.nodeType === Node.ELEMENT_NODE) {
        if (c.dataset && c.dataset.md !== undefined) { out += c.dataset.md; continue; }
        const tag = c.tagName;
        const dedans = ecrireEnLigne(c, opt, true);
        if (!dedans.trim()) { out += dedans; continue; }
        if (tag === "STRONG" || tag === "B") out += emphase(dedans, c.dataset.delim || opt.gras);
        else if (tag === "EM" || tag === "I") out += emphase(dedans, c.dataset.delim || opt.italique);
        else if (tag === "A") out += "[" + dedans + "](" + (c.dataset.href || c.getAttribute("href") || "") + ")";
        else if (tag === "BR") out += "\\\n";
        else out += dedans;
      }
    }
    return out;
  }

  // Une emphase ne peut ni commencer ni finir par une espace : les espaces
  // passent à l'extérieur.
  function emphase(s, delim) {
    const m = /^(\s*)([\s\S]*?)(\s*)$/.exec(s);
    return m[1] + delim + m[2] + delim + m[3];
  }

  function ecrire(racine, opt) {
    opt = Object.assign({}, opt, { textes: racine.__glose_textes || [] });
    // Paragraphes de l'éditeur ; un texte tapé hors de tout paragraphe (dans
    // un éditeur ouvert vide, le navigateur n'en crée pas toujours) forme un
    // paragraphe à lui seul.
    const ps = [];
    let libre = null;
    for (const n of racine.childNodes) {
      if (n.nodeType === Node.ELEMENT_NODE && (n.tagName === "P" || n.tagName === "DIV")) {
        libre = null;
        ps.push(n);
      } else {
        if (!libre) { libre = { childNodes: [], dataset: {} }; ps.push(libre); }
        libre.childNodes.push(n);
      }
    }
    const textes = [];
    for (const p of ps) {
      const t = ecrireEnLigne(p, opt);
      if (p.tagName || t !== "") textes.push([t, p.dataset.sep || opt.sep]);
    }
    let out = "";
    textes.forEach(([t, sep], k) => { out += t; if (k < textes.length - 1) out += sep; });
    return out;
  }

  // Options d'écriture tirées de l'original : marques d'emphase en usage,
  // fin de ligne, espaces insécables.
  function options(md) {
    return {
      gras: /__\S/.test(md) && !/\*\*\S/.test(md) ? "__" : "**",
      italique: /(^|[^_\w])_\S/.test(md) && !/(^|[^*])\*[^*\s]/.test(md) ? "_" : "*",
      sep: md.includes("\r\n") ? "\r\n\r\n" : "\n\n",
      nbsp: md.includes(" "),
    };
  }

  // Ouvre md dans racine si la lecture puis l'écriture le redonnent à
  // l'identique. Renvoie les options, ou null.
  function ouvrir(md, racine) {
    if (!lire(md, racine)) return null;
    const opt = options(md);
    return ecrire(racine, opt) === md ? opt : null;
  }

  return { ouvrir, ecrire, atome, lire };
})();
