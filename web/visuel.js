// Glose: formatted editor for the "Suggérer" dialog.
//
// The author writes in formatted text; the result becomes Markdown
// again, which the server turns into proposals (as in Source mode).
// To never damage Markdown:
//   - only paragraphs can be edited this way (no heading, list, table,
//     code block…);
//   - anything the editor cannot rewrite identically becomes an "untouchable
//     block" (formula, image, code, footnote reference, {#…} label,
//     escape, entity, HTML…) retaining its exact source;
//   - on opening, parsing and rewriting the source must yield the original,
//     byte for byte; otherwise, the mode is not offered.
"use strict";
window.GloseVisuel = (() => {
  // ---------- Reading: Markdown → elements ----------
  // A paragraph is a sequence of elements: text, bold, italic, link, and
  // untouchable blocks. Return null if the text does not consist of simple
  // paragraphs.
  // Line start opening a block the editor does not handle.
  const STRUCTURE = /^(#{1,6}(\s|$)|[-*+]\s|\d{1,9}[.)]\s|\||```|~~~|:::|\[\^[^\]\s]+\]:|-{3,}\s*$|\*{3,}\s*$|_{3,}\s*$|={2,}\s*$)/;
  // Prefix of a paragraph's first line: blockquote markers, then
  // a list item marker; it becomes an untouchable block.
  const PREFIXE = /^ {0,3}(?:> ?)*(?: {0,3}(?:[-*+]|\d{1,9}[.)])[ \t]+)?/;

  function blocs(md) {
    // Paragraphs and separators (blank lines), kept as is.
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

  // Find the closing emphasis delimiter delim ("**", "*", "__", "_")
  // in t starting at i, at the same bracket depth and outside code.
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

  // Closing bracket matching the one opened at i.
  function crochet(t, i) {
    let d = 0;
    for (let j = i; j < t.length; j++) {
      if (t[j] === "\\") { j++; continue; }
      if (t[j] === "[") d++;
      else if (t[j] === "]" && --d === 0) return j;
    }
    return -1;
  }

  // Texts read by the current lire call, kept on its root.
  let textesLus = null;

  function enLigne(t, parent) {
    let texte = "";
    // Each text retains its source: when rewritten, unchanged parts
    // reuse those bytes as is (see ecrireTexte).
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

  // Read md into racine (emptied). Return false if the text is unsuitable.
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

  // ---------- Writing: elements → Markdown ----------
  // Typed text stays text (F7): any character that could form a Markdown
  // construct is escaped (pandoc reads "\x" as the character x).
  // Source text is reused as is: only the changed portion
  // of a text is escaped, preserving an identical round trip.
  function echapperNouveau(s, debutDeLigne) {
    let out = "";
    for (let i = 0; i < s.length; i++) {
      const c = s[i], reste = s.slice(i), avant = i ? s[i - 1] : "";
      let e = "\\`*_[]{}<>~^$|".includes(c);
      if (c === "&") e = /^&(#\d+|#x[0-9a-fA-F]+|[a-zA-Z]+);/.test(reste);
      else if (c === ":") e = /^:[\w+-]+:/.test(reste); // emoji
      else if (c === "@") e = /^@\w/.test(reste) && !/[\p{L}\p{N}]/u.test(avant); // citation
      out += (e ? "\\" : "") + c;
    }
    // Line start: heading, list, blockquote, definition, underline.
    if (debutDeLigne) {
      out = out.replace(/^( {0,3})([#+\-=:])/, "$1\\$2").replace(/^( {0,3}\d{1,9})([.)])(?=\s|$)/, "$1\\$2");
    }
    return out;
  }

  // Markdown for an editor text node: the portion differing from its
  // source (all of it for new text) is escaped; the rest is reused as
  // is. The changed portion extends to the words it touches: a character typed
  // next to retained text ("[a]" then "(b)") does not form a construct.
  // A node recreated by the browser (word made bold, split) has lost its
  // source: recover it among the texts read on opening.
  function ecrireTexte(c, opt, debutDeLigne) {
    let s = c.nodeValue;
    if (!opt.nbsp) s = s.replace(/\u00a0/g, " ");
    let src = c.__glose_src;
    // A recreated node at a line start whose beginning would form a heading
    // or list: its first word is always treated as typed input.
    const debutRisque = src === undefined && debutDeLigne && echapperNouveau(s, true) !== echapperNouveau(s, false);
    if (src === undefined && s) {
      const textes = opt.textes || [];
      // An unchanged source fragment, reused as is.
      // A source fragment is literal text where it was; what might
      // change is the line start (debutRisque) and its neighbours,
      // typed and therefore escaped. Also applies to a paragraph split
      // by Enter, whose second half is a recreated node.
      // Both edges must be ordinary characters: a character at an edge
      // ("_bar_" taken from "foo_bar_baz") would form a construct
      // with its new neighbours; inside, each character keeps
      // its own neighbours.
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
    // Include spaces bordering the changed portion.
    while (p > 0 && !/\s/.test(s[p - 1])) p--;
    while (q > 0 && !/\s/.test(s[s.length - q])) q--;
    return s.slice(0, p) + echapperNouveau(s.slice(p, s.length - q), debutDeLigne && p === 0) + s.slice(s.length - q);
  }

  // Lengths of the common prefix and suffix of s and t (without overlap).
  function communs(s, t) {
    let p = 0;
    while (p < s.length && p < t.length && s[p] === t[p]) p++;
    let q = 0;
    while (q < s.length - p && q < t.length - p && s[s.length - 1 - q] === t[t.length - 1 - q]) q++;
    return [p, q];
  }

  // Text starts a line if it opens the paragraph (after its prefix)
  // or follows a line break.
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

  // Emphasis cannot start or end with a space: spaces
  // move outside.
  function emphase(s, delim) {
    const m = /^(\s*)([\s\S]*?)(\s*)$/.exec(s);
    return m[1] + delim + m[2] + delim + m[3];
  }

  function ecrire(racine, opt) {
    opt = Object.assign({}, opt, { textes: racine.__glose_textes || [] });
    // Editor paragraphs; text typed outside any paragraph (in an editor
    // opened empty, the browser does not always create one) forms a
    // paragraph of its own.
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

  // Writing options taken from the original: emphasis markers in use,
  // line ending, non-breaking spaces.
  function options(md) {
    return {
      gras: /__\S/.test(md) && !/\*\*\S/.test(md) ? "__" : "**",
      italique: /(^|[^_\w])_\S/.test(md) && !/(^|[^*])\*[^*\s]/.test(md) ? "_" : "*",
      sep: md.includes("\r\n") ? "\r\n\r\n" : "\n\n",
      nbsp: md.includes(" "),
    };
  }

  // Open md in racine if parsing then writing reproduces it
  // identically. Return options, or null.
  function ouvrir(md, racine) {
    if (!lire(md, racine)) return null;
    const opt = options(md);
    return ecrire(racine, opt) === md ? opt : null;
  }

  return { ouvrir, ecrire, atome, lire };
})();
