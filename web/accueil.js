"use strict";
(() => {
  const $ = (id) => document.getElementById(id);
  const creer = (tag, classe, texte) => {
    const n = document.createElement(tag);
    if (classe) n.className = classe;
    if (texte !== undefined) n.textContent = texte;
    return n;
  };
  const icone = (nom) => {
    const t = document.createElement('template');
    t.innerHTML = GloseIcones.svg(nom);
    return t.content.firstElementChild;
  };
  const dire = (n, texte) => { n.textContent = texte || ''; n.hidden = !texte; };
  const statuts = { draft: 'Brouillon', active: 'En cours', review: 'En révision', done: 'Terminé', archived: 'Archivé' };
  const etats = { missing: 'Dossier introuvable', 'no-profile': 'Profil absent', profile: 'Profil illisible', journal: 'Journal abîmé' };
  const remplirStatuts = (select) => {
    for (const [valeur, texte] of Object.entries(statuts)) {
      const o = creer('option', '', texte); o.value = valeur; select.append(o);
    }
  };
  remplirStatuts($('acc-statut'));
  const formulaire = $('acc-form-modifier');
  formulaire.elements.status.append(creer('option', '', 'Non renseigné'));
  formulaire.elements.status.firstElementChild.value = '';
  remplirStatuts(formulaire.elements.status);
  let jeton = '';
  const fragment = /(?:^#|&)t=([0-9a-f]{64})(?:&|$)/i.exec(location.hash);
  try {
    if (fragment) sessionStorage.setItem('glose-jeton', fragment[1]);
    jeton = fragment ? fragment[1] : sessionStorage.getItem('glose-jeton') || '';
  } catch (_) { jeton = fragment ? fragment[1] : ''; }
  if (fragment) history.replaceState(null, '', location.pathname + location.search);
  $('acc-agents').href = '/set' + (jeton ? '#t=' + jeton : '');
  let donnees = null, type = '', tri = '', sens = 1, selection = null, retrait = null, chargement = false;
  let avisMinuterie, ouverture = false;
  async function api(chemin, corps) {
    const options = { headers: { 'X-Glose-Token': jeton }, signal: AbortSignal.timeout(15000) };
    if (corps !== undefined) {
      options.method = 'POST'; options.headers['Content-Type'] = 'application/json'; options.body = JSON.stringify(corps);
    }
    try {
      const r = await fetch(chemin, options);
      const d = await r.json();
      dire($('acc-connexion'), '');
      if (!r.ok) throw Object.assign(new Error(d.message || d.error || 'La requête a échoué.'), { reponse: true });
      return d;
    } catch (e) {
      if (!e.reponse) {
        const message = 'Le serveur Glose ne répond plus. Relancez « glose home ».';
        dire($('acc-connexion'), message);
        throw new Error(message);
      }
      throw e;
    }
  }
  function ouvrirDialogue(id) {
    const d = $(id), origine = document.activeElement;
    dire(d.querySelector('[data-erreur]'), '');
    d.addEventListener('close', () => { if (origine && origine.isConnected) origine.focus(); else $('acc-recherche').focus(); }, { once: true });
    d.showModal();
  }
  for (const b of document.querySelectorAll('[data-fermer]')) b.onclick = () => b.closest('dialog').close();
  function nouveau() {
    if (!donnees) return;
    const select = $('acc-form-nouveau').elements.type;
    const valeur = select.value;
    select.replaceChildren();
    const sans = creer('option', '', 'Sans gabarit'); sans.value = ''; select.append(sans);
    for (const nom of donnees.templates || []) { const o = creer('option', '', nom); o.value = nom; select.append(o); }
    select.value = valeur;
    if (select.selectedIndex < 0) select.value = '';
    $('acc-gabarits').textContent = (donnees.templates || []).length ? '' : 'Aucun gabarit disponible. Ajoutez un dossier de gabarit dans votre configuration Glose (' + donnees.templateDir + ') pour initialiser un nouveau projet. Vous pouvez déjà ouvrir un dossier Markdown avec glose open.';
    ouvrirDialogue('acc-nouveau');
  }
  for (const b of document.querySelectorAll('[data-nouveau]')) b.onclick = nouveau;
  function modifier(p) {
    selection = { id: p.id, hash: p.profileHash, meta: { ...p.meta } };
    $('acc-projet-modifie').textContent = p.title;
    for (const cle of ['type', 'status', 'deadline', 'nextAction']) formulaire.elements[cle].value = p.meta[cle] || '';
    ouvrirDialogue('acc-modifier');
  }
  async function soumettre(form, travail) {
    const bouton = form.querySelector('[type=submit]'); bouton.disabled = true;
    dire(form.querySelector('[data-erreur]'), '');
    try { await travail(); } catch (e) { dire(form.querySelector('[data-erreur]'), e.reponse ? e.message : 'Le serveur ne répond pas. Votre saisie est conservée.'); }
    finally { bouton.disabled = false; }
  }
  formulaire.onsubmit = (e) => {
    e.preventDefault();
    soumettre(formulaire, async () => {
      const corps = { id: selection.id, hash: selection.hash };
      for (const cle of ['type', 'status', 'deadline', 'nextAction']) {
        const valeur = formulaire.elements[cle].value;
        if (valeur !== (selection.meta[cle] || '')) corps[cle] = valeur;
      }
      await api('/api/accueil/meta', corps); $('acc-modifier').close(); await charger();
    });
  };
  $('acc-form-nouveau').onsubmit = (e) => {
    e.preventDefault(); const f = e.currentTarget;
    soumettre(f, async () => {
      const r = await api('/api/accueil/new', { path: f.elements.path.value, type: f.elements.type.value });
      $('acc-nouveau').close();
      clearTimeout(avisMinuterie);
      dire($('acc-avis'), [r.output, r.warnings, r.registered ? '' : 'Ce dossier temporaire n’est pas inscrit au registre.'].filter(Boolean).join('\n'));
      avisMinuterie = setTimeout(() => dire($('acc-avis'), ''), 20000);
      f.reset(); await charger();
    });
  };
  $('acc-form-retirer').onsubmit = (e) => {
    e.preventDefault(); soumettre(e.currentTarget, async () => {
      await api('/api/accueil/forget', { id: retrait }); $('acc-retirer').close(); await charger();
    });
  };
  const normal = (p) => p.state === 'ok' || (p.state === 'journal' && ['author', 'agent'].includes(p.hand));
  function action(p) {
    return p.meta.nextAction || (p.hand === 'agent' ? 'Attendre le retour de ' + (p.agent?.name || 'l’agent') : p.hand === 'author' ? p.annotations ? 'Traiter les annotations' : 'Relire le texte' : 'Main inconnue');
  }
  function echeance(p) {
    const valeur = p.meta.deadline;
    if (!valeur) return creer('span', 'acc-discret', '—');
    const [a, m, j] = valeur.split('-').map(Number);
    const date = new Date(a, m - 1, j), maintenant = new Date();
    // Compare calendar days, independently of time zones and daylight saving.
    const jours = Math.round((Date.UTC(a, m - 1, j) - Date.UTC(maintenant.getFullYear(), maintenant.getMonth(), maintenant.getDate())) / 86400000);
    const active = !['done', 'archived'].includes(p.meta.status);
    const n = creer('span', active && jours < 0 ? 'acc-retard' : active && jours <= 7 ? 'acc-proche' : '', date.toLocaleDateString('fr-FR', { day: 'numeric', month: 'short' }));
    if (active && jours < 0) n.append(creer('small', '', 'Dépassée'));
    return n;
  }
  async function ouvrirProjet(p, ligne) {
    // One opening at a time: the first project asked for is the one that opens.
    if (ouverture) return;
    ouverture = true;
    dire($('acc-erreur'), 'Ouverture de « ' + p.title + ' »…');
    try { const r = await api('/api/accueil/open', { id: p.id }); location.href = r.url; }
    catch (e) { dire($('acc-erreur'), e.reponse ? e.message : 'Impossible d’ouvrir le projet : le serveur ne répond pas.'); }
    finally { ouverture = false; }
  }
  function ligneProjet(p) {
    const tr = creer('tr', normal(p) ? 'acc-ouvrable' : 'acc-etat');
    tr.dataset.projet = p.id;
    const td = () => { const n = creer('td'); tr.append(n); return n; };
    const titre = td();
    if (!normal(p)) {
      titre.colSpan = 9;
      const contenu = creer('div', 'acc-etat-contenu'); contenu.append(icone(p.state === 'missing' ? 'introuvable' : 'alerte'), creer('strong', '', p.title), creer('strong', 'acc-retard', etats[p.state] || 'Projet illisible'), creer('span', 'acc-discret', p.message));
      if (['missing', 'no-profile'].includes(p.state)) {
        const b = creer('button', '', 'Retirer du registre');
        b.onclick = () => { retrait = p.id; $('acc-projet-retire').textContent = p.title; ouvrirDialogue('acc-retirer'); }; contenu.append(b);
      }
      contenu.title = [...(p.warnings || []), ...(p.problems || [])].join('\n');
      titre.append(contenu); return tr;
    }
    tr.onclick = (e) => { if (!e.target.closest('button, a, input, select')) ouvrirProjet(p, tr); };
    const b = creer('button', 'acc-titre', p.title); b.prepend(icone('a-fichier')); b.onclick = () => ouvrirProjet(p, tr); titre.append(b);
    const alertes = [...(p.warnings || []), ...(p.problems || []), ...(p.state !== 'ok' ? [(etats[p.state] || p.state) + ' : ' + p.message] : [])];
    if (alertes.length) {
      const alerte = creer('span', 'acc-proche'); alerte.tabIndex = 0; alerte.title = alertes.join('\n'); alerte.setAttribute('aria-label', alertes.join('. ')); alerte.append(icone('alerte')); titre.append(alerte);
    }
    let activite = '';
    if (p.lastActivity) { const d = new Date(p.lastActivity); activite = Number.isNaN(d.getTime()) ? p.lastActivity : 'Activité le ' + d.toLocaleDateString('fr-FR', { day: 'numeric', month: 'short' }) + ' à ' + d.toLocaleTimeString('fr-FR', { hour: '2-digit', minute: '2-digit' }); }
    titre.append(creer('small', 'acc-discret', [activite, p.server ? 'serveur en marche' : ''].filter(Boolean).join(' · ')));
    titre.append(creer('small', 'acc-complement', [p.meta.type, action(p)].filter(Boolean).join(' · ')));
    td().append(creer('span', p.meta.type ? 'acc-badge' : 'acc-discret', p.meta.type || '—')); tr.lastChild.className = 'acc-type';
    const statut = statuts[p.meta.status] ? creer('span', 'acc-badge acc-statut-' + p.meta.status, statuts[p.meta.status]) : creer('span', 'acc-discret', '—');
    if (p.meta.status === 'done') statut.prepend(icone('accepter')); td().append(statut);
    const main = creer('span', p.hand === 'author' ? 'acc-main-auteur' : '', p.hand === 'author' ? 'Vous' : p.hand === 'agent' ? p.agent?.name || 'Agent' : 'Inconnue');
    if (['author', 'agent'].includes(p.hand)) main.prepend(icone(p.hand === 'author' ? 'vous' : 'agent')); td().append(main);
    const annotations = td(); annotations.append(creer('span', '', p.annotations));
    if (p.issues) annotations.append(creer('small', 'acc-proche', p.issues + ' anomalie(s)'));
    const progression = td();
    if (!p.progress) progression.textContent = 'Inconnu';
    else {
      const g = p.progress, pourcent = g.total > 0 ? Math.round(100 * g.read / g.total) : null;
      progression.append(creer('span', '', pourcent === null ? 'Inconnu' : pourcent + ' %'));
      if (pourcent !== null) { const barre = creer('progress'); barre.max = 100; barre.value = pourcent; barre.setAttribute('aria-label', 'Progression de la relecture'); progression.append(barre); }
      if (g.filesSeen < g.files) { const partiel = creer('small', 'acc-discret', g.filesSeen + '/' + g.files + ' · partiel'); partiel.title = 'Progression connue pour ' + g.filesSeen + ' fichiers sur ' + g.files + '. Les autres fichiers n’ont pas encore été mesurés.'; partiel.tabIndex = 0; progression.append(partiel); }
    }
    td().append(echeance(p)); td().append(creer('span', '', action(p))); tr.lastChild.className = 'acc-suite';
    const outils = td(); const editer = creer('button', 'acc-modifier'); editer.title = 'Modifier les métadonnées de ' + p.title; editer.setAttribute('aria-label', editer.title); editer.append(icone('corriger')); editer.onclick = () => modifier(p); outils.append(editer, icone('fleche'));
    return tr;
  }
  function afficher() {
    if (!donnees) return;
    const actif = document.activeElement;
    const ligneActive = actif?.closest('[data-projet]');
    const focus = ligneActive ? { id: ligneActive.dataset.projet, modifier: actif.classList.contains('acc-modifier') } : null;
    const filtreActif = actif?.closest('#acc-types') ? type : null;
    const projets = donnees.projects || [], total = donnees.totals;
    dire($('acc-erreur'), donnees.error || '');
    $('acc-stats').replaceChildren();
    if (!donnees.error) {
      for (const [cle, nom, dessin, detail] of [
        ['projects', 'Projets', 'a-dossier', ''], ['annotations', 'Annotations ouvertes', 'commenter', ''],
        ['inReview', 'En révision', 'activite', total.agentHand + ' avec la main à un agent'],
        ['dueSoon', 'Échéances proches', 'calendrier', 'Dans les 7 jours · ' + total.overdue + ' dépassée(s)']
      ]) {
        const n = creer('div', 'acc-stat acc-stat-' + cle), label = creer('span', '', nom); label.prepend(icone(dessin)); n.append(label, creer('strong', '', total[cle])); if (detail) n.append(creer('small', '', detail)); $('acc-stats').append(n);
      }
    }
    dire($('acc-illisibles'), total.unreadable ? total.unreadable + ' projet(s) illisible(s) : voir les alertes dans le tableau.' : '');
    $('acc-premier').hidden = !!projets.length || !!donnees.error; $('acc-projets').hidden = !projets.length;
    const types = new Map(); for (const p of projets) if (p.meta.type) types.set(p.meta.type, (types.get(p.meta.type) || 0) + 1);
    $('acc-types').replaceChildren();
    for (const [valeur, nombre] of [['', projets.length], ...types]) {
      const b = creer('button', valeur === type ? 'acc-selection' : '', (valeur || 'Tous') + ' (' + nombre + ')'); b.type = 'button'; b.setAttribute('aria-pressed', String(valeur === type)); b.onclick = () => { type = valeur; afficher(); }; $('acc-types').append(b);
    }
    const recherche = $('acc-recherche').value.toLocaleLowerCase('fr');
    const statut = $('acc-statut').value;
    const liste = projets.filter(p => p.title.toLocaleLowerCase('fr').includes(recherche) && (!type || p.meta.type === type) && (!statut || p.meta.status === statut));
    if (tri) liste.sort((a, b) => {
      if (normal(a) !== normal(b)) return normal(a) ? -1 : 1;
      const valeur = p => tri === 'status' ? p.meta.status || '' : tri === 'deadline' ? p.meta.deadline || '' : p[tri];
      const x = valeur(a), y = valeur(b);
      return sens * (typeof x === 'number' ? x - y : String(x).localeCompare(String(y), 'fr'));
    });
    const lignes = $('acc-lignes'); lignes.replaceChildren(...liste.map(ligneProjet));
    if (!liste.length) {
      const tr = creer('tr'), td = creer('td', 'acc-vide', 'Aucun projet ne correspond à votre recherche. '); td.colSpan = 9;
      const b = creer('button', '', 'Tout effacer'); b.onclick = () => { type = ''; $('acc-statut').value = ''; $('acc-recherche').value = ''; afficher(); }; td.append(b); tr.append(td); lignes.append(tr);
    }
    if (focus) {
      const ligne = Array.from(lignes.children).find(n => n.dataset.projet === focus.id);
      const bouton = ligne?.querySelector(focus.modifier ? '.acc-modifier' : 'button');
      if (bouton) bouton.focus({ preventScroll: true });
    } else if (filtreActif !== null) {
      $('acc-types').querySelector('[aria-pressed="true"]')?.focus({ preventScroll: true });
    }
  }
  for (const th of document.querySelectorAll('[data-tri]')) {
    th.setAttribute('aria-sort', 'none');
    th.querySelector('button').append(icone('tri'));
    th.querySelector('button').onclick = () => {
      sens = tri === th.dataset.tri ? -sens : 1; tri = th.dataset.tri;
      for (const entete of document.querySelectorAll('[data-tri]')) entete.setAttribute('aria-sort', entete === th ? sens === 1 ? 'ascending' : 'descending' : 'none');
      afficher();
    };
  }
  $('acc-recherche').oninput = afficher; $('acc-statut').onchange = afficher;
  async function charger() {
    if (chargement) return;
    chargement = true;
    try { donnees = await api('/api/accueil'); afficher(); }
    catch (e) { if (e.reponse) dire($('acc-erreur'), e.message); }
    finally { chargement = false; }
  }
  window.GloseEspace.brancher(jeton);
  if (!/^[0-9a-f]{64}$/i.test(jeton)) {
    dire($('acc-erreur'), 'Ouvrez l’adresse donnée par « glose home » pour accéder à vos projets.'); return;
  }
  charger();
  setInterval(() => api('/api/ping').catch(() => {}), 10000);
  setInterval(charger, 30000);
  document.addEventListener('visibilitychange', () => { if (!document.hidden) charger(); });
})();
