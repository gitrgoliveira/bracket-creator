// Operator ruling, 2026-09-20 (bc-sccl): the word "Final" names the LAST MATCH
// OF A KNOCKOUT and nothing else. It may not double as a done/not-done marker
// on an ordinary bout, because those matches are not a final.
//
// So the round label STAYS (bracket.jsx owns it) and three match-state badges
// went:
//   admin_schedule_score_editor.jsx  the scores-list row
//   admin_shiaijo.jsx                the court-console queue row
//   viewer_match.jsx                 the public schedule card
// In each case the surface still says the match is done by other means: the
// recorded score is on the row, the action reads "Correct" rather than "Score"
// (admin), and the completed rows sit under a "Completed" / "Recent results"
// heading.
//
// A removed label leaves no failing test behind on its own, which is why this
// file exists, following running_state_static.test.jsx (a removed animation)
// and team_editor_vacancy_not_flagged (a removed warning). It pins the RULE,
// both halves of it, so the sanctioned use cannot be deleted as over-zealous
// cleanup either.
//
// These are SOURCE assertions, not render ones: the three surfaces mount over
// the API or behind a court console, so rendering them here would exercise
// their fetch harnesses rather than the label. What a regression does is put
// the literal back, and that is what these catch.

import { describe, it, expect } from 'vitest';
// readCode strips comments: every removal site explains itself in one, so a raw
// read would match its own explanation. Shared with the sibling suite.
import { readCode as codeOf, retentionRatio, modules, renderedLiteral } from './helpers/source.js';
import { roundLabel } from '../bracket.jsx';


// Every SPA module. Read from disk rather than listed, so a surface added later
// is swept without anyone remembering to add it here.

const SCORES_LIST = 'admin_schedule_score_editor.jsx';
const STATE_BADGE_SURFACES = [SCORES_LIST, 'admin_shiaijo.jsx', 'viewer_match.jsx'];

// The word as the USER would see it: a string literal or a JSX text node
// (allowing whitespace, so a node broken across lines still counts). Matching
// the bare word instead would flag identifiers that merely contain it --
// viewer_match.jsx's own `isFinalized` is exactly that, and it caught this
// assertion on its first run.
const RENDERED_FINAL = renderedLiteral('Final');

describe('"Final" is a knockout round, not a match state', () => {
  for (const file of STATE_BADGE_SURFACES) {
    it(`${file} renders no "Final" literal`, () => {
      expect(codeOf(file)).not.toMatch(RENDERED_FINAL);
    });
  }

  // The other half of the rule: the sanctioned use must survive. Without this,
  // a later sweep for the word would "helpfully" take the round label too.
  //
  // CALLED, not grepped. This used to pin the source line
  // `if (fromEnd === 0) return "Final";` character for character, so a braced
  // if, a ternary or a lookup table -- all neutral refactors that keep the
  // label working -- reddened it, and the message would have sent the next
  // reader hunting a rendering bug that did not exist.
  it('bracket.jsx still names the last knockout round "Final"', () => {
    expect(roundLabel(3, 4)).toBe('Final');
    expect(roundLabel(2, 4)).toBe('Semifinals');
  });

});

// The second ruling of that day, kept in this file because it was decided in
// the same pass and removes the same kind of thing (a badge restating what the
// row already shows), but under its own heading so a failure reads as what it
// is rather than as something about "Final".
//
// A HANTEI BADGE MUST NEVER EXIST, on any surface (operator ruling 2026-09-20,
// absolute). The verdict is an ippon: it rides in the winner's cell as "Ht",
// which every score string already renders. A badge repeating it is a second
// home for one fact, the duplicate the TV header and lobby chips were removed
// for. The last one lived on the viewer card and dated from 2026-05-20, when
// decidedByHantei was a boolean and the row showed nothing about the verdict;
// the 2026-08-21 ippon ruling made it redundant and nobody swept it.
//
// NOT covered, deliberately: the score editors' own "Hantei" control labels and
// tooltips, and match_scoreboard.jsx's WAZA_NAMES gloss for the Ht mark. Those
// name a CONTROL or explain a MARK; the ruling is about a badge restating a
// recorded verdict.
describe('a HANTEI badge must never exist', () => {
  // Swept across EVERY module rather than the one that had it, because "never"
  // is the rule and the next badge would land on a different surface.
  it('no module renders a HANTEI badge', () => {
    const files = modules();
    // A sweep that reads nothing passes vacuously, which would silently retire
    // this guard the day the layout moves. 79 modules today; the floor only has
    // to be high enough that an empty or truncated listing fails loudly.
    expect(files.length, 'the sweep must actually find the modules').toBeGreaterThan(50);
    const offenders = files.filter(f => renderedLiteral('HANTEI').test(codeOf(f)));
    expect(offenders, 'a HANTEI badge may never exist on any surface').toEqual([]);
  });

  // The sweep above guards against reading no FILES. This guards against
  // reading a truncated STRING from each one, which is the failure that
  // actually happened: readCode stripped block comments in a separate earlier
  // pass, so a "/*" inside a // comment opened a phantom block that ran to the
  // next "*/". match_scoreboard.jsx -- the likeliest home for a re-added badge
  // -- kept 16.3% of its bytes, api_client.jsx 25.9%, and the sweep read only
  // that. Both now retain 36.7% and 41.3%.
  //
  // Extended to the three STATE_BADGE_SURFACES files too: the
  // STATE_BADGE_SURFACES loop earlier in this file (the "renders no Final
  // literal" checks) reads each of them individually with no retention floor
  // of their own, so a truncated read there would pass just as vacuously as
  // it did for match_scoreboard.jsx and api_client.jsx before this guard
  // existed. They retain 51.8%, 59.5% and 47.0% today, all comfortably above
  // the 0.30 floor. Still not every module: a small doc-heavy leaf
  // legitimately retains 14% (result_recency.jsx), so a blanket floor would
  // either be too low to catch anything or would fail honest files.
  it('reads most of the badge-sweep modules, including the two the phantom-block bug used to swallow', () => {
    for (const f of [...STATE_BADGE_SURFACES, 'match_scoreboard.jsx', 'api_client.jsx']) {
      expect(retentionRatio(f), `${f} is being truncated before the sweep reads it`)
        .toBeGreaterThan(0.30);
    }
  });
});

describe('the scores row still says a match is done, without the word', () => {
  const code = () => codeOf(SCORES_LIST);

  // "Corrected" is not redundant the way "Final" was: nothing else on the row
  // says a result was re-entered. isCorrection already implies completed.
  it('keeps "Corrected", gated on isCorrection alone', () => {
    const m = code().match(/\{isCorrection && <span[^>]*>([^<]*)<\/span>\}/);
    expect(m, 'the status cell renders on isCorrection alone').not.toBeNull();
    expect(m[1]).toBe('Corrected');
  });

  // The button is now what tells a completed row from an unplayed one, so the
  // removals depend on it: "Correct" only ever appears on a finished match,
  // "Score" on a running or scheduled one.
  //
  // Still a SOURCE pin, not a rendered one: this file runs under vitest's
  // "unit" project (vitest.config.js), whose setup stubs global.React with
  // plain non-DOM objects, so @testing-library/react cannot mount
  // AdminScoreEditorPage here -- only a *.render.test.jsx file under
  // js/__tests__/render/ gets real React. Loosened from the exact ternary
  // expression to formatting-tolerant substring checks, so a neutral rewrite
  // (a lookup table, a helper function) does not redden this test.
  it('leaves the button carrying the state: Correct when completed, Score otherwise', () => {
    const c = code();
    expect(c.includes('"Correct"') && c.includes('"Score"'), 'both button labels must exist in the module').toBe(true);
    expect(c).toMatch(/^(?=.*"completed")(?=.*"Correct").*$/m);
  });
});
