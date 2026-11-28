// admin_swap_candidates.jsx: the marks of the Swap competitors mode (bc-drev
// K4). Once the organiser picks a first competitor on Pools (preview) or
// Bracket (preview), every other unseeded competitor is marked by what swapping
// the two would do to dojo-mates, each dojo judged as a whole: green
// "better" keeps a dojo apart for longer and leaves none worse off, amber
// "worse" leaves a dojo worse off (earlier, or into one pool), and a plain
// pick changes no dojo. The server judges each candidate (GET
// .../draw/swap-candidates, by the same rule as the swap check's before and
// after), this module only reads it and words it, and useSwapCompetitors
// (admin_competition_bracket.jsx) is its one caller, for both tabs.
//
// A mark is advice, never a gate: the swap itself still goes through "If you
// swap them", so a read that fails shows no marks and the swap works as
// before. A mark's line ("Worse for Fudo, Hizen", operator decision C9: the
// dojos the swap harms or helps, never the candidate's own) is drawn under
// the dojo from the moment the mode is on, and the banner's key line from the
// pick on, each blank until it shows, so the marks arriving move nothing
// under the finger.

import { DRAW_MOVED_CODES } from './admin_draw_swap_check.jsx';
import { useLatestRef } from './latest_ref.jsx';

const { useState, useEffect } = React;

// The refusals that mean the draw on screen is no longer the stored one, as
// for the swap, plus not_in_draw: the draw no longer holds the pick.
const CANDIDATES_DRAW_MOVED = new Set([...DRAW_MOVED_CODES, 'not_in_draw']);

// The word a mark opens with beside its colour. "same" carries none.
const VERDICT_WORDS = { better: 'Better', worse: 'Worse' };

// candidateMarks: the server's answer for one pick as two maps from each
// candidate's id: `marks`, its mark ("better" or "worse"; "same" and anything
// else leave the candidate plain), and `dojos`, the dojos that mark is about
// (the answer's `dojos`, worse off for worse, better off for better). An
// answer about another pick is no answer.
export function candidateMarks(answer, pickId) {
  const marks = new Map();
  const dojos = new Map();
  if (!answer || !pickId || (answer.pick && answer.pick.id && answer.pick.id !== pickId)) return { marks, dojos };
  for (const cand of answer.candidates || []) {
    if (!cand || !cand.id || cand.id === pickId || !VERDICT_WORDS[cand.verdict]) continue;
    marks.set(cand.id, cand.verdict);
    dojos.set(cand.id, cand.dojos);
  }
  return { marks, dojos };
}

// candidateVerdicts: candidateMarks' marks alone.
export function candidateVerdicts(answer, pickId) {
  return candidateMarks(answer, pickId).marks;
}

// markText: a mark in words, naming the dojos it is about: "Worse for Fudo,
// Hizen", "Better for Kenyu", or '' for no mark. The server names at least
// one dojo for every better or worse mark (judgeDojos).
export function markText(mark, dojos) {
  const word = VERDICT_WORDS[mark];
  if (!word) return '';
  return `${word} for ${dojos.join(', ')}`;
}

// MARK_KEYS: every sentence the banner's key line can say: that the marks
// are on their way while the read is out, then which colours show and what
// they mean, that no swap changes anything, or that the swaps could not be
// marked (the first and the last two quiet).
export const MARK_KEYS = {
  loading: { text: 'Marking the swaps…', quiet: true },
  both: { text: 'Green keeps dojo-mates apart longer, amber brings some closer.', quiet: false },
  better: { text: 'Green keeps dojo-mates apart longer.', quiet: false },
  worse: { text: 'No swap here helps. Amber brings dojo-mates closer.', quiet: false },
  same: { text: 'No swap changes when dojo-mates can meet.', quiet: true },
  failed: { text: 'The swaps could not be marked.', quiet: true },
};

// marksKey: which of MARK_KEYS the key line shows for the marks as read, or
// 'loading' while the read is out.
export function marksKey({ marks, failed }) {
  if (failed) return 'failed';
  if (!marks) return 'loading';
  const vs = [...marks.values()];
  const better = vs.includes('better');
  const worse = vs.includes('worse');
  if (better && worse) return 'both';
  if (better) return 'better';
  return worse ? 'worse' : 'same';
}

// SwapMarksKey: the banner's key line, drawn from the pick on with EVERY
// sentence of MARK_KEYS stacked in one grid cell and only the current one
// visible ('loading' while the read is out; none while the swap check is
// open), so the line holds the height of its longest sentence from the pick
// on and nothing below it moves when the read answers, fails, or finds no
// swap that changes anything. Every sentence fits two lines at 375 px
// (operator decision C7), so the line never holds more than two.
// The stacked sentences are for the eye alone, all hidden from a screen
// reader, since a sentence shown by visibility is not announced: a visually
// hidden live node, always drawn, holds the current sentence's text and
// nothing else, so each change of it is announced.
export function SwapMarksKey({ current }) {
  const said = (current && MARK_KEYS[current]) ? MARK_KEYS[current].text : '';
  return (
    <div className="swap-mode__key" data-testid="swap-marks-key">
      {Object.entries(MARK_KEYS).map(([key, line]) => (
        <span key={key} data-key={key} aria-hidden="true"
          className={`swap-mode__key-line${line.quiet ? ' swap-mode__key-line--quiet' : ''}${key === current ? ' is-shown' : ''}`}>
          {line.text}
        </span>
      ))}
      <span className="sr-only" aria-live="polite" data-testid="swap-marks-key-said">{said}</span>
    </div>
  );
}

const EMPTY = { pickId: null, drawKey: null, marks: null, dojos: null, failed: false };

// useSwapCandidates: the marks for the competitor picked first, read when the
// pick changes or the draw changes (`drawKey`, where every competitor of the
// draw the host shows stands: a swap made on another device keeps the pick
// but moves places, so the old marks are dropped until the new read answers,
// while a re-read of the same draw keeps them). The answer of a read is kept only while its pick and draw are
// still the ones on screen (a newer pick, a cancel or a swap drops it), and
// a draw that moved under the page goes to onDrawChanged, the swap check's
// own path.
export function useSwapCandidates({ compId, pickId, drawKey = null, password, onDrawChanged }) {
  const [state, setState] = useState(EMPTY);
  const changedRef = useLatestRef(onDrawChanged);
  useEffect(() => {
    setState(EMPTY);
    if (!pickId) return undefined;
    let live = true;
    Promise.resolve().then(() => window.API.fetchSwapCandidates(compId, pickId, password))
      .then((answer) => { if (live) setState({ pickId, drawKey, ...candidateMarks(answer, pickId), failed: false }); })
      .catch((e) => {
        if (!live) return;
        if (e && CANDIDATES_DRAW_MOVED.has(e.code)) { changedRef.current(e.message); return; }
        setState({ pickId, drawKey, marks: null, dojos: null, failed: true });
      });
    return () => { live = false; };
  }, [compId, pickId, drawKey, password, changedRef]);
  const current = pickId && state.pickId === pickId && state.drawKey === drawKey;
  return current ? { marks: state.marks, dojos: state.dojos, failed: state.failed } : { marks: null, dojos: null, failed: false };
}

// swapLocked: is this entrant of the swap mode a seed, who keeps their place
// in the draw (operator ruling 2026-11-27)? A seed is drawn grey and is not a
// button; the server refuses a swap naming one (seeded_competitor) and never
// offers one as a candidate.
export function swapLocked(entrant) {
  return !!entrant && entrant.seed > 0;
}

// lockedText: a seed's line in the mark's place: "Seed 1, stays in place".
export function lockedText(seed) {
  return `Seed ${seed}, stays in place`;
}

// SwapLockedWord: a seed's line, in the place SwapVerdictWord holds on every
// other competitor, so the seed reads as deliberately left out and the rows
// keep one height.
export function SwapLockedWord({ seed }) {
  return (
    <span className="swap-verdict swap-verdict--locked" data-testid="swap-verdict-word">{lockedText(seed)}</span>
  );
}

// SwapVerdictWord: a candidate's mark in words (markText), on a line of its
// own under the dojo, so a long list of dojos never pushes the name. The line
// is drawn on every pick from the moment the mode is on, blank (a no-break
// space, hidden from a screen reader) until a mark shows, so the space it
// takes is the same before, during and after the marks.
export function SwapVerdictWord({ mark, dojos }) {
  const text = markText(mark, dojos);
  return (
    <span className={`swap-verdict${text ? ` swap-verdict--${mark}` : ''}`} data-testid="swap-verdict-word"
      aria-hidden={text ? undefined : 'true'}>
      {text || '\u00A0'}
    </span>
  );
}
