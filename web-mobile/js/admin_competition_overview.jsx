// admin_competition_overview.jsx: Overview + Fighting-Spirit awards sections
// of the competition admin. Split out of admin_competition.jsx (mp-hpe3) as
// cohesive section modules; loaded only via the admin_competition.js entry's
// imports (no standalone <script> tag) and consumed by the AdminCompetition
// shell via window.*.

import { formatEstimateMinutes, finishClock, timeToMinutes } from './admin_schedule_utils.jsx';
import { pastMidnight, pauseDayIndex } from './calendar_rules.jsx';
import { blockMinutes, parseClock, formatClock, computePauseRowStatus } from './pause_list.jsx';
// seededRanks: the one owner of "which seed ranks has this roster actually
// got" (integer > 0 normalization), shared with the seeding blocker and the
// settings preview so this stat, the checklist and the preview cannot
// disagree about what counts as seeded.
import { seededRanks, deriveTournamentDays } from './admin_helpers.jsx';
import { checkInApplies } from './competition_shape.jsx';
import { useUnsavedInput } from './app_version.jsx';
import { isOffline, savedOfflineSentence } from './write_result.jsx';
import { onDocumentSettled } from './document_versions.jsx';
// One owner for what the operator is told when a file on disk does not say what
// the app expects; shared with the pool surfaces so the wording cannot drift.
import {
  DataIssueBanner,
  bracketRecoveryKind,
  bracketResetPrompt,
  bracketResetToast,
  rosterUnreadableIssues,
} from './data_integrity.jsx';

const { useState, useEffect, useRef } = React;

const compMatchStats = window.compMatchStats;

// ---------------------------------------------------------------------------
// Pure helper: competitionNextSteps
// ---------------------------------------------------------------------------
// Returns an ordered array of step descriptors for the "Next steps" checklist
// shown in the setup state. Each step is a plain object so the function is
// unit-testable without mounting any component.
//
// Shape:
//   { id: string, label: string, detail: string, cta: string|null,
//     state: 'done'|'active'|'pending', section: string|null,
//     blocking?: boolean }
//
// Rules:
//   - "Create competition" is always done (the comp already exists).
//   - "Add participants" is done when players >= 2, else active.
//   - "Review seeds & settings" is always emitted as a non-done step: there is
//     no reliable "seeds reviewed" signal, so it never auto-completes (its
//     detail line reflects the current seed count, but its state stays
//     pending/active and is resolved by the active-step pass below). Its CTA
//     navigates to the "settings" section (format, courts, duration); seeds
//     are accessible from the seeding card on the "participants" page.
//   - Team comps get an extra "Set team lineups" step (→ "lineups") inserted
//     AFTER "Review seeds & settings" and before "Generate the draw". This is
//     the checklist's logical setup order; the sidebar lists lineups after
//     participants but before settings, which differs from this ordering.
//   - "Generate the draw" is always non-done (the button is in the page-head).
//     Its detail line points at that button, EXCEPT when a court rule has
//     disabled it: then it names the blocker and routes to Settings, because
//     telling an operator to press a greyed-out button is worse than saying
//     nothing. `tournamentCourts` is optional and only feeds that check (the
//     orphaned-shiaijo half of it); omit it and only the count rule applies.
//   - The FIRST non-done step is promoted to "active"; the rest stay "pending".
//     EXCEPT when a step is `blocking` (today only "Generate the draw", when a
//     court rule has disabled it): that one is promoted instead, because
//     nothing further in the checklist can happen until it is fixed, and
//     because "Review seeds & settings" never completes and would otherwise
//     hold `active` forever and hide the blocker's CTA.
//
// Exported on window.competitionNextSteps AND in the ES export block below.
function competitionNextSteps(c, tournamentCourts) {
  const players = (c && c.players) || [];
  const numPlayers = players.length;
  const isTeam = c && c.kind === "team";
  const hasEnoughPlayers = numPlayers >= 2;
  const seededCount = seededRanks(players).length;

  const steps = [];

  // Step 1: always done.
  steps.push({
    id: "create",
    label: "Create competition",
    detail: `${c && c.name ? `“${c.name}” created` : "Competition created"}`,
    state: "done",
    section: null,
    cta: null,
  });

  // Step 2: participants. Left out when participants.csv could not be read
  // (operator decision D26): the roster then arrives empty, and "0 added, add
  // participants" would send the operator to a page that shows only the file's
  // notice. The banner above the checklist carries the repair.
  const rosterUnreadable = rosterUnreadableIssues(c).length > 0;
  const participantDetail = hasEnoughPlayers
    ? `${numPlayers} ${isTeam ? "teams" : "participants"} added${seededCount > 0 ? `, ${seededCount} seeded` : ""}`
    : `${numPlayers} ${isTeam ? "team" : "participant"}${numPlayers !== 1 ? "s" : ""} added. Need at least 2`;
  if (!rosterUnreadable) steps.push({
    id: "participants",
    label: `Add ${isTeam ? "teams" : "participants"}`,
    detail: participantDetail,
    state: hasEnoughPlayers ? "done" : "active",
    section: "participants",
    // `cta` is the button label shown only when this step is active. Authored
    // here (not in JSX) so the copy is centralised and unit-testable; null for
    // steps with no navigable action (the generate step lives in the header).
    cta: `Add ${isTeam ? "teams" : "participants"} →`,
  });

  // Step 3: seeds & settings (becomes active once participants done). With the
  // roster unreadable there is nobody to seed, so the step speaks of settings
  // alone.
  steps.push({
    id: "settings",
    label: rosterUnreadable ? "Review settings" : "Review seeds & settings",
    detail: rosterUnreadable
      ? "Adjust format and settings"
      : seededCount > 0
        ? `${seededCount} seed${seededCount !== 1 ? "s" : ""} assigned`
        : "Optionally assign seeds and adjust format",
    state: "pending",
    section: "settings",
    cta: "Review settings →",
  });

  // Step 3b (team only): lineups, after settings.
  if (isTeam) {
    steps.push({
      id: "lineups",
      label: "Set team lineups",
      detail: "Assign positions (Senpo, Jiho, …) before the draw",
      state: "pending",
      section: "lineups",
      cta: "Set lineups →",
    });
  }

  // Final step: generate draw (button is in the page-head; no section nav).
  //
  // When a rule blocks the draw, that header button is DISABLED, so pointing at
  // it would be sending the operator to a dead control. The step names the
  // blocker and the screen that fixes it instead. All of it comes from
  // competitionDrawBlocker, including WHICH screen, so this row can never state
  // the rule differently from the header block or send the operator to the
  // wrong tab for it.
  const drawBlocker = typeof window !== "undefined" && window.competitionDrawBlocker
    ? window.competitionDrawBlocker(c, tournamentCourts)
    : null;
  let generateDetail = "Use the “Generate draw” button in the header above";
  if (rosterUnreadable) generateDetail = "Repair the roster first: see the notice above";
  else if (!hasEnoughPlayers) generateDetail = "Add at least 2 participants first";
  else if (drawBlocker) generateDetail = `${drawBlocker.reason} ${drawBlocker.fix} The “Generate draw” button stays disabled until you do.`;
  const drawIsBlocked = !!drawBlocker && hasEnoughPlayers && !rosterUnreadable;
  steps.push({
    id: "generate",
    label: "Generate the draw",
    detail: generateDetail,
    state: "pending",
    section: drawIsBlocked ? drawBlocker.section : null,
    cta: drawIsBlocked ? drawBlocker.cta : null,
    // `blocking` means "nothing further in this checklist can happen until
    // this is fixed", which is what promotes it past the ordering rule below.
    // An unreadable roster blocks it too, with no button: the repair is the
    // banner's Reload, so no settings CTA leads the page meanwhile.
    blocking: drawIsBlocked || rosterUnreadable,
  });

  // A blocking step JUMPS THE QUEUE.
  //
  // The default rule (first non-done becomes active) can never reach the
  // generate step: "Review seeds & settings" is deliberately emitted non-done
  // forever, because there is no "seeds reviewed" signal, so it takes `active`
  // on every render and everything after it stays `pending`. Only an active step
  // renders its CTA, so the "Reassign shiaijo →" button authored above was
  // unreachable in every state the checklist can be in.
  //
  // Promoting the blocker instead of relaxing the CTA's render condition also
  // avoids showing two buttons that both go to Settings ("Review settings →"
  // and "Reassign shiaijo →"), and it is the honest ordering: a competition
  // whose draw is refused has exactly one next action.
  // One pass, one rule: a blocking step takes `active` if there is one,
  // otherwise the first non-done step does, and every other non-done step is
  // `pending`. Writing the two cases as separate passes meant the blocker branch
  // also had to re-clear the states the default branch would have set, which
  // was a no-op for every step this checklist actually emits and would have
  // silently stopped being one the moment a later step was pushed as "active".
  const active = steps.find((s) => s.blocking && s.state !== "done")
    || steps.find((s) => s.state !== "done");
  steps.forEach((s) => {
    if (s.state === "done") return;
    s.state = s === active ? "active" : "pending";
  });

  return steps;
}

// ---------------------------------------------------------------------------
// Pure helper: overviewViewMode
// ---------------------------------------------------------------------------
// Maps a CompetitionStatus wire value to the overview's render mode. The wire
// values (internal/state/models.go:441-446) are:
//   "setup" → "draw-ready" → "pools" / "knockout" (the two RUNNING phases) →
//   "completed", plus "invalid". There is NO literal "running" status. An
//   active competition reports its phase ("pools" or "knockout"). Anything that
//   is not setup/draw-ready/completed (including "pools", "knockout", "invalid",
//   or any future phase) maps to "running", so an active competition keeps the
//   progress + scores/results widgets instead of mis-rendering the "completed"
//   card. Extracted as a pure function so this mapping is unit-tested and the
//   regression (treating only the non-existent "running" string as active)
//   cannot silently return. Exported on window + ES.
function overviewViewMode(status) {
  if (!status || status === "setup") return "setup";
  if (status === "draw-ready") return "draw-ready";
  if (status === "completed") return "completed";
  return "running";
}

// ---------------------------------------------------------------------------
// Pure helper: hasBracketRounds
// ---------------------------------------------------------------------------
// True only when a bracket has at least one real round of matches.
// internal/state/bracket.go's LoadBracket has a never-nil contract: a
// competition with no bracket.json (every league, which has no knockout
// phase) still loads as &Bracket{Rounds: []}, a truthy object with zero
// rounds. Plain `!!bracket` is therefore ALWAYS true and cannot distinguish
// "has a real bracket" from "has no bracket at all" - callers must check
// rounds.length. Pure + exported so this never-nil footgun is unit-tested.
function hasBracketRounds(bracket) {
  return !!(bracket && bracket.rounds && bracket.rounds.length);
}

// ---------------------------------------------------------------------------
// Pure helper: overviewResultsSection
// ---------------------------------------------------------------------------
// The section that holds standings/results for a given competition format.
// Swiss competitions have no pools/bracket section. Their results live in the
// dedicated "swiss" section, so a Results/podium CTA that defaulted to
// pools/bracket would strand the operator on an empty/hidden view. Route swiss
// to "swiss"; otherwise prefer the bracket when one exists, else pools.
// Pure + exported so the swiss routing is unit-tested (Copilot review #312).
// `hasBracket` must come from hasBracketRounds, not raw truthiness (mp-ahu6
// follow-up): see that helper's doc for why a bare bracket object always
// passes a truthy check even with zero rounds.
function overviewResultsSection(format, hasBracket) {
  if (format === "swiss") return "swiss";
  return hasBracket ? "bracket" : "pools";
}

// ---------------------------------------------------------------------------
// Schedule estimate helpers for Overview & Settings
// ---------------------------------------------------------------------------

export function latestCourtFinish(courtEnds, startTime) {
  if (!courtEnds || typeof courtEnds !== "object") return null;
  const entries = Object.values(courtEnds).filter(Boolean);
  if (entries.length === 0) return null;

  const startM = timeToMinutes(startTime);

  let latestTotalMinutes = -Infinity;

  for (const timeStr of entries) {
    if (typeof timeStr !== "string") continue;
    const parts = timeStr.trim().split(":");
    if (parts.length !== 2) continue;
    const h = Number(parts[0]);
    const m = Number(parts[1]);
    if (!Number.isFinite(h) || !Number.isFinite(m) || h < 0 || m < 0 || m > 59) continue;

    let totalM = h * 60 + m;
    // An end that reads earlier than the start ran over midnight.
    if (totalM < 1440 && startM !== null && totalM < startM) totalM += 1440;

    if (totalM > latestTotalMinutes) latestTotalMinutes = totalM;
  }

  if (latestTotalMinutes === -Infinity) return null;

  // After midnight by the calendar's rule: an end at exactly 24:00 is not.
  const nextDay = pastMidnight(latestTotalMinutes);
  const wrapped = ((latestTotalMinutes % 1440) + 1440) % 1440;
  const outH = Math.floor(wrapped / 60);
  const outM = wrapped % 60;
  const clock = `${String(outH).padStart(2, "0")}:${String(outM).padStart(2, "0")}`;

  return { clock, nextDay };
}

export function formatPausesRow(tournament, c) {
  const pauses = Array.isArray(tournament?.pauses) ? tournament.pauses : [];
  const durationDays = Number(tournament?.durationDays) || 1;
  const days = deriveTournamentDays(tournament?.date, durationDays);
  // The server's dayBreaks rule: no date, or an undated tournament, is day 1,
  // and a date that is not one of the tournament's days (0 here) gets only the
  // every-day pauses.
  const compDay = pauseDayIndex(days, c?.date);

  const relevant = pauses.filter(p => (p && (p.day === 0 || p.day === compDay)));

  const usable = [];
  // Pauses the estimate does not work around: matches go on through them by
  // choice, or the row is incomplete. Named after the day's pauses.
  const unused = [];

  for (let i = 0; i < relevant.length; i++) {
    const p = relevant[i];
    const name = (p.name || "").trim() || "Pause";
    const status = computePauseRowStatus(p, i, relevant, durationDays);
    if (status.type === 'matches_go_on') {
      unused.push(`${name}: matches go on`);
    } else if (!status.warn) {
      const startMin = parseClock(p.start);
      const durMin = blockMinutes(p.length);
      const endClock = formatClock(startMin + durMin);
      usable.push({
        text: `${name} ${p.start} to ${endClock}`,
        startMin,
      });
    } else {
      unused.push(`${name}: ${status.type === 'missing_start' ? "no start time" : "no length"}, matches will not stop`);
    }
  }

  usable.sort((a, b) => a.startMin - b.startMin);

  const dayText = usable.length > 0 ? usable.map(u => u.text).join(", ") : "No pauses that day";
  const allParts = [dayText, ...unused];
  return allParts.join(" · ");
}

export function formatPerShiaijoRow(estimate, courts) {
  if (!estimate || !Array.isArray(estimate.perCourtMinutes) || estimate.perCourtMinutes.length === 0) {
    return null;
  }
  return estimate.perCourtMinutes.map((m, i) => {
    const letter = (Array.isArray(courts) && courts[i]) || String.fromCharCode(65 + i);
    if (!m || m <= 0) {
      return `${letter} has no matches`;
    }
    return `${letter} about ${formatEstimateMinutes(m)}`;
  }).join(" · ");
}

export function EstimateDurationTile({
  c,
  estimate,
  estimateLoading,
  rosterUnreadable,
  estimateCheckIn,
}) {
  const isKachinuki = c?.teamMatchType === "kachinuki";
  const label = isKachinuki ? "Average est. duration" : "Est. duration";
  const loadingSuffix = estimateLoading && !rosterUnreadable ? " …" : "";

  const hasMinutes = estimate && Number.isFinite(estimate.totalDurationMinutes) && estimate.totalDurationMinutes > 0;
  const dispValue = (!rosterUnreadable && hasMinutes)
    ? `about ${formatEstimateMinutes(estimate.totalDurationMinutes)}`
    : "n/a";

  const vCls = dispValue === "n/a" ? "v" : "v v--sm";

  let rangeLine = null;
  if (!rosterUnreadable && isKachinuki && estimate &&
      Number.isFinite(estimate.bestCaseMinutes) &&
      Number.isFinite(estimate.worstCaseMinutes) &&
      estimate.bestCaseMinutes !== estimate.worstCaseMinutes) {
    rangeLine = `Best ${formatEstimateMinutes(estimate.bestCaseMinutes)} to worst ${formatEstimateMinutes(estimate.worstCaseMinutes)}`;
  }

  let finishLine = null;
  let finishNextDay = false;
  if (!rosterUnreadable && estimate) {
    const isSetup = c?.status === "setup";
    let finishRes = null;
    if (isSetup) {
      finishRes = finishClock(c?.startTime, estimate.totalDurationMinutes);
    } else {
      finishRes = latestCourtFinish(estimate.courtEnds, c?.startTime) || finishClock(c?.startTime, estimate.totalDurationMinutes);
    }
    if (finishRes && finishRes.clock) {
      finishNextDay = Boolean(finishRes.nextDay);
      finishLine = `Finishes about ${finishRes.clock}${finishNextDay ? ", after midnight" : ""}`;
    }
  }

  let countLine = null;
  let hintLine = null;
  if (!rosterUnreadable && estimate) {
    const isTeam = c?.kind === "team" || Number(c?.teamSize) > 0;
    const isSetup = c?.status === "setup";

    if (estimate.participantsSource === "expected" && Number.isFinite(estimate.participants)) {
      const expCount = estimate.participants;
      const unit = expCount === 1 ? (isTeam ? "team" : "participant") : (isTeam ? "teams" : "participants");
      const regCount = (c?.players || []).length;
      const chkCount = estimateCheckIn ? (c?.players || []).filter(p => p && p.checkedIn).length : 0;
      const actualCount = estimateCheckIn ? chkCount : regCount;
      const actionWord = estimateCheckIn ? "checked in" : "registered";
      const nounWord = estimateCheckIn ? "check-ins" : "registrations";

      countLine = `For ${expCount} expected ${unit} (${actualCount} ${actionWord})`;

      if (actualCount > 0 && actualCount < expCount) {
        hintLine = `${actualCount} ${actionWord}, fewer than the ${expCount} you expected. Estimates still use ${expCount}; lower this number to use the ${nounWord}.`;
      }
    } else if (estimate.participantsSource === "roster" && !isSetup && Number.isFinite(estimate.participants)) {
      const count = estimate.participants;
      const unit = count === 1 ? (isTeam ? "team" : "participant") : (isTeam ? "teams" : "participants");
      countLine = `For the ${count} ${unit} in the draw`;
    }
  }

  return (
    <div className="stat-box" data-testid="stat-est-duration">
      <div className={vCls}>{dispValue}</div>
      <div className="l">{label}{loadingSuffix}</div>
      {rangeLine && <div className="stat-box__range">{rangeLine}</div>}
      {finishLine && (
        <div className={`stat-box__finish${finishNextDay ? " stat-box__finish--warn" : ""}`}>
          {finishLine}
        </div>
      )}
      {countLine && <div className="stat-box__count">{countLine}</div>}
      {hintLine && <div className="stat-box__hint">{hintLine}</div>}
    </div>
  );
}

// ---------------------------------------------------------------------------
// AdminCompOverview: status-aware overview component
// ---------------------------------------------------------------------------
function AdminCompOverview({ c, tournament, pools, poolMatches, bracket, onSection, password, showToast, onRefreshCompetition }) {
  // Recovery from a competition file that will not parse. Held here rather than
  // in the banner because the banner owns the WORDING (shared with the other
  // data-integrity surfaces) and this page owns the network call and the
  // refresh, exactly like discardDraw in admin_competition.jsx.
  const [quarantining, setQuarantining] = useState(false);
  const quarantineBracket = async () => {
    // The words come from data_integrity.jsx, which owns every one the operator
    // reads about unreadable data. A league has no knockout stage, so it must
    // not be asked to confirm resetting one.
    const prompt = bracketResetPrompt(bracketRecoveryKind(c), c.name);
    const ok = await window.confirmDialog({
      message: prompt.message,
      confirmLabel: prompt.confirmLabel,
      danger: true,
    });
    if (!ok) return;
    setQuarantining(true);
    try {
      const res = await window.API.quarantineBracket(c.id, password);
      // res.rebuilt is the SERVER's answer, not the kind we predicted above: it
      // is what actually happened, and a toast that guessed would one day tell
      // an operator a stage was rebuilt when nothing was.
      showToast?.(bracketResetToast(res.quarantinedAs, res.rebuilt));
      onRefreshCompetition?.();
    } catch (e) {
      console.error("Quarantine bracket failed:", e);
      showToast?.(e.message, "error");
    } finally {
      setQuarantining(false);
    }
  };
  // Prefer the props passed from the detail fetch, but fall back to whatever
  // is already on `c` when the detail hasn't loaded yet (or errored). Using ??
  // avoids overwriting non-null fields on `c` with undefined prop values.
  const statsSource = {
    ...c,
    pools: pools ?? c.pools,
    poolMatches: poolMatches ?? c.poolMatches,
    bracket: bracket ?? c.bracket,
  };
  const { total, done, running: runningCount } = compMatchStats(statsSource);
  const effectiveBracket = bracket ?? c.bracket;

  // Honour the OS reduced-motion preference for the progress-bar animation.
  const prefersReducedMotion = typeof window !== "undefined" && window.matchMedia
    ? window.matchMedia("(prefers-reduced-motion: reduce)").matches
    : false;

  // ---------------------------------------------------------------------------
  // Status derivation (single source of truth, derived once)
  // ---------------------------------------------------------------------------
  // CompetitionStatus wire values (internal/state/models.go:441-446):
  //   "setup" → "draw-ready" → "pools"/"knockout" (running phases) → "completed",
  //   plus "invalid". There is NO literal "running" status. An active
  //   competition is in its phase status ("pools" or "knockout"). isRunning is
  //   therefore the catch-all for any status that is not setup/draw-ready/
  //   completed, which also lets an "invalid" competition keep the progress +
  //   scores/results widgets (matching the pre-redesign always-on behaviour)
  //   rather than mis-rendering the "Competition complete" card.
  // viewMode ∈ { setup, draw-ready, running, completed }. The render helpers
  // branch on isSetup/isDrawReady/isRunning and treat the remaining case
  // (completed) as the trailing else, so no separate isCompleted flag is needed.
  const viewMode = overviewViewMode(c.status);
  const isSetup = viewMode === "setup";
  const isDrawReady = viewMode === "draw-ready";
  const isRunning = viewMode === "running";

  // ---------------------------------------------------------------------------
  // Schedule estimate fetch: same AbortController pattern as AdminSettings.
  // Never blocks render: estimate starts as null and fills in asynchronously.
  // ---------------------------------------------------------------------------
  const [estimate, setEstimate] = useState(null);
  const [estimateLoading, setEstimateLoading] = useState(false);
  const [estimateErr, setEstimateErr] = useState(null);

  // Re-fetch when config, participant count, or tournament-level timing changes.
  // When check-in is enabled the backend counts only checked-in participants, so
  // toggling check-in changes the effective count even if roster size stays fixed.
  // Tournament ceremony/timing fields mirror AdminSettings' estimate effect deps.
  const estimateCheckIn = checkInApplies(tournament, c.checkInEnabled);
  const estimatePlayerCount = (c.players || []).length;
  const estimateCheckedInCount = estimateCheckIn ? (c.players || []).filter(p => p && p.checkedIn).length : 0;
  // With participants.csv unreadable there is no roster to estimate from: the
  // request would only answer the server's opaque failure. The page says so
  // through the tiles and the footer below instead (D26).
  const rosterUnreadable = rosterUnreadableIssues(c).length > 0;
  const pausesKey = JSON.stringify(tournament?.pauses || []);
  useEffect(() => {
    if (!c.id || rosterUnreadable) return;
    const controller = new AbortController();
    setEstimateLoading(true);
    setEstimateErr(null);
    window.API.estimateCompetitionSchedule(c.id, password, controller.signal)
      .then(res => {
        setEstimate(res);
        setEstimateLoading(false);
      })
      .catch(e => {
        if (!controller.signal.aborted) {
          // The server's text ("internal error") means nothing to an
          // organiser: it goes to the console and the panel says what happened.
          console.error("Schedule estimate failed:", e && e.message);
          setEstimateErr(true);
          setEstimateLoading(false);
        }
      });
    return () => controller.abort();
  }, [c.id, c.format, c.kind, c.poolMatchDurationSeconds, c.knockoutMatchDurationSeconds, c.courts,
    c.teamSize, c.teamMatchType, c.poolSize, c.poolSizeMode, c.poolWinners, c.roundRobin, c.poolFormat,
    c.swissRounds, c.extraQualifiers, c.twoThirdPlaces, c.naginata, c.expectedParticipants, c.status, c.date, c.startTime,
    estimateCheckIn, password, estimatePlayerCount, estimateCheckedInCount, rosterUnreadable,
    pausesKey,
    tournament?.clockToElapsedMultiplier, tournament?.slowestCourtBufferPct]);

  // ---------------------------------------------------------------------------
  // Derived stat values
  // ---------------------------------------------------------------------------
  const participantLabel = c.kind === "team" ? "Teams" : "Participants";
  const numPlayers = (c.players || []).length;
  const seededCount = seededRanks(c.players).length;
  // The ASSIGNED count, not courtCount(). courtCount floors at 1 on purpose
  // (a running competition always occupies at least one shiaijo, and the
  // schedule surfaces divide by it), but this box sits one line under a page
  // head that reports the same record's allocation, and "1 Court" directly
  // contradicted it. The dashboard card already reports 0 for the same
  // record; this brings the three into line.
  //
  // 0 here means "none of its own", NOT "the draw has no shiaijo": an empty
  // list inherits the tournament's. That fact belongs on the page head, which
  // has the room to name the inherited shiaijo (admin_competition.jsx's
  // courtsSummary); a two-line stat box can only carry the count, and the
  // count it must carry is the one the operator chose.
  const numCourts = Array.isArray(c.courts) ? c.courts.length : 0;

  const formatCompMinutes = (typeof window !== "undefined" && window.formatCompMinutes)
    ? window.formatCompMinutes
    : (m) => (Number.isFinite(m) && m > 0 ? `${m}m` : null);

  // draw-ready: pool count vs bracket size
  const poolCount = (Array.isArray(pools) && pools.length > 0) ? pools.length : (c.pools ? c.pools.length : null);
  const bracketRounds = effectiveBracket && effectiveBracket.rounds ? effectiveBracket.rounds.length : null;

  const pct = total ? Math.round((done / total) * 100) : 0;

  // Standings/results target for this competition's format (see helpers above).
  const resultsSection = overviewResultsSection(c.format, hasBracketRounds(effectiveBracket));

  // ---------------------------------------------------------------------------
  // Render helpers
  // ---------------------------------------------------------------------------

  // The participants box, the same in every state. With participants.csv
  // unreadable the roster arrives empty, so a 0 here would contradict the
  // banner above it: the box shows the strip's one placeholder, "n/a", and says
  // why in muted ink (operator decision D26); the banner is the one full,
  // danger-coloured statement of the failure. The Seeded box follows it.
  function participantsTile() {
    return (
      <div className="stat-box" data-testid="stat-participants">
        {rosterUnreadable ? (
          <>
            <div className="v">n/a</div>
            <div className="l">{participantLabel} <span className="stat-box__flag">unreadable</span></div>
          </>
        ) : (
          <>
            <div className="v">{numPlayers}</div>
            <div className="l">{participantLabel}</div>
          </>
        )}
      </div>
    );
  }
  function seededTile() {
    return (
      <div className="stat-box" data-testid="stat-seeded">
        <div className="v">{rosterUnreadable ? "n/a" : seededCount}</div>
        <div className="l">Seeded</div>
      </div>
    );
  }

  // Stats strip: 4 boxes, varies by state.
  function renderStatsStrip() {
    if (isSetup) {
      return (
        <div className="stats-strip">
          {participantsTile()}
          {seededTile()}
          <div className="stat-box">
            <div className="v">{numCourts}</div>
            <div className="l">Shiaijo</div>
          </div>
          <EstimateDurationTile
            c={c}
            estimate={estimate}
            estimateLoading={estimateLoading}
            rosterUnreadable={rosterUnreadable}
            estimateCheckIn={estimateCheckIn}
          />
        </div>
      );
    }

    if (isDrawReady) {
      // Swiss has no pools at all (it borrows the pool pipeline but never
      // writes pools.csv), so poolCount and bracketRounds are both null and the
      // generic branches below would render "n/a" under a "Pools" heading. Its
      // draw size is the round count the operator configured. Same "Swiss is
      // not pools" boundary as StatusBadge and leagueAwareLabel (mp-dej2).
      const isSwissDraw = c.format === "swiss";
      const swissRoundCount = Number(c.swissRounds) || 0;
      const drawSizeVal = isSwissDraw
        ? (swissRoundCount ? `${swissRoundCount} round${swissRoundCount !== 1 ? "s" : ""}` : "n/a")
        : (c.format === "knockout" || (!poolCount && bracketRounds)
          ? (bracketRounds ? `${Math.pow(2, bracketRounds - 1) * 2} max` : "n/a")
          : (poolCount ? `${poolCount} pool${poolCount !== 1 ? "s" : ""}` : "n/a"));
      const drawSizeLabel = c.format === "knockout" ? "Bracket size"
        : c.format === "league" ? "League"
        : isSwissDraw ? "Swiss"
        : "Pools";
      return (
        <div className="stats-strip">
          {participantsTile()}
          {seededTile()}
          <div className="stat-box">
            <div className="v v--sm">{drawSizeVal}</div>
            <div className="l">{drawSizeLabel}</div>
          </div>
          <EstimateDurationTile
            c={c}
            estimate={estimate}
            estimateLoading={estimateLoading}
            rosterUnreadable={rosterUnreadable}
            estimateCheckIn={estimateCheckIn}
          />
        </div>
      );
    }

    if (isRunning) {
      return (
        <div className="stats-strip">
          {participantsTile()}
          <div className="stat-box">
            <div className="v">{done}/{total}</div>
            <div className="l">Matches done</div>
          </div>
          <div className="stat-box">
            <div className="v" style={{ color: runningCount > 0 ? "var(--red)" : "inherit" }}>
              {runningCount}
            </div>
            <div className="l">Now</div>
          </div>
          <EstimateDurationTile
            c={c}
            estimate={estimate}
            estimateLoading={estimateLoading}
            rosterUnreadable={rosterUnreadable}
            estimateCheckIn={estimateCheckIn}
          />
        </div>
      );
    }

    // completed
    return (
      <div className="stats-strip">
        {participantsTile()}
        <div className="stat-box">
          <div className="v">{done}</div>
          <div className="l">Matches played</div>
        </div>
        <div className="stat-box">
          <button
            type="button"
            className="stat-box__link"
            onClick={() => onSection(resultsSection)}
          >
            <div className="v v--sm" style={{ color: "var(--accent)" }}>View podium →</div>
            <div className="l">Standings</div>
          </button>
        </div>
        <EstimateDurationTile
          c={c}
          estimate={estimate}
          estimateLoading={estimateLoading}
          rosterUnreadable={rosterUnreadable}
          estimateCheckIn={estimateCheckIn}
        />
      </div>
    );
  }

  // Primary content: branched by state.
  function renderPrimaryContent() {
    // --- setup ---
    if (isSetup) {
      const steps = competitionNextSteps(c, tournament && tournament.courts);
      return (
        <div className="card" style={{ marginBottom: 16 }} data-testid="next-steps-card">
          <div className="card__head">
            <div className="card__title">Next steps</div>
          </div>
          <div className="comp-steps">
            {steps.map(step => (
              <div
                key={step.id}
                className={`comp-step comp-step--${step.state}`}
                data-testid={`step-${step.id}`}
              >
                <div className="comp-step__icon" aria-hidden="true">
                  {step.state === "done" ? "✓" : step.state === "active" ? "●" : "○"}
                </div>
                <div className="comp-step__body">
                  <div className="comp-step__label">{step.label}</div>
                  <div className="comp-step__detail">{step.detail}</div>
                  {step.state === "active" && step.section && step.cta && (
                    <button
                      type="button"
                      className="btn btn--primary comp-step__cta"
                      onClick={() => onSection(step.section)}
                      data-testid={`step-${step.id}-cta`}
                    >
                      {step.cta}
                    </button>
                  )}
                </div>
              </div>
            ))}
          </div>
          {numPlayers < 2 && !rosterUnreadable && (
            <div className="comp-steps__hint" data-testid="participants-hint">
              Need at least 2 {c.kind === "team" ? "teams" : "participants"} to generate a draw.
            </div>
          )}
        </div>
      );
    }

    // --- draw-ready ---
    if (isDrawReady) {
      // Swiss rounds are generated dynamically on start; there is no static
      // pools/bracket preview to show. Only non-swiss formats expose a preview
      // section in the nav at this stage.
      const isSwiss = c.format === "swiss";
      const previewSection = c.format === "knockout" ? "bracket" : "pools";
      const previewLabel = c.format === "knockout" ? "bracket" : c.format === "league" ? "league" : "pools";
      return (
        <div className="card" style={{ marginBottom: 16 }} data-testid="draw-ready-card">
          <div className="card__head">
            <div className="card__title">Draw ready</div>
            <div className="card__sub">{numPlayers} {participantLabel} · {seededCount > 0 ? `${seededCount} seeded` : "no seeds"}</div>
          </div>
          <div style={{ fontSize: 13.5, color: "var(--ink-2)", marginBottom: 14 }}>
            {isSwiss
              ? <>Participants are set. Use the <strong>Start competition →</strong> button in the header to begin the first round.</>
              : <>The draw has been generated. Preview it below, then use the <strong>Start competition →</strong> button in the header to begin.</>}
          </div>
          <div className="row">
            {!isSwiss && (
              <button
                type="button"
                className="card card--sm card--action"
                style={{ textAlign: "left" }}
                onClick={() => onSection(previewSection)}
                data-testid="draw-ready-preview-btn"
              >
                <div className="card__title" style={{ marginBottom: 4 }}>
                  Preview {previewLabel} →
                </div>
                <div className="card__sub">Check placements before starting</div>
              </button>
            )}
            <div className="card card--sm" style={{ background: "var(--bg)" }}>
              <div className="card__title" style={{ marginBottom: 4, color: "var(--ink-2)" }}>To start</div>
              <div className="card__sub">
                Use the <strong>Start competition →</strong> button above. To change the draw, use <strong>Discard draw</strong> first.
              </div>
            </div>
          </div>
        </div>
      );
    }

    // --- running ---
    if (isRunning) {
      return (
        <div>
          <div className="card" style={{ marginBottom: 16 }}>
            <div className="card__head">
              <div className="card__title">Progress</div>
              <div className="card__sub">{pct}%</div>
            </div>
            {/* Animate via transform: scaleX (compositor-only) rather than width,
                which forced a layout/paint each frame. The inner bar is full-width
                and scaled from the left edge; the rounded track clips it so the
                visual is equivalent. Transition is dropped under
                prefers-reduced-motion. */}
            <div
              style={{ height: 8, background: "var(--line-2)", borderRadius: 999, overflow: "hidden" }}
              role="progressbar"
              aria-valuenow={pct}
              aria-valuemin={0}
              aria-valuemax={100}
            >
              <div style={{
                height: "100%",
                width: "100%",
                background: "var(--accent)",
                transformOrigin: "left center",
                transform: `scaleX(${pct / 100})`,
                transition: prefersReducedMotion ? "none" : "transform 300ms",
              }}></div>
            </div>
          </div>
          <div className="row">
            <button
              type="button"
              className="card card--action"
              style={{ textAlign: "left" }}
              onClick={() => onSection("scores")}
            >
              <div className="card__title" style={{ marginBottom: 6 }}>Scores →</div>
              <div className="card__sub">Update or correct match results</div>
            </button>
            <button
              type="button"
              className="card card--action"
              style={{ textAlign: "left" }}
              onClick={() => onSection(resultsSection)}
            >
              <div className="card__title" style={{ marginBottom: 6 }}>Results →</div>
              <div className="card__sub">Visual bracket / pool standings</div>
            </button>
          </div>
        </div>
      );
    }

    // --- completed ---
    return (
      <div>
        <div className="card" style={{ marginBottom: 16 }} data-testid="completed-card">
          <div className="card__head">
            <div className="card__title">Competition complete</div>
          </div>
          <div style={{ fontSize: 13.5, color: "var(--ink-2)", marginBottom: 14 }}>
            All matches have been played. View final standings or export results.
          </div>
          <div className="row">
            <button
              type="button"
              className="card card--sm card--action"
              style={{ textAlign: "left" }}
              onClick={() => onSection(resultsSection)}
              data-testid="completed-results-btn"
            >
              <div className="card__title" style={{ marginBottom: 4 }}>Results →</div>
              <div className="card__sub">View final standings &amp; podium</div>
            </button>
            <button
              type="button"
              className="card card--sm card--action"
              style={{ textAlign: "left" }}
              onClick={() => onSection("export")}
              data-testid="completed-export-btn"
            >
              <div className="card__title" style={{ marginBottom: 4 }}>Export →</div>
              <div className="card__sub">Print brackets &amp; certificates</div>
            </button>
          </div>
        </div>
      </div>
    );
  }

  // Footer: schedule estimate strip. Shown whenever estimate resolves.
  function renderEstimateFooter() {
    // No roster, no estimate: say that rather than show whatever the request
    // last answered (it was the server's bare "internal error").
    if (rosterUnreadable) {
      return (
        <div className="schedule-estimate" data-testid="overview-estimate">
          <div className="schedule-estimate__title">Schedule estimate</div>
          <div className="schedule-estimate__empty">No estimate until the roster can be read.</div>
        </div>
      );
    }
    if (!estimate && !estimateLoading && !estimateErr) return null;
    const total = formatCompMinutes(estimate && estimate.totalDurationMinutes);
    const perShiaijo = formatPerShiaijoRow(estimate, c.courts);
    const pausesRow = formatPausesRow(tournament, c);
    // The footer carries no total: the stats strip's Est. duration tile,
    // drawn in every state beside this footer, already reads "about 1h 50m"
    // (and kachinuki's best-to-worst range), so a second "Total:" here only
    // repeated it in different words.
    return (
      <div className="schedule-estimate" data-testid="overview-estimate">
        <div className="schedule-estimate__title">
          Schedule estimate
          {estimateLoading && <span className="spinner schedule-estimate__spinner" />}
        </div>
        {estimateErr && (
          <div className="schedule-estimate__error">Could not estimate the schedule. Reload the page to try again.</div>
        )}
        {estimate && !estimateErr && (() => {
          if (!total) {
            return (
              <div className="schedule-estimate__empty">
                No estimate yet. Add participants and configure duration to see a projection.
              </div>
            );
          }
          return (
            <div className="schedule-estimate__body">
              {perShiaijo && (
                <div className="schedule-estimate__row">
                  <strong>Per shiaijo:</strong> {perShiaijo}
                </div>
              )}
              <div className="schedule-estimate__row">
                <strong>Pauses:</strong> {pausesRow}
              </div>
            </div>
          );
        })()}
      </div>
    );
  }

  return (
    <div>
      {/* First in the overview COLUMN: a file that will not parse blocks
          scoring, and every other number on this page is describing whatever
          survived the failed load.
          Not first on the PAGE below 720px, though. There `.comp-rail` stops
          being a left column and stacks above this one, so the banner lands
          about 650px down, under the section nav and off a phone's first
          screen (measured at 375x812). Putting it genuinely first would mean
          hoisting it into the page shell above the rail, which is a change to
          a surface this bead does not otherwise touch. Left here because the
          admin console is a tablet-and-up surface in practice (DESIGN.md
          Principle 4) and 768px still renders the rail beside this column,
          with the banner above the fold. */}
      <DataIssueBanner
        issues={c.dataIssues}
        competition={c}
        onReset={quarantineBracket}
        resetting={quarantining}
        onReload={onRefreshCompetition}
      />
      {renderStatsStrip()}
      {renderPrimaryContent()}
      {renderEstimateFooter()}
    </div>
  );
}

// FightingSpiritAwardsEditor: free-text form for adding/removing/saving
// optional fighting-spirit (敢闘賞) awards for a competition. Each award has
// a title, recipient name, and optional dojo. Save calls
// API.updateCompetitionAwards (PUT /api/competitions/:id/awards).
// v1 = free-text only; no competitor picker (deferred).
function FightingSpiritAwardsEditor({ c, password, showToast }) {
  // Each row carries a stable client-only `_key` so React keys survive
  // mid-list removals (index keys cause inputs to "jump" / reuse the wrong
  // DOM node when a middle row is deleted). `_key` is stripped before save.
  const keyCounterRef = useRef(0);
  const withKey = (a) => ({ ...a, _key: keyCounterRef.current++ });
  const [awards, setAwards] = useState(() => (c.fightingSpiritAwards || []).map(withKey));
  const [saving, setSaving] = useState(false);
  // The list changed on another device after this one was opened
  // (awards_changed): nothing was written, and `conflict` holds the list and
  // version now stored. Save waits for Reload, which takes them.
  const [conflict, setConflict] = useState(null);
  // `dirty` guards the prop-sync effect: the parent refetches the competition
  // on unrelated SSE events (match updates, status changes), which would
  // otherwise clobber the operator's typed-but-unsaved edits. We only re-sync
  // from props when there are no pending edits (except on a competition
  // SWITCH, where we always reset to the new comp's list.
  const [dirty, setDirty] = useState(false);
  // Unsaved edits hold a new build's automatic reload (app_version.jsx).
  useUnsavedInput(dirty);
  const lastCompIdRef = useRef(c.id);
  const mountedRef = useRef(true);
  useEffect(() => () => { mountedRef.current = false; }, []);
  // The version of the awards list the next save is made from: the one the
  // competition detail served, or the one the last save answered (the detail
  // refetch after a save can land later than a second save). A served version
  // is taken only along with the list it describes: while unsaved edits keep an
  // older list on screen, the version stays with that list, so a save over a
  // list another device has since changed is refused (awards_changed) instead
  // of overwriting it.
  const awardsVersionRef = useRef(c.awardsVersion);
  // The versions this editor has moved past on this competition, until the
  // served version catches up with the one held. A refetch that lands late can
  // still carry one of them, and taking it would put the older version back
  // under a list a later save has replaced.
  const pastVersionsRef = useRef(new Set());
  const holdVersion = (version) => {
    if (version === awardsVersionRef.current) return;
    pastVersionsRef.current.add(awardsVersionRef.current);
    pastVersionsRef.current.delete(version);
    awardsVersionRef.current = version;
  };
  // Set by Reload: the first version the props serve after it can be a refetch
  // that was out before the refusal came back, carrying a copy between the one
  // this editor was made from and the one Reload took. It is moved past, not
  // held.
  const reloadedRef = useRef(false);
  const dirtyRef = useRef(dirty);
  dirtyRef.current = dirty;
  const versionCompIdRef = useRef(c.id);
  useEffect(() => {
    const switched = versionCompIdRef.current !== c.id;
    versionCompIdRef.current = c.id;
    const afterReload = reloadedRef.current;
    reloadedRef.current = false;
    if (switched) {
      pastVersionsRef.current.clear();
      awardsVersionRef.current = c.awardsVersion;
    } else if (!dirtyRef.current) {
      if (c.awardsVersion !== awardsVersionRef.current && !pastVersionsRef.current.has(c.awardsVersion)) {
        if (afterReload) pastVersionsRef.current.add(c.awardsVersion);
        else holdVersion(c.awardsVersion);
      }
      // Once the served version is the one held, what this editor moved past is
      // settled: a version is a hash of the list, and a list that later returns
      // to older content brings that version back.
      if (c.awardsVersion === awardsVersionRef.current) pastVersionsRef.current.clear();
    }
  }, [c.id, c.awardsVersion]);

  // Sync from parent. Reset unconditionally when switching competitions;
  // otherwise skip while the user has unsaved edits (see `dirty` above).
  useEffect(() => {
    const switched = lastCompIdRef.current !== c.id;
    lastCompIdRef.current = c.id;
    // The served list is taken only when it is the version held: a reload (or
    // a save) answers a newer list than the props may carry yet, and the props
    // catching up must not put the older one back under the newer version.
    if (switched || (!dirty && c.awardsVersion === awardsVersionRef.current)) {
      setAwards((c.fightingSpiritAwards || []).map(withKey));
      if (switched) {
        setDirty(false);
        // A refusal on the competition just left says nothing about this one.
        setConflict(null);
      }
    }
  }, [c.id, c.fightingSpiritAwards, c.awardsVersion, dirty]);

  const reloadConflict = () => {
    holdVersion(conflict.version);
    reloadedRef.current = c.awardsVersion !== conflict.version;
    setAwards(conflict.awards.map(withKey));
    setConflict(null);
    setDirty(false);
  };

  // Counts the operator's edits, so a save that lands after more were typed
  // does not mark them saved.
  const editsRef = useRef(0);
  const addRow = () => { editsRef.current++; setDirty(true); setAwards(prev => [...prev, withKey({ title: "Fighting Spirit", recipientName: "", recipientDojo: "" })]); };
  const removeRow = (idx) => { editsRef.current++; setDirty(true); setAwards(prev => prev.filter((_, i) => i !== idx)); };
  const updateField = (idx, field, val) => { editsRef.current++; setDirty(true); setAwards(prev => prev.map((a, i) => i === idx ? { ...a, [field]: val } : a)); };

  // bc-obox G4b: a save goes to the outbox and is answered at once ({ pending,
  // settled }): the panel stops saving on that answer and keeps what was typed
  // on screen (dirty) until the server's answer comes, which is when the list
  // counts as saved. A save refused as stale (awards_changed) or on its merits
  // arrives the same way, and this panel, when still open on the competition,
  // shows the current list in the conflict banner with the typed values kept
  // until Reload; else the outbox's alert says it.
  const save = async () => {
    setSaving(true);
    const compID = c.id;
    const editsAt = editsRef.current;
    try {
      // Strip the client-only `_key` before sending to the API.
      const payload = awards.map(({ _key, ...rest }) => rest);
      const res = await window.API.updateCompetitionAwards(compID, payload, password, awardsVersionRef.current, {
        onRefusal: (e) => {
          if (!mountedRef.current || lastCompIdRef.current !== compID) return false;
          if (e && e.code === "awards_changed") setConflict({ version: e.awardsVersion, awards: e.fightingSpiritAwards });
          else showToast((e && e.message) || "Failed to save awards", "error");
          return true;
        },
      });
      // The save is kept on this device and goes out when the link allows: a
      // save made with no connection says so now, not when it lands (the same
      // line the court-call composer gives).
      if (mountedRef.current && isOffline()) showToast(savedOfflineSentence("Fighting Spirit awards"), "success");
      onDocumentSettled(res.settled, (saved) => {
        // Discarded from the held-writes list (or sent by another tab, whose
        // answer this one does not hold): not saved here, so the typed list
        // stays unsaved on screen and the version stays the one it was made at.
        if (!saved || saved.discarded || lastCompIdRef.current !== compID) return;
        holdVersion(saved.awardsVersion);
        if (!mountedRef.current) return;
        // Edits typed after this save was made are not in it: they stay unsaved.
        if (editsRef.current === editsAt) setDirty(false);
        showToast("Fighting Spirit awards saved.", "success");
      }, () => { /* told by onRefusal, or by the outbox's alert */ });
    } catch (e) {
      if (!mountedRef.current) return;
      showToast(e.message || "Failed to save awards", "error");
    } finally {
      if (mountedRef.current) setSaving(false);
    }
  };

  // The competition list item carries no awardsVersion (only the detail does),
  // so a save from it would be refused. When the detail has not loaded there is
  // nothing to edit against: say so, and the form comes with the detail.
  if (!c.awardsVersion) {
    return (
      <div className="row">
        <div className="card">
          <div className="card__title">🔥 Fighting Spirit Awards</div>
          <div className="card__sub" data-testid="fs-award-unavailable">
            The awards are not available because the details of this competition did not load. Reload the page, then open Awards again.
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="row">
      <div className="card" style={{ display: "flex", flexDirection: "column", gap: 10 }}>
        <div className="card__title">🔥 Fighting Spirit Awards <span style={{ fontWeight: 400, fontSize: 12, color: "var(--ink-3)" }}>(optional)</span></div>
        <div className="card__sub" style={{ marginTop: -4 }}>Record individual honourees independent of the placement podium. Shown to viewers on the Awards tab.</div>
      {conflict && (
        <div className="alert alert--error settings-conflict-alert" role="alert" data-testid="fs-award-conflict-alert">
          <div className="settings-conflict-alert__text">
            Not saved: the awards changed somewhere else after you opened this list (another screen or device). Reload to see the current awards, then make your change again.
          </div>
          <button type="button" className="btn btn--primary" onClick={reloadConflict} data-testid="fs-award-reload">Reload</button>
        </div>
      )}
      {awards.length === 0 && (
        <div style={{ fontSize: 12, color: "var(--ink-3)", fontStyle: "italic" }}>No awards yet.</div>
      )}
      {awards.map((a, idx) => (
        <div key={a._key} style={{ display: "flex", gap: 6, alignItems: "flex-start", flexWrap: "wrap" }}>
          <input
            className="input"
            placeholder="Title (e.g. Fighting Spirit)"
            value={a.title || ""}
            onChange={e => updateField(idx, "title", e.target.value)}
            style={{ flex: "1 1 140px", minWidth: 100 }}
            data-testid={`fs-award-title-${idx}`}
          />
          <input
            className="input"
            placeholder="Recipient name"
            value={a.recipientName || ""}
            onChange={e => updateField(idx, "recipientName", e.target.value)}
            style={{ flex: "2 1 180px", minWidth: 120 }}
            data-testid={`fs-award-name-${idx}`}
          />
          <input
            className="input"
            placeholder="Dojo (optional)"
            value={a.recipientDojo || ""}
            onChange={e => updateField(idx, "recipientDojo", e.target.value)}
            style={{ flex: "1 1 120px", minWidth: 90 }}
            data-testid={`fs-award-dojo-${idx}`}
          />
          <button type="button"
            className="btn btn--sm btn--ghost"
            onClick={() => removeRow(idx)}
            aria-label="Remove award"
            data-testid={`fs-award-remove-${idx}`}
          >✕</button>
        </div>
      ))}
      <div style={{ display: "flex", gap: 8, marginTop: 4 }}>
        <button type="button" className="btn btn--sm btn--ghost" onClick={addRow} data-testid="fs-award-add">+ Add award</button>
        <button type="button" className="btn btn--sm btn--primary" onClick={save} disabled={saving || !!conflict} data-testid="fs-award-save">
          {saving && <span className="spinner" />}
          {saving ? "Saving…" : "Save awards"}
        </button>
      </div>
      </div>
    </div>
  );
}

if (typeof window !== "undefined") {
  window.AdminCompOverview = AdminCompOverview;
  window.FightingSpiritAwardsEditor = FightingSpiritAwardsEditor;
  window.competitionNextSteps = competitionNextSteps;
  window.overviewViewMode = overviewViewMode;
  window.overviewResultsSection = overviewResultsSection;
  window.latestCourtFinish = latestCourtFinish;
  window.formatPausesRow = formatPausesRow;
  window.formatPerShiaijoRow = formatPerShiaijoRow;
  window.EstimateDurationTile = EstimateDurationTile;
}

export {
  competitionNextSteps,
  overviewViewMode,
  overviewResultsSection,
  hasBracketRounds,
};
