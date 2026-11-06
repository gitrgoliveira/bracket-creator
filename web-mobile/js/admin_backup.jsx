// Admin backup and restore page (/admin/backup)
// bc-bkrs Phase 3 (P3-C)

const { useState, useEffect, useRef, useCallback } = React;

const AdminTopbar = window.AdminTopbar;
const Breadcrumbs = window.Breadcrumbs;
const formatLabel = window.formatLabel;
const pluralize = window.pluralize;

const RESTORE_TOO_LARGE_MESSAGE = "The backup file is over 64 MB. Restore it from Copies kept on this computer or the command line.";

// After a choice or a copy name changes, the plan is asked again with the
// answers so far, so a name already used shows before the password.
const RECHECK_DELAY_MS = 400;

function formatFileSize(bytes) {
  if (typeof bytes !== "number" || isNaN(bytes)) return "";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}

function triggerBlobDownload(blob, filename) {
  const url = window.URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  window.URL.revokeObjectURL(url);
}

// A restore plan row the operator is not asked about says what the restore does
// with it by itself (an undo answers every row).
const UNASKED_NOTES = {
  create: "Will be created as a new competition.",
  replace: "Will be put back as it was before the restore.",
  remove: "Will be removed: the restore added it.",
  keep: "Will be left as it is.",
};
const LEFT_OUT_NOTE = "Will be left out.";
const COPY_NAME_TAKEN_NOTE = "That name is already used.";
const NAME_TAKEN_NOTE = "Another competition already has this name.";

// A copy is listed under the name of the competition it copies; its own name
// lives only in the operator's answer.
// A plan row for a competition the folder has and the tournament backup lacks.
// A row the backup holds may offer keep too (keep the one I have, or do not
// add it), so only remove, or keep on a row that is not a create, says so.
function isMissingFromArchive(c) {
  if (c.asked) return c.choices.includes("remove");
  return c.action === "remove" || (c.action === "keep" && !canLeaveOut(c));
}

// A competition only the backup holds, which a whole-tournament restore adds
// unless the operator ticks "Don't add it". An unasked row carries choices only
// then (the server omits an empty list).
function canLeaveOut(c) {
  return !c.asked && Array.isArray(c.choices) && c.choices.includes("keep");
}

function lookupCompetitionName(competitions, compId, copyNames) {
  if (!Array.isArray(competitions)) return compId;
  const match = competitions.find((c) => c.newId === compId || c.id === compId);
  if (match && match.newId === compId && copyNames?.[match.id]) return copyNames[match.id];
  return match?.name || compId;
}

function joinClauses(parts) {
  if (parts.length <= 1) return parts.join("");
  return `${parts.slice(0, -1).join(", ")} and ${parts[parts.length - 1]}`;
}

// What a restore will do, in one or two sentences, for the confirm dialog.
function restoreSummary(plan, decisions) {
  const counts = { replace: 0, copy: 0, create: 0, remove: 0, leaveOut: 0 };
  let results = 0;
  for (const c of plan?.competitions || []) {
    const action = decisions[c.id]?.action || (c.asked ? undefined : c.action);
    if (action === "keep") {
      // Keeping a competition the backup lacks changes nothing worth saying;
      // keeping one the backup holds leaves that one out of the restore.
      if (!isMissingFromArchive(c)) counts.leaveOut += 1;
    } else if (action in counts) {
      counts[action] += 1;
    }
    if ((action === "replace" || action === "remove") && Array.isArray(c.resultsChanged)) {
      results += c.resultsChanged.length;
    }
  }
  const parts = [];
  if (counts.replace) parts.push(`${plan.undo ? "puts back" : "replaces"} ${pluralize(counts.replace, "competition")}`);
  if (counts.copy) parts.push(`adds ${pluralize(counts.copy, "copy", "copies")}`);
  if (counts.create) parts.push(`adds ${pluralize(counts.create, "new competition")}`);
  if (counts.remove) parts.push(`removes ${pluralize(counts.remove, "competition")}`);
  if (counts.leaveOut) parts.push(`leaves out ${pluralize(counts.leaveOut, "competition")}`);
  if (plan?.tournamentReplaced) parts.push("puts back the tournament settings");
  let text = parts.length > 0 ? `This ${plan.undo ? "undo" : "restore"} ${joinClauses(parts)}.` : "";
  if (results > 0) text += ` ${pluralize(results, "recorded result")} will be lost or changed.`;
  return text.trim();
}

function planHasRunningMatches(p) {
  if (!p) return false;
  if ((p.competitions || []).some((c) => Array.isArray(c.runningNow) && c.runningNow.length > 0)) {
    return true;
  }
  if ((p.warnings || []).some((w) => w.code === "matches_running")) {
    return true;
  }
  return false;
}

function AdminBackupPage({ tournament, onBack, onLogout, onViewerMode, password, showToast }) {
  const [backupsList, setBackupsList] = useState([]);
  const [loadingBackups, setLoadingBackups] = useState(false);
  const [selectedFile, setSelectedFile] = useState(null);
  const [selectedFromName, setSelectedFromName] = useState(null);
  const [planning, setPlanning] = useState(false);
  const [plan, setPlan] = useState(null);
  const [planError, setPlanError] = useState(null);
  const [onlyCompId, setOnlyCompId] = useState(null);
  // The competitions a whole-tournament backup holds, for the Restore picker;
  // kept while the plan is narrowed to one of them.
  const [archiveComps, setArchiveComps] = useState([]);
  const [decisions, setDecisions] = useState({});
  // The latest plan asked again with the answers (key: those answers as JSON),
  // its rows by id. Only the row flags are read from it (a copy name or a name
  // already used); the plan on screen stays the one first asked for.
  const [checked, setChecked] = useState(null);
  const [restoreResult, setRestoreResult] = useState(null);
  // The backup being downloaded: "tournament" or a competition id.
  const [backingUp, setBackingUp] = useState(null);

  const mountedRef = useRef(true);
  const fileInputRef = useRef(null);
  const chooseFileRef = useRef(null);
  const resultTitleRef = useRef(null);
  const planRef = useRef(null);
  const planTitleRef = useRef(null);
  // Each check, and each new plan, takes the next number; an answer that
  // arrives for an older one is dropped.
  const checkSeqRef = useRef(0);
  const checkTimerRef = useRef(null);
  useEffect(() => () => {
    mountedRef.current = false;
    clearTimeout(checkTimerRef.current);
  }, []);

  // The button that opened the confirm dialog is gone once a restore lands, so the
  // dialog cannot hand focus back to it: focus the result's title, which reads it out
  // and brings the card into view.
  useEffect(() => {
    if (restoreResult) resultTitleRef.current?.focus();
  }, [restoreResult]);

  // A plan opens in the Restore from a file card, away from a kept copy's Restore
  // or the Undo button that asked for it: focus its title, which reads it out and
  // brings it into view.
  useEffect(() => {
    if (plan) planTitleRef.current?.focus();
  }, [plan]);

  const loadKeptBackups = useCallback(async () => {
    if (!window.API || !window.API.listBackups) return;
    setLoadingBackups(true);
    try {
      const list = await window.API.listBackups(password);
      if (mountedRef.current) setBackupsList(Array.isArray(list) ? list : (list?.backups || []));
    } catch (e) {
      console.error("Failed to load backups list:", e);
      if (mountedRef.current) showToast?.(e.message || "Failed to load backups", "error");
    } finally {
      if (mountedRef.current) setLoadingBackups(false);
    }
  }, [password, showToast]);

  useEffect(() => {
    loadKeptBackups();
  }, [loadKeptBackups]);

  // One backup at a time: the buttons are disabled while an archive is being
  // made, so a second tap does not download it twice.
  const runBackup = async (target, download, doneMessage) => {
    setBackingUp(target);
    try {
      const { blob, filename } = await download();
      triggerBlobDownload(blob, filename);
      showToast?.(doneMessage);
    } catch (e) {
      showToast?.(e.message || "Backup failed", "error");
    } finally {
      if (mountedRef.current) setBackingUp(null);
    }
  };

  const handleBackupTournament = () =>
    runBackup("tournament", () => window.API.backupTournament(password), "Tournament backup downloaded");

  const handleBackupCompetition = (comp) =>
    runBackup(comp.id, () => window.API.backupCompetition(comp.id, password), `Backup for "${comp.name}" downloaded`);

  // A new plan drops every check asked for the one before.
  const forgetChecks = () => {
    checkSeqRef.current += 1;
    clearTimeout(checkTimerRef.current);
    setChecked(null);
  };

  // Puts the plan away: the restore landed, or the operator cancelled it.
  const clearPlan = () => {
    forgetChecks();
    setPlan(null);
    setDecisions({});
    setSelectedFile(null);
    setSelectedFromName(null);
    setOnlyCompId(null);
    setArchiveComps([]);
  };

  const handleCancelPlan = () => {
    clearPlan();
    setPlanError(null);
    chooseFileRef.current?.focus();
  };

  const runPlan = async ({ file, from, only, undo = false }) => {
    forgetChecks();
    setPlanning(true);
    setPlanError(null);
    setPlan(null);
    setDecisions({});
    try {
      const p = await window.API.planRestore({ file, from, only, undo }, password);
      if (!mountedRef.current) return;
      setPlan(p);
      setSelectedFile(file || null);
      setSelectedFromName(from || null);
      setOnlyCompId(only || null);
      if (!only) {
        setArchiveComps(p.scope === "tournament" ? p.competitions.filter((c) => !isMissingFromArchive(c)) : []);
      }
    } catch (e) {
      if (!mountedRef.current) return;
      if (e?.status === 413) {
        setPlanError(RESTORE_TOO_LARGE_MESSAGE);
      } else {
        setPlanError(e.message || "Failed to check restore plan");
      }
    } finally {
      if (mountedRef.current) setPlanning(false);
    }
  };

  const handleFileChange = (e) => {
    const file = e.target.files?.[0];
    if (!file) return;
    // Clear input so picking the same file again fires change
    e.target.value = "";
    runPlan({ file });
  };

  const handleScopeChange = (e) => {
    runPlan({
      file: selectedFile,
      from: selectedFromName,
      only: e.target.value || undefined,
    });
  };

  // Asks the plan again with these answers. Resolves to the check, or to null
  // when a newer check or plan has been asked for since.
  const checkDecisions = useCallback(async (sent) => {
    checkSeqRef.current += 1;
    const seq = checkSeqRef.current;
    const p = await window.API.planRestore(
      { file: selectedFile, from: selectedFromName, only: onlyCompId, decisions: sent },
      password
    );
    if (!mountedRef.current || seq !== checkSeqRef.current) return null;
    const result = { key: JSON.stringify(sent), byId: Object.fromEntries(p.competitions.map((c) => [c.id, c])) };
    setChecked(result);
    return result;
  }, [selectedFile, selectedFromName, onlyCompId, password]);

  useEffect(() => {
    if (!plan || plan.undo || Object.keys(decisions).length === 0) return undefined;
    checkTimerRef.current = setTimeout(() => {
      // Restore asks again before the password, and says what went wrong then.
      checkDecisions(decisions).catch((e) => console.warn("Restore plan check failed:", e));
    }, RECHECK_DELAY_MS);
    return () => clearTimeout(checkTimerRef.current);
  }, [plan, decisions, checkDecisions]);

  const handleLeaveOutChange = (compId, leaveOut) => {
    setDecisions((prev) => {
      const next = { ...prev };
      if (leaveOut) next[compId] = { action: "keep" };
      else delete next[compId];
      return next;
    });
  };

  const handleDecisionChange = (compId, action, copyName) => {
    setDecisions((prev) => ({
      ...prev,
      [compId]: {
        action,
        copyName: copyName !== undefined ? copyName : (prev[compId]?.copyName || ""),
      },
    }));
  };

  const askedComps = (plan?.competitions || []).filter((c) => c.asked);
  let firstOpenComp = null;
  for (const c of askedComps) {
    const d = decisions[c.id];
    if (!d || !d.action) {
      firstOpenComp = c;
      break;
    }
    if (d.action === "copy" && (!d.copyName || !d.copyName.trim())) {
      firstOpenComp = { ...c, needsCopyName: true };
      break;
    }
  }

  // The flags for the answers on screen: the plan's own while nothing is
  // answered, else the check asked with exactly these answers (none while it
  // is on its way).
  const decisionsKey = JSON.stringify(decisions);
  const flagsById = Object.keys(decisions).length === 0
    ? Object.fromEntries((plan?.competitions || []).map((c) => [c.id, c]))
    : checked?.key === decisionsKey ? checked.byId : {};
  const copyNameTaken = (c) =>
    decisions[c.id]?.action === "copy" && Boolean(decisions[c.id].copyName.trim()) && Boolean(flagsById[c.id]?.copyNameTaken);
  const nameTaken = (c) => Boolean(flagsById[c.id]?.nameTaken);
  if (firstOpenComp === null) {
    const taken = (plan?.competitions || []).find((c) => copyNameTaken(c) || nameTaken(c));
    if (taken) firstOpenComp = { ...taken, taken: copyNameTaken(taken) ? "copy" : "name" };
  }

  const allChoicesMade = firstOpenComp === null;
  const replaceableComps = askedComps.filter((c) => c.choices?.includes("replace"));

  const handleReplaceAll = () => {
    setDecisions((prev) => {
      const next = { ...prev };
      for (const c of replaceableComps) next[c.id] = { action: "replace", copyName: prev[c.id]?.copyName || "" };
      return next;
    });
  };

  // The button names the next open choice; a tap on it takes the operator there.
  const focusFirstOpenChoice = () => {
    const inputs = Array.from(planRef.current.querySelectorAll("input"));
    const target = firstOpenComp.needsCopyName || firstOpenComp.taken === "copy"
      ? inputs.find((el) => el.dataset.copyNameFor === firstOpenComp.id)
      : inputs.find((el) => el.name === `decision-${firstOpenComp.id}`);
    // A name taken by a competition restored with competition scope offers no
    // choice to go to: the competition in the folder must be renamed first.
    target?.focus();
  };

  const handleRestoreSubmit = async () => {
    if (!allChoicesMade) {
      focusFirstOpenChoice();
      return;
    }
    const prompt = window.promptDialog;
    if (!prompt) return;

    // One last check with these answers, so a name already used is shown on
    // its row (and names the button) instead of refusing after the password.
    if (Object.keys(decisions).length > 0 && !plan.undo) {
      clearTimeout(checkTimerRef.current);
      let result;
      try {
        result = await checkDecisions(decisions);
      } catch (e) {
        if (mountedRef.current) setPlanError(e.message || "Failed to check restore plan");
        return;
      }
      if (!result) return;
      if (Object.values(result.byId).some((c) => c.copyNameTaken || c.nameTaken)) return;
    }

    const hasRunning = planHasRunningMatches(plan);
    const copyNames = {};
    for (const [id, d] of Object.entries(decisions)) {
      if (d.action === "copy") copyNames[id] = d.copyName.trim();
    }
    const summary = restoreSummary(plan, decisions);

    prompt({
      title: "Confirm restore",
      message: `${summary ? `${summary} ` : ""}Enter the admin password to confirm the restore.`,
      placeholder: "Admin password",
      password: true,
      danger: true,
      confirmLabel: "Restore",
      cancelLabel: "Cancel",
      submit: async (typed) => {
        if (!typed) return "Enter the password.";
        try {
          const res = await window.API.restore(
            {
              file: selectedFile,
              from: selectedFromName,
              only: onlyCompId,
              undo: Boolean(plan?.undo),
              decisions,
              ...(hasRunning ? { allowRunning: true } : {}),
            },
            password,
            typed
          );
          if (mountedRef.current) {
            setRestoreResult({ ...res, copyNames, isUndoResult: Boolean(plan?.undo) });
            clearPlan();
            loadKeptBackups();
          }
        } catch (e) {
          if (typeof e.code === "string" && e.code.startsWith("confirm_password_")) {
            return e.message;
          }
          if (e?.status === 413) {
            setPlanError(RESTORE_TOO_LARGE_MESSAGE);
          } else {
            setPlanError(e.message || "Failed to restore");
          }
          if (e.code === "matches_running") {
            runPlan({
              file: selectedFile,
              from: selectedFromName,
              only: onlyCompId,
              undo: Boolean(plan?.undo),
            });
          }
          return null;
        }
        return null;
      },
    });
  };

  const handleUndo = async () => {
    if (!restoreResult?.safetyCopy) return;
    const safetyCopy = restoreResult.safetyCopy;
    let undoPlanResult;
    forgetChecks();
    try {
      setPlanning(true);
      setPlanError(null);
      undoPlanResult = await window.API.planRestore({ from: safetyCopy, undo: true }, password);
      if (!mountedRef.current) return;
      setPlan(undoPlanResult);
      setSelectedFile(null);
      setSelectedFromName(safetyCopy);
      setOnlyCompId(null);
      setArchiveComps([]);
      setDecisions({});
    } catch (e) {
      if (!mountedRef.current) return;
      setPlanError(e.message || "Failed to check undo plan");
      return;
    } finally {
      if (mountedRef.current) setPlanning(false);
    }

    const prompt = window.promptDialog;
    if (!prompt) return;

    const hasRunning = planHasRunningMatches(undoPlanResult);
    const summary = restoreSummary(undoPlanResult, {});

    prompt({
      title: "Confirm undo",
      message: `${summary ? `${summary} ` : ""}Enter the admin password to undo the restore using safety copy ${safetyCopy}.`,
      placeholder: "Admin password",
      password: true,
      danger: true,
      confirmLabel: "Undo restore",
      cancelLabel: "Cancel",
      submit: async (typed) => {
        if (!typed) return "Enter the password.";
        try {
          const res = await window.API.restore(
            {
              from: safetyCopy,
              undo: true,
              ...(hasRunning ? { allowRunning: true } : {}),
            },
            password,
            typed
          );
          if (mountedRef.current) {
            setRestoreResult({ ...res, isUndoResult: true });
            clearPlan();
            loadKeptBackups();
          }
        } catch (e) {
          if (typeof e.code === "string" && e.code.startsWith("confirm_password_")) {
            return e.message;
          }
          setPlanError(e.message || "Failed to undo restore");
          if (e.code === "matches_running") {
            runPlan({
              from: safetyCopy,
              undo: true,
            });
          }
          return null;
        }
        return null;
      },
    });
  };

  const handleDeleteBackup = (backupItem) => {
    const prompt = window.promptDialog;
    if (!prompt) return;

    prompt({
      title: "Delete backup",
      message: `Enter the admin password to delete backup "${backupItem.name}".`,
      placeholder: "Admin password",
      password: true,
      danger: true,
      confirmLabel: "Delete",
      cancelLabel: "Cancel",
      submit: async (typed) => {
        if (!typed) return "Enter the password.";
        try {
          await window.API.deleteBackup(backupItem.name, password, typed);
          if (mountedRef.current) {
            showToast?.(`Deleted "${backupItem.name}"`);
            loadKeptBackups();
          }
        } catch (e) {
          if (typeof e.code === "string" && e.code.startsWith("confirm_password_")) {
            return e.message;
          }
          showToast?.(e.message || "Failed to delete backup", "error");
          return null;
        }
        return null;
      },
    });
  };

  const comps = tournament?.competitions || [];

  return (
    <div className="app">
      {AdminTopbar && (
        <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={tournament} />
      )}
      <div className="page backup-page">
        {Breadcrumbs && (
          <Breadcrumbs
            items={[
              { label: tournament?.name || "Tournament", onClick: onBack },
              { label: "Backup and restore" },
            ]}
          />
        )}
        <div className="page-head">
          <h1 className="page-head__title">Backup and restore</h1>
        </div>

        <div className="alert alert--info">
          A backup file does not contain the admin password, but it does hold every competitor&apos;s name and dojo, so keep it private.
        </div>

        {restoreResult && (
          <div className="card backup-result">
            <h2 ref={resultTitleRef} className="card__title" tabIndex={-1}>
              {restoreResult.isUndoResult ? "Restore undone" : "Restore completed"}
            </h2>
            <div className="card__body">
              {restoreResult.safetyCopy && (
                <p className="backup-detail">
                  Safety copy created: <strong>{restoreResult.safetyCopy}</strong>
                </p>
              )}
              {restoreResult.prefixWarnings && restoreResult.prefixWarnings.length > 0 && (
                <div className="alert alert--warn">
                  {restoreResult.prefixWarnings.map((w) => {
                    const compName = lookupCompetitionName(restoreResult.plan?.competitions, w.competitionId, restoreResult.copyNames);
                    return (
                      <div key={w.competitionId}>
                        {w.newPrefix ? (
                          <>{compName}: number prefix changed from {w.oldPrefix} to {w.newPrefix}. Reprint its competitors&apos; tags.</>
                        ) : (
                          w.message
                        )}
                      </div>
                    );
                  })}
                </div>
              )}
              {restoreResult.prefixError && (
                <div className="alert alert--error">{restoreResult.prefixError}</div>
              )}
              {restoreResult.requeueError && (
                <div className="alert alert--error">{restoreResult.requeueError}</div>
              )}
              {restoreResult.safetyCopy && !restoreResult.isUndoResult && (
                <>
                  <div className="backup-actions">
                    <button
                      type="button"
                      className="btn btn--secondary"
                      onClick={handleUndo}
                    >
                      Undo this restore
                    </button>
                  </div>
                  <p className="backup-detail">
                    This card goes when you leave or reload the page. After that, restore the safety copy from Copies kept on this computer.
                  </p>
                </>
              )}
            </div>
          </div>
        )}

        <div className="card">
          <h2 className="card__title">Back up tournament</h2>
          <div className="card__body">
            <p className="backup-intro">
              Download a complete archive of the tournament including all competitions, participants, matches and settings.
            </p>
            <div className="backup-actions">
              <button
                type="button"
                className="btn btn--primary"
                disabled={backingUp !== null}
                onClick={handleBackupTournament}
              >
                {backingUp === "tournament" && <span className="spinner" />}
                Back up the whole tournament
              </button>
            </div>

            {comps.length > 0 && (
              <>
                <h3 className="backup-subhead">Back up individual competitions</h3>
                <div className="backup-list">
                  {comps.map((c) => (
                    <div key={c.id} className="backup-row">
                      <div className="backup-row__main">
                        <span className="backup-row__name">{c.name}</span>
                        <span className="backup-row__meta">{formatLabel(c.format)}</span>
                      </div>
                      <button
                        type="button"
                        className="btn btn--sm"
                        aria-label={`Back up "${c.name}"`}
                        disabled={backingUp !== null}
                        onClick={() => handleBackupCompetition(c)}
                      >
                        {backingUp === c.id && <span className="spinner" />}
                        Back up
                      </button>
                    </div>
                  ))}
                </div>
              </>
            )}
          </div>
        </div>

        <div className="card">
          <h2 className="card__title">Restore from a file</h2>
          <div className="card__body">
            <p className="backup-intro">
              Upload a zip archive to preview the restore. No changes will be made until you confirm.
            </p>
            <div className="backup-actions">
              <input
                ref={fileInputRef}
                type="file"
                accept=".zip"
                hidden
                onChange={handleFileChange}
                disabled={planning}
              />
              <button
                ref={chooseFileRef}
                type="button"
                className="btn"
                disabled={planning}
                onClick={() => fileInputRef.current?.click()}
              >
                Choose a backup file
              </button>
              <span className="backup-row__meta" role="status">{planning ? "Checking backup archive…" : ""}</span>
            </div>

            {planError && (
              <div className="alert alert--error backup-plan-error">{planError}</div>
            )}

            {plan && (
              <div ref={planRef} className="backup-plan">
                <h3 ref={planTitleRef} className="backup-subhead backup-plan__title" tabIndex={-1}>
                  {plan.undo
                    ? "Undo plan"
                    : plan.scope === "tournament"
                      ? "Restore plan: whole tournament"
                      : "Restore plan: one competition"}
                </h3>
                {(selectedFromName || selectedFile?.name) && (
                  <p className="backup-detail">
                    From <strong>{selectedFromName || selectedFile.name}</strong>
                  </p>
                )}

                <div className="backup-alerts">
                  <details className="alert alert--warn backup-sync">
                    <summary><strong>Let every court device sync before you restore.</strong></summary>
                    A score a device sends after the restore:
                    <ul>
                      <li>is applied when it is newer than the restored match</li>
                      <li>goes to the match&apos;s history, not the result, when it is older or the backup has that match finished</li>
                      <li>is refused when the backup does not hold that match</li>
                    </ul>
                  </details>
                  {(plan.warnings || []).map((w) => (
                    <div key={`${w.code}-${w.competitionId || ""}-${w.court || ""}`} className="alert alert--warn">
                      {w.message}
                    </div>
                  ))}
                  {(plan.competitions || []).some((c) => c.resultsChanged && c.resultsChanged.length > 0) && (
                    <div className="alert alert--error">
                      <strong>Results that will be lost or changed:</strong>
                      <ul>
                        {plan.competitions
                          .filter((c) => c.resultsChanged && c.resultsChanged.length > 0)
                          .map((c) => (
                            <li key={c.id}>
                              {pluralize(c.resultsChanged.length, "recorded result")} in &quot;{c.name}&quot;
                            </li>
                          ))}
                      </ul>
                    </div>
                  )}
                </div>

                {archiveComps.length >= 2 && !plan.undo && (
                  <label className="backup-scope">
                    <span className="backup-scope__label">Restore</span>
                    <select
                      className="input"
                      value={onlyCompId || ""}
                      disabled={planning}
                      onChange={handleScopeChange}
                    >
                      <option value="">All competitions</option>
                      {archiveComps.map((c) => (
                        <option key={c.id} value={c.id}>Only {c.name}</option>
                      ))}
                    </select>
                  </label>
                )}

                <div className="backup-plan__listhead">
                  <h4 className="backup-subhead">Competitions to restore</h4>
                  {replaceableComps.length >= 2 && (
                    <button type="button" className="btn btn--sm" onClick={handleReplaceAll}>
                      Replace all existing
                    </button>
                  )}
                </div>
                <div className="backup-list">
                  {plan.competitions.map((c) => {
                    const currentDecision = decisions[c.id] || {};
                    const nameId = `backup-comp-${c.id}`;
                    const leftOut = canLeaveOut(c) && currentDecision.action === "keep";
                    const copyTaken = copyNameTaken(c);
                    // A competition renamed after the backup is read with its current name too.
                    const labelledBy = c.currentName ? `${nameId} ${nameId}-current ${nameId}-note` : `${nameId} ${nameId}-note`;

                    return (
                      <div key={c.id} className="backup-row backup-row--plan">
                        <div className="backup-row__head">
                          <span id={nameId} className="backup-row__name">{c.name}</span>
                        </div>
                        {c.currentName && (
                          <div id={`${nameId}-current`} className="backup-row__note">Called &ldquo;{c.currentName}&rdquo; in this tournament now.</div>
                        )}

                        {canLeaveOut(c) ? (
                          <div role="group" aria-labelledby={labelledBy}>
                            <div id={`${nameId}-note`} className="backup-row__note">{leftOut ? LEFT_OUT_NOTE : UNASKED_NOTES.create}</div>
                            <label className="backup-choice">
                              <input
                                type="checkbox"
                                name={`decision-${c.id}`}
                                checked={leftOut}
                                onChange={(e) => handleLeaveOutChange(c.id, e.target.checked)}
                              />
                              Don&apos;t add it
                            </label>
                          </div>
                        ) : !c.asked ? (
                          <div className="backup-row__note">{UNASKED_NOTES[c.action]}</div>
                        ) : isMissingFromArchive(c) ? (
                          <fieldset className="backup-choices" aria-labelledby={labelledBy}>
                            <legend id={`${nameId}-note`} className="backup-row__note">This competition is not in the backup archive:</legend>
                            <label className="backup-choice">
                              <input
                                type="radio"
                                name={`decision-${c.id}`}
                                value="keep"
                                checked={currentDecision.action === "keep"}
                                onChange={() => handleDecisionChange(c.id, "keep")}
                              />
                              Keep it
                            </label>
                            <label className="backup-choice">
                              <input
                                type="radio"
                                name={`decision-${c.id}`}
                                value="remove"
                                checked={currentDecision.action === "remove"}
                                onChange={() => handleDecisionChange(c.id, "remove")}
                              />
                              Remove it
                            </label>
                          </fieldset>
                        ) : (
                          <fieldset className="backup-choices" aria-labelledby={labelledBy}>
                            <legend id={`${nameId}-note`} className="backup-row__note">This competition already exists:</legend>
                            <label className="backup-choice">
                              <input
                                type="radio"
                                name={`decision-${c.id}`}
                                value="replace"
                                checked={currentDecision.action === "replace"}
                                onChange={() => handleDecisionChange(c.id, "replace")}
                              />
                              Replace it
                            </label>
                            <label className="backup-choice backup-choice--copy">
                              <span className="backup-choice__radio">
                                <input
                                  type="radio"
                                  name={`decision-${c.id}`}
                                  value="copy"
                                  checked={currentDecision.action === "copy"}
                                  onChange={() => handleDecisionChange(c.id, "copy")}
                                />
                                Restore as a copy named:
                              </span>
                              <input
                                type="text"
                                className="input backup-copy-name"
                                aria-label={`Name for the copy of "${c.name}"`}
                                data-copy-name-for={c.id}
                                aria-invalid={copyTaken ? "true" : undefined}
                                aria-describedby={copyTaken ? `${nameId}-copy-taken` : undefined}
                                value={currentDecision.copyName || ""}
                                onChange={(e) => handleDecisionChange(c.id, "copy", e.target.value)}
                                onClick={() => handleDecisionChange(c.id, "copy")}
                              />
                            </label>
                            {copyTaken && (
                              <div id={`${nameId}-copy-taken`} className="backup-row__note backup-row__note--error">
                                {COPY_NAME_TAKEN_NOTE}
                              </div>
                            )}
                            {c.choices.includes("keep") && (
                              <label className="backup-choice">
                                <input
                                  type="radio"
                                  name={`decision-${c.id}`}
                                  value="keep"
                                  checked={currentDecision.action === "keep"}
                                  onChange={() => handleDecisionChange(c.id, "keep")}
                                />
                                Keep the one I have (don&apos;t restore it)
                              </label>
                            )}
                          </fieldset>
                        )}
                        {nameTaken(c) && (
                          <div className="backup-row__note backup-row__note--error">{NAME_TAKEN_NOTE}</div>
                        )}
                      </div>
                    );
                  })}
                </div>

                {plan.nothingToRestore && (
                  <div className="alert alert--info backup-plan-end">
                    {plan.message || "Nothing to restore."}
                  </div>
                )}
                <div className="backup-actions backup-plan-end">
                  {!plan.nothingToRestore && (
                    <button
                      type="button"
                      className="btn btn--primary"
                      aria-disabled={allChoicesMade ? undefined : "true"}
                      onClick={handleRestoreSubmit}
                    >
                      {allChoicesMade
                        ? "Restore"
                        : firstOpenComp.needsCopyName
                          ? `Enter name for copy of "${firstOpenComp.name}"`
                          : firstOpenComp.taken === "copy"
                            ? `Pick another name for the copy of "${firstOpenComp.name}"`
                            : firstOpenComp.taken === "name"
                              ? `"${firstOpenComp.name}": its name is already used`
                              : `Choose what to do with "${firstOpenComp.name}"`}
                    </button>
                  )}
                  <button type="button" className="btn" onClick={handleCancelPlan}>
                    Cancel
                  </button>
                </div>
              </div>
            )}
          </div>
        </div>

        <div className="card">
          <h2 className="card__title">Copies kept on this computer</h2>
          <div className="card__body">
            <p className="backup-intro">
              The app takes a daily copy by itself when something changed. Copies are never deleted automatically.
            </p>

            <div className="backup-row__meta" role="status">
              {loadingBackups && backupsList.length === 0 ? "Loading kept copies…" : ""}
            </div>
            {backupsList.length === 0 ? (
              !loadingBackups && <p className="backup-row__meta">No backup copies kept yet.</p>
            ) : (
              <div className="backup-list">
                {backupsList.map((b) => {
                  const kindLabel = b.kind === "scheduled" ? "Daily" : "Before restore";

                  return (
                    <div key={b.name} className="backup-row">
                      <div className="backup-row__main">
                        <div className="backup-row__title">
                          <span className="badge badge--archive">{kindLabel}</span>
                          <span className="backup-row__name">
                            {new Date(b.time).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "medium" })}
                          </span>
                        </div>
                        <div className="backup-row__meta">
                          <span className="backup-row__file">{b.name}</span> · {formatFileSize(b.size)}
                        </div>
                      </div>
                      <div className="backup-row__actions">
                        <button
                          type="button"
                          className="btn btn--sm"
                          aria-label={`Download ${b.name}`}
                          onClick={async () => {
                            try {
                              const blob = await window.API.downloadBackup(b.name, password);
                              triggerBlobDownload(blob, b.name);
                            } catch (e) {
                              showToast?.(e.message || "Download failed", "error");
                            }
                          }}
                        >
                          Download
                        </button>
                        <button
                          type="button"
                          className="btn btn--sm"
                          aria-label={`Restore ${b.name}`}
                          onClick={() => runPlan({ from: b.name, undo: false })}
                        >
                          Restore
                        </button>
                        <button
                          type="button"
                          className="btn btn--sm btn--ghost btn--danger"
                          aria-label={`Delete ${b.name}`}
                          onClick={() => handleDeleteBackup(b)}
                        >
                          Delete
                        </button>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

if (typeof window !== "undefined") {
  window.AdminBackupPage = AdminBackupPage;
}

export { AdminBackupPage };
