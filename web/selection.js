// Glose: from a selection in the browser (UTF-16 positions in text nodes) to
// bytes of the source (P8.2). This is the fragile part of the interface: the
// redesign does not touch it. Tests without a browser:
// node --test scripts/selection.test.cjs
"use strict";
globalThis.GloseSelection = (() => {
  const TEXT_NODE = 3, ELEMENT_NODE = 1;
  const enc = new TextEncoder();

  // Bound inside the word g: its byte in the source. « ajuste » says that the
  // bound moved: the excerpt shown is then computed again, nothing is widened
  // silently.
  function borneDansMot(g, t, offset, debut) {
    if (offset <= 0) return { f: g.f, o: g.s, ajuste: false };
    if (offset >= t.length) return { f: g.f, o: g.e, ajuste: false };
    if (g.t) return { f: g.f, o: debut ? g.s : g.e, ajuste: true }; // a transformed word: its bounds only
    // In the middle of a surrogate pair (emoji): at the edge of the character,
    // outwards; ajusterGrapheme does the rest.
    const c = t.charCodeAt(offset - 1);
    const milieu = c >= 0xd800 && c <= 0xdbff;
    if (milieu) offset += debut ? -1 : 1;
    return { f: g.f, o: g.s + enc.encode(t.slice(0, offset)).length, ajuste: milieu };
  }

  // Brings a bound (byte o of a file whose words are mots) back to the edge of
  // the character as the reader sees it: an emoji and its modifiers, a flag, a
  // letter and its accent are not cut (pandoc sometimes makes several words of
  // them). Outwards: backwards for a start, forwards for an end.
  const segmenteur = globalThis.Intl && Intl.Segmenter ? new Intl.Segmenter("fr", { granularity: "grapheme" }) : null;
  function ajusterGrapheme(mots, o, debut, premierApres) {
    if (!segmenteur) return o;
    let k = premierApres(mots, o);
    if (k > 0 && mots[k - 1].e > o) k--; // the word that holds o
    // The contiguous words around o, without a transformed word (its bytes differ from its text).
    let a = Math.max(0, k - 1), b = Math.min(mots.length - 1, k);
    while (a > 0 && mots[a - 1].e === mots[a].s && k - a < 6) a--;
    while (b + 1 < mots.length && mots[b].e === mots[b + 1].s && b - k < 6) b++;
    let texte = "", octet = mots[a] ? mots[a].s : o;
    const bornes = new Map(); // UTF-16 index → byte
    for (let j = a; j <= b; j++) {
      if (j > a && mots[j - 1].e !== mots[j].s) { texte = ""; bornes.clear(); octet = mots[j].s; }
      if (/,t$/.test(mots[j].el.dataset.g)) return o;
      for (const c of mots[j].el.textContent) { bornes.set(texte.length, octet); texte += c; octet += enc.encode(c).length; }
      bornes.set(texte.length, octet);
    }
    const debuts = new Set([...segmenteur.segment(texte)].map((x) => bornes.get(x.index)));
    debuts.add(bornes.get(texte.length));
    if (debuts.has(o) || ![...bornes.values()].includes(o)) return o;
    const liste = [...debuts].filter((x) => x !== undefined).sort((x, y) => x - y);
    if (debut) return liste.filter((x) => x <= o).pop() ?? o;
    return liste.find((x) => x >= o) ?? o;
  }

  // Dependencies, read when used since the interface renders again:
  //   zone(): the element of the rendered text; spans(): its words, in order;
  //   mots(f): the words of file f, by byte; blocs: selector of a block;
  //   texteSource(f, s, e), court(t, n), avis(message, erreur), lireG, premierApres;
  //   selection(), creerRange(): the browser's, replaced by the tests.
  function creer(d) {
    const selection = d.selection || (() => window.getSelection());
    const creerRange = d.creerRange || (() => document.createRange());

    function borne(noeud, offset, debut) {
      const parent = noeud.nodeType === TEXT_NODE ? noeud.parentElement : null;
      if (parent && parent.dataset.g !== undefined) {
        const g = d.lireG(parent.dataset.g);
        if (g) return borneDansMot(g, noeud.data, offset, debut);
      }
      // Elsewhere (a number, a computed reference, a space without a
      // position): the nearest known word, inwards.
      const r = creerRange();
      r.setStart(noeud, offset);
      const spans = d.spans();
      const n = spans.length;
      let lo = 0, hi = n;
      if (debut) {
        while (lo < hi) { const m = (lo + hi) >> 1; if (r.comparePoint(spans[m], 0) >= 0) hi = m; else lo = m + 1; }
        if (lo >= n) return null;
        const g = d.lireG(spans[lo].dataset.g);
        return g && { f: g.f, o: g.s, ajuste: true, span: spans[lo] };
      }
      while (lo < hi) {
        const m = (lo + hi) >> 1;
        const sp = spans[m];
        if (r.comparePoint(sp, sp.childNodes.length) <= 0) lo = m + 1; else hi = m;
      }
      if (lo === 0) return null;
      const g = d.lireG(spans[lo - 1].dataset.g);
      return g && { f: g.f, o: g.e, ajuste: true, span: spans[lo - 1] };
    }

    function selectionDuTexte() {
      const sel = selection();
      if (!sel || !sel.rangeCount || sel.isCollapsed) return null;
      const r = sel.getRangeAt(0);
      return d.zone().contains(r.commonAncestorContainer) ? r : null;
    }

    function blocDe(noeud) {
      const e = noeud.nodeType === ELEMENT_NODE ? noeud : noeud.parentElement;
      return e && e.closest(d.blocs);
    }

    // The current selection in bytes of the source; null, with a notice, if it
    // matches no text of the source.
    function lireSelection() {
      const r = selectionDuTexte();
      if (!r) return null;
      const debut = borne(r.startContainer, r.startOffset, true);
      let fin = borne(r.endContainer, r.endOffset, false);
      // Triple click: the selection runs up to the start of the next block;
      // it is brought back to the end of the block it starts in.
      const blocDebut = blocDe(r.startContainer);
      if (fin && blocDebut && blocDe(r.endContainer) !== blocDebut && r.endOffset === 0) {
        const mots = blocDebut.querySelectorAll("span[data-g]");
        const g = mots.length && d.lireG(mots[mots.length - 1].dataset.g);
        if (g) fin = { f: g.f, o: g.e, ajuste: true };
      }
      for (const [x, estDebut] of [[debut, true], [fin, false]]) {
        if (!x) continue;
        const o = ajusterGrapheme(d.mots(x.f), x.o, estDebut, d.premierApres);
        if (o !== x.o) { x.o = o; x.ajuste = true; }
      }
      if (!debut || !fin || (debut.f === fin.f && debut.o >= fin.o)) {
        d.avis("Ce passage ne correspond à aucun texte de la source (numéro, renvoi calculé…). Utilisez la poignée « + » de la marge.", true);
        return null;
      }
      if (debut.f !== fin.f) { d.avis("La sélection s'étend sur deux fichiers : sélectionnez un passage plus court.", true); return null; }
      const ajuste = debut.ajuste || fin.ajuste;
      const extrait = d.court(ajuste ? d.texteSource(debut.f, debut.o, fin.o) || r.toString() : r.toString(), 400);
      return { f: debut.f, start: debut.o, end: fin.o, extrait, ajuste };
    }

    return { borne, selectionDuTexte, blocDe, lireSelection };
  }

  return { creer, borneDansMot, ajusterGrapheme };
})();
