"use strict";
// Shared interface utilities, independent of the review state (P8.3).
window.GloseInterface = (() => {
  function creer({ $ }) {
    // Lucide button icons (web/icones.js); each button keeps its name
    // in its aria-label and tooltip.
    function icone(nom) {
      return window.GloseIcones ? GloseIcones.svg(nom) : "";
    }
    function el(tag, classe, texte) {
      const e = document.createElement(tag);
      if (classe) e.className = classe;
      if (texte !== undefined) e.textContent = texte;
      return e;
    }

    // A message with its bold and italics, built by nodes: nothing of it is HTML.
    function riche(tag, classe, texte) {
      const e = el(tag, classe);
      const poser = (parent, morceaux) => {
        for (const m of morceaux) {
          if (typeof m === "string") { parent.append(m); continue; }
          const x = document.createElement(m.t);
          poser(x, m.c);
          parent.append(x);
        }
      };
      poser(e, GloseDonnees.emphase(texte));
      return e;
    }

    // ---------- Dialog boxes ----------
    // Interface dialogs replace system windows (confirm, prompt). Without
    // fields, return true or false; with fields, return their values
    // ({name: value}), or null on cancellation.
    // valider(values) may return an error message to keep the dialog open.
    function boite({ titre, message = "", icone = "alerte", ton = "", champs = [], ok = "Valider", annuler = "Annuler", valider }) {
      return new Promise((resoudre) => {
        const d = $("boite");
        d.className = "boite " + ton;
        $("boite-icone").innerHTML = window.GloseIcones ? GloseIcones.traces[icone] || "" : "";
        $("boite-titre").textContent = titre;
        $("boite-message").textContent = message;
        $("boite-erreur").textContent = "";
        $("boite-ok").textContent = ok;
        $("boite-annuler").textContent = annuler;
        const zoneChamps = $("boite-champs");
        zoneChamps.textContent = "";
        const entrees = {};
        let rang = null;
        for (const c of champs) {
          const l = el("label");
          l.append(document.createTextNode(c.libelle));
          if (c.aide) l.append(el("small", "", c.aide));
          const e = el(c.type === "textarea" ? "textarea" : "input");
          if (c.type !== "textarea") e.type = c.type || "text";
          if (c.min !== undefined) e.min = c.min;
          if (c.max !== undefined) e.max = c.max;
          e.value = c.valeur ?? "";
          if (c.placeholder) e.placeholder = c.placeholder;
          e.spellcheck = c.type === "textarea";
          entrees[c.nom] = e;
          l.append(e);
          // Two short fields side by side (table columns and rows).
          if (c.court) {
            if (!rang) { rang = el("div", "rang"); zoneChamps.append(rang); }
            rang.append(l);
          } else {
            rang = null;
            zoneChamps.append(l);
          }
        }
        const valeurs = () => Object.fromEntries(Object.entries(entrees).map(([k, e]) => [k, e.value]));
        const fin = (v) => {
          d.removeEventListener("cancel", surAnnulation);
          d.querySelector("form").onsubmit = null;
          $("boite-annuler").onclick = null;
          if (d.open) d.close();
          resoudre(v);
        };
        const surAnnulation = (e) => { e.preventDefault(); fin(champs.length ? null : false); };
        d.addEventListener("cancel", surAnnulation);
        $("boite-annuler").onclick = () => fin(champs.length ? null : false);
        d.querySelector("form").onsubmit = (e) => {
          e.preventDefault();
          const v = valeurs();
          const erreur = valider && valider(v);
          if (erreur) { $("boite-erreur").textContent = erreur; return; }
          fin(champs.length ? v : true);
        };
        for (const e of Object.values(entrees)) {
          if (e.tagName === "TEXTAREA") {
            e.addEventListener("keydown", (ev) => {
              if (ev.key === "Enter" && (ev.ctrlKey || ev.metaKey)) { ev.preventDefault(); d.querySelector("form").requestSubmit(); }
            });
          }
        }
        d.showModal();
        const premier = Object.values(entrees)[0];
        if (premier) { premier.focus(); if (premier.select) premier.select(); } else $("boite-ok").focus();
      });
    }

    // ---------- Tooltips ----------
    // One Glose tooltip (instead of the browser tooltip) for every element
    // with data-infobulle: above it, or below if space is lacking, always
    // within the viewport; inside an open panel or dialog, it is placed
    // in the same layer, in front.
    const bulle = document.createElement("div");
    bulle.className = "infobulle";
    bulle.setAttribute("role", "tooltip");
    bulle.hidden = true;
    let bulleCible = null, bulleMinuterie = 0;
    function cacherBulle() {
      clearTimeout(bulleMinuterie);
      bulleCible = null;
      bulle.hidden = true;
      bulle.classList.remove("visible");
    }
    function montrerBulle(t) {
      if (!t.isConnected || !t.dataset.infobulle) return;
      const hote = t.closest("dialog[open]") || document.body;
      if (bulle.parentNode !== hote) hote.append(bulle);
      // Tooltips are read at a glance: text is bounded here, and limited
      // to four lines by the stylesheet.
      const texte = t.dataset.infobulle;
      bulle.textContent = texte.length > 240 ? texte.slice(0, 239).trimEnd() + "…" : texte;
      bulle.hidden = false;
      const r = t.getBoundingClientRect();
      const w = bulle.offsetWidth, h = bulle.offsetHeight;
      let top = r.top - h - 8;
      bulle.classList.toggle("dessous", top < 6);
      if (top < 6) top = r.bottom + 8;
      const left = Math.max(6, Math.min(window.innerWidth - w - 6, r.left + r.width / 2 - w / 2));
      // Inside a dialog (fixed position, sometimes animated), coordinates
      // are relative to it.
      const base = hote === document.body ? { left: 0, top: 0 } : hote.getBoundingClientRect();
      bulle.style.position = hote === document.body ? "fixed" : "absolute";
      bulle.style.left = left - base.left + "px";
      bulle.style.top = top - base.top + "px";
      requestAnimationFrame(() => bulle.classList.add("visible"));
    }
    document.addEventListener("pointerover", (e) => {
      const t = e.target.closest && e.target.closest("[data-infobulle]");
      if (t === bulleCible) return;
      cacherBulle();
      if (!t) return;
      bulleCible = t;
      bulleMinuterie = setTimeout(() => montrerBulle(t), 350);
    });
    document.addEventListener("focusin", (e) => {
      const t = e.target.closest && e.target.closest("[data-infobulle]");
      cacherBulle();
      if (t && t.matches(":focus-visible")) { bulleCible = t; montrerBulle(t); }
    });
    for (const ev of ["pointerdown", "focusout", "scroll", "wheel"]) document.addEventListener(ev, cacherBulle, true);
    document.addEventListener("keydown", (e) => { if (e.key === "Escape") cacherBulle(); });

    // ---------- Utilities ----------
    let minuterieAvis = 0;
    function avis(msg, erreur) {
      const el = $("avis");
      el.textContent = msg;
      el.className = "visible" + (erreur ? " erreur" : "");
      clearTimeout(minuterieAvis);
      minuterieAvis = setTimeout(() => { el.className = ""; }, erreur ? 8000 : 3000);
    }

    // Persistent banner for issues that prevent work (session, rendering).
    function bandeau(msg) {
      $("bandeau").textContent = msg || "";
      $("bandeau").hidden = !msg;
    }

    // Non-modal editors keep the manuscript scrollable. Geometry never
    // changes editor contents or the DOM range held by the visual editor.
    function panneau({ dialogue, cible, large = false }) {
      let retour = null, compose = false, prevu = 0, lateral = false;
      const classe = large ? "edition-laterale" : "saisie-laterale";
      function placer() {
        prevu = 0;
        if (!dialogue.open) return;
        const haut = $("barre").getBoundingClientRect().bottom + 12;
        const passage = cible();
        let r = passage && passage.getBoundingClientRect();
        // A resized viewport must still leave the actions on screen.
        if (!lateral && large && r && (r.bottom + 240 > window.innerHeight || $("texte").getBoundingClientRect().width < 480)) {
          lateral = true;
          document.body.classList.add(classe);
          r = passage.getBoundingClientRect();
        }
        const texte = $("texte").getBoundingClientRect();
        // Choose once at opening; scrolling must not flip the layout.
        if (!lateral && large && r) {
          dialogue.style.left = texte.left + "px";
          dialogue.style.width = texte.width + "px";
          dialogue.style.top = Math.max(haut, r.bottom + 12) + "px";
        } else {
          const marge = $("marge").getBoundingClientRect();
          const largeur = Math.min(window.innerWidth - 24, Math.max(320, marge.width));
          dialogue.style.width = largeur + "px";
          dialogue.style.left = Math.max(12, Math.min(marge.left, window.innerWidth - largeur - 12)) + "px";
          // At the height of the passage, moved up as far as its whole height
          // requires: it only scrolls if the window itself is too small.
          dialogue.style.maxHeight = "none";
          const pied = $("pied").getBoundingClientRect().height + 8;
          const bas = window.innerHeight - dialogue.getBoundingClientRect().height - pied;
          dialogue.style.top = Math.max(haut, Math.min(r ? r.top : haut, bas)) + "px";
        }
        const top = parseFloat(dialogue.style.top);
        dialogue.style.maxHeight = Math.max(100, window.innerHeight - top - $("pied").getBoundingClientRect().height - 8) + "px";
      }
      function planifier() {
        if (dialogue.open && !prevu) prevu = requestAnimationFrame(placer);
      }
      function ouvrir() {
        retour = document.activeElement;
        const passage = cible(), r = passage && passage.getBoundingClientRect();
        lateral = !large || !r || $("texte").getBoundingClientRect().width < 480 ||
          window.innerHeight - r.bottom < 440;
        document.body.classList.toggle(classe, lateral);
        dialogue.show();
        placer();
      }
      dialogue.addEventListener("compositionstart", () => { compose = true; });
      dialogue.addEventListener("compositionend", () => { compose = false; });
      document.addEventListener("keydown", (e) => {
        if (!dialogue.open || e.key !== "Escape" || e.isComposing || e.keyCode === 229 || compose ||
          document.querySelector("dialog:modal")) return;
        e.preventDefault();
        const annulation = new Event("cancel", { cancelable: true });
        if (dialogue.dispatchEvent(annulation)) dialogue.close();
      });
      dialogue.addEventListener("close", () => {
        // A close event may arrive after a different editor was opened.
        if (dialogue.open) return;
        document.body.classList.remove(classe);
        if (!document.querySelector("dialog[open]")) {
          // The close event comes after the fact: focus already placed elsewhere
          // stays there. An element no longer shown (a menu item) cannot take it.
          const actif = document.activeElement;
          if (actif && actif !== document.body && !dialogue.contains(actif)) return;
          const visible = retour && retour.isConnected && retour !== document.body && retour.getClientRects().length > 0;
          (visible ? retour : $("texte")).focus({ preventScroll: true });
        }
      });
      window.addEventListener("scroll", planifier, { passive: true });
      window.addEventListener("resize", planifier);
      const dimensions = new ResizeObserver(planifier);
      dimensions.observe($("barre"));
      dimensions.observe($("texte"));
      return { ouvrir, placer };
    }

    return { riche, boite, avis, bandeau, el, icone, panneau };
  }

  return { creer };
})();
