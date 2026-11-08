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
  function marquer() { modifie = true; statut('Modifications non enregistrées.', 'attente'); }

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

  function afficher() {
    detectes();
    const zone = $('ra-alias');
    zone.textContent = '';
    for (const a of etat.config.aliases) zone.append(ligne(a, a.name === etat.config.default));
    vide();
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
    for (const c of document.querySelectorAll('.ra input, .ra select, .ra button')) {
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
    return { version: 1, default: def, aliases };
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
  $('ra-enregistrer').addEventListener('click', enregistrer);
  $('ra-detecter').addEventListener('click', detecter);
  $('ra-verifier').addEventListener('click', verifier);
  window.addEventListener('beforeunload', (e) => { if (modifie) { e.preventDefault(); e.returnValue = ''; } });
  demarrer();
})();
