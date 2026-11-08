"use strict";
// Agents (lot P10) : le menu de la capsule de la navette et la liaison d'une
// session d'agent à ce Glose ouvert à la main (P10.2). La liaison donne une
// consigne prête à coller, avec un code court ; la capsule et ce panneau
// montrent l'agent dès qu'il a donné signe de vie (glose join).
window.GloseAgents = (() => {
  function creer({ api, etat, avis, jeton, doc, recharger }) {
    const $ = (id) => document.getElementById(id);
    const bouton = $('menu-navette-bouton'), menu = $('menu-navette');
    const d = $('lien'), consigne = $('lien-consigne'), statut = $('lien-etat');
    let lien = null, minuteur = 0;

    // ---------- Menu de la capsule ----------
    function ouvrirMenu(ouvert) {
      menu.hidden = !ouvert;
      bouton.setAttribute('aria-expanded', String(ouvert));
      if (ouvert) { const premier = menu.querySelector('button:not([hidden])'); if (premier) premier.focus(); }
    }
    bouton.addEventListener('click', () => ouvrirMenu(menu.hidden));
    bouton.addEventListener('keydown', (e) => { if (e.key === 'Escape' && !menu.hidden) { e.preventDefault(); ouvrirMenu(false); } });
    menu.addEventListener('keydown', (e) => {
      const items = [...menu.querySelectorAll('button:not([hidden])')];
      const i = items.indexOf(document.activeElement);
      if (e.key === 'Escape') { e.preventDefault(); ouvrirMenu(false); bouton.focus(); }
      else if (e.key === 'ArrowDown' && items.length) { e.preventDefault(); items[(i + 1) % items.length].focus(); }
      else if (e.key === 'ArrowUp' && items.length) { e.preventDefault(); items[(i - 1 + items.length) % items.length].focus(); }
    });
    document.addEventListener('click', (e) => {
      if (!menu.hidden && !menu.contains(e.target) && !bouton.contains(e.target)) ouvrirMenu(false);
    });
    menu.addEventListener('click', (e) => { if (e.target.closest('button')) ouvrirMenu(false); });

    // ---------- Lier un agent (P10.2) ----------
    function restant() {
      if (!lien) return '';
      const s = Math.max(0, Math.round((new Date(lien.expires) - Date.now()) / 1000));
      return Math.floor(s / 60) + ':' + String(s % 60).padStart(2, '0');
    }
    // Lié : l'agent a donné ce code à glose join (et non un autre signe).
    // Trois états distincts (audit de Codex) : le code pris par une session
    // (link.json) prouve la liaison ; sa présence fraîche dit si elle attend
    // vraiment ou mène le tour ; sinon, sa commande ne tourne plus.
    function majStatut() {
      if (!d.open || !lien) return;
      const D = doc(), l = D && D.link, st = etat(), a = st && st.agent;
      if (l && l.used && l.code === lien.code) {
        $('lien-reste').textContent = '';
        const n = l.used === 'agent' ? "L'agent" : l.used;
        // Sa commande tourne : un signe frais de cette session, en attente
        // (glose join renouvelle « wait » toutes les 10 s).
        const sienne = a && a.name === l.used && a.fresh;
        const attend = sienne && (a.activity === 'wait' || a.activity === 'joined');
        // Le tour de l'agent en cours appartient-il à cette liaison ?
        const o = D.owner, mene = st.hand === 'agent' && o && o.kind === 'link' && o.id === lien.code.replace(/-/g, '');
        const autre = st.hand === 'agent' && !mene && ((D.run && D.run.running) || (o && o.kind === 'launch'));
        let texte, classe = 'lien-etat lie';
        if (mene && sienne) texte = n + ' est lié et mène le tour.';
        else if (autre && attend) texte = n + ' est lié : il prendra la main à la remise suivante (' + ((D.run && D.run.alias) || o.name) + ' mène ce tour).';
        else if (attend) texte = n + ' est lié et attend votre remise.';
        else { texte = n + ' est lié, mais sa commande ne tourne pas : demandez-lui de relancer « ' + lien.command + ' ».'; classe = 'lien-etat expire'; }
        if (statut.textContent !== texte) { statut.className = classe; statut.textContent = texte; }
        return;
      }
      if (new Date(lien.expires) <= Date.now()) {
        $('lien-reste').textContent = '';
        statut.className = 'lien-etat expire';
        statut.textContent = 'Code ' + lien.code + ' expiré : demandez-en un nouveau.';
        clearInterval(minuteur); minuteur = 0;
        return;
      }
      // Le compte à rebours n'est pas dans la région annoncée (lecteurs
      // d'écran) : seuls les changements d'état le sont.
      const attente = 'Code ' + lien.code + ' · en attente de l’agent';
      if (statut.textContent !== attente) { statut.className = 'lien-etat'; statut.textContent = attente; }
      $('lien-reste').textContent = '· expire dans ' + restant();
    }
    async function nouveauCode() {
      statut.className = 'lien-etat';
      statut.textContent = 'Création du code…';
      const r = await api('/api/link', {});
      if (!d.open) return; // fermé pendant la création : pas de minuteur
      if (!r.ok) { statut.className = 'lien-etat expire'; statut.textContent = r.data.message || 'Code non créé.'; return; }
      lien = r.data;
      consigne.value = lien.instruction;
      // Un agent lancé par Glose travaille (P10.9) : la session liée prendra
      // la main à la remise suivante, jamais en même temps que lui.
      $('lien-occupe').hidden = !lien.busy;
      $('lien-occupe').textContent = lien.busy ? lien.busy + ' (lancé par Glose) mène le tour en cours : la session que vous liez attendra votre remise suivante.' : '';
      clearInterval(minuteur);
      minuteur = setInterval(majStatut, 1000);
      majStatut();
    }
    async function ouvrirLien() {
      if (d.open) return;
      if (typeof d.showModal === 'function') d.showModal(); else d.setAttribute('open', '');
      consigne.value = '';
      await nouveauCode();
      consigne.focus();
      consigne.select();
    }
    async function copier() {
      consigne.focus();
      consigne.select();
      let ok = false;
      try { await navigator.clipboard.writeText(consigne.value); ok = true; } catch (_) {
        try { ok = document.execCommand('copy'); } catch (_) { ok = false; }
      }
      avis(ok ? 'Consigne copiée : collez-la dans la session de votre agent.' : 'Copie impossible : sélectionnez la consigne et copiez-la (Ctrl+C).', !ok);
    }
    $('lien-copier').addEventListener('click', copier);
    $('lien-nouveau').addEventListener('click', nouveauCode);
    d.addEventListener('close', () => { clearInterval(minuteur); minuteur = 0; lien = null; });
    $('m-lier').addEventListener('click', ouvrirLien);

    // ---------- Configurer (P10.3) : la page de glose set, dans un onglet ----------
    function configurer() {
      // Sans « noopener », qui fait toujours rendre null : l'ouvreur est
      // retiré à la main (même origine).
      const w = window.open('/set#t=' + jeton(), '_blank');
      if (w) w.opener = null;
      else avis('Fenêtre bloquée : ouvrez les réglages des agents par « glose set ».', true);
    }
    function boutonConfigurer() {
      const b = document.createElement('button');
      b.type = 'button';
      b.className = 'lien-configurer';
      b.textContent = 'Configurer';
      b.addEventListener('click', configurer);
      return b;
    }
    $('m-configurer').addEventListener('click', configurer);
    $('configurer-agents').addEventListener('click', configurer);

    // ---------- L'agent lancé par Glose (P10.5) ----------
    const nomAlias = (a) => a.name + ' (' + ({ claude: 'Claude Code', codex: 'Codex', opencode: 'OpenCode' }[a.agent] || a.agent) + (a.model ? ', ' + a.model : '') + ')';
    const sortie = $('sortie');
    let minuteurSortie = 0;
    function etatRun(run) {
      if (!run) return '';
      if (run.running) return (run.orphan ? 'Lancé par un serveur précédent, ' : 'Lancé ') + (run.started ? quandCourt(run.started) : '') + ', processus ' + run.pid + '.';
      const fin = run.ended ? ' le ' + new Date(run.ended).toLocaleString('fr-FR') : '';
      if (run.timedOut) return 'Arrêté à la durée maximale' + fin + '.';
      if (run.stopped) return 'Arrêté par vous' + fin + '.';
      return 'Terminé' + fin + (run.exitCode ? ' avec le code ' + run.exitCode : '') + (run.error ? ' : ' + run.error : '') + '.';
    }
    function quandCourt(t) { return 'à ' + new Date(t).toLocaleTimeString('fr-FR', { hour: '2-digit', minute: '2-digit' }); }
    async function majSortie() {
      const r = await api('/api/agent/log');
      if (!sortie.open) return;
      if (!r.ok) { $('sortie-etat').textContent = r.data.message || 'Sortie indisponible.'; return; }
      const pre = $('sortie-texte');
      const enBas = pre.scrollTop + pre.clientHeight >= pre.scrollHeight - 24;
      pre.textContent = r.data.text || '(rien encore)';
      if (enBas) pre.scrollTop = pre.scrollHeight;
      $('sortie-titre').textContent = 'Ce que fait ' + (r.data.run.alias || 'l\u2019agent');
      const etatTexte = etatRun(r.data.run) + ' Sortie : .glose/' + r.data.run.log;
      if ($('sortie-etat').textContent !== etatTexte) $('sortie-etat').textContent = etatTexte;
      $('sortie-arreter').hidden = !r.data.run.running;
    }
    function voirSortie() {
      if (!sortie.open) { if (typeof sortie.showModal === 'function') sortie.showModal(); else sortie.setAttribute('open', ''); }
      majSortie();
      clearInterval(minuteurSortie);
      minuteurSortie = setInterval(majSortie, 2000);
    }
    sortie.addEventListener('close', () => { clearInterval(minuteurSortie); minuteurSortie = 0; });
    async function arreter() {
      const r = await api('/api/agent/stop', {});
      avis(r.ok ? 'Agent arrêté. La main reste à l\u2019agent : reprenez-la, ou relancez-le.' : (r.data.message || 'Arrêt impossible.'), !r.ok);
      if (sortie.open) majSortie();
      recharger();
    }
    async function lancer() {
      const r = await api('/api/agent/launch', {});
      avis(r.ok ? 'Agent lancé.' : 'Agent non lancé : ' + (r.data.message || 'erreur'), !r.ok);
      recharger();
    }
    $('m-sortie').addEventListener('click', voirSortie);
    $('m-arreter').addEventListener('click', arreter);
    $('sortie-arreter').addEventListener('click', arreter);
    $('m-lancer').addEventListener('click', lancer);

    // La case « Lancer » de la boîte de remise, et les outils requis.
    let verif = 0;
    // Le choix de l'auteur sur « Lancer » est retenu d'une ouverture à
    // l'autre, tant que la présence d'un agent ne change pas de sens.
    let choixLancer = null, presenceVue = null;
    $('remise-lancer').addEventListener('change', () => { choixLancer = $('remise-lancer').checked; });
    function preparerRemise(present) {
      if (presenceVue !== present) { choixLancer = null; presenceVue = present; }
      const D = doc();
      const zone = $('remise-lancement');
      const a = D && D.textAgent;
      zone.hidden = !a;
      $('remise-outils').hidden = true;
      if (!a) return;
      const occupe = D.run && D.run.running;
      $('remise-lancer-texte').textContent = occupe ? 'Un agent lancé par Glose travaille déjà : pas de second lancement'
        : 'Lancer ' + nomAlias(a) + ' à la remise' + (present ? ' (un agent écoute déjà)' : '');
      $('remise-lancer').checked = !occupe && (choixLancer === null ? !present : choixLancer);
      $('remise-lancer').disabled = !!occupe;
      const req = D.textRequires || [];
      if (!req.length || occupe) return;
      const ul = $('remise-outils');
      ul.hidden = false;
      ul.replaceChildren(Object.assign(document.createElement('li'), { textContent: 'Vérification des outils requis (' + req.join(', ') + ')…' }));
      const n = ++verif;
      api('/api/agents/tools').then((r) => {
        if (n !== verif) return;
        ul.textContent = '';
        if (!r.ok) { ul.append(Object.assign(document.createElement('li'), { textContent: r.data.message || 'Outils non vérifiés.' })); return; }
        const manque = (r.data.checks || []).filter((c) => !c.found);
        const li = document.createElement('li');
        li.className = manque.length ? 'manque' : 'ok';
        li.textContent = manque.length ? 'Outils non trouvés chez ' + r.data.alias.name + ' : ' + manque.map((c) => c.require).join(', ') +
          '. Le lancement reste possible ; l\u2019agent ne pourra peut-être pas suivre toutes les consignes.' : 'Outils requis déclarés par ' + r.data.alias.name + '.';
        ul.append(li);
      });
    }
    const lancerARemise = () => !$('remise-lancement').hidden && $('remise-lancer').checked && !$('remise-lancer').disabled;
    const choixLancement = () => ($('remise-lancement').hidden ? undefined : lancerARemise());

    // configure : un agent par défaut est enregistré (glose set).
    function maj(configure) {
      if (configure !== undefined) {
        $('configurer-agents').hidden = configure;
        $('m-configurer').classList.toggle('a-faire', !configure);
      }
      const D = doc(), st = D && D.state, run = D && D.run;
      $('m-sortie').hidden = !run;
      $('m-arreter').hidden = !(run && run.running);
      $('m-lancer').hidden = !(st && st.hand === 'agent' && D.textAgent && !(run && run.running));
      if (D && D.textAgent) $('m-lancer-texte').textContent = 'Lancer ' + D.textAgent.name;
      majStatut();
    }

    // ---------- Destinataire d'une annotation (P10.6, D-49) ----------
    // « Réponse par » : le nom court de l'agent qui répondra ; vide, celui
    // du texte (ou par défaut). Montré dès qu'il y a deux noms courts.
    function preparerDestinataire(type, choisi) {
      const D = doc();
      const noms = (D && D.aliases) || [];
      const select = $(type + '-pour');
      select.textContent = '';
      const def = D && D.textAgent ? D.textAgent.name : '';
      const o0 = document.createElement('option');
      o0.value = '';
      o0.textContent = def ? 'Par défaut (' + def + ')' : 'Par défaut';
      select.append(o0);
      for (const n of noms) {
        const o = document.createElement('option');
        o.value = n; o.textContent = n;
        select.append(o);
      }
      select.value = choisi && noms.includes(choisi) ? choisi : '';
      $(type + '-pour-ligne').hidden = noms.length < 2;
    }
    // Le destinataire choisi, ou « @Nom » en tête du texte (retiré du texte),
    // reconnu seulement si ce nom court existe.
    function destinataire(type, texte) {
      const D = doc();
      const noms = (D && D.aliases) || [];
      let pour = $(type + '-pour').value;
      const m = /^@([A-Za-z0-9._-]{1,32})\s+/.exec(texte || '');
      if (m) {
        const n = noms.find((x) => x.toLowerCase() === m[1].toLowerCase());
        if (n) { texte = texte.slice(m[0].length); pour = n; } // le @Nom écrit prime sur la liste
      }
      return { pour, texte };
    }

    return { preparerDestinataire, destinataire, ouvrirLien, ouvert: () => d.open || !menu.hidden || sortie.open, maj, boutonConfigurer, configurer, preparerRemise, choixLancement, voirSortie };
  }
  return { creer };
})();
