"use strict";
// Turn display, agent presence and handoff/reclaim dialog (P8.3).
window.GloseNavette = (() => {
  function creer({ $, S, api, avis, el, icone, court, quand, agents, majBoutons, charger, saisieEnCours, voirAnnotation }) {
    const { modeleCourt } = GloseDonnees;
    const NOMS_PUCES = { comments: "commentaire", proposals: "suggestion", replies: "réponse" };
    const nomAlias = (a) => a.name + ' (' + ({ claude: 'Claude Code', codex: 'Codex', opencode: 'OpenCode' }[a.agent] || a.agent) + (a.model ? ', ' + a.model : '') + ')';
    // Launch checkbox in the handoff dialog, and required tools.
    let verif = 0;
    // Keep the author's launch choice between openings,
    // until agent presence changes meaning.
    let choixLancer = null, presenceVue = null;
    $('remise-lancer').addEventListener('change', () => { choixLancer = $('remise-lancer').checked; });
    function preparerRemise(present) {
      if (presenceVue !== present) { choixLancer = null; presenceVue = present; }
      const D = S.doc;
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

    let apercuConsigne = null, generationConsigne = 0;
    const tacheChoisie = () => (S.doc.tasks || []).find(t => t.id === $('remise-consigne').value);
    function barreConsignes() {
      const zone = $('consignes-barre'); zone.textContent = '';
      for (const t of S.doc.tasks || []) {
        if (!(t.place || []).includes('bar')) continue;
        // P20: icon and name, the accent of the actions that launch an agent.
        const b = el('button', ''); b.type = 'button'; b.dataset.task = t.id;
        b.dataset.infobulle = 'Lancer la consigne « ' + t.name + ' »'; b.setAttribute('aria-label', t.name);
        b.innerHTML = icone(t.icon); b.append(el('span', '', t.name)); b.disabled = S.doc.state.hand === 'agent';
        b.addEventListener('click', () => ouvrirRemise(t.id)); zone.append(b);
      }
    }
    async function apercuTache() {
      const n = ++generationConsigne; apercuConsigne = null;
      $('remise-blocages').textContent = '';
      const t = tacheChoisie();
      $('remise-lancement').hidden = !!t || !(S.doc && S.doc.textAgent);
      $('remise').classList.toggle('lancement', !!t);
      if (!t) { $('remise-titre').textContent = 'Remettre la main à l’agent'; $('remise-ok').textContent = 'Remettre'; $('remise-acces').textContent = ''; $('remise-ok').disabled = false; return; }
      $('remise-ok').disabled = true;
      const files = [...$('remise-fichiers').querySelectorAll('input:checked')].map(i => i.value);
      const params = new URLSearchParams({id:t.id}); for (const f of files) params.append('file',f);
      $('remise-acces').textContent = 'Vérification des accès…';
      const r = await api('/api/tasks/preview?' + params);
      if (n !== generationConsigne) return;
      if (!r.ok) { $('remise-acces').textContent = r.data.message || 'Consigne indisponible.'; return; }
      apercuConsigne = r.data;
      $('remise-ok').disabled = !!(r.data.blocks || []).length;
      $('remise-blocages').textContent = '';
      // P20 (docs/maquettes/p20/lancement.html): what blocks the launch is
      // said before the click, in a notice, with the way to the annotation.
      for (const b of r.data.blocks || []) {
        const encart = el('div', 'remise-encart'), tete = el('p', 'remise-encart-tete');
        tete.innerHTML = icone('alerte'); tete.append(el('strong', '', 'Lancement en attente'));
        const lien = el('button', 'lien', 'Aller à la première annotation'); lien.type = 'button';
        lien.addEventListener('click', () => { $('remise').close(); S.remise = null; voirAnnotation(b); });
        encart.append(tete, el('p', '', b.message), lien); $('remise-blocages').append(encart);
      }
      const q = r.data.task, a = q.access || {};
      const titre = $('remise-titre'), tuile = el('span', 'remise-tuile'), nomT = el('span', 'remise-nom');
      tuile.innerHTML = icone(t.icon); nomT.append(el('span', '', t.name));
      if (t.description) nomT.append(el('small', '', t.description));
      titre.replaceChildren(tuile, nomT);
      $('remise-ok').textContent = 'Lancer'; $('remise-ok').insertAdjacentHTML('afterbegin', icone('essayer'));
      const zone = $('remise-acces'); zone.textContent = '';
      const meta = el('dl', 'remise-meta');
      for (const [cle, valeur] of [['Agent', nomAlias(q.agent)], [q.files.length > 1 ? 'Fichiers' : 'Fichier', q.files.join(', ')]]) meta.append(el('dt', '', cle), el('dd', '', valeur));
      zone.append(meta, el('h3', '', 'Accès accordés'));
      const liste = el('ul', 'remise-droits');
      // Each access with its answer: a check when granted, a cross otherwise.
      const droit = (oui, nom, valeur) => {
        const li = el('li', oui ? 'accorde' : ''); li.innerHTML = icone(oui ? 'resoudre' : 'rejeter');
        const texte = el('span'); texte.append(el('strong', '', nom + ' : '), document.createTextNode(valeur));
        li.append(texte); liste.append(li);
      };
      droit(true, 'Lire', a.externalDirs === 'all' ? 'tous les dossiers (déconseillé)' : a.externalDirs === 'list' ? ['le projet', ...(a.dirs || [])].join(', ') : 'le projet');
      droit(!!a.externalWrite, 'Écrire hors du projet', a.externalWrite ? (a.dirs || []).join(', ') || 'dossiers listés' : 'non');
      droit(!!a.web, 'Web', a.web ? 'oui' : 'non');
      droit(a.scripts === 'python' || a.scripts === 'shell', 'Scripts', ({python:'Python', shell:'tout le shell'}[a.scripts] || 'non'));
      droit(!!a.subagents, 'Sous-agents', a.subagents ? 'oui' : 'non');
      droit(!!(a.mcp || []).length, 'MCP', (a.mcp || []).join(', ') || 'aucun');
      zone.append(liste);
      const details = el('details'), resume = el('summary'); resume.innerHTML = icone('a-chevron'); resume.append('Détails'); details.append(resume);
      details.append(el('p', '', 'Dossier : ' + q.workdir), el('p', '', 'Durée maximale : ' + q.maxMinutes + ' min'));
      for (const warning of q.warnings || []) details.append(el('p', '', warning));
      details.append(el('p', '', 'Pour changer ces accès, modifiez la consigne dans glose set.')); zone.append(details);

    }
    function fichiersTache() {
      const t = tacheChoisie(), zone = $('remise-fichiers'); zone.textContent = '';
      if (!t) { apercuTache(); return; }
      const scope = t.scope || 'file';
      const visibles = [...document.querySelectorAll('#texte .glose-file')];
      const courant = visibles.find(e => e.getBoundingClientRect().bottom > 160) || visibles[0];
      const courantPath = courant && courant.dataset.path;
      if (scope !== 'project') {
        zone.append(el('p','aide',scope === 'file' ? 'Fichier affiché (vérifiez la sélection) :' : 'Choisissez les fichiers :'));
        for (const f of S.doc.files) {
          const label = el('label','case'), input = el('input');
          input.type = scope === 'file' ? 'radio' : 'checkbox'; input.name = 'consigne-fichier'; input.value = f.path;
          input.checked = f.path === courantPath || (!courantPath && f === S.doc.files[0]);
          input.addEventListener('change',apercuTache); label.append(input,document.createTextNode(f.path)); zone.append(label);
        }
      }
      apercuTache();
    }
    function preparerConsignes(id, reprise) {
      $('remise-consignes').hidden = reprise; apercuConsigne = null; ++generationConsigne;
      const select = $('remise-consigne'); select.textContent = '';
      const aucun = el('option','','Aucune (remise habituelle)'); aucun.value = ''; select.append(aucun);
      for (const t of S.doc.tasks || []) {
        const o = el('option','',t.name); o.value = t.id; select.append(o);
      }
      select.value = id || '';
      if (!reprise) fichiersTache();
    }
    $('remise-consigne').addEventListener('change',fichiersTache);

    // ---------- Turn ownership and handoff ----------
    function majNavette() {
      const st = S.doc.state;
      barreConsignes();
      const zone = $("navette");
      zone.textContent = "";
      if (!st) return;
      const agent = st.hand === "agent";
      document.body.classList.toggle("main-agent", agent);
      $("carte-tour").classList.toggle("agent", agent);
      zone.className = agent ? "agent" : "";
      const tour = "Tour " + (st.round || st.turn);
      const depuis = st.since ? new Date(st.since) : null;
      // What will be handed over (P9.3): comments, proposals and replies from this turn.
      // During the agent turn: what it has written since handoff.
      const n = st.thisTurn || {};
      const detail = [];
      for (const [cle, un, plusieurs] of [["comments", "commentaire", "commentaires"],
        ["proposals", "suggestion", "suggestions"], ["replies", "réponse", "réponses"]]) {
        if (n[cle]) detail.push(n[cle] + " " + (n[cle] > 1 ? plusieurs : un));
      }
      // Agent presence (P10.1): who is listening, has taken the turn, or is writing.
      const pres = presenceAgent(st);
      $("carte-tour").classList.toggle("absent", !pres.present);
      let fort = tour + " · à vous";
      if (agent) {
        fort = !pres.present ? pres.libelle
          : !pres.pris ? pres.nom + " va prendre la main"
          : (detail.length || (st.agent && st.agent.progress)) ? pres.nom + " travaille" + (pres.etape || "") + (detail.length ? " · " + detail.join(", ") : "") : pres.lance ? pres.nom + " travaille" + (pres.etape || "") : pres.nom + " a pris la main";
      }
      if (agent && st.task && pres.present) fort = st.task.agent.name + ' travaille · « ' + st.task.name + ' »';
      zone.append(el("strong", "", fort));
      const a = st.agent;
      const avancement = agent && pres.present && a && a.fresh && a.taken && a.progress
        ? a.progress + ' · ' + new Date(a.progressAt).toLocaleTimeString('fr-FR', {hour:'2-digit',minute:'2-digit'}) : '';
      if (avancement) zone.append(el('small', 'agent-avancement', avancement));
      for (const cible of document.querySelectorAll('.tour-avancement')) { cible.textContent = avancement; cible.hidden = !avancement; }

      const petit = [];
      if (!agent) petit.push(pres.present ? pres.nom + " attend" : pres.libelle);
      if (depuis) petit.push((st.reclaimed ? "Reprise " : agent ? "Remise " : "Rendue ") + quand(depuis));
      if (petit.length) {
        const sm = el("small", "", petit.join(" · "));
        if (!agent && !pres.present) sm.classList.add("absent");
        zone.append(sm);
      }
      if (st.problems && st.problems.length) {
        const b = el("span", "alerte");
        b.innerHTML = icone("alerte"); // constant icon
        b.append(String(st.problems.length));
        zone.append(b);
      }
      const puces = $("puces");
      puces.textContent = "";
      for (const [cle, ic] of [["comments", "commenter"], ["proposals", "suggerer"], ["replies", "repondre"]]) {
        if (!n[cle]) continue;
        const p = el("span", "puce " + cle);
        p.innerHTML = icone(ic); // constant icon
        p.append(el("b", "", String(n[cle])), " " + NOMS_PUCES[cle] + (n[cle] > 1 ? "s" : ""));
        puces.append(p);
      }
      puces.hidden = agent || !detail.length;
      const nomMain = agent ? ((S.doc.run && S.doc.run.running && S.doc.run.alias) || (st.agent && st.agent.name) || "Agent") : "Auteur";
      $("main-navette").textContent = "Main : " + nomMain;
      $("carte-tour").querySelector(".jeton").dataset.infobulle = $("main-navette").textContent + " · " + zone.textContent;
      $("remettre-libelle").textContent = agent ? "Reprendre la main" : "Remettre à l'agent";
      $("remettre-court").textContent = agent ? "Reprendre" : "Remettre";
      const bt = $("remettre");
      bt.hidden = false;
      const resume = detail.length ? detail.join(", ") : "aucune annotation pour l'instant";
      bt.setAttribute("aria-label", agent ? "L'agent a la main. Reprendre la main sans attendre sa remise"
        : tour + ", à vous. Remettre la main à l'agent : " + resume);
      bt.dataset.infobulle = (agent ? "La main est à l'agent : vous lisez. Cliquer pour reprendre la main (ses modifications deviennent des propositions)"
        : "Remettre la main à l'agent, avec une note facultative.\nCe tour : " + resume) +
        "\n" + pres.infobulle +
        (depuis ? "\n" + (st.reclaimed ? "Main reprise le " : "Remis par " + (st.lastActor === "agent" ? "l'agent" : "vous") + " le ") +
          depuis.toLocaleString("fr-FR") + (st.note ? " : « " + court(st.note, 160) + " »" : "") : "") +
        (st.problems && st.problems.length ? "\n" + st.problems.join("\n") : "");
      if (S.dernierEtat !== undefined && S.dernierEtat !== st.hand) $("navette-annonce").textContent = agent ? "La main est à l'agent." : "La main vous revient.";
      if (agent && S.dernierEtat !== "agent" && S.dernierEtat !== undefined) avis("Tour remis à l'agent.");
      if (!agent && S.dernierEtat === "agent") avis("L'agent vous a remis la main" + (st.note ? " : « " + court(st.note, 120) + " »" : "."));
      S.dernierEtat = st.hand;
      agents.maj(S.doc.agentsConfigured !== false);
    }

    // Agent presence as shown by the relay banner (P10.1). The server
    // computes freshness: 60 s while waiting, 30 min after taking the
    // turn (an agent may think or write without invoking glose).
    // Short agent model (P10.11): « claude-agent/opus#high » → « opus ».
    function presenceAgent(st) {
      const a = st && st.agent;
      const run = S.doc && S.doc.run;
      const modele = a && a.model ? " · " + modeleCourt(a.model) : run && run.model ? " · " + modeleCourt(run.model) : "";
      const nom = (a && a.name && a.name !== "agent" ? a.name : run && run.alias ? run.alias : "L'agent") + modele;
      // An agent launched by Glose (P10.5) works while its process lives;
      // even without invoking glose; the relay banner reports a stop without handoff.
      if (st && st.hand === "agent" && run && run.turn === st.turn) {
        const n = run.alias ? run.alias + (run.model ? " · " + modeleCourt(run.model) : "") : nom;
        // Multiple recipients (P10.6): the current pass.
        const ps = S.doc.passes && S.doc.passes.turn === st.turn ? S.doc.passes.passes : null;
        const k = ps ? ps.findIndex((x) => x.state === "running") : -1;
        const etape = ps && ps.length > 1 && k >= 0 ? " · passage " + (k + 1) + "/" + ps.length : "";
        if (run.running) return { present: true, pris: true, nom: n, configure: true, lance: true, etape,
          infobulle: n + " travaille (lancé par Glose)" + (ps ? " ; passages : " + ps.map((x) => x.alias + " (" + x.count + ")").join(", ") : "") + ". Menu : voir ce qu'il fait, l'arrêter." };
        // Between passes, Glose continues: nothing has stopped.
        if (ps && ps.some((x) => x.state === "waiting")) return { present: true, pris: true, nom: "Glose", configure: true, lance: true, etape: " · passage suivant",
          infobulle: "Glose lance l'agent du passage suivant." };
        // Finished: only a sign after its end (another agent) means work continues.
        if (!a || !a.fresh || !a.taken || !run.ended || new Date(a.seen) <= new Date(run.ended)) {
          return { present: false, pris: false, nom: n, configure: true, arrete: true, libelle: n + " s'est arrêté sans remettre",
            infobulle: n + " s'est arrêté sans remettre la main. Menu : voir ce qu'il a fait, le relancer ; ou reprenez la main." };
        }
      }
      // During your turn, only a waiting agent (glose wait, or just
      // linked) listens: after « done », it may have left.
      if (!a || !a.fresh || (st.hand !== "agent" && !a.listening)) {
        // A linked session that does not restart its command (P10.12) is
        // linked, but nobody is waiting for handoff.
        const lien = S.doc && S.doc.link;
        if (a && a.link && lien && lien.used === a.name && lien.code.replace(/-/g, "") === a.link) {
          return { present: false, pris: false, nom, configure: true, lie: true,
            libelle: nom + (st.hand === "agent" ? " (lié) n'a pas pris la main" : " est lié mais n'attend pas"),
            infobulle: nom + " est lié, mais n'attend plus (dernier signe " + quand(new Date(a.seen)) + ") : dans sa session, faites-lui relancer " +
              "« glose join " + lien.code + " --agent \"" + a.name + "\" », ou reprenez la main." };
        }
        // Until glose set has been run, Glose reports it and offers setup (P10.3).
        const configure = !S.doc || S.doc.agentsConfigured !== false;
        return { present: false, pris: false, nom, configure,
          libelle: configure ? "Aucun agent à l'écoute" : "Aucun agent configuré",
          infobulle: (configure ? "Aucun agent à l'écoute" : "Aucun agent configuré (glose set)") +
            (a ? " (dernier signe de " + nom + " " + quand(new Date(a.seen)) + ")" : "") +
            " : lancez ou liez un agent (menu du bandeau), ou reprenez la main." };
      }
      const pris = st.hand === "agent" && a.taken;
      return { present: true, pris, nom, configure: true,
        infobulle: nom + (pris ? " a pris la main" : st.hand === "agent" ? " va prendre la main" : " attend votre remise") +
          " (dernier signe " + quand(new Date(a.seen)) + ")." };
    }

    function ouvrirRemise(id) {
      if (typeof id !== "string") id = "";
      const st = S.doc && S.doc.state;
      if (!st) return;
      // Hand over nothing while an input is open: it would stay out of the turn.
      if (saisieEnCours()) return;
      const agent = st.hand === "agent";
      S.remise = { reprise: agent };
      $("remise-titre").textContent = agent ? "Reprendre la main" : "Remettre la main à l'agent";
      const nouv = (st.statuses && st.statuses["new"]) || 0;
      $("remise-info").textContent = agent
        ? "L'agent n'a pas remis son tour. Ses modifications des fichiers seront converties en propositions, comme par « glose done », puis la main vous reviendra."
        : (nouv > 1 ? nouv + " annotations de ce tour seront remises. " : nouv === 1 ? "L'annotation de ce tour sera remise. " : "") +
          "L'agent pourra répondre et proposer ; vous ne pourrez plus écrire jusqu'à sa remise, sauf à reprendre la main.";
      if (!agent) {
        const puces = el("span", "tokens");
        const n = st.thisTurn || {};
        for (const [cle, nom, dessin] of [["comments", "commentaire", "commenter"], ["proposals", "suggestion", "suggerer"], ["replies", "réponse", "repondre"]]) {
          if (!n[cle]) continue;
          const p = el("span");
          p.innerHTML = icone(dessin);
          p.append(n[cle] + " " + nom + (n[cle] > 1 ? "s" : ""));
          puces.append(p);
        }
        if (puces.childNodes.length) $("remise-info").prepend(puces);
      }
      const pres = presenceAgent(st);
      const alerte = $("remise-alerte");
      alerte.hidden = agent || pres.present;
      alerte.textContent = (pres.configure ? "Aucun agent n'est à l'écoute" : "Aucun agent configuré, et aucun n'est à l'écoute") +
        " : la main restera à l'agent jusqu'à ce qu'un agent la prenne. Vous pourrez la reprendre.";
      if (!pres.configure) alerte.append(" ", agents.boutonConfigurer());
      if (agent) $("remise-lancement").hidden = true; else preparerRemise(pres.present);
      if (!agent && $("remise-lancer").checked && !$("remise-lancement").hidden) alerte.hidden = true;
      $("remise-note").value = "";
      $("remise-message").textContent = "";
      $("remise-force").hidden = true;
      $("remise-ok").textContent = agent ? "Reprendre la main" : "Remettre";
      preparerConsignes(id, agent);
      if (agent) $("remise-ok").disabled = false;
      $("remise").showModal();
      $("remise-note").focus();
    }

    async function remettre(force) {
      if (!S.remise || S.envoi) return;
      if (!S.remise.reprise && tacheChoisie() && (!apercuConsigne || (apercuConsigne.blocks || []).length)) return;
      S.envoi = true;
      S.occupe++;
      $("remise-ok").disabled = true;
      const corps = { note: $("remise-note").value.trim(), force: !!force };
      if (!S.remise.reprise) {
        corps.launch = choixLancement();
        if (tacheChoisie()) {
          corps.task = apercuConsigne.task.id; corps.files = apercuConsigne.task.files;
          corps.taskHash = apercuConsigne.hash; corps.confirmAccess = true; corps.launch = true;
        }
      }
      const r = await api(S.remise.reprise ? "/api/reclaim" : "/api/handoff", corps);
      $("remise-ok").disabled = false;
      S.envoi = false;
      try {
        if (r.ok) {
          $("remise").close();
          const d = r.data;
          let m = S.remise.reprise ? "Vous avez repris la main." : "Tour remis à l'agent.";
          if (d.converted) m += " " + d.converted + " proposition" + (d.converted > 1 ? "s" : "") + " tirée" + (d.converted > 1 ? "s" : "") + " des modifications de l'agent.";
          if (!S.remise.reprise && d.run && d.run.running) m += " " + d.run.alias + " est lancé.";
          if (d.launchError) m += " Agent non lancé : " + d.launchError;
          S.annuler = []; S.retablir = []; majBoutons();
          S.dernierEtat = undefined;
          avis(m, !!d.launchError);
          await charger(true);
          return;
        }
        const zone = $("remise-message");
        zone.textContent = r.data.message || "La remise a échoué.";
        if (r.data.problems) {
          const ul = el("ul");
          for (const p of r.data.problems) ul.append(el("li", "", p));
          zone.append(ul);
          if (S.remise.reprise) {
            $("remise-force").hidden = false;
            zone.append(el("p", "", "Vous pouvez reprendre la main sans convertir : les modifications de l'agent resteront dans les fichiers, sans propositions."));
          }
        }
      } finally {
        S.occupe--;
      }
    }

    function brancher(compose) {
      $("remettre").addEventListener("click", ouvrirRemise);
      $("remise").querySelector("form").addEventListener("submit", (e) => { e.preventDefault(); remettre(false); });
      $("remise-force").addEventListener("click", () => remettre(true));
      $("remise-fermer").addEventListener("click", () => { S.remise = null; ++generationConsigne; apercuConsigne = null; $("remise").close(); });
      $("remise-lier").addEventListener("click", () => { S.remise = null; ++generationConsigne; apercuConsigne = null; $("remise").close(); agents.ouvrirLien(); });
      // Launching counts as a listening agent: the warning follows the checkbox.
      $("remise-lancer").addEventListener("change", () => {
        const pres = presenceAgent(S.doc && S.doc.state);
        $("remise-alerte").hidden = pres.present || $("remise-lancer").checked;
      });
      $("remise-note").addEventListener("keydown", (e) => {
        if (compose(e)) return;
        if (e.key === "Enter" && (e.ctrlKey || e.metaKey)) { e.preventDefault(); if (!e.repeat) remettre(false); }
      });
    }

    return { majNavette, brancher };
  }
  return { creer };
})();
