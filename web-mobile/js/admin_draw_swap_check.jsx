// admin_draw_swap_check.jsx: "If you swap them", the check every swap of two
// competitors in a drawn competition passes through first (bc-drev FR7), and
// the words the draw review uses for places and meetings.
//
// Every swap door opens this panel: a suggestion and the two pickers on the
// Overview's Draw review card (admin_draw_review.jsx), and the preview tabs'
// Swap competitors mode, which imports it from here without touching the card.
// On open it asks the server for a dry run of the swap (POST .../draw/swap with
// dryRun, which saves nothing) and shows what the swap would do in plain words,
// opening with whether any dojo-mates would meet earlier; "Swap them" ("Swap
// anyway", in the warning colour, when some would) sends the same two places
// without dryRun. The server judges
// everything (refusals, where each goes, the numbers, the seed warning, the
// lineups, the byes, the separation before and after); this module only words
// its answer.
//
// The layer is opened by a tap, so it takes that tap's bounce
// (useOpenedTapGuard, a confirm: every click in the layer for TAP_BOUNCE_MS)
// and moves focus in to its first button, Cancel, and back when it closes
// (useDialogFocus). Escape and a tap outside cancel, except while the swap is
// being sent: its answer has to land here.

import { bracketRoundLabel } from './bracket.jsx';
import { useDialogFocus } from './dialog_focus.jsx';
import { useOpenedTapGuard } from './tap_guard.jsx';
import { useEscapeToClose } from './ui.jsx';
import { useLatestRef } from './latest_ref.jsx';

const { useState, useEffect, useRef } = React;

// The refusals that mean the draw on screen is no longer the stored one: the
// panel closes and its host reads the draw again.
export const DRAW_MOVED_CODES = new Set(['draw_changed', 'draw_started', 'no_draw']);

// competitorLabel: a competitor as the review names them, number first when
// the competition numbers ("IMS2 Elijah Everdeen").
export function competitorLabel(side) {
  if (!side) return '';
  return [side.number, side.name].filter(Boolean).join(' ');
}

// poolLabel: a pool as pools.csv holds its label ("Pool C"); a bare label is
// given its word.
export function poolLabel(pool) {
  const p = String(pool || '');
  return /^pool\b/i.test(p) ? p : `Pool ${p}`;
}

// firstRoundMatchLabel: the first-round match a knockout place belongs to,
// by the number the bracket prints on its card.
function firstRoundMatchLabel(bracket, matchIndex) {
  const m = bracket && bracket.rounds && bracket.rounds[0] ? bracket.rounds[0][matchIndex] : null;
  return m && m.matchNumber ? `first-round Match ${m.matchNumber}` : `first-round match ${matchIndex + 1}`;
}

// drawPlaceLabel: where a place is, in words: "Pool C" (", place 2" with
// withPosition, for a swap inside one pool, where only the order changes), or
// "first-round Match 3" for a knockout place.
export function drawPlaceLabel(place, bracket, { withPosition = false } = {}) {
  if (!place) return '';
  if (place.pool) return withPosition && place.position ? `${poolLabel(place.pool)}, place ${place.position}` : poolLabel(place.pool);
  if (typeof place.slot === 'number') return firstRoundMatchLabel(bracket, Math.floor(place.slot / 2));
  return '';
}

// effectiveFormat: a competition's format as the server reads it
// (Competition.EffectiveFormat): a config with no format key is a knockout.
export function effectiveFormat(comp) {
  return (comp && comp.format) || 'knockout';
}

// drawEntrants: every competitor the draw holds, with the place a swap names
// them by, grouped as the draw groups them: by pool for a pools draw
// ({pool, position}, 1-based, the pool's own order), by first-round match for
// a knockout-only one ({slot}, side A of match m is 2m, side B 2m+1). Only a
// competitor with a participant id is listed: an id is what a swap names. A
// bracket side carries no seed, so a knockout entrant's seed is read off the
// competition's roster by id; a pool's player carries it from pools.csv.
export function drawEntrants(comp, pools, bracket) {
  const groups = [];
  if (effectiveFormat(comp) === 'knockout') {
    const seedOf = new Map((comp.players || []).filter((p) => p && p.id && p.seed > 0).map((p) => [p.id, p.seed]));
    const first = (bracket && bracket.rounds && bracket.rounds[0]) || [];
    first.forEach((m, mi) => {
      const entrants = [];
      [[m && m.sideA, 2 * mi], [m && m.sideB, 2 * mi + 1]].forEach(([side, slot]) => {
        if (side && side.id && side.name) entrants.push({ ...side, seed: seedOf.get(side.id) || 0, place: { slot } });
      });
      if (entrants.length) groups.push({ label: firstRoundMatchLabel(bracket, mi).replace(/^first-round/, 'First-round'), entrants });
    });
  } else {
    (pools || []).forEach((p) => {
      const entrants = [];
      (p.players || []).forEach((pl, i) => {
        if (pl && pl.id) entrants.push({ ...pl, place: { pool: p.poolName, position: i + 1 } });
      });
      if (entrants.length) groups.push({ label: poolLabel(p.poolName), entrants });
    });
  }
  return groups;
}

// meetingRoundName: the round of a bracket meeting, by the one owner of a
// round's name (bracketRoundLabel), from the meeting's effective round.
export function meetingRoundName(meeting) {
  if (!meeting) return '';
  return bracketRoundLabel({ displayRound: meeting.displayRound }, meeting.round, meeting.rounds);
}

// meetingMatchName: a meeting as the bracket card names it, the round and
// then the match number the card prints: "Semifinals (M5)". The round alone
// when the meeting carries no match number.
export function meetingMatchName(meeting) {
  if (!meeting) return '';
  const round = meetingRoundName(meeting);
  return meeting.matchNumber ? `${round} (M${meeting.matchNumber})` : round;
}

// bothWinRound: the round two pool winners meet in, when that round is not
// the one of their earliest meeting ("Final" for a pair who can first meet in
// the Semifinals); empty when the two are the same round, or the pair shares
// a pool, cannot meet, or comes from a knockout-only draw. The round's NAME
// decides: another match in the same round is not a second meeting to name.
export function bothWinRound(row) {
  if (!row || row.samePool || !row.earliest || !row.ifBothWin) return '';
  const round = meetingRoundName(row.ifBothWin);
  return round !== meetingRoundName(row.earliest) ? round : '';
}

// meetingChange: how a pair of dojo-mates' first meeting moves, in words,
// the meeting before, then after the swap: "could first meet in the
// Quarterfinals (M2); now not before the Semifinals (M5)", "shared Pool B; now
// not before the Semifinals (M5), or the Final if both win their pools" or
// "could first meet in the Semifinals (M5), or the Final if both win their
// pools; now the Semifinals (M6) either way". Words, never an arrow between
// the two (Hallmark gate 63). `later` is true when the meeting moves later,
// as separationChanges and the suggestions judge it.
export function meetingChange(before, after, later) {
  return `${meetingWas(before)}; ${meetingNow(before, after, later)}`;
}

function meetingWas(row) {
  if (row && row.samePool) return `shared ${poolLabel(row.samePool)}`;
  if (row && row.earliest) {
    const both = bothWinRound(row);
    return `could first meet in the ${meetingMatchName(row.earliest)}${both ? `, or the ${both} if both win their pools` : ''}`;
  }
  return 'could not meet in the knockout';
}

// meetingNow: the meeting after the swap. A new earliest round says which way
// it moved ("not before", "as early as"); the same round names the new match
// alone ("now the Semifinals (M6)"). The pool winners' round follows when it
// differs from the earliest, and "either way" says that a difference the
// meeting had before is gone.
function meetingNow(before, after, later) {
  if (after && after.samePool) return `now share ${poolLabel(after.samePool)}`;
  if (!after || !after.earliest) return 'now cannot meet in the knockout';
  const sameRound = !!(before && !before.samePool && before.earliest)
    && meetingRoundName(before.earliest) === meetingRoundName(after.earliest);
  const where = sameRound ? `now the ${meetingMatchName(after.earliest)}` : `now ${later ? 'not before' : 'as early as'} the ${meetingMatchName(after.earliest)}`;
  const both = bothWinRound(after);
  if (both) return `${where}, or the ${both} if both win their pools`;
  return bothWinRound(before) ? `${where} either way` : where;
}

// fromEnd: how many rounds before the final a meeting is (0 = the final).
function fromEnd(meeting) {
  if (typeof meeting.displayRound === 'number' && meeting.displayRound > 0) return meeting.displayRound - 1;
  return (meeting.rounds || 0) - 1 - (meeting.round || 0);
}

// meetingLateness: larger is a later first meeting. A shared pool is the
// earliest of all, a pair that cannot meet in the knockout the latest; the
// if-both-win meeting breaks a tie, as the server ranks suggestions.
export function meetingLateness(row) {
  if (!row) return [0, 0];
  if (row.samePool) return [-1000, -1000];
  if (!row.earliest) return [1000, 1000];
  return [-fromEnd(row.earliest), row.ifBothWin ? -fromEnd(row.ifBothWin) : -fromEnd(row.earliest)];
}

function compareLateness(a, b) {
  const x = meetingLateness(a);
  const y = meetingLateness(b);
  return x[0] - y[0] || x[1] - y[1];
}

const pairKey = (row) => [row.a && row.a.id, row.b && row.b.id].sort().join('\u001f');

// separationChanges: the dojo separation rows a swap touches, before and
// after (the answer's `separation`), as one entry per dojo, in the order the
// dojos first appear: `changes` lists each pair whose first meeting moves
// earlier or later (`later` true when it moves later), empty when the dojo's
// pairs all meet when they did. A move to another match at the same depth,
// with the pool winners' meeting just as deep (or to share another pool), is
// not a change of when they can meet, so it is no change. A pair is keyed by
// its two ids, which a swap never changes.
export function separationChanges(separation) {
  const before = (separation && separation.before) || [];
  const after = (separation && separation.after) || [];
  const dojos = [];
  const byDojo = new Map();
  const dojoOf = (name) => {
    if (!byDojo.has(name)) { byDojo.set(name, { dojo: name, pairs: 0, changes: [] }); dojos.push(byDojo.get(name)); }
    return byDojo.get(name);
  };
  const afterByPair = new Map(after.map((r) => [pairKey(r), r]));
  for (const row of before) {
    const d = dojoOf(row.dojo);
    d.pairs += 1;
    const next = afterByPair.get(pairKey(row));
    const moved = next ? compareLateness(next, row) : 0;
    if (moved !== 0) d.changes.push({ a: row.a, b: row.b, before: row, after: next, later: moved > 0 });
  }
  for (const row of after) {
    if (!before.some((r) => pairKey(r) === pairKey(row))) dojoOf(row.dojo).pairs += 1;
  }
  return dojos;
}

// separationSentences: separationChanges in words, one line per dojo:
// "Team Omega: shared Pool B; now not before the Semifinals (M5)." or "Team
// Kappa: no change." A dojo of more than one pair names the pair.
export function separationSentences(separation) {
  const lines = [];
  for (const d of separationChanges(separation)) {
    if (d.changes.length === 0) { lines.push({ dojo: d.dojo, text: `${d.dojo}: no change.`, earlier: false }); continue; }
    for (const ch of d.changes) {
      const who = d.pairs > 1 ? ` (${ch.a.name} and ${ch.b.name})` : '';
      lines.push({
        dojo: d.dojo,
        text: `${d.dojo}${who}: ${meetingChange(ch.before, ch.after, ch.later)}.`,
        earlier: !ch.later,
      });
    }
  }
  return lines;
}

const plural = (n, one, many) => (n === 1 ? one : many);

// swapVerdict: the one line the check opens with, counted from the pairs
// whose first meeting the swap brings earlier: "2 pairs would share a pool
// and 2 would meet earlier.", either part alone, or "No dojo-mates meet
// earlier." `worse` is true when any pair would, and the check then warns.
export function swapVerdict(separation) {
  let pool = 0;
  let earlier = 0;
  for (const d of separationChanges(separation)) {
    for (const ch of d.changes) {
      if (ch.later) continue;
      if (ch.after && ch.after.samePool) pool += 1;
      else earlier += 1;
    }
  }
  if (pool === 0 && earlier === 0) return { worse: false, text: 'No dojo-mates meet earlier.' };
  const pairs = (n) => `${n} ${plural(n, 'pair', 'pairs')}`;
  const parts = [];
  if (pool > 0) parts.push(`${pairs(pool)} would share a pool`);
  if (earlier > 0) parts.push(`${pool > 0 ? earlier : pairs(earlier)} would meet earlier`);
  return { worse: true, text: `${parts.join(' and ')}.` };
}

// movesSentence: "IMS4 Arthur Dick goes to Pool C, IMS8 Lena Park to Pool
// B.", each competitor named as the check's headline names them, by the
// number they hold now (read off `people`, the two picked competitors, by
// id). A swap inside one pool names the places, since only the bout order
// changes.
export function movesSentence(moves, bracket, people) {
  const list = moves || [];
  if (list.length === 0) return '';
  const numberOf = new Map((people || []).filter((p) => p && p.id).map((p) => [p.id, p.number]));
  const who = (m) => competitorLabel({ name: m.name, number: numberOf.get(m.id) });
  const samePool = list.length === 2 && list[0].to.pool && list[0].to.pool === list[1].to.pool;
  const where = (m) => drawPlaceLabel(m.to, bracket, { withPosition: samePool });
  const [first, ...rest] = list;
  return `${who(first)} goes to ${where(first)}${rest.map((m) => `, ${who(m)} to ${where(m)}`).join('')}.`;
}

// numbersSentence (FR4): a number belongs to its place, so the two exchange
// numbers, and their tags with them. Empty when the competition does not number.
export function numbersSentence(numbers) {
  const list = (numbers || []).filter((n) => n.to);
  if (list.length === 0) return '';
  return `Numbers stay with the place: ${list.map((n) => `${n.name} takes ${n.to}`).join(', ')}. Swap their tags.`;
}

// swapDetailLines: everything else the answer says, one sentence each: the
// seed warning (FR5), the rebuilt bracket and its new times, the lineups that
// move or go (FR6), and the byes the swap hands over.
export function swapDetailLines(answer, bracket) {
  const lines = [];
  for (const w of answer.seededWarning || []) {
    lines.push({ kind: 'warn', text: `${w.name} is seed ${w.seed} and moves to ${drawPlaceLabel(w.to, bracket)}.` });
  }
  if (answer.bracketRebuilt) lines.push({ kind: 'warn', text: 'The knockout bracket is rebuilt from the new pools.' });
  if (answer.timesRescheduled) lines.push({ kind: 'warn', text: 'The knockout matches are scheduled again, so their times and shiaijo can change.' });
  if (answer.lineupsMoved > 0) {
    lines.push({ kind: 'info', text: `${answer.lineupsMoved} saved ${plural(answer.lineupsMoved, 'lineup moves', 'lineups move')} with ${plural(answer.lineupsMoved, 'its team', 'their teams')}.` });
  }
  if (answer.lineupsRemoved > 0) {
    lines.push({ kind: 'warn', text: `${answer.lineupsRemoved} saved ${plural(answer.lineupsRemoved, 'lineup has', 'lineups have')} no match at the new place and ${plural(answer.lineupsRemoved, 'is', 'are')} removed.` });
  }
  for (const b of answer.byes || []) {
    lines.push({ kind: 'info', text: b.bye ? `${b.name} now has a bye in the first round.` : `${b.name} no longer has a bye in the first round.` });
  }
  return lines;
}

// swapDoneToast: what the host says once a swap is answered: the two
// competitors swapped, with the lineups moved or removed for a team swap; a
// resend of a swap that already landed says so instead.
export function swapDoneToast(answer, a, b) {
  if (answer && answer.reason === 'already_swapped') return 'That swap was already made. The draw shows it now.';
  const names = [a && a.name, b && b.name].filter(Boolean).join(' and ');
  let text = names ? `Swapped ${names}.` : 'Swapped.';
  if (answer && answer.lineupsMoved > 0) text += ` ${answer.lineupsMoved} ${plural(answer.lineupsMoved, 'lineup', 'lineups')} moved.`;
  if (answer && answer.lineupsRemoved > 0) text += ` ${answer.lineupsRemoved} ${plural(answer.lineupsRemoved, 'lineup', 'lineups')} removed.`;
  return text;
}

// swapBody: the request naming the two competitors and where the page saw them.
export function swapBody(a, b, dryRun) {
  const body = { a: { id: a.id, place: a.place }, b: { id: b.id, place: b.place } };
  if (dryRun) body.dryRun = true;
  return body;
}

/**
 * DrawSwapCheck: the "If you swap them" panel.
 *   comp      the competition ({id, ...})
 *   bracket   its bracket, to name a knockout place by its match number
 *   a, b      the two competitors ({id, name, number?, place})
 *   onClose()                   Cancel, Escape or a tap outside
 *   onSwapped(answer)           the swap is in (applied, or already_swapped)
 *   onDrawChanged(message)      the draw moved under the page (draw_changed,
 *                               draw_started, no_draw): the host reads it again
 */
export function DrawSwapCheck({ comp, bracket, a, b, password, onClose, onSwapped, onDrawChanged }) {
  const [check, setCheck] = useState(null);
  const [error, setError] = useState(null);
  const [sending, setSending] = useState(false);
  const boxRef = useRef(null);
  const aliveRef = useRef(true);
  useEffect(() => () => { aliveRef.current = false; }, []);
  const { openedRef, onClickCapture } = useOpenedTapGuard();
  useDialogFocus(boxRef, true);
  useEscapeToClose(sending ? undefined : onClose);

  const refused = (e) => {
    if (e && DRAW_MOVED_CODES.has(e.code)) { onDrawChanged(e.message); return; }
    setError((e && e.message) || 'The swap could not be checked.');
  };

  const refusedRef = useLatestRef(refused);
  // The two competitors and their places name the check; a new pair is a new one.
  const dryRunKey = JSON.stringify(swapBody(a, b, true));
  useEffect(() => {
    setCheck(null);
    setError(null);
    window.API.swapDrawPlaces(comp.id, JSON.parse(dryRunKey), password)
      .then((answer) => { if (aliveRef.current) setCheck(answer); })
      .catch((e) => { if (aliveRef.current) refusedRef.current(e); });
  }, [comp.id, dryRunKey, password, refusedRef]);

  const swapNow = async () => {
    setSending(true);
    setError(null);
    try {
      const answer = await window.API.swapDrawPlaces(comp.id, swapBody(a, b, false), password);
      if (!aliveRef.current) return;
      setSending(false);
      onSwapped(answer);
    } catch (e) {
      if (!aliveRef.current) return;
      setSending(false);
      refused(e);
    }
  };

  const separation = check ? separationSentences(check.separation) : [];
  const verdict = check ? swapVerdict(check.separation) : null;
  const numbers = check ? numbersSentence(check.numbers) : '';
  const details = check ? swapDetailLines(check, bracket) : [];

  return (
    <div className="modal-backdrop" ref={openedRef} onClickCapture={onClickCapture} onClick={() => { if (!sending) onClose(); }}>
      <div className="shiaijo-move-confirm draw-swap-check" role="dialog" aria-modal="true" ref={boxRef}
        aria-labelledby="draw-swap-check-title" data-testid="draw-swap-check" onClick={(e) => e.stopPropagation()}>
        <h3 id="draw-swap-check-title" className="shiaijo-move-confirm__title">If you swap them</h3>
        <p className="shiaijo-move-confirm__body">
          {competitorLabel(a)} and {competitorLabel(b)} trade places in the draw.
        </p>
        {!check && !error && <p className="shiaijo-move-confirm__body" role="status">Working out what the swap does…</p>}
        {check && (
          <div className="draw-swap-check__answer" aria-live="polite">
            <p className={`draw-swap-check__line${verdict.worse ? ' draw-swap-check__line--warn' : ''}`} data-testid="swap-verdict">{verdict.text}</p>
            <p className="draw-swap-check__line" data-testid="swap-moves">{movesSentence(check.moves, bracket, [a, b])}</p>
            {separation.length > 0 && (
              <ul className="draw-swap-check__list" data-testid="swap-separation">
                {separation.map((l) => (
                  <li key={l.text} className={l.earlier ? 'draw-swap-check__earlier' : undefined}>{l.text}</li>
                ))}
              </ul>
            )}
            {numbers && <p className="draw-swap-check__line" data-testid="swap-numbers">{numbers}</p>}
            {details.map((l) => (
              <p key={l.text} className={`draw-swap-check__line${l.kind === 'warn' ? ' draw-swap-check__line--warn' : ''}`}>{l.text}</p>
            ))}
            <p className="draw-swap-check__line">The public pages update at once.</p>
          </div>
        )}
        {error && <div className="alert alert--error" role="alert">{error}</div>}
        <div className="shiaijo-move-confirm__actions">
          <button type="button" className="btn" onClick={onClose} disabled={sending}>Cancel</button>
          <button type="button" className={`btn ${verdict && verdict.worse ? 'btn--warn' : 'btn--primary'}`} onClick={swapNow}
            disabled={!check || sending} data-testid="swap-them">
            {sending && <span className="spinner" />}
            {sending ? 'Swapping…' : verdict && verdict.worse ? 'Swap anyway' : 'Swap them'}
          </button>
        </div>
      </div>
    </div>
  );
}
