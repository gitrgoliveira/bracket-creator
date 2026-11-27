// admin_competition_bracket.jsx: AdminBracket section plus its RunningMatchPanel
// winner-picker detail panel and the pure scoreboard helpers they consume.
// Split out of admin_competition.jsx (mp-hpe3). buildRunningIpponResult and
// loadScoreboardPoints are ES-exported and re-exported by the entry for tests.

import { realIppons } from './result_slot.jsx';
import { writeKeepsEditorOpen } from './write_result.jsx';
import { confirmLeave } from './unsaved_entries.jsx';
import { competitionHasRecordedResult } from './recorded_result.jsx';
import { reviewsDraw } from './admin_draw_review.jsx';
import { DrawSwapCheck, effectiveFormat, drawEntrants, drawPlaceLabel, competitorLabel, swapDoneToast } from './admin_draw_swap_check.jsx';
import { useSwapCandidates, marksKey, SwapMarksKey, SwapVerdictWord } from './admin_swap_candidates.jsx';
import { acceptTap } from './tap_guard.jsx';
import { useEscapeToClose } from './ui.jsx';
import { publishHeight } from './published_height.jsx';
import { takeSwapMode } from './swap_mode_request.jsx';
const { useState, useEffect, useRef, useMemo } = React;

const hasBothSides = window.hasBothSides;
const hasPoolOriginPlaceholder = window.hasPoolOriginPlaceholder;
const CourtPicker = window.CourtPicker;
const EmptyState = window.EmptyState;

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
function buildRunningIpponResult(winnerSide, sideA, sideB, winnerIppons, loserIppons) {
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

// Pure loader for RunningMatchPanel's scoreboard-mode aPoints/bPoints from
// a (possibly completed) match. Reads each side's letters DIRECTLY from
// match.ipponsA / match.ipponsB rather than from score.ippons (which is
// only the winner's letters, by buildPatch / normalizeMatch convention).
//
// Bug fix companion to buildRunningIpponResult: the previous loader gated
// on `winner.id === sideX.id` and pulled from `score.ippons`, which
// returned only the winner's letters. That was fine when the writer
// always recorded loser=[]; once buildRunningIpponResult started writing
// 2-1 wins correctly (loser's single ippon preserved), the loader's
// truncation surfaced: a 2-1 win came back as 2-0 on re-render and
// re-submission silently dropped the loser's letter.
//
// admin_scoring_modal.jsx's initialAPts at line 30-31 already used the
// `m.ipponsA?.filter(...)` pattern; this helper aligns the running panel
// with the same shape.
//
// "•" placeholders are filtered (the full editor uses "•" as an empty
// slot marker; running panel doesn't write those but the data round-trips
// through the same backend fields so filtering is defensive).
//
// Exported for vitest at __tests__/admin_competition.test.jsx.
function loadScoreboardPoints(match) {
  if (!match) return { aPoints: [], bPoints: [] };
  return {
    aPoints: realIppons(match.ipponsA),
    bPoints: realIppons(match.ipponsB),
  };
}

const RunningMatchPanel = React.memo(({ match, compId, courts, matchNum, onMoveCourt, onEditScore, password, hostUnsavedRef }) => {
  // Reuse the shared components wholesale: both off the window bridge at render
  // time so module load order never matters:
  //   ScoreEditorModal (variant="inline"): the ONE scoring editor the shiaijo /
  //     pools / scores screens use. We keep ITS header (it's the same in every
  //     host), so the panel doesn't add a competing title. `match` is enriched
  //     upstream with the comp metadata the editor needs (compId, compKind,
  //     teamSize, phase, round): see AdminBracket.scoringMatch.
  //   MatchDetailCard: the SAME read-only result card the public viewer shows
  //     for a completed match (names + winner highlight + FINAL + score marks),
  //     instead of a stripped-down lookalike.
  const ScoreEditorModal = window.ScoreEditorModal;
  const MatchDetailCard = window.MatchDetailCard;
  const isComplete = match.status === "completed";
  // A completed match shows its read-only result by default; re-opening the
  // editor to change a recorded result is gated behind a confirmation so it's
  // not changed by accident. An un-played match goes straight to the editor
  // (nothing to overwrite). Reset when switching matches.
  const [editing, setEditing] = useState(false);
  useEffect(() => { setEditing(false); }, [match.id]);
  const showEditor = !isComplete || editing;

  return (
    <div className="running-panel">
      {/* The card fills the row to align with the bracket card above; this inner
          column caps the content to a comfortable width and centres it so the
          reused editor / result card aren't stretched edge-to-edge. */}
      <div className="running-panel__inner">
      {/* Slim header: only the bits the reused components' own headers DON'T
          carry: the bracket match number and the move-court control. Comp /
          shiaijo / round / time / status come from the embedded component. */}
      <div className="running-panel__head">
        <div className="running-panel__title">{matchNum != null ? `Match M${matchNum}` : `Match · ${match.id.slice(-6)}`}</div>
        {onMoveCourt && courts && courts.length ? (
          <div className="running-panel__court">
            <CourtPicker
              value={match.court}
              courts={courts}
              onChange={(cc) => onMoveCourt(compId, match.id, cc, { shown: match, hostUnsavedRef })}
              btnClassName="running-panel__court-btn"
              label="Shiaijo "
            />
          </div>
        ) : null}
      </div>

      {showEditor ? (
        ScoreEditorModal && (
          <ScoreEditorModal
            // NO subResults.length here. It used to be in the key because the
            // editor read team sub-bouts into refs at mount, so a daihyosen
            // added or removed elsewhere could only be picked up by remounting.
            // Those refs are gone (bc-tsub): the board is derived from the live
            // prop and reconciled by position, so it follows a shape change in
            // place. Remounting on every bout-log change also DISCARDED the
            // operator's unsaved scores — on a kachinuki encounter that is every
            // appended pairing — which is the harm, not the fix.
            // Nor the status (bc-tmfn), for the same reason: the editor
            // follows a status change in place, as it does on the Scores tab
            // and the court console, whose keys never carried one. Remounting
            // on completed -> running threw away what a Clear withdrawal and
            // reopen came back with (its notice naming a later match it also
            // reopened) at the moment the operator goes on scoring here.
            key={match.id}
            variant="inline"
            match={match}
            hostUnsavedRef={hostUnsavedRef}
            // Offer "close" only when there's a result card to fall back to
            // (editing a completed match); an un-played match has nowhere to go.
            canClose={isComplete}
            onClose={() => setEditing(false)}
            allowCourtForce
            onSubmit={async (patch, opts) => {
              try {
                // opts: the editor's own claim on a refusal (onRefusal).
                const res = await onEditScore(compId, match.id, patch, match, { ...opts, allowCourtForce: true });
                // Recorded → fall back to the result card, unless the editor's
                // job is not done (writeKeepsEditorOpen).
                if (!writeKeepsEditorOpen(patch, res)) setEditing(false);
                // What the write came back with: a refused one threw and
                // returns nothing, which is how the editor tells them apart.
                return res;
              } catch (_e) { /* surfaced via toast in the parent */ }
            }}
            onSubmitAndNext={null}
            password={password}
          />
        )
      ) : (
        <div className="running-panel__result">
          {MatchDetailCard && <MatchDetailCard match={match} escapeToClose={false} />}
          <button type="button" className="btn btn--sm btn--full" onClick={async () => {
            const ok = await window.confirmDialog({
              title: "Edit recorded result?",
              message: `${matchNum != null ? `Match M${matchNum}` : "This match"} already has a result. Re-open scoring to change it?`,
              confirmLabel: "Edit result",
            });
            if (ok) setEditing(true);
          }}>Edit result</button>
        </div>
      )}
      </div>
    </div>
  );
});
RunningMatchPanel.displayName = "RunningMatchPanel";
// ---------------------------------------------------------------------------
// The Swap competitors mode (bc-drev FR10, decision D8 (b)): on Pools
// (preview) of a pools + knockout draw and Bracket (preview) of a
// knockout-only one, while the competition is draw-ready with nothing
// recorded, the organiser taps two competitors to swap them. Both hosts
// (AdminPools, AdminBracket below) run this one hook; the shared viewers
// (PoolsViewer, BracketTree) take its `swap` prop, which only these hosts pass,
// so the public pages render as before.
//
// A tap picks, a tap on the picked name unpicks, a tap on another opens "If
// you swap them" (DrawSwapCheck), the only door that sends a swap. Every tap
// of the mode (turning it on, a pick, Cancel, Done) goes through ONE keyless
// tap guard (acceptTap), so the bounce of a double tap never picks and
// unpicks, picks twice, or lands on the Done that took the place of the
// button it turned on with; the check panel takes the bounce of the tap that
// opened it itself. Once a competitor is picked, every other one is marked
// better or worse for dojo-mates (useSwapCandidates, admin_swap_candidates.jsx)
// until the pick is cancelled or the second tap opens the check. After a
// swap the mode stays on with nothing picked, so a
// second swap can follow; Done, Escape, or leaving the tab (the host
// unmounts) turns it off. The Draw review card's "Swap competitors on Pools"
// button opens the tab with the mode already on (swap_mode_request.jsx, taken
// once as the tab mounts). While the mode is on its banner is pinned under
// the admin top bar, and publishes its height as --swap-banner-h
// (published_height.jsx) for the scroll margin that keeps a tapped or
// focused competitor clear of both bars. The page reads the swapped draw on
// the draw_generated broadcast the swap makes (admin.jsx).
// ---------------------------------------------------------------------------

// swapModeAvailable: does this tab offer the mode now? A pools + knockout
// draw is swapped on its pools (its bracket holds "Pool A-1st" places, not
// competitors), a knockout-only draw on its bracket.
export function swapModeAvailable(c, tab, poolMatches, bracket) {
  if (!c || c.status !== 'draw-ready' || !reviewsDraw(c)) return false;
  if (effectiveFormat(c) !== (tab === 'bracket' ? 'knockout' : 'mixed')) return false;
  return !competitionHasRecordedResult(poolMatches, bracket);
}

// swapPickLine: the banner's line once one competitor is picked, naming them
// number first and then their place ("Swap P1 Player 08 (Pool A) with…",
// "Swap K1 Player 01 (M3) with…", operator decision C10). What to do next and
// what the marks mean are said by the key line (SwapMarksKey) and the marks,
// so this one never changes length when they arrive.
export function swapPickLine(picked, bracket) {
  const where = drawPlaceLabel(picked.place, bracket);
  return `Swap ${competitorLabel(picked)}${where ? ` (${where})` : ''} with…`;
}

// useSwapCompetitors: the mode's state for one host tab. Returns `swap` (the
// prop for the viewer, null while the mode is off), `active`, and `controls`
// (the button or banner, and the check panel) for the host to draw above the
// viewer. `beforeTurnOn` may refuse (an editor holding unsaved entries).
export function useSwapCompetitors({ c, tab, pools, poolMatches, bracket, password, drawHidden, beforeTurnOn, onTurnOn, onRefreshCompetition }) {
  // Taken once, as the tab mounts: on when the Draw review card's button
  // opened the tab to swap competitors.
  const [on, setOn] = useState(() => takeSwapMode(c && c.id));
  const [pickedId, setPickedId] = useState(null);
  const bannerRef = useRef(null);
  const [checking, setChecking] = useState(null);
  const [note, setNote] = useState(null);
  const tapRef = useRef(null);
  const available = swapModeAvailable(c, tab, poolMatches, bracket);
  const active = on && available;
  // Opened from the card's button, the page scrolls to the banner, with the
  // first competitors under it in view (operator decision S4); on a phone the
  // tab's menu sits above the banner, so the top of the page would hide it.
  // The page first goes to its top: the Overview's scroll is kept on a tab
  // switch, so the banner may already be pinned there, which scrollIntoView
  // takes as in view. Once, and only for that entry: a plain tab switch keeps
  // the page where it is. The card's button went with the Overview, so focus
  // goes to the banner (focusable for this alone), whose instruction is the
  // next thing to read; without scrolling, since the page was just placed.
  const askedRef = useRef(on);
  useEffect(() => {
    if (!active || !askedRef.current || !bannerRef.current) return;
    askedRef.current = false;
    window.scrollTo(0, 0);
    bannerRef.current.scrollIntoView({ block: 'start' });
    bannerRef.current.focus({ preventScroll: true });
  }, [active]);
  // The pinned banner's height, for the competitors' scroll margin, published
  // while the banner is drawn.
  useEffect(() => {
    if (!active || !bannerRef.current) return undefined;
    return publishHeight(bannerRef.current, document.documentElement, '--swap-banner-h');
  }, [active]);
  const entrants = useMemo(() => {
    const byId = new Map();
    if (available) drawEntrants(c, pools, bracket).forEach((g) => g.entrants.forEach((e) => byId.set(e.id, e)));
    return byId;
  }, [available, c, pools, bracket]);
  // A pick the draw no longer holds (it changed under the page) reads as none,
  // and a pick the draw still holds carries the place it holds now.
  const picked = active && pickedId ? entrants.get(pickedId) || null : null;

  const turnOff = () => { setOn(false); setPickedId(null); setChecking(null); setNote(null); };
  // One Escape closes one layer: while the check panel is open it is the
  // panel's (DrawSwapCheck cancels on it), never the mode's as well.
  useEscapeToClose(active && !checking ? turnOff : undefined);

  const guarded = (fn) => (ev) => { if (!acceptTap(tapRef, ev)) return; fn(ev); };
  const turnOn = guarded(async () => {
    if (beforeTurnOn && !(await beforeTurnOn())) return;
    setOn(true);
    setPickedId(null);
    setNote(null);
    if (onTurnOn) onTurnOn();
  });
  const onPick = (entrant, ev) => {
    if (!acceptTap(tapRef, ev)) return;
    setNote(null);
    if (!picked) { setPickedId(entrant.id); return; }
    if (picked.id === entrant.id) { setPickedId(null); return; }
    setChecking({ a: picked, b: entrant });
  };
  // After a swap, or a draw that moved under the page, the page reads the
  // competition again itself, as the Draw review card does, rather than
  // waiting for the event stream (which may be down). The panel's close
  // returns focus to the second pick's row, which the re-read takes away:
  // focus goes to the banner's line instead, after the panel's own move.
  const [focusLine, setFocusLine] = useState(0);
  const lineRef = useRef(null);
  useEffect(() => {
    if (focusLine && lineRef.current) lineRef.current.focus({ preventScroll: true });
  }, [focusLine]);
  const onSwapped = (answer) => {
    const pair = checking;
    setChecking(null);
    setPickedId(null);
    setNote({ text: swapDoneToast(answer, pair && pair.a, pair && pair.b) });
    setFocusLine((n) => n + 1);
    if (onRefreshCompetition) onRefreshCompetition();
  };
  const onDrawChanged = (message) => {
    setChecking(null);
    setPickedId(null);
    setNote({ text: message || 'The draw has changed. Pick the two competitors again.', error: true });
    setFocusLine((n) => n + 1);
    if (onRefreshCompetition) onRefreshCompetition();
  };

  // The marks belong to the first pick alone: none while nothing is picked or
  // while the check panel is open for the second.
  // A draw that places someone elsewhere (another device's swap) reads the
  // marks again too. The key is where every competitor stands, not the
  // object's identity, so a re-read of the same draw keeps the marks.
  const drawKey = useMemo(
    () => [...entrants.values()].map((e) => `${e.id}@${JSON.stringify(e.place)}`).join('|'),
    [entrants],
  );
  const { marks, dojos, failed } = useSwapCandidates({
    compId: c && c.id, pickId: picked && !checking ? picked.id : null, drawKey, password, onDrawChanged,
  });
  const markOf = (id) => (marks && marks.get(id)) || null;
  const dojosOf = (id) => (dojos && dojos.get(id)) || null;

  const swap = active ? {
    entrantOf: (id) => entrants.get(id) || null,
    pickedId: picked ? picked.id : null,
    onPick,
    markOf,
    // The mark's line under the dojo ("Worse for Fudo, Hizen"), drawn blank
    // on every pick while the mode is on, so a mark that appears moves
    // nothing.
    verdictWord: (id) => <SwapVerdictWord mark={markOf(id)} dojos={dojosOf(id)} />,
  } : null;

  let controls = null;
  if (available && !active) {
    controls = (
      <div className="swap-mode swap-mode--off">
        <button type="button" className="btn btn--sm" data-testid="swap-mode-on" onClick={turnOn}>Swap competitors</button>
      </div>
    );
  } else if (active) {
    // The check panel is drawn beside the banner, never inside it: the pinned
    // banner is a stacking context under the top bar, which would hold the
    // panel's backdrop under the bar too.
    controls = (
      <>
      <div className="swap-mode swap-mode--pinned" role="region" aria-label="Swap competitors" data-testid="swap-mode" ref={bannerRef} tabIndex={-1}>
        {/* One layout on every screen (operator decision C7): the text, then
            the buttons on the right of the same row. Before the first pick
            the text is the instruction; from the pick on it is the pick line
            alone, with Cancel beside Done, and the key line goes under. */}
        <div className="swap-mode__head">
          <div className="swap-mode__text">
            {!picked && (
              <p className="swap-mode__lead">
                <strong>Swap competitors:</strong> tap two competitors to swap them. While this is on, a tap picks a competitor instead of opening a score sheet.
              </p>
            )}
            {/* Always drawn, so the pick line and a note are announced as
                they are written. */}
            <p className="swap-mode__line" aria-live="polite" data-testid="swap-mode-line" ref={lineRef} tabIndex={-1}>
              {picked ? swapPickLine(picked, bracket) : note ? (
                <span className={note.error ? 'swap-mode__note swap-mode__note--error' : 'swap-mode__note'}>{note.text}</span>
              ) : null}
            </p>
          </div>
          <div className="swap-mode__actions">
            {picked && (
              <button type="button" className="btn btn--sm" data-testid="swap-mode-cancel" onClick={guarded(() => setPickedId(null))}>Cancel</button>
            )}
            <button type="button" className="btn btn--sm btn--primary" data-testid="swap-mode-done" onClick={guarded(turnOff)}>Done</button>
          </div>
        </div>
        {/* What the marks mean, on a line of its own drawn from the pick on at
            the height of its longest sentence (two lines at most on a phone),
            so the marks arriving move nothing. */}
        {picked && <SwapMarksKey current={checking ? null : marksKey({ marks, failed })} />}
      </div>
      {checking && (
        <DrawSwapCheck comp={c} bracket={bracket} a={checking.a} b={checking.b} password={password} drawHidden={drawHidden}
          onClose={() => setChecking(null)} onSwapped={onSwapped} onDrawChanged={onDrawChanged} />
      )}
      </>
    );
  }
  return { swap, active, controls };
}

function AdminBracket({ c, t, bracket, onMoveCourt, onEditScore, tweaks, password, drawHidden, hostUnsavedRef = null, onRefreshCompetition }) {
  const [selected, setSelected] = useState(null);
  // The panel's editor publishes its unsaved entries here: choosing another
  // match puts it in the editor's place (keyed by match id), so over unsaved
  // entries that asks first (R1, stage 5 review 8). The competition page
  // hands its own ref down (`hostUnsavedRef`), so its tabs and exits ask on
  // the same entries (stage 5 review 9, M1).
  const ownUnsavedRef = useRef(null);
  const editorUnsavedRef = hostUnsavedRef || ownUnsavedRef;
  const scrollRef = useRef(null);
  const panelRef = useRef(null);
  const [autoScrollId, setAutoScrollId] = useState(null);

  // The scoring panel sits BELOW the bracket (operator decision), so picking a
  // match brings it into view. Keyed on the picked match's id, so a re-render
  // of the same pick (an SSE update) does not scroll the page back to it.
  const selectedId = selected?.matchId;
  useEffect(() => {
    if (!selectedId || !panelRef.current) return;
    const reduce = typeof window.matchMedia === "function" &&
      window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    panelRef.current.scrollIntoView({ block: "start", behavior: reduce ? "auto" : "smooth" });
  }, [selectedId]);

  // Recenter on the running match whenever it changes (initial bracket
  // load, or one match finishing and the next starting). Empty deps would
  // miss the case where `bracket` is still null on first mount and only
  // populates via the detail fetch / SSE. Also check thirdPlaceMatch.
  const runningMatchId = (() => {
    const inRounds = (bracket?.rounds || []).flatMap(r => r || []).find(m => m && m.status === "running");
    if (inRounds) return inRounds.id;
    const bronze = bracket?.thirdPlaceMatch;
    if (bronze && bronze.status === "running") return bronze.id;
    return null;
  })();
  useEffect(() => {
    if (runningMatchId) setAutoScrollId(runningMatchId + "::" + Date.now());
  }, [runningMatchId]);

  // Label a selected match with the SAME number ("M1") and round the bracket
  // tree shows: derived from the shared buildDisplayModel so the panel and the
  // cards/columns can never disagree. Recomputed from bracket.rounds (not stored
  // at click time) so it survives SSE topology refreshes. Hook stays above the
  // early return below so the hook order is stable.
  const displayModel = React.useMemo(
    () => (bracket?.rounds && window.buildDisplayModel ? window.buildDisplayModel(bracket.rounds) : { hasMeta: false, matchNumById: null }),
    [bracket]
  );
  // The 3rd-place card's empty sides ("Loser of M<n>"), numbered from the same
  // display model and memoised with it, so MatchCard's memo holds across renders.
  const bronzeEmptyLabels = React.useMemo(
    () => (bracket?.rounds && window.bronzeSlotLabels ? window.bronzeSlotLabels(bracket.rounds, displayModel.matchNumById) : undefined),
    [bracket, displayModel]
  );
  // Swap competitors (knockout-only draws): turning it on closes the scoring
  // panel, so over unsaved entries it asks first, as choosing another match does.
  const swapMode = useSwapCompetitors({
    c, tab: 'bracket', pools: null, poolMatches: null, bracket, password, drawHidden,
    beforeTurnOn: () => confirmLeave(editorUnsavedRef),
    onTurnOn: () => setSelected(null),
    onRefreshCompetition,
  });
  const matchMeta = (matchId) => {
    if (!matchId || !bracket?.rounds) return { matchNum: null, roundName: null };
    const cols = displayModel.hasMeta ? displayModel.columns : bracket.rounds;
    let ci = -1, found = null;
    for (let i = 0; i < cols.length; i++) {
      const hit = (cols[i] || []).find((m) => m && m.id === matchId);
      if (hit) { ci = i; found = hit; break; }
    }
    return {
      matchNum: displayModel.matchNumById ? displayModel.matchNumById[matchId] : null,
      // window.bracketRoundLabel is the one round-naming primitive (mp-u37s), so
      // this panel, the column header beside it and the viewer/board rows for the
      // same match can never disagree. `ci` is the DISPLAY column whenever the bracket carries metadata, and the raw index otherwise (where no displayRound exists to disagree with),
      // which is why this surface was correct before; keep it derived from the
      // match anyway rather than holding a second copy of the rule.
      roundName: (ci >= 0 && window.bracketRoundLabel) ? window.bracketRoundLabel(found, ci, cols.length) : null,
    };
  };

  if (!bracket || !bracket.rounds) {
    const previewMode = c && c.status === "draw-ready";
    return <EmptyState icon="settings" title="Bracket not generated yet" message={previewMode ? "Bracket not available for this format preview." : "Start the competition to build the bracket."} />;
  }
  const select = async (m, ri, mi) => {
    if (selected && selected.matchId !== m.id && !(await confirmLeave(editorUnsavedRef))) return;
    setSelected({ matchId: m.id, ri, mi });
  };
  // Look up the selected match by ID rather than [ri][mi] index. The
  // index can go stale if an SSE-driven bracket rebuild (knockout
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
    // Also check the bronze/3rd-place match (any competition requiring a single 3rd; bc-3rdp).
    const bronze = bracket?.thirdPlaceMatch;
    if (bronze && bronze.id === selected.matchId) return bronze;
    return null;
  };
  // All bracket scoring now flows through the shared inline ScoreEditorModal →
  // onEditScore (the same path pools / scores / shiaijo use), so the bespoke
  // score entry points are gone from this panel.
  const selectedMatch = findSelectedMatch();
  // Compute the match's number/round ONCE (one scan of the display model).
  const selectedMeta = selectedMatch ? matchMeta(selectedMatch.id) : { matchNum: null, roundName: null };
  // Enrich the bracket match with the competition metadata the shared scorer
  // (ScoreEditorModal / MatchDetailCard) reads off the match object: the raw
  // bracket.rounds entries carry none of it. Mirrors enrichPoolMatchWithComp:
  //   compId      → decision endpoints + naginata fetch
  //   compKind /  → individual vs team editor routing
  //   teamSize
  //   phase       → "bracket" makes isKnockoutPhase true: blocks hikiwake (no
  //                 draws in elimination: decide by hantei after encho)
  //   compFormat, round, matchNumber → header/label fields
  const scoringMatch = selectedMatch && {
    ...selectedMatch,
    compId: selectedMatch.compId || c.id || "",
    compName: selectedMatch.compName || c.name || "",
    compFormat: selectedMatch.compFormat || c.format || "",
    compKind: selectedMatch.compKind || c.kind || "",
    teamSize: selectedMatch.teamSize ?? c.teamSize ?? 0,
    compEngi: !!(selectedMatch.compEngi ?? c.engi),
    phase: selectedMatch.phase || "bracket",
    round: selectedMatch.round || selectedMeta.roundName,
    matchNumber: selectedMatch.matchNumber || selectedMeta.matchNum || 0,
  };
  // mp-turx: per-match playability: a bracket match is running iff hasBothSides()
  // returns true (both sides are resolved real participants, not “Winner of rX-mY”
  // feeders or pool-origin placeholders like “Pool A-1st”). The old bracket-wide
  // `isPreview` gate is replaced: the tree always renders, and individual matches
  // are non-interactive until their sides are decided. This supports the incremental
  // fill-in model where pool finishers seed into the knockout as each pool completes.
  // Show the "filling in" banner ONLY when pool-origin placeholders remain (a
  // mixed comp whose feeder pools haven't all finished): NOT for ordinary
  // "Winner of rX-mY" feeders or structural byes, which standalone knockout and
  // bye-containing brackets legitimately have.
  const hasUnseededPools = bracket.rounds.some(r => (r || []).some(m => hasPoolOriginPlaceholder(m)));
  return (
    // The bracket takes the full width and the scoring panel sits below it
    // (operator decision; see .bracket-layout in styles.css): beside it, the
    // panel left a four-round tree too little width at any desktop size. The
    // tree's columns narrow to fit (.bracket-canvas--fit).
    <div className="bracket-layout">
      <div className="bracket-layout__bracket">
        {swapMode.controls}
        {hasUnseededPools && (
          <div className="banner banner--info" style={{ marginBottom: 12, padding: "10px 14px", background: "var(--accent-soft)", border: "1px solid var(--accent)", borderRadius: 6, fontSize: 13 }}>
            <strong>Knockout filling in</strong>: this bracket fills in automatically as each pool finishes. Matches start once both sides are decided.
          </div>
        )}
        <div className="bracket-canvas bracket-canvas--fit" ref={scrollRef}>
          <div className="bracket-canvas__inner">
            <window.BracketTree
              rounds={bracket.rounds}
              isEngi={!!c.engi}
              variant={tweaks.cardVariant}
              showDojo={tweaks.showDojo}
              onMatchClick={(m, ri, mi) => { if (hasBothSides(m)) select(m, ri, mi); }}
              highlightedMatchId={swapMode.active ? undefined : selected?.matchId}
              autoScrollMatchId={autoScrollId}
              scrollContainerRef={scrollRef}
              swap={swapMode.swap}
            />
            {bracket.thirdPlaceMatch && (() => {
              const bm = bracket.thirdPlaceMatch;
              const isReady = hasBothSides(bm) && !swapMode.active;
              const isHighlighted = selected?.matchId === bm.id;
              // The "3rd Place Match" section header identifies the lone bronze
              // card, so the card omits a per-card meta badge (no redundant
              // "3RD" repeating the header). It renders smaller and UNDER the
              // final match card, so the two "end" matches read together and it
              // can't be misread as a semifinal. The tree's columns narrow to
              // fit here, so the bronze sits in a row of column slots sized by
              // the same rules (.bracket-bronze-row), the last one under the
              // final, rather than at a fixed offset.
              const slots = window.bracketColumnCount(bracket.rounds);
              return (
                <div className="bracket-bronze-row" data-testid="bracket-bronze-match">
                  {Array.from({ length: Math.max(0, slots - 1) }, (_, i) => (
                    // Empty column slots, never reordered: the index is the key.
                    <div key={i} className="bracket-bronze-row__slot" aria-hidden="true" />
                  ))}
                  <div className="bracket-bronze-row__slot bracket-bronze-section">
                    <div className="bracket-bronze-label">
                      3rd Place Match
                    </div>
                    <window.MatchCard
                      match={bm}
                      isEngi={!!c.engi}
                      variant={tweaks.cardVariant}
                      showDojo={tweaks.showDojo}
                      highlighted={isHighlighted}
                      onClick={isReady ? () => select(bm, -1, 0) : undefined}
                      emptyLabels={bronzeEmptyLabels}
                    />
                  </div>
                </div>
              );
            })()}
          </div>
        </div>
      </div>
      <div className="bracket-layout__panel" ref={panelRef}>
        {swapMode.active ? null : hasBothSides(selectedMatch) ? (
          <RunningMatchPanel
            hostUnsavedRef={editorUnsavedRef}
            match={scoringMatch}
            compId={c.id}
            courts={t?.courts || []}
            matchNum={selectedMeta.matchNum}
            onMoveCourt={onMoveCourt}
            onEditScore={onEditScore}
            password={password}
          />
        ) : selectedMatch ? (
          <EmptyState title="Match not ready" message="Waiting for upstream winners." />
        ) : (
          <EmptyState icon="pointer" title="Pick a match" message="Click any match in the bracket to record results." />
        )}
      </div>
    </div>
  );
}

window.AdminBracket = AdminBracket;

export { buildRunningIpponResult, loadScoreboardPoints };
