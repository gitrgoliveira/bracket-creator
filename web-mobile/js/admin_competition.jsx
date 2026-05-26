// Competition shell + the sections it embeds (Overview, Settings, Bracket).
// LiveMatchPanel is the bracket-side detail panel for picking match winners.
// See web-mobile/admin_split_plan.md.

const { useState: useStateA, useEffect: useEffectA, useRef: useRefA } = React;

const compMatchStats = window.compMatchStats;
const hasBothSides = window.hasBothSides;
const dmyToIso = window.dmyToIso;
const isoToDmy = window.isoToDmy;
const isValidDate = window.isValidDate;
const validateAndNormalizeDate = window.validateAndNormalizeDate;
const decideNumericUpdate = window.decideNumericUpdate;
// Canonical numeric bounds (admin_helpers.jsx) so the team-size input cap
// stays in lockstep with TEAM_POSITIONS in the scoring modal.
const MIN_YEAR = window.MIN_YEAR;
const MAX_YEAR = window.MAX_YEAR;
const MAX_TEAM_SIZE = window.MAX_TEAM_SIZE;
const StatusBadge = window.StatusBadge;
const formatDate = window.formatDate;
const AdminTopbar = window.AdminTopbar;
const Breadcrumbs = window.Breadcrumbs;
const CourtPicker = window.CourtPicker;
const AdminParticipants = window.AdminParticipants;
const AdminPools = window.AdminPools;
const AdminScoreEditor = window.AdminScoreEditor;
const AdminExport = window.AdminExport;

// Pure result-builder for AdminBracket.recordWinner. Captures the schema
// for a completed-ippon result so it's unit-testable and can't drift
// from the canonical shape that admin_scoring_modal.jsx's buildPatch
// produces (which the backend persists via /recordScore).
//
// Inputs:
//   winnerSide:    "a" | "b"
//   sideA, sideB:  match.sideA / match.sideB (each {id, name, ...})
//   winnerIppons:  array of letter codes ("M","K","D","T","H") the
//                  winning side scored. Empty/missing → ["M"] (the
//                  tap-mode default: a single unspecified ippon).
//   loserIppons:   array of letter codes the losing side scored.
//                  Empty by default; tap/card modes don't expose
//                  loser points.
//
// Output: the result object POST'd to /recordScore.
//
// Copilot finding (PR #103): the scoreboard mode supports 2-ippon wins
// but the previous recordWinner only ever recorded a 1-ippon result
// (winnerPts=1, single-letter ippons array). A 2-ippon match was
// silently truncated to 1 ippon. Fix lifts winnerPts/ipponsA/ipponsB
// from the full array; loserIppons is now first-class too so a 2–1
// win records the loser's ippon instead of dropping it.
//
// Kendo win conditions this helper covers (one side strictly leads):
//   - 2 ippons (sansoo) → automatic win
//   - 1 ippon at time-up → valid win when opponent has 0
//   - 2-1 at time-up → valid win when opponent has 1
// Tied counts (0-0, 1-1, 2-2) are not wins; the scoreboard's Submit
// button is disabled in those states and the operator routes the
// match through the full editor's hikiwake toggle instead.
//
// Exported for vitest at __tests__/admin_competition.test.jsx.
function buildLiveIpponResult(winnerSide, sideA, sideB, winnerIppons, loserIppons) {
  const winner = winnerSide === "a" ? sideA : sideB;
  const winnerLetters = (winnerIppons && winnerIppons.length > 0) ? winnerIppons : ["M"];
  const loserLetters = loserIppons || [];
  return {
    winner,
    status: "completed",
    ipponsA: winnerSide === "a" ? winnerLetters : loserLetters,
    ipponsB: winnerSide === "b" ? winnerLetters : loserLetters,
    score: {
      type: "ippon",
      winnerPts: winnerLetters.length,
      loserPts: loserLetters.length,
      ippons: winnerLetters,
      fouls: { a: 0, b: 0 },
    },
  };
}

// Pure loader for LiveMatchPanel's scoreboard-mode aPoints/bPoints from
// a (possibly completed) match. Reads each side's letters DIRECTLY from
// match.ipponsA / match.ipponsB rather than from score.ippons (which is
// only the winner's letters, by buildPatch / normalizeMatch convention).
//
// Bug fix companion to buildLiveIpponResult: the previous loader gated
// on `winner.id === sideX.id` and pulled from `score.ippons`, which
// returned only the winner's letters. That was fine when the writer
// always recorded loser=[]; once buildLiveIpponResult started writing
// 2-1 wins correctly (loser's single ippon preserved), the loader's
// truncation surfaced — a 2-1 win came back as 2-0 on re-render and
// re-submission silently dropped the loser's letter.
//
// admin_scoring_modal.jsx's initialAPts at line 30-31 already used the
// `m.ipponsA?.filter(...)` pattern; this helper aligns the live panel
// with the same shape.
//
// "•" placeholders are filtered (the full editor uses "•" as an empty
// slot marker; live panel doesn't write those but the data round-trips
// through the same backend fields so filtering is defensive).
//
// Exported for vitest at __tests__/admin_competition.test.jsx.
function loadScoreboardPoints(match) {
  if (!match) return { aPoints: [], bPoints: [] };
  return {
    aPoints: (match.ipponsA || []).filter(x => x && x !== "•"),
    bPoints: (match.ipponsB || []).filter(x => x && x !== "•"),
  };
}

const LiveMatchPanel = React.memo(({ match, compId, courts, isNaginata, onMoveCourt, onRecord, onOverride }) => {
  const [mode, setMode] = useStateA("tap");
  const [aPoints, setAPoints] = useStateA([]);
  const [bPoints, setBPoints] = useStateA([]);
  useEffectA(() => {
    // Load both sides' letters from the canonical match.ipponsA /
    // match.ipponsB fields so the read shape matches what
    // buildLiveIpponResult writes — see loadScoreboardPoints above.
    const { aPoints: a, bPoints: b } = loadScoreboardPoints(match);
    setAPoints(a);
    setBPoints(b);
    // Include status + both sides' ippons in deps so an SSE update for
    // the same match (e.g. an off-panel correction) doesn't leave the
    // scoreboard view showing stale points.
  }, [match.id, match.status, match.ipponsA?.join(","), match.ipponsB?.join(",")]);
  const a = match.sideA, b = match.sideB;
  const isComplete = match.status === "completed";
  return (
    <div className="live-panel">
      <div className="live-panel__head">
        <div className="live-panel__title">Match · {match.id.slice(-6)}</div>
        <div className="live-panel__court">
          {onMoveCourt && courts && courts.length ? (
            <>
              <CourtPicker
                value={match.court}
                courts={courts}
                onChange={(cc) => onMoveCourt(compId, match.id, cc)}
                btnClassName="live-panel__court-btn"
                label="SHIAIJO "
              />
              <span> · {match.scheduledAt || "TBA"}</span>
            </>
          ) : (
            <span>SHIAIJO {match.court} · {match.scheduledAt || "TBA"}</span>
          )}
        </div>
      </div>
      <div className="mode-tabs">
        <button className={mode === "tap" ? "is-active" : ""} onClick={() => setMode("tap")}>Tap winner</button>
        <button className={mode === "card" ? "is-active" : ""} onClick={() => setMode("card")}>Match card</button>
        <button className={mode === "scoreboard" ? "is-active" : ""} onClick={() => setMode("scoreboard")}>Scoreboard</button>
      </div>
      {mode === "tap" && (<>
        {/* Layout convention: SHIRO (White, sideB) on the LEFT, AKA (Red, sideA) on the RIGHT. */}
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8, marginBottom: 10 }}>
          <button className="card" style={{ padding: 16, textAlign: "center", cursor: "pointer", borderColor: match.winner?.id === b.id ? "var(--accent)" : "var(--line)", background: match.winner?.id === b.id ? "var(--accent)" : "var(--surface)", color: match.winner?.id === b.id ? "white" : "inherit" }} onClick={() => onRecord("b", "ippon")}>
            <div style={{ fontSize: 10, fontWeight: 700, opacity: 0.7, letterSpacing: "0.1em" }}>SHIRO (WHITE)</div>
            <div style={{ fontWeight: 600, fontSize: 15, marginTop: 6 }}>{b.name}</div>
            <div style={{ fontSize: 12, opacity: 0.8, marginTop: 2 }}>{b.dojo}</div>
          </button>
          <button className="card" style={{ padding: 16, textAlign: "center", cursor: "pointer", borderColor: match.winner?.id === a.id ? "var(--red)" : "var(--line)", background: match.winner?.id === a.id ? "var(--red)" : "var(--surface)", color: match.winner?.id === a.id ? "white" : "inherit" }} onClick={() => onRecord("a", "ippon")}>
            {/* Label tinted red when unselected (button is on white), inherits white when selected (button background is red) */}
            <div style={{ fontSize: 10, fontWeight: 700, opacity: 0.7, letterSpacing: "0.1em", color: match.winner?.id === a.id ? "inherit" : "var(--red)" }}>AKA (RED)</div>
            <div style={{ fontWeight: 600, fontSize: 15, marginTop: 6 }}>{a.name}</div>
            <div style={{ fontSize: 12, opacity: 0.8, marginTop: 2 }}>{a.dojo}</div>
          </button>
        </div>
        <div className="field__hint" style={{ textAlign: "center" }}>Tap the winner. Use Match card or Scoreboard for detail.</div>
      </>)}
      {mode === "card" && (
        <div className="score-card">
          <div className="score-side score-side--white">
            <div><div className="score-side__lbl">Shiro (White)</div><div className="score-side__name">{b.name}</div><div className="score-side__dojo">{b.dojo}</div></div>
            <div className="score-side__buttons"><button className="btn btn--sm btn--primary" onClick={() => onRecord("b", "ippon", ["M"])}>Win (Ippon)</button></div>
          </div>
          <div className="score-vs">VS</div>
          <div className="score-side score-side--red">
            <div><div className="score-side__lbl">Aka (Red)</div><div className="score-side__name">{a.name}</div><div className="score-side__dojo">{a.dojo}</div></div>
            <div className="score-side__buttons"><button className="btn btn--sm btn--danger" onClick={() => onRecord("a", "ippon", ["M"])}>Win (Ippon)</button></div>
          </div>
        </div>
      )}
      {mode === "scoreboard" && (
        <div className="score-card">
          <div className="score-side score-side--white">
            <div><div className="score-side__lbl">Shiro (White)</div><div className="score-side__name">{b.name}</div></div>
            <div className="score-side__points">{[0, 1].map((i) => (<span key={i} className={`score-pt score-pt--shiro ${bPoints[i] ? "score-pt--filled" : "score-pt--empty"}`}>{bPoints[i] || "·"}</span>))}</div>
            <div className="score-side__buttons">{(isNaginata ? ["M", "K", "D", "T", "S"] : ["M", "K", "D", "T"]).map((cc) => (<button key={cc} className="ipt-btn" onClick={() => setBPoints((p) => p.length < 2 ? [...p, cc] : p)}>{cc}</button>))}<button className="ipt-btn" onClick={() => setBPoints([])}>↺</button></div>
          </div>
          <div className="score-vs">VS</div>
          <div className="score-side score-side--red">
            <div><div className="score-side__lbl">Aka (Red)</div><div className="score-side__name">{a.name}</div></div>
            <div className="score-side__points">{[0, 1].map((i) => (<span key={i} className={`score-pt score-pt--aka ${aPoints[i] ? "score-pt--filled" : "score-pt--empty"}`}>{aPoints[i] || "·"}</span>))}</div>
            <div className="score-side__buttons">{(isNaginata ? ["M", "K", "D", "T", "S"] : ["M", "K", "D", "T"]).map((cc) => (<button key={cc} className="ipt-btn ipt-btn--aka" onClick={() => setAPoints((p) => p.length < 2 ? [...p, cc] : p)}>{cc}</button>))}<button className="ipt-btn" onClick={() => setAPoints([])}>↺</button></div>
          </div>
        </div>
      )}
      {mode === "scoreboard" && (() => {
        // Submit is only valid when one side strictly leads. Tied counts
        // (e.g. 1–1) would otherwise silently get attributed to SHIRO via
        // `aWins ? "a" : "b"`. For draws, use the full editor's hikiwake toggle.
        const aWins = aPoints.length > bPoints.length;
        const bWins = bPoints.length > aPoints.length;
        const hasWinner = aWins || bWins;
        const isTied = !hasWinner && (aPoints.length > 0 || bPoints.length > 0);
        // Submit the FULL points arrays — pre-fix, only aPoints[0]/bPoints[0]
        // (a single letter) was passed and recordWinner hardcoded winnerPts=1,
        // so a 2-ippon win was silently truncated to 1 ippon. recordWinner now
        // builds winnerPts / ipponsA / ipponsB from the array lengths via
        // buildLiveIpponResult.
        const winnerArr = aWins ? aPoints : bPoints;
        const loserArr = aWins ? bPoints : aPoints;
        return (
          <div className="live-panel__actions">
            <button
              className="btn btn--primary btn--full"
              disabled={!hasWinner}
              onClick={() => onRecord(aWins ? "a" : "b", "ippon", winnerArr, loserArr)}
            >Submit result</button>
            {isTied && (
              <div className="field__hint" style={{ textAlign: "center", marginTop: 6 }}>
                Tied — open the full score editor to record a draw (hikiwake).
              </div>
            )}
          </div>
        );
      })()}
      {isComplete && (
        <div style={{ marginTop: 12, padding: 10, background: "#ecfdf5", border: "1px solid #a7f3d0", borderRadius: 8, fontSize: 12.5, color: "#065f46" }}>
          ✓ Recorded — {match.winner?.name} advances
        </div>
      )}
      <div style={{ marginTop: 14, paddingTop: 14, borderTop: "1px dashed var(--line)" }}>
        <button className="btn btn--sm btn--full" onClick={() => {
          // prompt() returns "   " for whitespace-only input — which is
          // truthy under `if (name)` and would persist a whitespace key
          // as `m.Winner` on the backend (and then mismatch the canonical
          // SideA / SideB names downstream). Trim defensively and only
          // override when there's a real value to record.
          const raw = prompt("Enter the name of the winner to override:", match.winner?.name || match.sideA?.name);
          const name = raw?.trim();
          if (name) onOverride(name);
        }}>Force winner (manual override)</button>
      </div>
    </div>
  );
});
LiveMatchPanel.displayName = "LiveMatchPanel";

function AdminCompOverview({ c, pools, poolMatches, bracket, onSection }) {
  // Prefer the props passed from the detail fetch, but fall back to whatever
  // is already on `c` when the detail hasn't loaded yet (or errored). Using ??
  // avoids overwriting non-null fields on `c` with undefined prop values.
  const statsSource = { ...c, pools: pools ?? c.pools, poolMatches: poolMatches ?? c.poolMatches, bracket: bracket ?? c.bracket };
  const { total, done, live } = compMatchStats(statsSource);
  const pct = total ? Math.round((done / total) * 100) : 0;
  const effectiveBracket = bracket ?? c.bracket;
  return (
    <div>
      <div className="stats-strip">
        <div className="stat-box"><div className="v">{c.players.length}</div><div className="l">{c.kind === "team" ? "Teams" : "Participants"}</div></div>
        <div className="stat-box"><div className="v">{c.players.filter((p) => p.seed).length}</div><div className="l">Seeded</div></div>
        <div className="stat-box"><div className="v">{done}/{total}</div><div className="l">Matches done</div></div>
        <div className="stat-box"><div className="v" style={{ color: live > 0 ? "var(--red)" : "inherit" }}>{live}</div><div className="l">Live now</div></div>
      </div>
      <div className="card" style={{ marginBottom: 16 }}>
        <div className="card__head"><div className="card__title">Progress</div><div className="card__sub">{pct}%</div></div>
        <div style={{ height: 8, background: "var(--line-2)", borderRadius: 999, overflow: "hidden" }}>
          <div style={{ height: "100%", width: pct + "%", background: "var(--accent)", transition: "width 300ms" }}></div>
        </div>
      </div>
      <div className="row">
        <button className="card" style={{ textAlign: "left", cursor: "pointer", border: "1px solid var(--line)" }} onClick={() => onSection("scores")}>
          <div className="card__title" style={{ marginBottom: 6 }}>Scores →</div>
          <div className="card__sub">Update or correct match results</div>
        </button>
        <button className="card" style={{ textAlign: "left", cursor: "pointer", border: "1px solid var(--line)" }} onClick={() => onSection(effectiveBracket ? "bracket" : "pools")}>
          <div className="card__title" style={{ marginBottom: 6 }}>Live results →</div>
          <div className="card__sub">Visual bracket / pool standings</div>
        </button>
      </div>
    </div>
  );
}

function AdminSettings({ c, tournament, onUpdate, onBack, password, showToast, onStatusChange }) {
  const [lastSaved, setLastSaved] = useStateA(null);
  const [saveErr, setSaveErr] = useStateA(null);
  const [deleting, setDeleting] = useStateA(false);
  const [invalidating, setInvalidating] = useStateA(false);
  const [local, setLocal] = useStateA({ ...c });
  const debounceRef = useRefA(null);
  // AdminSettings unmounts when the user navigates to a different section
  // via onSection() (AdminCompetition rerenders with a different child).
  // saveNow's .then/.catch and the delete handler's finally fire on
  // own state — gate via mountedRef. Same teardown-race shape as
  // admin_participants.jsx apply().
  const mountedRef = useRefA(true);
  useEffectA(() => () => { mountedRef.current = false; }, []);

  // Refs for the async saveNow timer to read fresh state at fire time
  // (NOT closure-captured at saveLater() call time). Same shape as
  // admin.jsx's tRef/onUpdateRef pattern from round-11.
  const cRef = useRefA(c);
  useEffectA(() => { cRef.current = c; }, [c]);
  const localRef = useRefA(local);
  useEffectA(() => { localRef.current = local; }, [local]);

  // Track which settings fields the user has actively edited since the
  // last successful save. Used by:
  //  (a) the sync effect below — preserve user's pending edits on these
  //      fields while still absorbing SSE updates to OTHER fields, and
  //  (b) saveNow's payload builder — overlay user-edited values onto a
  //      FRESH snapshot of `c` (cRef.current), so the PUT body reflects
  //      concurrent server-side changes to fields the user isn't editing
  //      rather than stale values captured at keystroke time.
  //
  // Without this set, the prior debounce-gate approach (`if
  // (debounceRef.current) return prev`) had a 400ms window where a
  // concurrent admin's settings change would land in `c` but be dropped
  // by the sync effect AND overwritten by saveNow's stale captured
  // snapshot — net effect: one-field edits silently revert
  // simultaneous edits to other fields. Caught by Copilot round-15.
  const editedFieldsRef = useRefA(new Set());

  // Sync server-driven changes into local state (SSE → AdminApp → c prop).
  // For each field on `c`, propagate to `local` UNLESS the user has an
  // unsaved edit pending on that field (tracked in editedFieldsRef).
  // This absorbs concurrent admin changes without clobbering the user's
  // in-progress typing.
  //
  // Deps cover UI-rendered fields and any field round-tripped through
  // saveNow's PUT allowlist (`finalNext`). Status is listed so a
  // concurrent start/complete propagates into local for the delete-
  // confirm prompt's `local.status && local.status !== "setup"` check.
  useEffectA(() => {
    setLocal(prev => {
      const next = { ...prev };
      Object.keys(c).forEach(k => {
        if (!editedFieldsRef.current.has(k)) {
          next[k] = c[k];
        }
      });
      return next;
    });
  }, [c.id, c.name, c.date, c.startTime, c.poolSize, c.poolWinners, c.poolSizeMode, c.courts, c.roundRobin, c.withZekkenName, c.teamSize, c.numberPrefix, c.format, c.kind, c.mirror, c.status, c.poolFormat, c.poolMatchDuration, c.playoffMatchDuration, c.swissRounds, c.swissCurrentRound, c.naginata, c.checkInEnabled]);

  const saveNow = () => {
    // Build `effective` from the LATEST server-known state (cRef.current)
    // overlaid with the user's currently-edited fields. Pre-fix this
    // function took a captured `next` arg from saveLater, which held a
    // snapshot from the moment of the keystroke — so any SSE updates
    // that landed during the 400ms debounce were ignored at save time,
    // and the PUT body silently reverted concurrent admin changes to
    // unrelated fields. Reading from refs at fire time (not closure
    // capture) absorbs those concurrent changes naturally. Caught by
    // Copilot round-15.
    const latestC = cRef.current;
    const localSnap = localRef.current;
    const effective = { ...latestC };
    editedFieldsRef.current.forEach(k => {
      effective[k] = localSnap[k];
    });

    // Use the shared validator (admin_helpers.jsx). Returns the
    // canonical DD-MM-YYYY form on success, or an error message on
    // failure (bad shape, semantic-invalid day, year out of range).
    //
    // Skip validation for empty date — the backend's validateDateDMY
    // accepts "" as "Date TBA" and competitions created via the import
    // path can land here with an empty Date. Without this skip, the
    // user would be unable to change ANY unrelated setting on a
    // date-less competition (round-robin toggle, pool size, etc.) — the
    // first debounced saveLater fires saveNow, which rejects with
    // "Invalid date" even though the user hasn't touched the date.
    let dateNorm = "";
    if (effective.date && effective.date.trim() !== "") {
      const { norm, error: dateError } = validateAndNormalizeDate(effective.date);
      if (dateError) {
        setSaveErr(dateError);
        return;
      }
      dateNorm = norm;
    }

    // Trim before comparing AND before sending. The backend trims
    // `comp.Name` on save, so without normalizing here the JS-side
    // uniqueness check would compare "  Men's Cup  " against the
    // canonical "Men's Cup" and miss — landing two competitions with the
    // same effective name. Send the trimmed value so the value the user
    // sees in the input matches what the server stores.
    const trimmedName = (effective.name || "").trim();
    // Cross-file guard symmetry with the tournament edit/create paths
    // (admin_setup.jsx AdminEditTournament:80, app.jsx CreateTournament:410)
    // and with handlers_competition.go PUT (which now returns 400 on
    // empty-after-trim Name). Without this client-side guard, every
    // saveLater-debounced keystroke that lands on an empty Name fires a
    // wasted PUT roundtrip and only surfaces the error in the inline
    // .catch handler 400ms later. Keep the failure inline + immediate
    // like the date validation above.
    if (!trimmedName) {
      setSaveErr("Competition name is required.");
      return;
    }
    if (trimmedName.toLowerCase() !== latestC.name.toLowerCase()) {
      const exists = (tournament.competitions || []).some(cc => cc.id !== latestC.id && cc.name.toLowerCase() === trimmedName.toLowerCase());
      if (exists) {
        setSaveErr(`Competition name "${trimmedName}" is already in use.`);
        return;
      }
    }

    // Trim numberPrefix — the input does substring(0, 3) per keystroke
    // but doesn't trim, so typing "  A" stores "  A" in local state and
    // (without this) lands "  A" on the server. The CREATE flow
    // (AdminCreateCompetition.create's deriveCompetitionName + trim
    // chain in admin_setup.jsx) already trims at create time; this
    // mirrors that for the SETTINGS edit flow so participant numbers
    // generated from the prefix can't end up like "  A1" / "  A2".
    // Cross-file guard symmetry: same shape as the comp.Name trim above.
    const trimmedPrefix = (effective.numberPrefix || "").trim();
    // Build the PUT payload from settings fields ONLY — do NOT spread the
    // full `c` snapshot or the full `next` snapshot. Pre-fix this was
    // `{ ...c, ...next, ... }`, which carried `local.status` and
    // `local.players` (and any other field the JSX/effects don't touch)
    // into the PUT body. If the sync-to-local effect deps list was
    // incomplete for any such field, SSE-pushed changes to that field
    // would not propagate into `local`, and the next save of ANY unrelated
    // setting would PUT the stale value back to the server — effectively
    // reverting the server-side change. Whitelisting the payload makes
    // AdminSettings genuinely settings-only and decouples save correctness
    // from the deps-list completeness of the sync effect.
    //
    // Fields server-managed via dedicated endpoints (status, players,
    // hasParticipantIDs) are deliberately excluded. If a new settings
    // field is added to the JSX or the OpenAPI settings list, also add
    // it here.
    //
    // `mirror` is in the allowlist even though AdminSettings doesn't
    // expose it as an editable control. data.jsx:200 (buildEmptyCompetition)
    // defaults new competitions to `mirror: true`; the backend transform
    // unconditionally applies `current.Mirror = comp.Mirror`, so an
    // omitted field would JSON-encode to false and clobber the disk
    // value on every settings save. Round-tripping `effective.mirror`
    // (sourced from latestC unless the user edited it) preserves the value.
    //
    // safeInt for the numeric fields: decideNumericUpdate stores NaN in
    // local state when the user clears a number input (so the render
    // layer can show "" instead of "0"). If the user clears poolSize
    // and THEN edits a non-numeric field, the new field's saveLater
    // fires saveNow with the cleared poolSize still in the edited
    // overlay. JSON.stringify({n: NaN}) produces '{"n":null}' — Go binds
    // JSON null to int as 0 — backend transform writes 0 to disk,
    // clobbering the prior good value. Falling back to `latestC.<field>`
    // when the effective value isn't a usable positive integer preserves
    // the disk value until the user types a valid replacement.
    const safeInt = (v, fallback) =>
      Number.isFinite(v) && Number.isInteger(v) && v >= 1 ? v : fallback;
    // safeNonNegInt is the >=0 sibling for the per-phase duration
    // fields. T047: 0 means "no override — fall through to the legacy
    // matchDuration default per backend ApplyCompetitionDefaults", so
    // we DO want 0 to round-trip. Same NaN/fractional/negative guards
    // as safeInt; the only difference is the lower bound. Same
    // disk-clobber concern as safeInt: cleared input → NaN → JSON
    // null → Go zero. We fall back to latestC.<field> in that case so
    // an unrelated-field save doesn't silently zero out a duration the
    // user previously typed.
    const safeNonNegInt = (v, fallback) =>
      Number.isFinite(v) && Number.isInteger(v) && v >= 0 ? v : fallback;
    const finalNext = {
      id: latestC.id,
      name: trimmedName,
      date: dateNorm,
      startTime: effective.startTime,
      poolSize: safeInt(effective.poolSize, latestC.poolSize),
      poolWinners: safeInt(effective.poolWinners, latestC.poolWinners),
      poolSizeMode: effective.poolSizeMode,
      courts: effective.courts,
      roundRobin: effective.roundRobin,
      withZekkenName: effective.withZekkenName,
      teamSize: safeInt(effective.teamSize, latestC.teamSize),
      numberPrefix: trimmedPrefix,
      format: effective.format,
      kind: effective.kind,
      mirror: effective.mirror,
      // FR-050 / T044: round-robin shape selector. Only meaningful when
      // the format runs pool play; the backend's validateCompetitionFormat
      // accepts the empty value, so a non-pool format can safely PUT "".
      poolFormat: effective.poolFormat || "",
      // FR-052..FR-054 / T047: per-phase duration overrides. Zero means
      // "use legacy default" — fall through to safeNonNegInt with
      // latestC's value to avoid NaN-clobbering a previously-set value.
      poolMatchDuration: safeNonNegInt(effective.poolMatchDuration, latestC.poolMatchDuration || 0),
      playoffMatchDuration: safeNonNegInt(effective.playoffMatchDuration, latestC.playoffMatchDuration || 0),
      // T190 (FR-050a): swissRounds is editable pre-start; safeInt
      // preserves the previously-saved value when the input is
      // cleared (so the cleared display doesn't clobber the disk
      // value before the user types a valid replacement).
      swissRounds: safeInt(effective.swissRounds, latestC.swissRounds || 0),
      naginata: !!effective.naginata,
      checkInEnabled: !!effective.checkInEnabled,
    };
    // Capture the snapshot of edited fields we're about to persist. On
    // success we clear ONLY those fields from the edited set — preserving
    // any fields the user touched DURING the in-flight save (those need
    // to round-trip through a subsequent save).
    const persistingFields = new Set(editedFieldsRef.current);
    Promise.resolve(onUpdate(finalNext)).then(() => {
      if (!mountedRef.current) return;
      // Drop the fields we just persisted from the edited set so the
      // sync effect can absorb the server-confirmed values on the next
      // SSE round-trip. Fields edited DURING the in-flight save stay in
      // the set and roll into the next saveLater.
      persistingFields.forEach(k => editedFieldsRef.current.delete(k));
      const now = new Date();
      setLastSaved(`${String(now.getHours()).padStart(2, "0")}:${String(now.getMinutes()).padStart(2, "0")}:${String(now.getSeconds()).padStart(2, "0")}`);
      setSaveErr(null);
    }).catch((e) => {
      if (!mountedRef.current) return;
      // Keep edited fields in the set — the user can retry. The next
      // saveLater will pick them up via the same edited-overlay path.
      // updateCompetition already surfaced the error via showToast;
      // mirror it inline next to the input so the user sees the cause
      // next to the field they were editing without a duplicate toast.
      setSaveErr(e?.message || "Save failed");
    });
  };

  // saveLater takes NO snapshot arg — pre-fix it captured `next` at
  // call time, which became stale if SSE updated `c` during the 400ms
  // debounce. saveNow now reads cRef.current + localRef.current at
  // fire time, so the captured-snapshot bug is gone.
  const saveLater = () => {
    clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      debounceRef.current = null;
      saveNow();
    }, 400);
  };

  // Cancel any pending debounced save on unmount so the timer can't fire
  // saveNow() (and trigger state updates / API calls) after teardown.
  useEffectA(() => () => {
    if (debounceRef.current) {
      clearTimeout(debounceRef.current);
      debounceRef.current = null;
    }
  }, []);

  const update = (k, v) => {
    editedFieldsRef.current.add(k);
    const next = { ...local, [k]: v };
    setLocal(next);
    saveLater();
  };

  const updateNow = (k, v) => {
    editedFieldsRef.current.add(k);
    const next = { ...local, [k]: v };
    setLocal(next);
    // localRef syncs via useEffect; for immediate-save handlers we need
    // saveNow to see the just-set value. Update the ref synchronously
    // so saveNow's read happens before React's batched state flush.
    localRef.current = next;
    saveNow();
  };

  // Number-input variant of `update`. Stores NaN in local state for empty
  // input so the render side can keep the display empty (see
  // decideNumericUpdate's contract). Marks the field as edited so the
  // sync effect preserves the user's in-progress clear / typed value
  // even if SSE pushes a c-update during the 400ms debounce window.
  //
  // safeInt in saveNow's finalNext allowlist bridges the gap: an invalid
  // value (NaN / 1.5 / -1) falls back to latestC.<field>, so the PUT is
  // a no-op for that field but cross-field saves (e.g. Name typed
  // concurrently) still land. The cleared display resolves to the saved
  // value on the next SSE / PUT-response merge after the user types a
  // valid replacement or moves on.
  const updateNumber = (k, raw, min = 1) => {
    const { value } = decideNumericUpdate(raw, min);
    editedFieldsRef.current.add(k);
    const next = { ...local, [k]: value };
    setLocal(next);
    saveLater();
  };

  const toggleCourt = (cc) => {
    const nextCourts = local.courts.includes(cc) ? local.courts.filter((x) => x !== cc) : [...local.courts, cc].sort();
    if (nextCourts.length) updateNow("courts", nextCourts);
  };

  return (
    <div className="card">
      <div className="card__head">
        <div className="card__title">Competition settings</div>
        <div style={{
          fontSize: 12.5,
          padding: "4px 8px",
          borderRadius: 4,
          background: saveErr ? "var(--red-soft)" : lastSaved ? "var(--accent-soft)" : "transparent",
          color: saveErr ? "var(--red)" : "var(--accent)",
          fontWeight: 600,
          transition: "all 300ms"
        }}>
          {saveErr ? `⚠ ${saveErr}` : lastSaved ? `✓ Saved at ${lastSaved}` : ""}
        </div>
      </div>
      <div className="row">
        <div className="field"><label className="field__label">Display name</label><input className="input" value={local.name} onChange={(e) => update("name", e.target.value)} /></div>
        <div className="field">
          <label className="field__label">Date</label>
          {/* HTML <input type="date"> uses ISO YYYY-MM-DD; convert at the */}
          {/* boundary so local state (and the saved payload) stays in the */}
          {/* canonical DD-MM-YYYY format. Picker bounds match MIN_YEAR/ */}
          {/* MAX_YEAR (admin_helpers.jsx) so a typed date can't pass */}
          {/* validation but be unreachable via the picker. */}
          <input className="input" type="date" min={`${MIN_YEAR}-01-01`} max={`${MAX_YEAR}-12-31`} value={dmyToIso(local.date)} onChange={(e) => update("date", isoToDmy(e.target.value))} />
          <div className="field__hint">Pick the competition day.</div>
        </div>
        <div className="field"><label className="field__label">Start time</label><input className="input" type="time" value={local.startTime} onChange={(e) => update("startTime", e.target.value)} /></div>
      </div>
      {local.kind === "team" && (
        <div className="field">
          <label className="field__label">Team size</label>
          {/* Cap is MAX_TEAM_SIZE (admin_helpers.jsx). TEAM_POSITIONS in */}
          {/* admin_scoring_modal.jsx is built from the same constant, so */}
          {/* this input can't allow a value the scoring UI doesn't render. */}
          {/* Render NaN as "" so clearing the input stays empty instead of */}
          {/* collapsing to "0"; updateNumber gates the debounced save so a */}
          {/* cleared/invalid value never lands on the backend as 0. */}
          <input
            className="input"
            type="number"
            min="1"
            max={MAX_TEAM_SIZE}
            value={Number.isFinite(local.teamSize) ? local.teamSize : ""}
            onChange={(e) => updateNumber("teamSize", e.target.value, 1)}
          />
        </div>
      )}
      <div className="field">
        <label className="field__label">Assigned shiaijo (courts)</label>
        <div className="radio-group">
          {tournament.courts.map((cc) => (
            <button key={cc} className={`radio-pill ${local.courts.includes(cc) ? "is-active" : ""}`} type="button" onClick={() => toggleCourt(cc)}>Shiaijo (court) {cc}</button>
          ))}
        </div>
        <div className="field__hint">Concurrency = number of shiaijo (courts) assigned. Schedule prevents double-booking with other competitions.</div>
      </div>
      {local.format === "mixed" && (
        <>
          <div className="field">
            <label className="field__label">Pool size is a</label>
            <div className="radio-group">
              <button className={`radio-pill ${local.poolSizeMode === "max" ? "is-active" : ""}`} type="button" onClick={() => updateNow("poolSizeMode", "max")}>maximum</button>
              <button className={`radio-pill ${local.poolSizeMode === "min" ? "is-active" : ""}`} type="button" onClick={() => updateNow("poolSizeMode", "min")}>minimum</button>
            </div>
          </div>
          <div className="row">
            {/* Same NaN-as-"" + gated-save pattern as Team size above. */}
            {/* min=3 for poolSize matches the backend's pool-size lower */}
            {/* bound (3 players minimum to run a round-robin). */}
            <div className="field"><label className="field__label">Players per pool</label><input
              className="input"
              type="number"
              min="3"
              value={Number.isFinite(local.poolSize) ? local.poolSize : ""}
              onChange={(e) => updateNumber("poolSize", e.target.value, 3)}
            /></div>
            <div className="field"><label className="field__label">Winners per pool</label><input
              className="input"
              type="number"
              min="1"
              value={Number.isFinite(local.poolWinners) ? local.poolWinners : ""}
              onChange={(e) => updateNumber("poolWinners", e.target.value, 1)}
            /></div>
          </div>
        </>
      )}
      {/* FR-052..FR-054 / T047: per-phase match-duration inputs. */}
      {/* Render rules: */}
      {(local.format === "mixed" || local.format === "league" || local.format === "playoffs" || local.format === "swiss") && (
        <div className="row">
          {(local.format === "mixed" || local.format === "league" || local.format === "swiss") && (
            <div className="field">
              <label className="field__label">{local.format === "swiss" ? "Round match duration (min)" : "Pool match duration (min)"}</label>
              <input
                className="input"
                type="number"
                min="0"
                step="1"
                value={Number.isFinite(local.poolMatchDuration) && local.poolMatchDuration > 0 ? local.poolMatchDuration : ""}
                onChange={(e) => updateNumber("poolMatchDuration", e.target.value, 0)}
                placeholder="default"
              />
              <div className="field__hint">{local.format === "swiss" ? "Estimated minutes per Swiss-round match. Leave blank for default." : "Estimated minutes per pool match. Leave blank for default."}</div>
            </div>
          )}
          {(local.format === "playoffs" || local.format === "mixed") && (
            <div className="field">
              <label className="field__label">Playoff match duration (min)</label>
              <input
                className="input"
                type="number"
                min="0"
                step="1"
                value={Number.isFinite(local.playoffMatchDuration) && local.playoffMatchDuration > 0 ? local.playoffMatchDuration : ""}
                onChange={(e) => updateNumber("playoffMatchDuration", e.target.value, 0)}
                placeholder="default"
              />
              <div className="field__hint">Estimated minutes per playoff/knockout match. Leave blank for default.</div>
            </div>
          )}
        </div>
      )}

      {/* T190 (FR-050a): swissRounds settings editor. Only rendered */}
      {/* when format=swiss. The backend allows editing pre-start; */}
      {/* changing rounds after start is allowed too (the next */}
      {/* "Generate next round" call will respect the new cap). */}
      {local.format === "swiss" && (
        <div className="field">
          <label className="field__label">Number of Swiss rounds</label>
          <input
            className="input"
            type="number"
            min="1"
            step="1"
            value={Number.isFinite(local.swissRounds) ? local.swissRounds : ""}
            onChange={(e) => updateNumber("swissRounds", e.target.value, 1)}
            style={{ maxWidth: 120 }}
          />
          <div className="field__hint">Typical: 4 rounds for 16 players, 5 for 32, 6 for 64 (≈ log₂ of field size).</div>
        </div>
      )}
      <div className="field">
        <label className="field__label">Player number prefix <span style={{ fontWeight: 400, color: "var(--ink-3)" }}>(optional)</span></label>
        <input className="input" placeholder="e.g. A" maxLength="3" value={local.numberPrefix || ""} onChange={(e) => update("numberPrefix", e.target.value.substring(0, 3))} style={{ maxWidth: 80 }} />
        <div className="field__hint">Single letter prefix for participant numbers (A1, B1…). Keeps numbers unique across competitions.</div>
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
        <label className="checkbox"><input type="checkbox" checked={local.roundRobin} onChange={(e) => updateNow("roundRobin", e.target.checked)} /> Round-robin in pools</label>
<div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <label className="checkbox"><input type="checkbox" checked={local.withZekkenName} onChange={(e) => updateNow("withZekkenName", e.target.checked)} disabled={local.kind === "team"} /> Use Zekken display name</label>
          <div className="field__hint" style={{ fontSize: 11, paddingLeft: 22 }}>{local.kind === "team" ? "(Only applicable for individual competitions)" : "When enabled, participant CSV uses three columns: Name, Zekken, Dojo."}</div>
        </div>
        <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <label className="checkbox"><input type="checkbox" checked={!!local.naginata} onChange={(e) => updateNow("naginata", e.target.checked)} /> Naginata competition</label>
          <div className="field__hint" style={{ fontSize: 11, paddingLeft: 22 }}>Adds the Sune (S) ippon button to the score editor. Use for Naginata divisions.</div>
        </div>
        <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
          <label className="checkbox"><input type="checkbox" checked={!!local.checkInEnabled} onChange={(e) => updateNow("checkInEnabled", e.target.checked)} /> Check-in tracking</label>
          <div className="field__hint" style={{ fontSize: 11, paddingLeft: 22 }}>Show check-in column and counter. Disable for competitions that don't need attendance tracking.</div>
        </div>
      </div>
      <div style={{ marginTop: 24, padding: 16, borderTop: "1px solid var(--line)", display: "flex", flexDirection: "column", gap: 12 }}>
        {(local.status === "pools" || local.status === "playoffs") && (
          <div>
            <button className="btn btn--danger btn--ghost" disabled={invalidating || deleting} onClick={async () => {
              if (confirm(`Mark "${local.name}" as invalid? It will be excluded from results and can be deleted afterwards.`)) {
                setInvalidating(true);
                try {
                  const updated = await window.API.invalidateCompetition(local.id, password);
                  if (mountedRef.current) {
                    // Use the server response (if any) so that server-side
                    // field updates are reflected immediately. Fall back to
                    // forcing only `status: "invalid"` if the response isn't
                    // a competition object. Don't call onUpdate — that would
                    // trigger a full PUT with unsanitised local state.
                    const newStatus = (updated && typeof updated === "object" ? updated.status : null) ?? "invalid";
                    setLocal(prev => (updated && typeof updated === "object"
                      ? { ...prev, ...updated, players: updated.players ?? prev.players }
                      : { ...prev, status: "invalid" }));
                    if (onStatusChange) onStatusChange(newStatus);
                    showToast("Competition marked invalid.", "success");
                  }
                } catch (e) {
                  if (mountedRef.current) showToast(e.message, "error");
                } finally {
                  if (mountedRef.current) setInvalidating(false);
                }
              }
            }}>
              {invalidating && <span className="spinner" />}
              {invalidating ? "Marking invalid…" : "Mark competition invalid"}
            </button>
            <div className="field__hint" style={{ marginTop: 4 }}>Required before deleting an in-progress competition.</div>
          </div>
        )}
        <button className="btn btn--danger btn--ghost" disabled={deleting || invalidating} onClick={async () => {
          const started = local.status && local.status !== "setup" && local.status !== "draw-ready";
          const msg = started
            ? `"${local.name}" has already started. Deleting it will remove ALL matches and results. This cannot be undone. Continue?`
            : `Are you sure you want to delete "${local.name}"? This action cannot be undone.`;
          if (confirm(msg)) {
            setDeleting(true);
            try {
              const ok = await window.API.deleteCompetition(local.id, password);
              // onBack() unmounts AdminSettings via the parent's view
              // switch; setDeleting(false) in finally would then fire on
              // a torn-down component. Gate via mountedRef.
              if (ok) onBack();
              else if (mountedRef.current) showToast("Failed to delete competition.", "error");
            } catch (e) {
              console.error("Delete competition failed:", e);
              if (mountedRef.current) showToast(e.message, "error");
            } finally {
              if (mountedRef.current) setDeleting(false);
            }
          }
        }}>
          {deleting && <span className="spinner" />}
          {deleting ? "Deleting…" : "Delete competition"}
        </button>
        <div className="field__hint" style={{ marginTop: 4 }}>Deleting a started competition will remove all matches and results.</div>
      </div>
    </div>
  );
}

function AdminBracket({ c, t, bracket, onMoveCourt, tweaks, password, showToast }) {
  const [selected, setSelected] = useStateA(null);
  const scrollRef = useRefA(null);
  const [autoScrollId, setAutoScrollId] = useStateA(null);

  // Recenter on the running match whenever it changes (initial bracket
  // load, or one match finishing and the next starting). Empty deps would
  // miss the case where `bracket` is still null on first mount and only
  // populates via the detail fetch / SSE.
  const runningMatchId = (bracket?.rounds || []).flatMap(r => r).find(m => m.status === "running")?.id || null;
  useEffectA(() => {
    if (runningMatchId) setAutoScrollId(runningMatchId + "::" + Date.now());
  }, [runningMatchId]);

  if (!bracket || !bracket.rounds) {
    const previewMode = c && c.status === "draw-ready";
    return <div className="empty"><div className="icon">⚙</div><h3>Bracket not generated yet</h3><div>{previewMode ? "Bracket not available for this format preview." : "Start the competition to build the bracket."}</div></div>;
  }
  const select = (m, ri, mi) => setSelected({ matchId: m.id, ri, mi });
  // Look up the selected match by ID rather than [ri][mi] index. The
  // index can go stale if an SSE-driven bracket rebuild (playoff
  // regeneration, source-comp promotion) reorders entries between the
  // user's click and the next render/action; the ID is the only stable
  // handle we set in `selected`. Returns null when the match has been
  // removed entirely from the bracket.
  const findSelectedMatch = () => {
    if (!selected || !bracket?.rounds) return null;
    for (const round of bracket.rounds) {
      for (const m of (round || [])) {
        if (m && m.id === selected.matchId) return m;
      }
    }
    return null;
  };
  // winnerIppons/loserIppons are arrays of letter codes. Tap mode (no
  // detail) and card mode (single explicit letter) pass a single-element
  // array; scoreboard mode passes the full points it accumulated for
  // each side. See buildLiveIpponResult above for the schema rationale.
  const recordWinner = (winnerSide, _mode = "ippon", winnerIppons = ["M"], loserIppons = []) => {
    const m = findSelectedMatch();
    if (!m) return;
    const winner = winnerSide === "a" ? m.sideA : m.sideB;
    if (!winner) return;

    const result = buildLiveIpponResult(winnerSide, m.sideA, m.sideB, winnerIppons, loserIppons);

    // Don't call onUpdate(c) on success — AdminApp's onUpdate is the
    // competition-config PUT, which would overwrite server state with
    // the (now-stale) c prop. SSE + patchCompetitionData in AdminApp
    // already refreshes the bracket after a recordScore.
    window.API.recordScore(c.id, m.id, result, password, m)
      .catch(err => showToast(err.message, "error"));
  };

  const overrideWinner = (winnerName) => {
    if (!selected) return;
    // Same reason as recordWinner: rely on SSE to refresh, don't
    // route the success path through the config-PUT callback.
    window.API.overrideBracketWinner(c.id, selected.matchId, winnerName, password)
      .catch(err => showToast(err.message, "error"));
  };
  const selectedMatch = findSelectedMatch();
  return (
    <div className="row" style={{ gridTemplateColumns: "1fr 360px", alignItems: "start" }}>
      <div>
        <div className="bracket-canvas" ref={scrollRef}>
          <div className="bracket-canvas__inner">
            <window.BracketTree
              rounds={bracket.rounds}
              variant={tweaks.cardVariant}
              showDojo={tweaks.showDojo}
              onMatchClick={select}
              highlightedMatchId={selected?.matchId}
              autoScrollMatchId={autoScrollId}
              scrollContainerRef={scrollRef}
            />
          </div>
        </div>
      </div>
      <div>
        {hasBothSides(selectedMatch) ? (
          <LiveMatchPanel
            match={selectedMatch}
            compId={c.id}
            courts={t?.courts || []}
            isNaginata={!!c.naginata}
            onMoveCourt={onMoveCourt}
            onRecord={recordWinner}
            onOverride={overrideWinner}
          />
        ) : selectedMatch ? (
          <div className="empty"><h3>Match not ready</h3><div style={{ fontSize: 13 }}>Waiting for upstream winners.</div></div>
        ) : (
          <div className="empty"><div className="icon">👆</div><h3>Pick a match</h3><div style={{ fontSize: 13 }}>Click any match in the bracket to record results.</div></div>
        )}
      </div>
    </div>
  );
}

// T191 (US13 — FR-050d): pure helpers for the Swiss-round admin
// section. Extracted so the conditional logic ("which round, are
// matches complete, can we generate next?") is unit-testable without
// mounting AdminSwissRounds. Mirrors the admin_scoring_modal.jsx
// pattern (buildDecisionBody / shouldShowEnchoMaxBanner pure helpers
// exported for tests).

// Returns the canonical match-ID prefix for a Swiss round. Matches
// engine/swiss.go's `swissPoolName`/`swissMatchID` — keep in sync.
function swissRoundIDPrefix(round) {
  return `Swiss-R${round}-`;
}

// Filter the competition's pool-matches list down to a single
// Swiss round's matches. Returns [] for non-Swiss formats and for
// rounds that have not been generated yet.
function filterSwissRoundMatches(poolMatches, round) {
  if (!poolMatches || !Array.isArray(poolMatches) || !round || round < 1) return [];
  const prefix = swissRoundIDPrefix(round);
  return poolMatches.filter(m => (m.id || "").startsWith(prefix));
}

// Returns true when every match in `matches` has status "completed".
// Returns false for an empty list — an unbegun round is not "complete"
// from the admin's perspective; the Generate Next Round button stays
// disabled until the round exists AND every match in it is done.
function isSwissRoundComplete(matches) {
  if (!matches || matches.length === 0) return false;
  return matches.every(m => m.status === "completed");
}

// Returns true when the operator should see the "Generate next round"
// button enabled. The conditions: format=swiss, current round generated
// (and complete), and we haven't reached the final round yet.
function canGenerateNextSwissRound(comp, currentRoundMatches) {
  if (!comp || comp.format !== "swiss") return false;
  const total = comp.swissRounds || 0;
  const current = comp.swissCurrentRound || 0;
  if (total < 1) return false;
  if (current >= total) return false;
  // First round is generated on competition start; if currentRound is
  // 0 (status=setup) we never enable Generate Next — operator should
  // hit "Start competition" first. After start, current >= 1 and we
  // require the current round to be complete to advance.
  if (current < 1) return false;
  return isSwissRoundComplete(currentRoundMatches);
}

// T193 (FR-050e): once every configured round has been generated and
// completed, the admin page hides the Generate button and surfaces a
// "Competition complete — view final standings" link. `currentRound
// >= swissRounds` AND every match in the final round done.
function isSwissCompetitionComplete(comp, currentRoundMatches) {
  if (!comp || comp.format !== "swiss") return false;
  const total = comp.swissRounds || 0;
  const current = comp.swissCurrentRound || 0;
  if (total < 1 || current < total) return false;
  return isSwissRoundComplete(currentRoundMatches);
}

function AdminSwissRounds({ c, poolMatches, password, onViewStandings, showToast }) {
  const [generating, setGenerating] = useStateA(false);
  const [genError, setGenError] = useStateA(null);
  const mountedRef = useRefA(true);
  useEffectA(() => () => { mountedRef.current = false; }, []);

  // Reset the inline error whenever the round changes or new matches
  // land via SSE — the prior "current round incomplete" message is
  // not meaningful once the operator has moved on.
  useEffectA(() => { setGenError(null); }, [c.swissCurrentRound, (poolMatches || []).length]);

  const currentRound = c.swissCurrentRound || 0;
  const totalRounds = c.swissRounds || 0;
  const currentMatches = filterSwissRoundMatches(poolMatches, currentRound);
  const complete = isSwissRoundComplete(currentMatches);
  const canGenerate = canGenerateNextSwissRound(c, currentMatches);
  const allDone = isSwissCompetitionComplete(c, currentMatches);

  const generate = async () => {
    setGenerating(true);
    setGenError(null);
    try {
      await window.API.swissGenerateRound(c.id, password);
      // SSE swiss_round_generated will trigger AdminApp's refetch —
      // no local-state mutation needed. Surface a toast so the
      // operator sees confirmation in case the SSE is slow.
      if (!mountedRef.current) return;
      if (showToast) showToast(`Round ${currentRound + 1} generated`);
    } catch (e) {
      if (!mountedRef.current) return;
      // 409 / round_incomplete is a known operator-error condition;
      // surface it inline rather than as a generic toast.
      if (e.code === "round_incomplete") {
        setGenError("Cannot generate — current round still has incomplete matches.");
      } else {
        setGenError(e.message || "Failed to generate next round");
        if (showToast) showToast(e.message || "Failed to generate next round", "error");
      }
    } finally {
      if (mountedRef.current) setGenerating(false);
    }
  };

  // Setup state (status === "setup") — nudge the operator to hit
  // Start so round 1 is generated.
  if (c.status === "setup") {
    return (
      <div className="card" style={{ padding: 14 }}>
        <div className="card__title" style={{ marginBottom: 6 }}>Swiss rounds</div>
        <div className="card__sub">{totalRounds} rounds configured. Round 1 will be generated when you start the competition.</div>
      </div>
    );
  }

  return (
    <div className="card" style={{ padding: 14 }}>
      <div className="card__head" style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 10 }}>
        <div className="card__title">Round {currentRound} of {totalRounds}</div>
        <div style={{ fontSize: 12, color: "var(--ink-3)" }}>
          {currentMatches.filter(m => m.status === "completed").length}/{currentMatches.length} matches complete
        </div>
      </div>

      {/* Current-round match list (id + sides + status). Kept compact — */}
      {/* the full edit experience lives in the Scores section. */}
      {currentMatches.length > 0 && (
        <table className="pool__table" style={{ marginBottom: 10 }}>
          <thead><tr><th style={{ width: 28 }}>#</th><th>White (Shiro)</th><th>Red (Aka)</th><th style={{ width: 80 }}>Court</th><th style={{ width: 100 }}>Status</th></tr></thead>
          <tbody>
            {currentMatches.map((m, i) => (
              <tr key={m.id}>
                <td style={{ color: "var(--ink-3)", fontFamily: "var(--font-mono)" }}>{i + 1}</td>
                <td>{m.sideB?.name || "—"}</td>
                <td>{m.sideA?.name || "—"}</td>
                <td style={{ fontFamily: "var(--font-mono)" }}>{m.court || "—"}</td>
                <td style={{ fontSize: 12, color: m.status === "completed" ? "var(--accent)" : m.status === "running" ? "var(--red)" : "var(--ink-3)" }}>
                  {m.status === "completed" ? "Done" : m.status === "running" ? "Live" : "Scheduled"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      {/* T193: when all rounds complete, hide the Generate button and */}
      {/* surface the final-standings link. */}
      {allDone ? (
        <div style={{ display: "flex", flexDirection: "column", gap: 8, padding: 12, background: "var(--accent-soft, #ecfdf5)", border: "1px solid var(--accent, #a7f3d0)", borderRadius: 8 }}>
          <div style={{ fontWeight: 600, color: "var(--accent, #065f46)" }}>Competition complete</div>
          {onViewStandings && (
            <button className="btn btn--primary btn--sm" onClick={onViewStandings}>View final standings →</button>
          )}
        </div>
      ) : (
        <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
          <button
            className="btn btn--primary"
            disabled={!canGenerate || generating}
            onClick={generate}
          >
            {generating && <span className="spinner" />}
            {generating ? "Generating…" : `Generate round ${currentRound + 1}`}
          </button>
          {!complete && currentMatches.length > 0 && (
            <div className="field__hint">Finish the remaining matches in round {currentRound} before generating the next round.</div>
          )}
          {genError && <div className="alert alert--error" style={{ fontSize: 13 }}>{genError}</div>}
        </div>
      )}
    </div>
  );
}

function AdminCompetition({ tournament, competition, pools, poolMatches, standings, bracket, reservedSlots, section, onSection, onBack, onOpenCompetition, onUpdate, onRefreshCompetition, onCreatePlayoff, onMoveCourt, onEditScore, onLogout, onViewerMode, tweaks, password, showToast }) {
  const c = competition;
  const t = tournament;
  const [starting, setStarting] = useStateA(false);
  const [generating, setGenerating] = useStateA(false);
  const [discarding, setDiscarding] = useStateA(false);
  // localStatus lets AdminSettings report an invalidation immediately so the
  // page-header StatusBadge flips without waiting for the SSE refresh.
  // Cleared automatically when the prop status changes (SSE arrives).
  const [localStatus, setLocalStatus] = useStateA(null);
  useEffectA(() => { setLocalStatus(null); }, [c.id, c.status]);
  // start() awaits a multi-second backend call (pool generation + bracket
  // build). If the user clicks Back during that window AdminCompetition
  // unmounts and setStarting(false) in finally targets a dead component.
  const mountedRef = useRefA(true);
  useEffectA(() => () => { mountedRef.current = false; }, []);

  // Use the shared isValidDate (admin_helpers.jsx) which delegates to
  // normalizeDate for semantic validity — rejects "32-13-2026" / Feb 31 /
  // Feb 29 in non-leap years. Without this, the Start button would enable
  // for shape-valid-but-impossible dates that AdminSettings.saveNow's
  // stricter check would reject — letting the operator start a competition
  // with a date that can't be saved back.
  const isDateValid = isValidDate;

  const generateDraw = async () => {
    setGenerating(true);
    try {
      await window.API.generateDraw(c.id, password);
      if (!mountedRef.current) return;
      // Refresh immediately so the UI reflects draw-ready status without
      // waiting for SSE (which may be slow or temporarily disconnected).
      onRefreshCompetition?.();
      showToast(`Draw generated for ${c.name}`);
      onSection(c.format === "playoffs" ? "bracket" : "pools");
    } catch (e) {
      console.error("Generate draw failed:", e);
      if (mountedRef.current) showToast(e.message, "error");
    } finally {
      if (mountedRef.current) setGenerating(false);
    }
  };

  const discardDraw = async () => {
    if (!confirm(`Discard the generated draw for "${c.name}"? The pools/bracket will be removed and you can regenerate.`)) return;
    setDiscarding(true);
    try {
      await window.API.discardDraw(c.id, password);
      if (!mountedRef.current) return;
      onRefreshCompetition?.();
      showToast(`Draw discarded for ${c.name}`);
      onSection("overview");
    } catch (e) {
      console.error("Discard draw failed:", e);
      if (mountedRef.current) showToast(e.message, "error");
    } finally {
      if (mountedRef.current) setDiscarding(false);
    }
  };

  const regenerateDraw = async () => {
    setGenerating(true);
    try {
      await window.API.discardDraw(c.id, password);
      if (!mountedRef.current) return;
      // Refresh after discard so the UI reflects setup status immediately;
      // if generateDraw then fails the UI is consistent with the server.
      onRefreshCompetition?.();
      await window.API.generateDraw(c.id, password);
      if (!mountedRef.current) return;
      onRefreshCompetition?.();
      showToast(`Draw regenerated for ${c.name}`);
      onSection(c.format === "playoffs" ? "bracket" : "pools");
    } catch (e) {
      console.error("Regenerate draw failed:", e);
      if (mountedRef.current) {
        onRefreshCompetition?.();
        showToast(e.message, "error");
      }
    } finally {
      if (mountedRef.current) setGenerating(false);
    }
  };

  const start = async () => {
    showToast(`Starting ${c.name}…`);

    setStarting(true);
    try {
      await window.API.startCompetition(c.id, password);
      // Don't attempt a local-state refresh here. Pre-fix this called
      // onUpdate({ ...t, competitions: comps }), but onUpdate at this
      // level is wired (in AdminApp's render — see the AdminCompetition
      // <Component onUpdate={...}> binding) to
      // (next) => updateCompetition(c.id, next), which fires
      // PUT /api/competitions/:id with `next` as the body.
      // Passing a tournament-shaped object would have Gin binding it as
      // state.Competition and silently overwriting the just-started
      // competition's Name/Date/etc. with tournament-level values.
      //
      // The backend broadcasts `competition_started` (and optionally
      // `competition_completed` when the zero-match auto-complete
      // path fires); both are in AdminApp's REFRESHABLE_EVENTS set, so
      // the SSE handler refetches the tournament + competitions list
      // and local state catches up within a roundtrip. Trade a tiny
      // perceived latency for not corrupting the record.
      if (!mountedRef.current) return;
      showToast(`${c.name} started`);
      onSection("scores");
    } catch (e) {
      console.error("Start competition failed:", e);
      if (mountedRef.current) showToast(e.message, "error");
    } finally {
      if (mountedRef.current) setStarting(false);
    }
  };

  const isDrawReady = c.status === "draw-ready";
  const sections = [
    {
      sec: "Preparation", items: [
        { id: "overview", label: "Overview" },
        { id: "participants", label: "Participants & seeds" },
        // T136 nav: Lineups is a team-only surface — hide it for
        // individual competitions so the sidebar stays uncluttered.
        c.kind === "team" ? { id: "lineups", label: "Lineups" } : null,
        { id: "settings", label: "Settings" },
      ].filter(Boolean)
    },
    {
      sec: "Run", items: [
        // Show pools/bracket in nav when draw is ready (preview) or running.
        // Use .length checks: the state store returns [] / {rounds:[]} (never null)
        // when files are absent, so plain truthiness would always show the items.
        (pools?.length || (isDrawReady && c.format !== "playoffs" && c.format !== "swiss")) ? { id: "pools", label: isDrawReady ? "Pools — preview" : "Pools — live" } : null,
        // T191 (FR-050d): Swiss competitions surface a dedicated round
        // management panel for the "Generate next round" workflow.
        c.format === "swiss" && !isDrawReady ? { id: "swiss", label: "Swiss rounds — manage" } : null,
        (bracket?.rounds?.length || (isDrawReady && c.format === "playoffs")) ? { id: "bracket", label: isDrawReady ? "Bracket — preview" : "Bracket — live" } : null,
        !isDrawReady ? { id: "scores", label: "Scores — edit" } : null,
      ].filter(Boolean)
    },
    {
      sec: "Output", items: [
        { id: "export", label: "Export & print" },
      ]
    },
  ];

  const currentItem = sections.flatMap(m => m.items).find(i => i.id === section);

  return (
    <div className="app">
      <AdminTopbar onLogout={onLogout} onViewerMode={onViewerMode} tournament={t} />
      <div className="page page--wide" style={{ maxWidth: 1400 }}>
        <Breadcrumbs items={[
          { label: "Dashboard", onClick: onBack },
          { label: c.name, onClick: section === "overview" ? null : () => onSection("overview") },
          currentItem && section !== "overview" ? { label: currentItem.label } : null
        ].filter(Boolean)} />
        <div className="page-head">
          <div>
            <div className="page-head__eyebrow">{t.name} ›</div>
            <div style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
              <h1 className="page-head__title">{c.name}</h1>
              <StatusBadge status={localStatus ?? c.status} />
            </div>
            <div className="page-head__sub">
              {window.competitionKindLabel(c)} · {c.players.length} {c.kind === "team" ? "teams" : "players"} ·
              {c.date && ` ${formatDate(c.date)} at `} {c.startTime} · {c.courts.join(", ")}
            </div>
          </div>
          <div className="page-head__actions" style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 8 }}>
            {(!c.status || c.status === "setup") && c.players.length >= 2 && (
              <>
                <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
                  <button className="btn btn--ghost" onClick={generateDraw} disabled={!isDateValid(c.date) || generating || starting}>
                    {generating && <span className="spinner" />}
                    {generating ? "Generating…" : "Preview draw"}
                  </button>
                  <button className="btn btn--primary" onClick={start} disabled={!isDateValid(c.date) || starting || generating}>
                    {starting && <span className="spinner" />}
                    {starting ? "Starting…" : "Start competition →"}
                  </button>
                </div>
                {!isDateValid(c.date) && (
                  <div style={{ color: "var(--red)", fontSize: 11, fontWeight: 600 }}>
                    ⚠ Cannot start: invalid date in Settings tab (e.g. "{c.date}")
                  </div>
                )}
              </>
            )}
            {isDrawReady && (
              <div style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 8 }}>
                <div style={{ display: "flex", gap: 8 }}>
                  <button className="btn btn--ghost btn--danger" onClick={discardDraw} disabled={discarding || starting || generating}>
                    {discarding && <span className="spinner" />}
                    {discarding ? "Discarding…" : "Discard draw"}
                  </button>
                  <button className="btn btn--ghost" onClick={regenerateDraw} disabled={generating || starting || discarding}>
                    {generating && <span className="spinner" />}
                    {generating ? "Regenerating…" : "Regenerate draw"}
                  </button>
                  <button className="btn btn--primary" onClick={start} disabled={starting || generating || discarding}>
                    {starting && <span className="spinner" />}
                    {starting ? "Starting…" : "Start competition →"}
                  </button>
                </div>
                <div style={{ fontSize: 11, color: "var(--ink-3)" }}>Draw generated — preview below, then start when ready</div>
              </div>
            )}
            {(c.format === "mixed" || c.format === "league") && c.status !== "setup" && c.status !== "draw-ready" && onCreatePlayoff && (() => {
              if (c.format === "league") {
                return <div style={{ fontSize: 11, color: "var(--ink-3)", fontWeight: 600 }}>League: standings determine the winner</div>;
              }
              const playoffName = c.name + " - Playoffs";
              const hasPlayoff = (t.competitions || []).some(cc => cc.name === playoffName);
              return hasPlayoff
                ? <div style={{ fontSize: 11, color: "var(--ink-3)", fontWeight: 600 }}>Playoff bracket already created</div>
                : <button className="btn btn--primary" onClick={() => onCreatePlayoff(c.id)}>Create playoff bracket →</button>;
            })()}
          </div>
        </div>

        <div className="workspace">
          <div className="side-nav">
            {sections.map((sec) => (
              <div key={sec.sec}>
                <div className="side-nav__sec">{sec.sec}</div>
                {sec.items.map((it) => (
                  <button key={it.id} className={section === it.id ? "is-active" : ""} onClick={() => onSection(it.id)}>{it.label}</button>
                ))}
              </div>
            ))}
            <div>
              <div className="side-nav__sec">Other competitions</div>
              {t.competitions.filter((cc) => cc.id !== c.id).map((cc) => (
                <button key={cc.id} onClick={() => onOpenCompetition(cc.id)}>{cc.name}</button>
              ))}
            </div>
          </div>
          <div>
            {section === "overview" && <AdminCompOverview c={c} pools={pools} poolMatches={poolMatches} bracket={bracket} onSection={onSection} />}
            {section === "participants" && <AdminParticipants c={c} tournament={t} reservedSlots={reservedSlots || []} onUpdate={onUpdate} password={password} showToast={showToast} onSection={onSection} />}
            {section === "lineups" && window.AdminTeamLineupsList && <window.AdminTeamLineupsList comp={c} password={password} showToast={showToast} />}
            {section === "settings" && <AdminSettings c={c} tournament={t} onUpdate={onUpdate} onBack={onBack} password={password} showToast={showToast} onStatusChange={setLocalStatus} />}
            {/* T191/T193 (FR-050d/e): Swiss-round management. */}
            {/* onViewStandings is wired to the public viewer URL so the */}
            {/* operator can navigate into the viewer-mode standings */}
            {/* page when the competition completes. */}
            {section === "swiss" && c.format === "swiss" && !isDrawReady && (
              <AdminSwissRounds
                c={c}
                poolMatches={poolMatches}
                password={password}
                showToast={showToast}
                onViewStandings={onViewerMode || null}
              />
            )}
            {section === "pools" && <AdminPools c={c} pools={pools} poolMatches={poolMatches} standings={standings} tweaks={tweaks} onEditScore={onEditScore} password={password} />}
            {section === "bracket" && <AdminBracket c={c} t={t} bracket={bracket} onMoveCourt={onMoveCourt} tweaks={tweaks} password={password} showToast={showToast} />}
            {section === "scores" && !isDrawReady && <AdminScoreEditor c={c} t={t} onEditScore={onEditScore} onMoveCourt={onMoveCourt} restrictToCompId={c.id} password={password} />}
            {section === "export" && <AdminExport c={c} t={t} password={password} />}
          </div>
        </div>
      </div>
    </div>
  );
}

window.AdminCompetition = AdminCompetition;
window.AdminSwissRounds = AdminSwissRounds;

// ES export for the vitest suite — pure helpers only. Components stay
// behind the window.* pattern to match the rest of admin_*.jsx.
export {
  buildLiveIpponResult,
  loadScoreboardPoints,
  swissRoundIDPrefix,
  filterSwissRoundMatches,
  isSwissRoundComplete,
  canGenerateNextSwissRound,
  isSwissCompetitionComplete,
};
