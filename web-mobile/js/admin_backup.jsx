// Admin backup and restore page (/admin/backup)
// bc-bkrs Phase 3 (P3-C)

const { useState, useEffect, useRef, useCallback } = React;

const AdminTopbar = window.AdminTopbar;
const Breadcrumbs = window.Breadcrumbs;

const RESTORE_TOO_LARGE_MESSAGE = "The backup file is over 64 MB. Restore it from Copies kept on this computer or the command line.";

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

function lookupCompetitionName(competitions, compId) {
  if (!Array.isArray(competitions)) return compId;
  const match = competitions.find((c) => c.newId === compId || c.id === compId);
  return match?.name || compId;
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
  const [decisions, setDecisions] = useState({});
  const [restoreResult, setRestoreResult] = useState(null);

  const mountedRef = useRef(true);
  const fileInputRef = useRef(null);
  useEffect(() => () => { mountedRef.current = false; }, []);

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

  const handleBackupTournament = async () => {
    try {
      const { blob, filename } = await window.API.backupTournament(password);
      triggerBlobDownload(blob, filename);
      showToast?.("Tournament backup downloaded");
    } catch (e) {
      showToast?.(e.message || "Backup failed", "error");
    }
  };

  const handleBackupCompetition = async (comp) => {
    try {
      const { blob, filename } = await window.API.backupCompetition(comp.id, password);
      triggerBlobDownload(blob, filename);
      showToast?.(`Backup for "${comp.name}" downloaded`);
    } catch (e) {
      showToast?.(e.message || "Backup failed", "error");
    }
  };

  const runPlan = async ({ file, from, only, undo = false }) => {
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

  const handleRestoreOnly = (compId) => {
    runPlan({
      file: selectedFile,
      from: selectedFromName,
      only: compId,
    });
  };

  const handleShowAll = () => {
    runPlan({
      file: selectedFile,
      from: selectedFromName,
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

  const allChoicesMade = askedComps.length === 0 || firstOpenComp === null;

  const handleRestoreSubmit = () => {
    if (!allChoicesMade) return;
    const prompt = window.promptDialog;
    if (!prompt) return;

    const hasRunning = planHasRunningMatches(plan);

    prompt({
      title: "Confirm restore",
      message: "Enter the admin password to confirm the restore.",
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
            setRestoreResult(res);
            setPlan(null);
            setDecisions({});
            setSelectedFile(null);
            setSelectedFromName(null);
            setOnlyCompId(null);
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
    try {
      setPlanning(true);
      setPlanError(null);
      undoPlanResult = await window.API.planRestore({ from: safetyCopy, undo: true }, password);
      if (!mountedRef.current) return;
      setPlan(undoPlanResult);
      setSelectedFile(null);
      setSelectedFromName(safetyCopy);
      setOnlyCompId(null);
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

    prompt({
      title: "Confirm undo",
      message: `Enter the admin password to undo the restore using safety copy ${safetyCopy}.`,
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
            setPlan(null);
            setDecisions({});
            setSelectedFromName(null);
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
            <div className="card__title">Restore completed</div>
            <div className="card__body">
              {restoreResult.safetyCopy && (
                <p className="backup-intro">
                  Safety copy created: <strong>{restoreResult.safetyCopy}</strong>
                </p>
              )}
              {restoreResult.prefixWarnings && restoreResult.prefixWarnings.length > 0 && (
                <div className="alert alert--warn">
                  {restoreResult.prefixWarnings.map((w) => {
                    const compName = lookupCompetitionName(restoreResult.plan?.competitions, w.competitionId);
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
                <div className="backup-actions">
                  <button
                    type="button"
                    className="btn btn--secondary"
                    onClick={handleUndo}
                  >
                    Undo this restore
                  </button>
                </div>
              )}
            </div>
          </div>
        )}

        <div className="card">
          <div className="card__title">Back up tournament</div>
          <div className="card__body">
            <p className="backup-intro">
              Download a complete archive of the tournament including all competitions, participants, matches and settings.
            </p>
            <div className="backup-actions">
              <button
                type="button"
                className="btn btn--primary"
                onClick={handleBackupTournament}
              >
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
                        <span className="backup-row__meta">{c.format || "pools"}</span>
                      </div>
                      <button
                        type="button"
                        className="btn btn--sm"
                        onClick={() => handleBackupCompetition(c)}
                      >
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
          <div className="card__title">Restore from a file</div>
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
                type="button"
                className="btn"
                disabled={planning}
                onClick={() => fileInputRef.current?.click()}
              >
                Choose a backup file
              </button>
              {planning && <span className="backup-row__meta" role="status">Checking backup archive…</span>}
            </div>

            {planError && (
              <div className="alert alert--error backup-plan-error">{planError}</div>
            )}

            {plan && (
              <div className="backup-plan">
                <h3 className="backup-subhead">Restore plan ({plan.scope} archive)</h3>
                {(selectedFromName || selectedFile?.name) && (
                  <p className="backup-intro">
                    From <strong>{selectedFromName || selectedFile.name}</strong>
                  </p>
                )}

                <div className="backup-alerts">
                  <div className="alert alert--warn">
                    Let every court device sync before you restore. A change a court device makes before the restore and sends after it is usually applied when it arrives, because it is newer than the restored record. If the restored copy already has that match finished, the change is kept in the match&apos;s history instead and not shown. If the restored copy does not hold that match, the change is refused.
                  </div>
                  {(plan.warnings || []).map((w) => (
                    <div key={`${w.code}-${w.competitionId || ""}-${w.court || ""}`} className="alert alert--warn">
                      {w.message}
                    </div>
                  ))}
                  {(plan.competitions || []).some((c) => c.resultsChanged && c.resultsChanged.length > 0) && (
                    <div className="alert alert--error">
                      <strong>Results that will be lost:</strong>
                      <ul>
                        {plan.competitions
                          .filter((c) => c.resultsChanged && c.resultsChanged.length > 0)
                          .map((c) => (
                            <li key={c.id}>
                              {c.resultsChanged.length} recorded results in &quot;{c.name}&quot; will be lost or changed.
                            </li>
                          ))}
                      </ul>
                    </div>
                  )}
                </div>

                <h4 className="backup-subhead">Competitions to restore</h4>
                <div className="backup-list">
                  {plan.competitions.map((c) => {
                    const currentDecision = decisions[c.id] || {};
                    const isMissingFromArchive = c.choices?.includes("keep") || c.choices?.includes("remove");

                    return (
                      <div key={c.id} className="backup-row backup-row--plan">
                        <div className="backup-row__head">
                          <span className="backup-row__name">{c.name}</span>
                          {plan.scope === "tournament" && !plan.undo && !onlyCompId && !isMissingFromArchive && (
                            <button
                              type="button"
                              className="btn btn--sm"
                              aria-label={`Restore only "${c.name}"`}
                              disabled={planning}
                              onClick={() => handleRestoreOnly(c.id)}
                            >
                              Restore only this one
                            </button>
                          )}
                          {plan.scope === "tournament" && !plan.undo && onlyCompId && (
                            <button
                              type="button"
                              className="btn btn--sm"
                              disabled={planning}
                              onClick={handleShowAll}
                            >
                              Show all competitions
                            </button>
                          )}
                        </div>

                        {!c.asked ? (
                          <div className="backup-row__note">Will be created as a new competition.</div>
                        ) : isMissingFromArchive ? (
                          <div>
                            <div className="backup-row__note">This competition is not in the backup archive:</div>
                            <label className="backup-choice">
                              <input
                                type="radio"
                                name={`decision-${c.id}`}
                                value="keep"
                                checked={currentDecision.action === "keep"}
                                onChange={() => handleDecisionChange(c.id, "keep")}
                              />
                              Keep &quot;{c.name}&quot;
                            </label>
                            <label className="backup-choice">
                              <input
                                type="radio"
                                name={`decision-${c.id}`}
                                value="remove"
                                checked={currentDecision.action === "remove"}
                                onChange={() => handleDecisionChange(c.id, "remove")}
                              />
                              Remove &quot;{c.name}&quot;
                            </label>
                          </div>
                        ) : (
                          <div>
                            <div className="backup-row__note">This competition already exists:</div>
                            <label className="backup-choice">
                              <input
                                type="radio"
                                name={`decision-${c.id}`}
                                value="replace"
                                checked={currentDecision.action === "replace"}
                                onChange={() => handleDecisionChange(c.id, "replace")}
                              />
                              Replace &quot;{c.name}&quot;
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
                                placeholder={`${c.name} (Copy)`}
                                value={currentDecision.copyName || ""}
                                onChange={(e) => handleDecisionChange(c.id, "copy", e.target.value)}
                                onClick={() => handleDecisionChange(c.id, "copy")}
                              />
                            </label>
                          </div>
                        )}
                      </div>
                    );
                  })}
                </div>

                {plan.nothingToRestore ? (
                  <div className="alert alert--info backup-plan-end">
                    {plan.message || "Nothing to restore."}
                  </div>
                ) : (
                  <div className="backup-actions backup-plan-end">
                    <button
                      type="button"
                      className="btn btn--primary"
                      disabled={!allChoicesMade}
                      onClick={handleRestoreSubmit}
                    >
                      {!allChoicesMade
                        ? firstOpenComp?.needsCopyName
                          ? `Enter name for copy of "${firstOpenComp.name}"`
                          : `Choose what to do with "${firstOpenComp?.name}"`
                        : "Restore"}
                    </button>
                  </div>
                )}
              </div>
            )}
          </div>
        </div>

        <div className="card">
          <div className="card__title">Copies kept on this computer</div>
          <div className="card__body">
            <p className="backup-intro">
              The app takes a daily copy by itself when something changed. Copies are never deleted automatically.
            </p>

            {loadingBackups ? (
              <p className="backup-row__meta" role="status">Loading kept copies…</p>
            ) : backupsList.length === 0 ? (
              <p className="backup-row__meta">No backup copies kept yet.</p>
            ) : (
              <div className="backup-list">
                {backupsList.map((b) => {
                  const kindLabel = b.kind === "scheduled" ? "Daily" : "Before restore";

                  return (
                    <div key={b.name} className="backup-row">
                      <div className="backup-row__main">
                        <div className="backup-row__title">
                          <span className="badge badge--archive">{kindLabel}</span>
                          <span className="backup-row__name">{b.name}</span>
                        </div>
                        <div className="backup-row__meta">
                          {b.time ? new Date(b.time).toLocaleString() : ""} · {formatFileSize(b.size)}
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
                          className="btn btn--sm btn--danger"
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
