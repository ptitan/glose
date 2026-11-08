/* What the Accueil and Agents pages share: the way back to the text being read,
   and the Réglages dialog, valid for all projects. */
"use strict";
window.GloseEspace = (() => {
  const POLICES = [["source", "Source Serif 4"], ["libertinus", "Libertinus Serif"], ["serif", "Classique (Sitka, Cambria)"], ["georgia", "Georgia"],
    ["sans", "Sans empattement"], ["lisible", "Très lisible (Verdana)"], ["mono", "Chasse fixe"]];

  function creer(tag, classe, texte) {
    const e = document.createElement(tag);
    if (classe) e.className = classe;
    if (texte !== undefined) e.textContent = texte;
    return e;
  }

  // The reader left its title in this tab: it can be returned to.
  function lienTexte() {
    let titre = null;
    try { titre = sessionStorage.getItem("glose-texte"); } catch (_) { /* private browsing */ }
    const nav = document.querySelector(".acc-nav nav");
    if (titre === null || !nav) return;
    const a = creer("a", "acc-revenir");
    a.id = "acc-texte";
    a.href = "/";
    a.dataset.infobulle = "Revenir au texte, à l'endroit où vous étiez";
    a.title = "Revenir au texte, à l'endroit où vous étiez";
    const t = document.createElement("template");
    t.innerHTML = window.GloseIcones ? window.GloseIcones.svg("retour") : "";
    if (t.content.firstElementChild) a.append(t.content.firstElementChild);
    const mots = creer("span");
    mots.append(creer("strong", "", "Revenir au texte"), creer("small", "", titre));
    a.append(mots);
    nav.prepend(a);
  }

  function carteTheme(t, choisir) {
    const l = creer("label", "theme-carte");
    const r = creer("input");
    r.type = "radio"; r.name = "espace-theme"; r.value = t.id;
    r.addEventListener("change", () => choisir(t.id));
    const a = creer("span", "theme-apercu");
    a.dataset.theme = t.id;
    a.setAttribute("aria-hidden", "true");
    a.append(creer("span", "theme-lettres", "Aa"), creer("span", "theme-ligne ajout"), creer("span", "theme-ligne retrait"), creer("span", "theme-ligne accent"));
    l.append(r, a, creer("span", "theme-nom", t.nom));
    return l;
  }

  function dialogue(jeton) {
    const d = creer("dialog", "acc-dialogue espace-reglages");
    d.id = "espace-reglages";
    d.setAttribute("aria-labelledby", "espace-reglages-titre");
    const titre = creer("h2", "", "Réglages");
    titre.id = "espace-reglages-titre";
    const aide = creer("p", "acc-discret", "Valables pour tous vos projets.");
    const erreur = creer("p", "acc-erreur");
    erreur.setAttribute("role", "alert"); erreur.hidden = true;

    let reglages = null, minuteur = 0;
    const appel = (corps) => fetch("/api/settings", { method: corps ? "POST" : "GET", headers: { "X-Glose-Token": jeton, "Content-Type": "application/json" },
      body: corps ? JSON.stringify(corps) : undefined }).then((r) => (r.ok ? r.json() : Promise.reject(new Error("Réglages non enregistrés."))));
    const dire = (texte) => { erreur.textContent = texte; erreur.hidden = !texte; };
    const enregistrer = (changement) => {
      if (!reglages) return;
      reglages = { ...reglages, ...changement };
      clearTimeout(minuteur);
      minuteur = setTimeout(() => appel(reglages).then(() => dire("")).catch((e) => dire(e.message)), 250);
    };

    // ---------- Theme ----------
    const choisirTheme = (id) => { window.GloseThemes.appliquer(id); enregistrer({ theme: id }); };
    const themes = creer("div", "espace-themes");
    for (const [teinte, nom] of [["system", ""], ["light", "Clairs"], ["dark", "Sombres"]]) {
      if (nom) themes.append(creer("h4", "", nom));
      const grille = creer("div", "theme-grille");
      for (const t of window.GloseThemes.LISTE) if (t.teinte === teinte) grille.append(carteTheme(t, choisirTheme));
      themes.append(grille);
    }

    // ---------- Reading ----------
    const lecture = creer("div", "espace-lecture");
    const champ = (nom, controle) => { const l = creer("label", "", nom); l.append(controle); lecture.append(l); return controle; };
    const police = champ("Police du texte", creer("select"));
    for (const [v, n] of POLICES) police.append(Object.assign(creer("option", "", n), { value: v }));
    const curseur = (nom, min, max, pas) => {
      const c = creer("input"); Object.assign(c, { type: "range", min, max, step: pas });
      return champ(nom, c);
    };
    const taille = curseur("Taille du texte", 12, 30, 1);
    const interligne = curseur("Interligne", 1.2, 2.2, 0.05);
    const largeur = curseur("Largeur du texte", 30, 70, 1);
    const justifie = creer("input"); justifie.type = "checkbox";
    const caseJ = creer("label", "case"); caseJ.append(justifie, document.createTextNode(" Justifier le texte"));
    lecture.append(caseJ);
    police.addEventListener("input", () => enregistrer({ font: police.value }));
    taille.addEventListener("input", () => enregistrer({ size: +taille.value }));
    interligne.addEventListener("input", () => enregistrer({ lineHeight: +interligne.value }));
    largeur.addEventListener("input", () => enregistrer({ textWidth: +largeur.value }));
    justifie.addEventListener("change", () => enregistrer({ justify: justifie.checked }));

    const actions = creer("div", "acc-actions");
    const fermer = creer("button", "", "Fermer");
    fermer.type = "button";
    fermer.addEventListener("click", () => d.close());
    actions.append(fermer);
    d.append(titre, aide, creer("h3", "", "Thème"), themes, creer("h3", "", "Lecture"), lecture, erreur, actions);
    document.body.append(d);

    appel().then((r) => {
      reglages = r;
      window.GloseThemes.appliquer(r.theme);
      for (const x of themes.querySelectorAll("input")) x.checked = x.value === r.theme;
      police.value = r.font; taille.value = r.size; interligne.value = r.lineHeight; largeur.value = r.textWidth; justifie.checked = r.justify;
    }).catch((e) => dire(e.message));
    return d;
  }

  function brancher(jeton) {
    lienTexte();
    const bouton = document.getElementById("acc-reglages");
    if (!bouton || !/^[0-9a-f]{64}$/i.test(jeton)) return;
    const d = dialogue(jeton);
    bouton.addEventListener("click", () => d.showModal());
  }

  return { brancher };
})();
