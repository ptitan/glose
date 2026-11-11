// Glose: saisie panel, with explicit dependencies.
"use strict";
window.GloseSaisie = (() => {
  function creer({ $, S, brouillons, avis, court, agents, viser, panneauSaisie, garderBrouillon, preparerBrouillon, apresEnvoiBrouillon, api, charger }) {
  function ouvrirSaisie(titre, extrait, info, texte = "") {
    if (!brouillons.pret) { avis("Ouvrez Brouillons pour rétablir la sauvegarde avant de saisir.", true); return; }
    $("saisie-titre").textContent = titre;
    $("saisie-ok").firstChild.textContent = S.saisie.mode === "amend" ? "Enregistrer " : S.saisie.mode === "reply" ? "Répondre " : "Ajouter ";
    $("saisie-extrait").textContent = extrait;
    $("saisie-texte").value = texte;
    $("saisie-brouillon").textContent = S.saisie.brouillon ? "Brouillon récupéré." : "";
    message(info || "", true);
    const o = S.brouillonOrphelin;
    if (o && !$("saisie-texte").value && o.file === S.saisie.file) {
      // A remark whose passage changed (conflict) is recovered here, in
      // the same file; the message recalls the originally targeted passage.
      $("saisie-texte").value = o.texte;
      S.brouillonOrphelin = null;
      message("Votre remarque précédente" + (o.extrait ? ", sur « " + court(o.extrait, 80) + " »," : "") +
        " n'a pas pu être enregistrée : elle est reprise ici. Effacez-la si ce n'est pas le bon passage.", true);
    }
    autre(null);
    $("menu-selection").hidden = true;
    agents.preparerDestinataire("saisie", S.saisie.pour);
    $("saisie-pour-ligne").hidden = $("saisie-pour-ligne").hidden || S.saisie.mode === "amend";
    panneauSaisie.ouvrir();
    viser(S.saisie);
    panneauSaisie.placer();
    $("saisie-texte").focus();
  }

  function message(t, info) {
    const m = $("saisie-message");
    m.textContent = t;
    m.className = info ? "info" : "";
  }

  function autre(libelle, action) {
    const b = $("saisie-autre");
    b.hidden = !libelle;
    b.textContent = libelle || "";
    b.onclick = action || null;
  }

  // Close the input; an unsaved remark remains attached to its passage.
  function fermerSaisie(enregistre) {
    if (S.envoi) return;
    if (!enregistre) garderBrouillon("saisie");
    if (S.saisie && S.saisie.brouillon) {
      S.saisie.brouillon.active = false;
      if (!enregistre) brouillons.sauver(S.saisie.brouillon);
    }
    if ($("saisie").open) $("saisie").close();
  }

  async function enregistrer() {
    const s = S.saisie;
    if (!s || S.envoi) return;
    if (s.envoiIncertain) { message("Un enregistrement a été tenté : vérifiez le texte avant de recommencer. Votre saisie reste dans Brouillons."); return; }
    // The recipient (P10.6): "Réponse par", or "@Nom" at the start of the text.
    const dest = s.mode === "amend" ? { pour: "", texte: $("saisie-texte").value.trim() } : agents.destinataire("saisie", $("saisie-texte").value.trim());
    const texte = dest.texte.trim();
    if (!texte) { message(s.mode === "amend" ? "Le message ne peut pas être vide : retirez-le plutôt." : "Écrivez une remarque."); return; }
    if (s.mode === "amend" && texte === s.original.trim()) { message("Le texte n'a pas changé."); return; }
    // The hash is that of the displayed text when the passage was selected:
    // if the file has changed since, the server refuses (409) instead of writing elsewhere.
    const req = { file: s.file, hash: s.hash, mode: s.mode, start: s.start, end: s.end, text: s.prefixe + texte, recipient: dest.pour };
    S.envoi = true;
    S.occupe++;
    let r = null;
    // Released whatever happens (audit A14), as in the editor.
    try {
      $("saisie-ok").disabled = true;
      $("saisie").querySelector(".corps").inert = true;
      const pret = await preparerBrouillon("saisie");
      r = !pret ? { ok: false, status: 400, data: { error: "draft", message: "Brouillon non confirmé : vérifiez sa sauvegarde avant d'enregistrer." } } : s.mode === "reply"
        ? await api("/api/reply", { file: s.file, hash: s.hash, start: s.start, text: req.text, recipient: dest.pour })
        : s.mode === "amend"
          ? await api("/api/amend", { file: s.file, hash: s.hash, start: s.start, text: texte })
          : await api("/api/comment", req);
      await apresEnvoiBrouillon(s, r);
    } catch (e) {
      r = r || { ok: false, status: 0, data: { error: "exception", message: "L'enregistrement a échoué (" + e.message + ") : votre saisie reste dans Brouillons." } };
    } finally {
      $("saisie").querySelector(".corps").inert = false;
      $("saisie-ok").disabled = false;
      S.envoi = false;
    }
    try {
      if (r.ok && s.mode === "amend") {
        S.annuler.push({ type: "modification", file: s.file, start: r.data.start, hashApres: r.data.hash, avant: s.original, apres: texte, reponse: s.reponse });
        S.retablir = [];
        fermerSaisie(true);
        avis(s.reponse ? "Réponse modifiée." : "Commentaire modifié.");
        await charger(true);
        return;
      }
      if (r.ok) {
        S.annuler.push({ type: s.mode === "reply" ? "reponse" : "", file: req.file, start: r.data.start, hashApres: r.data.hash, req });
        S.retablir = [];
        fermerSaisie(true);
        avis((s.mode === "reply" ? "Réponse enregistrée dans " : "Commentaire enregistré dans ") + S.doc.files[req.file].path + ".");
        await charger(true);
        return;
      }
      const d = r.data;
      if (d.error === "turn") { message(d.message); return; }
      if (s.mode === "amend") {
        if (r.status === 409) {
          // No recovery in another passage: the text remains in Brouillons.
          fermerSaisie(false);
          avis("Le fichier a changé sur le disque. Votre texte est conservé dans Brouillons : rouvrez « Modifier » sur la carte.", true);
          await charger(true);
          return;
        }
        message(d.message || "La modification a échoué.");
        return;
      }
      if (r.status === 409) {
        fermerSaisie(false);
        avis("Le fichier a changé sur le disque. Votre remarque est conservée : sélectionnez de nouveau le passage.", true);
        S.brouillonOrphelin = { texte, file: S.saisie.file, extrait: S.saisie.extrait };
        await charger(true);
        return;
      }
      if (d.error === "widen") {
        message(d.message, true);
        autre("Commenter le passage élargi", () => {
          s.start = d.start; s.end = d.end; s.extrait = court(d.excerpt, 400);
          $("saisie-extrait").textContent = s.extrait;
          autre(null);
          enregistrer();
        });
        return;
      }
      if (d.error === "fallback" && d.at !== undefined) {
        message("Impossible d'annoter ce passage : " + d.message + ".");
        autre("Commenter la fin du paragraphe, en citant l'extrait", () => {
          s.mode = "point"; s.start = s.end = d.at; s.prefixe = "« " + court(s.extrait, 200) + " » : ";
          autre(null);
          enregistrer();
        });
        return;
      }
      message(d.message || "L'enregistrement a échoué.");
    } finally {
      S.occupe--;
    }
  }


    return { ouvrirSaisie, message, autre, fermerSaisie, enregistrer };
  }
  return { creer };
})();
