"use strict";
// Agents (P10): the shuttle banner menu and linking an agent
// session to this manually opened Glose (P10.2). Linking provides
// a ready-to-paste instruction with a short code; the banner and this panel
// show the agent as soon as it gives a sign of life (glose join).
window.GloseAgents = (() => {
  function creer({ api, etat, avis, jeton, doc, recharger }) {
    const $ = (id) => document.getElementById(id);
    const bouton = $('menu-navette-bouton'), menu = $('menu-navette');
    const d = $('lien'), consigne = $('lien-consigne'), statut = $('lien-etat');
    let lien = null, minuteur = 0;

    // ---------- Banner menu ----------
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

    // ---------- Link an agent (P10.2) ----------
    function restant() {
      if (!lien) return '';
      const s = Math.max(0, Math.round((new Date(lien.expires) - Date.now()) / 1000));
      return Math.floor(s / 60) + ':' + String(s % 60).padStart(2, '0');
    }
    // Linked: the agent gave this code to glose join (not another sign of life).
    // Three distinct states (Codex audit): a code taken by a session
    // (link.json) proves the link; fresh presence says whether it is actually
    // waiting or leading the turn; otherwise, its command is no longer running.
    function majStatut() {
      if (!d.open || !lien) return;
      const D = doc(), l = D && D.link, st = etat(), a = st && st.agent;
      if (l && l.used && l.code === lien.code) {
        $('lien-reste').textContent = '';
        const n = l.used === 'agent' ? "L'agent" : l.used;
        // Its command is running: a fresh sign from this waiting session
        // (glose join renews "wait" every 10 s).
        const sienne = a && a.name === l.used && a.fresh;
        const attend = sienne && (a.activity === 'wait' || a.activity === 'joined');
        // Does the current agent turn belong to this link?
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
      // The countdown is outside the announced region (screen
      // readers): only state changes are in it.
      const attente = 'Code ' + lien.code + ' · en attente de l’agent';
      if (statut.textContent !== attente) { statut.className = 'lien-etat'; statut.textContent = attente; }
      $('lien-reste').textContent = '· expire dans ' + restant();
    }
    async function nouveauCode() {
      statut.className = 'lien-etat';
      statut.textContent = 'Création du code…';
      const r = await api('/api/link', {});
      if (!d.open) return; // closed during creation: no timer
      if (!r.ok) { statut.className = 'lien-etat expire'; statut.textContent = r.data.message || 'Code non créé.'; return; }
      lien = r.data;
      consigne.value = lien.instruction;
      // An agent launched by Glose is working (P10.9): the linked session will
      // take the turn at the next handoff, never at the same time as that agent.
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

    // ---------- Configure (P10.3): the glose set page, in a tab ----------
    function configurer() {
      // Without "noopener", which always makes it return null: the opener
      // is removed manually (same origin).
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

    // ---------- The agent launched by Glose (P10.5) ----------
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

    // configure: a default agent is recorded (glose set).
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

    // ---------- Annotation recipient (P10.6, D-49) ----------
    // "Réponse par": the short name of the agent that will reply; if empty,
    // the text's agent (or the default). Shown as soon as there are two short names.
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
    // The selected recipient, or "@Nom" at the start of the text (removed from it),
    // recognized only if that short name exists.
    function destinataire(type, texte) {
      const D = doc();
      const noms = (D && D.aliases) || [];
      let pour = $(type + '-pour').value;
      const m = /^@([A-Za-z0-9._-]{1,32})\s+/.exec(texte || '');
      if (m) {
        const n = noms.find((x) => x.toLowerCase() === m[1].toLowerCase());
        if (n) { texte = texte.slice(m[0].length); pour = n; } // the typed @Nom takes precedence over the list
      }
      return { pour, texte };
    }

    return { preparerDestinataire, destinataire, ouvrirLien, ouvert: () => d.open || !menu.hidden || sortie.open, maj, boutonConfigurer, configurer, voirSortie };
  }
  return { creer };
})();
