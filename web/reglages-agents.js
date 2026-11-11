"use strict";
// Agent settings page (P10.3, "glose set"): installed agents,
// short names (agent + model) and the default one. General settings,
// shared by all projects (D-48). Served by a project server or
// the temporary settings server; the token comes from the address.
(() => {
  const $ = (id) => document.getElementById(id);
  const el = (tag, classe, texte) => {
    const n = document.createElement(tag);
    if (classe) n.className = classe;
    if (texte !== undefined) n.textContent = texte;
    return n;
  };
  const icone = (nom) => {
    const t = document.createElement('template');
    t.innerHTML = window.GloseIcones ? window.GloseIcones.svg(nom) : '';
    return t.content.firstElementChild || el('span');
  };
  let jeton = '';
  const m = /(?:^#|&)t=([0-9a-f]{64})/.exec(location.hash);
  if (m) {
    jeton = m[1];
    sessionStorage.setItem('glose-jeton-agents', jeton);
    history.replaceState(null, '', location.pathname);
  } else {
    jeton = sessionStorage.getItem('glose-jeton-agents') || sessionStorage.getItem('glose-jeton') || '';
  }

  for (const lien of [$('ra-accueil'), ...document.querySelectorAll('[data-accueil]')]) {
    lien.href = '/home' + (jeton ? '#t=' + jeton : '');
  }

  async function api(chemin, corps) {
    const opt = { headers: { 'X-Glose-Token': jeton } };
    if (corps !== undefined) {
      opt.method = 'POST';
      opt.headers['Content-Type'] = 'application/json';
      opt.body = JSON.stringify(corps);
    }
    try {
      const r = await fetch(chemin, opt);
      let data = {};
      try { data = await r.json(); } catch (_) { /* without JSON */ }
      return { ok: r.ok, status: r.status, data };
    } catch (_) {
      return { ok: false, status: 0, data: { message: 'Le serveur Glose ne répond pas : relancez « glose set ».' } };
    }
  }

  let etat = null; // last response from /api/agents
  let modifie = false;
  const modeles = new Map(); // agent -> { liste, message }

  function erreur(msg) {
    $('ra-erreur').hidden = !msg;
    $('ra-erreur').textContent = msg || '';
  }
  function statut(msg, classe) {
    $('ra-statut').textContent = msg;
    $('ra-statut').className = classe || '';
  }
  function marquer() { modifie = true; statut('Les modifications attendent votre enregistrement.', 'attente'); }

  function detectes() {
    const ul = $('ra-detectes');
    ul.textContent = '';
    for (const a of etat.detection.agents) {
      const li = el('li', a.found ? (a.windows || a.error ? 'reserve' : 'present') : 'absent');
      li.append(el('strong', '', a.name));
      let txt = 'non trouvé';
      if (a.found && a.windows) txt = 'installé côté Windows (' + a.path + ') : non lancé depuis WSL';
      else if (a.found) txt = (a.version || 'version inconnue') + ' · ' + a.path + (a.error ? ' · ' + a.error : '');
      li.append(el('span', '', txt));
      ul.append(li);
    }
    if (etat.detection.at) {
      const li = el('li', 'date', 'Recherche du ' + new Date(etat.detection.at).toLocaleString('fr-FR'));
      ul.append(li);
    }
  }

  function trouve(id) { return etat.detection.agents.find((a) => a.id === id && a.found && !a.windows); }

  async function chargerModeles(agent, liste) {
    if (!agent) return;
    if (!modeles.has(agent)) {
      modeles.set(agent, { liste: [], message: 'Liste des modèles…' });
      const r = await api('/api/agents/models?agent=' + encodeURIComponent(agent));
      modeles.set(agent, r.ok ? { liste: r.data.models || [], message: r.data.message || (r.data.fromAgent ? '' : 'Noms connus de Glose ; saisie libre.') }
        : { liste: [], message: r.data.message || 'Liste indisponible.' });
    }
    const info = modeles.get(agent);
    liste.textContent = '';
    for (const nom of info.liste) { const o = el('option'); o.value = nom; liste.append(o); }
    return info;
  }

  let compteur = 0;
  function ligne(a, parDefaut) {
    const n = ++compteur;
    const div = el('div', 'ra-ligne');
    const nom = el('input'); nom.type = 'text'; nom.value = a.name || ''; nom.placeholder = 'Nom'; nom.maxLength = 32;
    nom.setAttribute('aria-label', 'Nom court'); nom.className = 'ra-nom'; nom.spellcheck = false;
    const agent = el('select'); agent.className = 'ra-agent'; agent.setAttribute('aria-label', 'Agent');
    for (const k of etat.detection.agents) {
      const o = el('option', '', k.name + (trouve(k.id) ? '' : ' (non trouvé)'));
      o.value = k.id;
      agent.append(o);
    }
    agent.value = a.agent || (etat.detection.agents.find((k) => trouve(k.id)) || etat.detection.agents[0]).id;
    const modele = el('input'); modele.type = 'text'; modele.value = a.model || ''; modele.placeholder = 'Modèle de l’agent';
    modele.className = 'ra-modele'; modele.setAttribute('aria-label', 'Modèle'); modele.spellcheck = false;
    const liste = el('datalist'); liste.id = 'ra-modeles-' + n; modele.setAttribute('list', liste.id);
    const aide = el('small', 'ra-aide-modele');
    const defaut = el('label', 'ra-defaut');
    const radio = el('input'); radio.type = 'radio'; radio.name = 'ra-defaut'; radio.checked = !!parDefaut;
    defaut.append(radio, el('span', '', 'Par défaut'));
    const essayer = el('button', 'icone discret ra-essayer'); essayer.type = 'button';
    essayer.setAttribute('aria-label', 'Essayer cet agent (tour à blanc)'); essayer.dataset.infobulle = 'Essayer (tour à blanc, consomme des jetons)';
    essayer.append(icone('essayer'));
    essayer.addEventListener('click', () => essai({ name: nom.value.trim(), agent: agent.value, model: modele.value.trim() }));
    const retirer = el('button', 'icone discret'); retirer.type = 'button';
    retirer.setAttribute('aria-label', 'Retirer ce nom court'); retirer.dataset.infobulle = 'Retirer';
    retirer.append(icone('rejeter'));
    const majModeles = async () => {
      aide.textContent = '';
      const info = await chargerModeles(agent.value, liste);
      if (info) aide.textContent = info.message || (info.liste.length + ' modèles proposés par l’agent ; saisie libre.');
    };
    agent.addEventListener('change', () => { marquer(); majModeles(); });
    modele.addEventListener('focus', majModeles, { once: true });
    for (const c of [nom, modele]) c.addEventListener('input', marquer);
    radio.addEventListener('change', marquer);
    retirer.addEventListener('click', () => {
      const etaitDefaut = radio.checked;
      div.remove();
      if (etaitDefaut) { const r = $('ra-alias').querySelector('input[type=radio]'); if (r) r.checked = true; }
      marquer(); vide();
    });
    const champs = el('div', 'ra-champs');
    const blocModele = el('div', 'ra-bloc-modele');
    blocModele.append(modele, liste, aide);
    champs.append(nom, agent, blocModele);
    div.append(champs, defaut, essayer, retirer);
    return div;
  }

  function vide() {
    const zone = $('ra-alias');
    const rien = zone.querySelector('.ra-vide');
    if (!zone.querySelector('.ra-ligne')) {
      if (!rien) zone.append(el('p', 'ra-vide', 'Aucun agent configuré : ajoutez un nom court, puis enregistrez.'));
    } else if (rien) rien.remove();
  }

  // P20 (docs/maquettes/p20/consignes.html): help in a bubble, behind an (i)
  // button; shown on hover and on focus, read by screen readers.
  let numero = 0;
  function aide(texte) {
    const zone = el('span', 'ra-aide'), id = 'ra-bulle-' + (++numero);
    const bouton = el('button', 'ra-info'); bouton.type = 'button'; bouton.setAttribute('aria-label', 'Aide');
    bouton.setAttribute('aria-describedby', id); bouton.append(icone('info'));
    const bulle = el('span', 'ra-bulle', texte); bulle.id = id; bulle.setAttribute('role', 'tooltip');
    bouton.addEventListener('keydown', (e) => { if (e.key === 'Escape') bouton.blur(); });
    zone.append(bouton, bulle);
    return zone;
  }
  const changer = (n) => n.dispatchEvent(new Event('change', { bubbles: true }));

  // Reused by task editors. Permissions are declarative, never arbitrary flags.
  // Switches for yes or no, segments for a choice among three (P20).
  function acces(zone, valeur = {}) {
    zone.textContent = '';
    const lus = {};
    const interrupteur = (cle, titre, sous) => {
      const ligne = el('div', 'ra-option'), texte = el('div'), id = 'ra-o-' + (++numero);
      const nom = el('span', 'ra-option-nom', titre); nom.id = id; texte.append(nom);
      if (sous) texte.append(el('small', 'aide', sous));
      const b = el('button', 'ra-interrupteur'); b.type = 'button'; b.setAttribute('role', 'switch'); b.setAttribute('aria-labelledby', id);
      b.append(el('span'));
      const poser = (oui) => b.setAttribute('aria-checked', String(oui));
      poser(!!valeur[cle]);
      b.addEventListener('click', () => { poser(b.getAttribute('aria-checked') !== 'true'); changer(b); });
      ligne.append(texte, b); zone.append(ligne);
      lus[cle] = () => b.getAttribute('aria-checked') === 'true';
      return b;
    };
    const segments = (cle, titre, valeurs) => {
      const champ = el('div', 'ra-champ'), id = 'ra-s-' + (++numero);
      const nom = el('div', 'ra-etiquette'); nom.append(el('span', '', titre)); nom.firstChild.id = id;
      const groupe = el('div', 'ra-segments'); groupe.setAttribute('role', 'group'); groupe.setAttribute('aria-labelledby', id);
      let courant = valeur[cle] || valeurs[0][0];
      for (const [v, texte] of valeurs) {
        const b = el('button', '', texte); b.type = 'button'; b.dataset.valeur = v;
        b.addEventListener('click', () => { courant = v; poser(); changer(b); });
        groupe.append(b);
      }
      const poser = () => { for (const b of groupe.children) b.setAttribute('aria-pressed', String(b.dataset.valeur === courant)); };
      poser();
      champ.append(nom, groupe); zone.append(champ);
      lus[cle] = () => courant;
      return groupe;
    };
    const texte = (cle, titre, type, valeurs, aideTexte, placeholder) => {
      const champ = el('div', 'ra-champ'), id = 'ra-t-' + (++numero);
      const nom = el('div', 'ra-etiquette'), label = el('label', '', titre); label.htmlFor = id; nom.append(label, aide(aideTexte));
      const input = el(type); input.id = id; input.value = valeurs; if (placeholder) input.placeholder = placeholder;
      champ.append(nom, input); zone.append(champ);
      return { champ, input };
    };
    interrupteur('web', 'Recherche et lecture sur le Web');
    interrupteur('subagents', 'Sous-agents');
    segments('scripts', 'Scripts', [['none', 'Aucun'], ['python', 'Python'], ['shell', 'Tout le shell']]);
    const dossiersExt = segments('externalDirs', 'Dossiers externes', [['none', 'Aucun'], ['list', 'Dossiers choisis'], ['all', 'Tous']]);
    const dossiers = texte('dirs', 'Dossiers choisis', 'textarea', (valeur.dirs || []).join('\n'), 'Un dossier existant par ligne ; un chemin relatif part du dossier du projet.');
    const ecriture = interrupteur('externalWrite', 'Écriture extérieure', 'Seulement dans les dossiers explicitement choisis.');
    const mcp = texte('mcp', 'Outils MCP', 'input', (valeur.mcp || []).join(', '), 'Noms de serveurs MCP, séparés par des virgules, ajoutés aux outils requis ; leur configuration doit déjà exister chez l’agent.', 'Outils requis par le texte');
    // The list of folders matters only when folders are chosen, or written.
    const montrer = () => { dossiers.champ.hidden = lus.externalDirs() !== 'list' && !lus.externalWrite(); };
    dossiersExt.addEventListener('click', montrer); ecriture.addEventListener('click', montrer); montrer();
    zone.addEventListener('input', marquer); zone.addEventListener('change', marquer);
    const liste = (s, sep) => s.split(sep).map((x) => x.trim()).filter(Boolean);
    return () => ({ externalDirs: lus.externalDirs(), scripts: lus.scripts(),
      externalWrite: lus.externalWrite(), web: lus.web(), subagents: lus.subagents(),
      dirs: liste(dossiers.input.value, '\n'), mcp: liste(mcp.input.value, /[,\n]/) });
  }

  // Icons offered for a task, with a name (P20); the current one is kept
  // even if it is not in this list.
  const ICONES = [['pen-line', 'Plume'], ['pencil', 'Crayon'], ['wand-sparkles', 'Réécriture'], ['spell-check', 'Orthographe'],
    ['languages', 'Langues'], ['scale', 'Droit'], ['book-check', 'Relecture'], ['search-check', 'Vérification'],
    ['pen-tool', 'Stylo'], ['file-pen', 'Fichier annoté'], ['list-checks', 'Liste de contrôle'], ['sparkles', 'Idées']];
  const NOM_ICONE = Object.fromEntries(ICONES);
  const AGENTS = { claude: 'Claude Code', codex: 'Codex', opencode: 'OpenCode' };
  const nomAgent = (court) => {
    const a = etat.config.aliases.find((x) => x.name === (court || etat.config.default));
    if (!a) return court || 'Agent par défaut';
    return a.name + ' · ' + (AGENTS[a.agent] || a.agent);
  };

  function consigne(t = {}) {
    const bloc = el('details', 'ra-consigne'); bloc.open = !t.id;
    const entete = el('summary'); bloc.append(entete);
    const formulaire = el('div', 'ra-formulaire'); bloc.append(formulaire);
    const options = el('details', 'ra-options');
    const titreOptions = el('summary'); titreOptions.append(icone('a-chevron'), el('span', '', 'Options'), el('span', 'ra-tag', 'Portée, accès et limites'));
    options.append(titreOptions);
    const champs = {};
    const champ = (cle, titre, type = 'input', valeur = '', texte = '', parent = formulaire) => {
      const zone = el('div', 'ra-champ'), id = 'ra-c-' + (++numero);
      const tete = el('div', 'ra-etiquette'), label = el('label', '', titre); label.htmlFor = id;
      tete.append(label); if (texte) tete.append(aide(texte));
      const input = el(type); input.id = id;
      input.value = t[cle] === undefined ? valeur : t[cle]; input.dataset.taskField = cle;
      zone.append(tete, input); parent.append(zone); champs[cle] = input; return input;
    };
    const deux = el('div', 'ra-deux'); formulaire.append(deux);
    const nom = champ('name', 'Nom', 'input', '', 'Nom affiché sur le bouton de la barre et dans le panneau de remise.', deux);
    // Icon: a summary that shows the choice, a grid of named icons.
    const zoneIcone = el('div', 'ra-champ'), teteIcone = el('div', 'ra-etiquette');
    teteIcone.append(el('span', '', 'Icône'), aide('Choisissez une icône dans la liste ; aucune saisie libre.'));
    const image = el('details', 'ra-choix-icone'), choixIcone = el('summary'); image.append(choixIcone);
    const liste = el('div', 'ra-icones'); image.append(liste);
    zoneIcone.append(teteIcone, image); deux.append(zoneIcone);
    let icon = t.icon || 'pen-line';
    const proposees = ICONES.filter(([v]) => (etat.taskIcons || []).includes(v));
    if (!NOM_ICONE[icon]) proposees.push([icon, icon]);
    const maj = () => {
      const libelle = NOM_ICONE[icon] || icon;
      choixIcone.replaceChildren(icone(icon), el('span', '', libelle), icone('menu-capsule'));
      for (const b of liste.children) b.setAttribute('aria-pressed', String(b.dataset.taskIcon === icon));
      const tuile = el('span', 'ra-tuile'); tuile.append(icone(icon));
      entete.replaceChildren(tuile, el('strong', '', nom.value || 'Nouvelle consigne'), el('span', 'ra-tag', nomAgent(champs.agent?.value)), icone('a-chevron'));
    };
    for (const [valeur, libelle] of proposees) {
      const bouton = el('button', 'ra-icone'); bouton.type = 'button'; bouton.dataset.taskIcon = valeur;
      bouton.append(icone(valeur), el('span', '', libelle));
      bouton.addEventListener('click', () => { icon = valeur; image.open = false; maj(); marquer(); choixIcone.focus(); }); liste.append(bouton);
    }
    champ('prompt', 'Consigne', 'textarea', '', 'Texte remis à l’agent : décrivez le travail attendu.');
    const agent = champ('agent', 'Agent', 'select', '', 'Nom court configuré plus haut ; vide : agent du texte, sinon agent par défaut.');
    for (const valeur of ['', ...etat.config.aliases.map(a => a.name)]) { const o = el('option', '', valeur ? nomAgent(valeur) : 'Agent du texte, sinon par défaut'); o.value = valeur; agent.append(o); }
    if (t.agent && !etat.config.aliases.some(a => a.name === t.agent)) { const o = el('option','',t.agent + ' (inconnu)'); o.value = t.agent; agent.append(o); }
    agent.value = t.agent || '';
    nom.addEventListener('input', maj); agent.addEventListener('change', maj);
    formulaire.append(options);
    const corpsOptions = el('div', 'ra-options-corps'); options.append(corpsOptions);
    const deuxOptions = el('div', 'ra-deux'); corpsOptions.append(deuxOptions);
    for (const [cle,titre,valeurs,defaut,texte] of [
      ['scope','Portée',[['file','Fichier affiché'],['files','Fichiers choisis'],['project','Projet entier']],'file','Fichiers confiés à l’agent, choisis au lancement.'],
      ['whenAnnotations','Annotations en cours',[['block','Refuser le lancement'],['process','Les traiter aussi']],'block','Refuser seulement si les fichiers choisis ont des annotations actives, ou demander de les traiter aussi.']]) {
      const select = champ(cle,titre,'select','',texte,deuxOptions);
      for (const [v,texte] of valeurs) { const o = el('option','',texte); o.value = v; select.append(o); } select.value = t[cle] || defaut;
    }
    const place = el('input'); place.type = 'checkbox'; place.dataset.taskField = 'bar'; place.checked = (t.place || []).includes('bar');
    const label = el('label', 'case ra-case'); label.append(place,document.createTextNode('Bouton dans la barre des vues'));
    const lignePlace = el('div', 'ra-etiquette'); lignePlace.append(label, aide('Ajoute un raccourci à droite des vues. La consigne figure toujours dans le panneau de remise.'));
    corpsOptions.append(lignePlace);
    const deuxAutres = el('div', 'ra-deux'); corpsOptions.append(deuxAutres);
    champ('workdir', 'Dossier de lancement', 'input', '', 'Vide : racine du projet ; les consignes de ce dossier seront lues.', deuxAutres);
    const max = champ('maxMinutes', 'Durée maximale en minutes', 'input', '', 'De 1 à 240 ; vide : réglage général.', deuxAutres); max.value = t.maxMinutes || ''; max.type = 'number'; max.min = 1; max.max = 240;
    champ('description', 'Description', 'input', '', 'Rappel facultatif du but de cette consigne.', corpsOptions);
    const zone = el('div', 'ra-acces'); corpsOptions.append(el('h3','','Accès supplémentaires'),el('p','aide','Ces accès élargissent les réglages généraux ; les limites de l’agent sont rappelées au lancement.'),zone); const lire = acces(zone,t.access);
    maj();
    // Freeze the generated identifier after the first save; never expose it as input.
    let identifiant = t.id || '';
    const lireTache = () => {
      const q = {}; for (const [k,v] of Object.entries(champs)) q[k] = v.value;
      if (!identifiant) {
        const base = (q.name.normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase().replace(/[^a-z0-9._-]+/g, '-').replace(/^-+|-+$/g, '') || 'consigne').slice(0,24);
        const pris = new Set([...(etat.config.tasks || []).map(t => t.id), ...[...$('ra-consignes').children].filter(b => b !== bloc).map(b => b.taskID || '')]);
        identifiant = base; let n = 2; while (pris.has(identifiant)) identifiant = base + '-' + n++;
      }
      bloc.taskID = identifiant; q.id = identifiant; q.icon = icon; q.maxMinutes = Number(q.maxMinutes) || 0;
      q.place = place.checked ? ['bar','handover'] : ['handover']; q.access = lire(); return q;
    };
    bloc.taskID = identifiant; bloc.lireTache = lireTache;
    const actions = el('div', 'ra-consigne-actions');
    const essayer = el('button','','Essayer (tour à blanc)'); essayer.type = 'button';
    essayer.addEventListener('click', () => { const q = lireTache(); const a = etat.config.aliases.find(a => a.name === (q.agent || etat.config.default)); if (a) essai({...a,task:q}); else erreur('Enregistrez un agent avant cet essai.'); });
    const retirer = el('button','','Retirer'); retirer.type = 'button'; retirer.addEventListener('click', () => { bloc.remove(); marquer(); });
    actions.append(essayer,retirer); corpsOptions.append(actions);
    bloc.addEventListener('input',marquer); bloc.addEventListener('change',marquer);
    return bloc;
  }
  let lireAcces = () => ({});
  function afficher() {
    detectes();
    lireAcces = acces($('ra-acces'), etat.config.access);
    $('ra-limites').textContent = Object.values(etat.accessWarnings || {}).flat().join(' ');

    const zone = $('ra-alias');
    zone.textContent = '';
    for (const a of etat.config.aliases) zone.append(ligne(a, a.name === etat.config.default));
    vide();
    $('ra-consignes').replaceChildren(...(etat.config.tasks || []).map(consigne));
    const p = etat.project;
    $('ra-projet').hidden = !p;
    if (p) {
      const parts = [];
      parts.push(p.agent.alias ? 'Nom court demandé par ce texte : « ' + p.agent.alias + ' »' +
        (etat.config.aliases.some((a) => a.name.toLowerCase() === p.agent.alias.toLowerCase()) ? '.' : ', inconnu sur cette machine : le nom court par défaut sera pris.')
        : 'Ce texte n’en demande pas : le nom court par défaut sera pris.');
      if (p.agent.requires.length) parts.push('Outils requis : ' + p.agent.requires.join(', ') + '.');
      if (p.agent.instructions) parts.push('Consignes propres au texte : « ' + p.agent.instructions + ' »');
      $('ra-projet-texte').textContent = (p.title ? p.title + ' — ' : '') + parts.join(' ');
    }
    erreur(etat.error || '');
    statut(etat.configured ? 'Agent par défaut : ' + etat.config.default + '.' : 'Aucun agent configuré.', etat.configured ? '' : 'attente');
    modifie = false;
  }

  // During a request, the form is frozen: its response cannot
  // erase input entered in the meantime.
  function occupe(oui) {
    for (const c of document.querySelectorAll('.ra input, .ra select, .ra button, .ra textarea')) {
      if (oui) { c.dataset.raAvant = c.disabled ? '1' : ''; c.disabled = true; } else { c.disabled = c.dataset.raAvant === '1'; delete c.dataset.raAvant; }
    }
  }

  function lire() {
    const aliases = [];
    let def = '';
    for (const d of $('ra-alias').querySelectorAll('.ra-ligne')) {
      const a = { name: d.querySelector('.ra-nom').value.trim(), agent: d.querySelector('.ra-agent').value, model: d.querySelector('.ra-modele').value.trim() };
      aliases.push(a);
      if (d.querySelector('input[type=radio]').checked) def = a.name;
    }
    return { ...etat.config, version: 1, default: def, aliases, tasks: [...$('ra-consignes').children].map(b => b.lireTache()), access: lireAcces() };
  }

  async function enregistrer() {
    if (!etat) { statut('Réglages non chargés : rechargez la page.', 'erreur'); return; }
    occupe(true);
    const r = await api('/api/agents', { ...lire(), hash: etat.hash });
    occupe(false);
    if (!r.ok) { statut(r.data.message || 'Réglages non enregistrés.', 'erreur'); return; }
    etat = r.data;
    afficher();
    statut('Enregistré. Agent par défaut : ' + (etat.config.default || 'aucun') + '.', 'ok');
  }

  async function detecter() {
    if (!etat) { statut('Réglages non chargés : rechargez la page.', 'erreur'); return; }
    statut('Recherche des agents…');
    const saisie = modifie ? lire() : null;
    const hash = etat.hash;
    occupe(true);
    const r = await api('/api/agents/detect', {});
    occupe(false);
    if (!r.ok) { statut(r.data.message || 'Recherche impossible.', 'erreur'); return; }
    etat = r.data;
    modeles.clear();
    // Unsaved input remains, with the hash of the settings
    // it was based on.
    if (saisie) { etat.config = saisie; etat.hash = hash; afficher(); marquer(); } else afficher();
  }

  // ---------- Try (P10.4) ----------
  async function essai(q) {
    if (!etat) return;
    const k = etat.detection.agents.find((a) => a.id === q.agent);
    $('ra-essai').hidden = false;
    $('ra-t-essai').textContent = 'Essai de ' + (q.name || 'cet agent') + ' (' + (k ? k.name : q.agent) + (q.model ? ', ' + q.model : '') + ')';
    $('ra-essai-etat').textContent = 'En cours… (deux minutes au plus)';
    $('ra-essai-sortie').textContent = '';
    $('ra-essai').scrollIntoView({ block: 'nearest' });
    occupe(true);
    const r = await api('/api/agents/try', q);
    occupe(false);
    if (!r.ok) { $('ra-essai-etat').textContent = r.data.message || 'Essai impossible.'; return; }
    const d = r.data;
    $('ra-essai-etat').textContent = d.error ? d.error : (d.exitCode === 0 ? 'Terminé' : 'Terminé avec le code ' + d.exitCode) + ' en ' + Math.round(d.seconds) + ' s';
    $('ra-essai-sortie').textContent = d.output || '(aucune sortie)';
  }

  // ---------- Tools required by the text (P10.4) ----------
  async function verifier() {
    const ul = $('ra-outils');
    ul.hidden = false;
    ul.textContent = '';
    ul.append(el('li', 'attente', 'Vérification… (jusqu’à une minute : l’agent interroge ses serveurs MCP)'));
    occupe(true);
    const r = await api('/api/agents/tools');
    occupe(false);
    ul.textContent = '';
    if (!r.ok) { ul.append(el('li', 'absent', r.data.message || 'Vérification impossible.')); return; }
    const a = r.data.alias;
    ul.append(el('li', 'tete', 'Agent du texte : ' + a.name + ' (' + a.agent + (a.model ? ', ' + a.model : '') + ')'));
    if (r.data.message) ul.append(el('li', 'absent', r.data.message));
    if (!r.data.checks.length) ul.append(el('li', '', 'Ce texte ne demande aucun outil.'));
    for (const c of r.data.checks) {
      const li = el('li', c.found ? 'present' : 'absent');
      li.append(el('strong', '', c.require), el('span', '', (c.found ? 'déclaré : ' : 'non trouvé : ') + c.detail));
      ul.append(li);
    }
  }

  async function demarrer() {
    window.GloseEspace.brancher(jeton);
    const r = await api('/api/agents');
    if (!r.ok) {
      erreur(r.status === 401 ? 'Session absente ou périmée : relancez « glose set » et ouvrez l’adresse qu’il affiche.' : (r.data.message || 'Réglages illisibles.'));
      return;
    }
    etat = r.data;
    afficher();
    // The settings server stops when the page no longer polls it.
    setInterval(() => api('/api/ping'), 10000);
  }

  $('ra-ajouter').addEventListener('click', () => {
    if (!etat) return;
    const premier = !$('ra-alias').querySelector('.ra-ligne');
    const d = ligne({}, premier);
    $('ra-alias').append(d);
    vide(); marquer();
    d.querySelector('.ra-nom').focus();
  });
  $('ra-ajouter-consigne').addEventListener('click', () => { if (!etat) return; const b = consigne(); $('ra-consignes').append(b); marquer(); b.querySelector('input').focus(); });
  $('ra-enregistrer').addEventListener('click', enregistrer);
  $('ra-detecter').addEventListener('click', detecter);
  $('ra-verifier').addEventListener('click', verifier);
  window.addEventListener('beforeunload', (e) => { if (modifie) { e.preventDefault(); e.returnValue = ''; } });
  demarrer();
})();
