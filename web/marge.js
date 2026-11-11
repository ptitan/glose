/* Annotation cards and collapsed-height layout. Writing stays in app.js. */
"use strict";
window.GloseMarge = (() => {
  function creer({ $, S, el, icone, md, riche, court, modeleCourt, NOMS, PROPOSITION, QUI, activer,
    modifiable, outilsMessage, teteModifiable, modifier, retirer, resoudre,
    corrigeable, corriger, repondre, decider, appliquerCorrection }) {
    function flecheRemplace() {
      const f = el("span", "fleche-remplace");
      f.innerHTML = icone("fleche");
      f.setAttribute("aria-label", "remplacé par");
      return f;
    }

    function destinataire(nom) {
      const d = el("span", "pour");
      d.innerHTML = icone("fleche");
      d.append(document.createTextNode(nom));
      d.dataset.infobulle = "Réponse demandée à " + nom;
      d.setAttribute("aria-label", "Réponse demandée à " + nom);
      return d;
    }

    // Who speaks, the same way everywhere: the author and the AI never look alike.
    function ton(message) {
      return message.by === "author" ? "auteur" : message.by === "agent" ? "agent" : "inconnu";
    }

    function signature(message) {
      const nom = message.via === "Typo" ? "Typo" : message.by === "author" ? "Auteur" : message.via || QUI[message.by] || "";
      const q = el("span", "qui " + ton(message));
      if (message.by === "author" || message.by === "agent") q.innerHTML = icone(message.by === "author" ? "vous" : "agent");
      q.append(document.createTextNode(nom + (message.model ? " · " + modeleCourt(message.model) : "")));
      if (message.model) q.dataset.infobulle = nom + " : " + message.model;
      return q;
    }

    function mission(id) {
      const t = S.doc.taskLabels?.[id] || S.doc.tasks?.find(t => t.id === id);
      const badge = el("span", "mission");
      badge.innerHTML = icone(t?.icon || "agent");
      badge.append(document.createTextNode(t?.name || id));
      badge.dataset.task = id;
      return badge;
    }

    function moment(annotation) {
      const tour = annotation.turn ? Math.floor((annotation.turn + 1) / 2) : 0;
      return el("span", "quand", annotation.status === "new" ? "de ce tour" : tour ? "remis au tour " + tour : "remis");
    }

    function nomAnnotation(a) {
      return a.primary && a.note ? (a.kind === "deletion" ? "Note supprimée" : a.kind === "addition" ? "Note ajoutée" : "Note modifiée") : NOMS[a.kind];
    }

    function fabriquerCarte(a, i) {
      const c = el("div", "carte" + (PROPOSITION[a.kind] ? " prop" : ""));
      c.tabIndex = 0;
      c.setAttribute("role", "button");
      const lieu = S.doc.files[a.file] ? S.doc.files[a.file].path : "?";
      c.setAttribute("aria-label", "Annotation " + (i + 1) + ", " + NOMS[a.kind] + ", " + lieu + " ligne " + a.line);
      const tete = el("div", "tete");
      const nom = nomAnnotation(a);
      const nature = el("span", "nature");
      nature.innerHTML = icone(PROPOSITION[a.kind] ? "suggerer" : "commenter");
      tete.append(el("span", "num", String(i + 1)), nature, el("strong", "", nom));
      tete.dataset.infobulle = lieu + ", ligne " + a.line;
      const origine = PROPOSITION[a.kind] ? a : (a.messages.find((m) => !m.reply && !m.fix) || a);
      // A proposal has no message of its own: its author is named here. A
      // comment speaks through its first message, below.
      const meta = el("span", "meta");
      if (PROPOSITION[a.kind] || !a.messages.length) meta.append(signature(origine));
      meta.append(moment(a));
      if (a.task) meta.append(mission(a.task));
      if (a.to) meta.append(destinataire(a.to));
      meta.append(el("span", "lieu", "l. " + a.line));
      c.append(tete, meta);
      const ex = el("div", "extrait");
      if (a.kind === "substitution") {
        ex.append(md("del", "", court(a.text, 120)), flecheRemplace(), md("ins", "", court(a.new, 120)));
      } else if (a.kind === "addition") {
        ex.append(md("ins", "", court(a.text, 160)));
      } else if (a.kind === "deletion") {
        ex.append(md("del", "", court(a.text, 160)));
      } else if (a.kind === "highlight") {
        ex.textContent = "« " + court(a.text, 160) + " »";
      }
      if (ex.childNodes.length && !(a.primary && a.note && a.kind !== "substitution")) c.append(ex);
      if (a.primary && a.note) {
        const n = el("div", "extrait note");
        n.append(el("span", "qui", "Note"), document.createTextNode(court(a.note, 300)));
        c.append(n);
      }
      for (const m of a.messages) {
        if (m.fix) { c.append(blocCorrection(a, m)); continue; }
        const p = el("div", "msg " + ton(m));
        const tete = el("div", "msg-tete");
        tete.append(signature(m));
        if (m.task) tete.append(mission(m.task));
        if (m !== origine || m.reply) tete.append(moment(m));
        if (m.by === "author" && m.to && m.reply) tete.append(destinataire(m.to));
        const corps = riche("div", "msg-texte", m.text);
        if (m.reply && modifiable(m)) {
          // Unsubmitted replies keep their inline editing controls.
          p.classList.add("a-vous");
          corps.append(outilsMessage(a, m, i));
        }
        p.append(tete, corps);
        c.append(p);
      }
      const outils = el("div", "outils");
      const bouton = (libelle, titre, classe, action, bulle) => {
        const b = el("button", "icone " + classe);
        b.innerHTML = icone(classe); // Fixed icon name, never document content.
        b.append(el("span", "", libelle));
        b.type = "button";
        b.dataset.infobulle = bulle || libelle;
        b.setAttribute("aria-description", titre);
        b.setAttribute("aria-label", libelle);
        b.addEventListener("click", (e) => { e.stopPropagation(); action(); });
        outils.append(b);
      };
      bouton("Répondre", "Répondre dans le fil de cette annotation", "repondre", () => repondre(a, i), "Répondre (touche R)");
      if (a.kind === "comment" || a.kind === "highlight") {
        const tete = teteModifiable(a);
        if (tete) {
          bouton("Modifier", "Modifier votre commentaire, qui n'a pas encore été remis à l'agent", "corriger", () => modifier(a, tete, i), "Modifier (touche E)");
          outils.lastChild.classList.add("neutre"); // Keep comment editing neutral.
        }
        if (a.status === "new") bouton("Retirer", "Retirer cette annotation, qui n'a pas encore été remise à l'agent", "retirer", () => retirer(a));
        else bouton("Résoudre", "Clore la discussion : les marques disparaissent du fichier, le fil est gardé au journal", "resoudre", () => resoudre(a));
      } else {
        if (corrigeable(a)) bouton("Modifier", "Modifier votre proposition : elle reste une seule proposition, avec son fil", "corriger", () => corriger(a, i), "Modifier (touche E)");
        // Decisions apply to the card proposal, never to a reply.
        const de = a.by === "author" ? "votre proposition" : a.by === "agent" ? "la proposition de l'agent" : "la proposition";
        // Summarize the size of long proposals in decision tooltips.
        const q = (t) => {
          const plat = t.replace(/\s+/g, " ").trim();
          if (plat.length <= 60) return "« " + plat + " »";
          const mots = plat.split(" ").length;
          const phrases = (plat.match(/[.!?…](\s|$)/g) || []).length;
          return "« " + court(plat, 45) + " » (" + (phrases > 1 ? phrases + " phrases, " : "") + mots + " mots)";
        };
        const accepte = a.kind === "substitution" ? "Le texte devient " + q(a.new) : a.kind === "addition" ? "Ajoute " + q(a.text) : "Supprime " + q(a.text);
        const rejete = a.kind === "addition" ? "N'ajoute rien" : "Garde " + q(a.text);
        bouton("Rejeter", "Rejeter " + de + " : le texte d'origine reste", "rejeter", () => decider(a, "reject"), "Rejeter " + de + " (touche X)\n" + rejete);
        bouton("Accepter", "Accepter " + de + " : le texte proposé remplace l'original", "accepter", () => decider(a, "accept"), "Accepter " + de + " (touche A)\n" + accepte);
      }
      // Accept first, then reject, then the others: what the author does most.
      for (const nom of ["rejeter", "accepter"]) {
        const b = outils.querySelector(":scope > button." + nom);
        if (b) outils.prepend(b);
      }
      c.append(outils);
      // A click on a card already unfolded changes nothing: the author is reading
      // it, the page must not go back to its passage. A first click scrolls only if
      // the passage is out of sight. Focus comes before the click, hence mousedown.
      let dejaActive = false;
      c.addEventListener("mousedown", () => { dejaActive = S.actif === i; });
      c.addEventListener("click", () => {
        const rester = dejaActive && S.actif === i;
        dejaActive = false;
        if (!rester) activer(i, !passageAffiche(i));
      });
      // Focus expands the card before any clipped button can be reached.
      c.addEventListener("focusin", () => { if (S.actif !== i) { activer(i, false); c.scrollTop = 0; } });
      c.addEventListener("keydown", (e) => {
        if (e.target === c && (e.key === "Enter" || e.key === " ")) { e.preventDefault(); activer(i, true); }
      });
      return c;
    }

    function passageAffiche(i) {
      const b = S.cartes[i] && S.cartes[i].badge;
      if (!b) return true;
      const r = b.getBoundingClientRect();
      return r.top >= $("barre").getBoundingClientRect().bottom && r.bottom <= $("pied").getBoundingClientRect().top;
    }

    // A card stays unfolded only while the reader is on it. The mouse: a press
    // anywhere else folds it, except on what acts on a card (its mark in the text,
    // its line in the list) and while an input panel is open, which belongs to it.
    // The keyboard: the focus going elsewhere folds it. Leaving the window does not.
    function brancherRepli(desactiver) {
      let souris = false;
      for (const type of ["mouseup", "keydown"]) document.addEventListener(type, () => { souris = false; }, true);
      document.addEventListener("mousedown", (e) => {
        souris = true;
        if (e.button !== 0 || S.actif < 0 || document.querySelector("dialog[open]")) return;
        const racine = document.documentElement;
        if (e.target === racine && e.clientX >= racine.clientWidth) return; // the scroll bar
        if (e.target.closest(".carte, .g-ancre, a[data-carte], .infobulle")) return;
        desactiver();
      }, true);
      $("marge").addEventListener("focusout", (e) => {
        const vers = e.relatedTarget;
        if (souris || S.actif < 0 || !vers || vers.closest(".carte, dialog")) return;
        desactiver();
      });
    }

    // Read all geometry before writing positions. Always reserve the collapsed
    // height (also in style.css), so expanding only overlays neighbouring cards.
    const CARTE_MAX = 216;
    function disposerMaintenant() {
      const marge = $("marge");
      if (!S.cartes.length) { marge.style.minHeight = ""; return; }
      const absolue = getComputedStyle(S.cartes[0].carte).position === "absolute";
      const base = marge.getBoundingClientRect().top;
      const liste = S.cartes.map((c) => {
        // Full height includes padding and borders, counted exactly once.
        const plein = c.carte.scrollHeight + (c.carte.offsetHeight - c.carte.clientHeight);
        return { c, y: c.badge ? c.badge.getBoundingClientRect().top - base - 6 : Infinity, plein,
          longue: plein > CARTE_MAX + 1, h: Math.min(plein, CARTE_MAX) };
      });
      for (const x of liste) x.c.carte.classList.toggle("longue", x.longue);
      if (!absolue) { marge.style.minHeight = ""; return; }
      liste.sort((x, y) => x.y - y.y);
      let bas = 0, fond = 0;
      for (const x of liste) {
        x.top = Math.max(x.y === Infinity ? bas : x.y, bas);
        bas = x.top + x.h + 6;
        if (x.c.carte.classList.contains("active")) fond = x.top + x.plein + 6;
      }
      for (const x of liste) x.c.carte.style.top = x.top + "px";
      marge.style.minHeight = Math.max(bas, fond) + "px";
    }

    function disposer() {
      if (S.attente) return;
      S.attente = requestAnimationFrame(() => { S.attente = 0; disposerMaintenant(); });
    }

    function blocCorrection(a, m) {
      const texte = m.text === "∅" ? "" : m.text;
      const b = el("div", "correction");
      const tete = el("div", "titre");
      tete.innerHTML = icone("corriger");
      tete.append(el("span", "", "Correction proposée par " + (m.via || "l'agent")));
      b.append(tete);
      const v = el("div", "version");
      if (a.kind === "substitution" || a.kind === "deletion") {
        v.append(md("del", "", court(a.text, 160)), flecheRemplace());
      }
      v.append(texte ? md("ins", "", court(texte, 200)) : el("em", "", "supprimer le passage"));
      b.append(v);
      if (a.by === "author") {
        const actions = el("div", "actions");
        const reprendre = el("button", "corriger");
        reprendre.innerHTML = icone("corriger");
        reprendre.append(el("span", "", "Reprendre"));
        reprendre.type = "button";
        reprendre.dataset.infobulle = "Votre proposition prend ce texte, sans être acceptée : vous pourrez encore la retoucher";
        reprendre.addEventListener("click", (e) => { e.stopPropagation(); appliquerCorrection(a, m, false); });
        const accepter = el("button", "accepter");
        accepter.innerHTML = icone("accepter");
        accepter.append(el("span", "", "Accepter cette version"));
        accepter.type = "button";
        accepter.dataset.infobulle = "Le texte devient celui de l'agent : votre proposition est corrigée puis acceptée";
        accepter.addEventListener("click", (e) => { e.stopPropagation(); appliquerCorrection(a, m, true); });
        actions.append(reprendre, accepter);
        b.append(actions);
      }
      return b;
    }

    return { fabriquerCarte, nomAnnotation, disposer, disposerMaintenant, brancherRepli };
  }
  return { creer };
})();
