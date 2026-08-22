// data_integrity.jsx: what the operator is told when a file on disk does not
// say what the app expects, and where that text is decided.
//
// The tool invites an organiser to open its files in a spreadsheet or a text
// editor and correct a wrong cell. Two things can go wrong with that, and they
// are NOT the same failure, so they get separate words here:
//
//   QUIET  a single cell will not parse. The row still loads, the cell degrades
//          to its documented default, and the competition keeps running. Only
//          a team encounter's sub-bout cell carries enough to be worth naming:
//          losing it zeroes that encounter's individual victories and points,
//          which is what decides a tied pool. Per match, via
//          `subResultsUnreadable` on the match.
//
//   LOUD   the file will not parse at all. The load fails, every write to it is
//          refused, and the competition is stuck until it is repaired or moved
//          aside. Per competition, via `dataIssues` on the aggregate.
//
// One module because the two share an audience and a voice, and because the
// three surfaces that show them (the pool match list, the pool standings, the
// competition overview) must not each invent their own wording for the same
// state -- the way to keep a display rule consistent here is to give it one
// owner, not to repeat a literal in three files.
//
// NOTE ON AUDIENCE. Both flags travel on the PUBLIC wire, because the admin
// console reads the same aggregate and the same SSE stream the viewer does, so
// the server cannot tell the two apart on that path. The gate is here instead:
// every consumer takes an explicit opt-in, and only the admin surfaces pass it.
// A spectator has nothing to do with a parse error, so they are never shown one.

// matchDataUnreadable: the ONE test for "this match lost its bouts to a cell
// that would not parse". Everything else asks this rather than reading the
// field, so a rename or a change of shape lands in one place.
export function matchDataUnreadable(m) {
  return !!(m && m.subResultsUnreadable);
}

// unreadableMatches filters a match list to the affected ones, for the
// pool-level and competition-level summaries.
export function unreadableMatches(matches) {
  return (matches || []).filter(matchDataUnreadable);
}

// dataIssueText renders one located file failure as a single line an operator
// can act on: which file, which line, and the parser's own description of what
// it found. Position is omitted when the parser could not place the fault
// rather than being printed as "line 0".
export function dataIssueText(issue) {
  if (!issue || !issue.file) return "";
  const where = issue.line > 0 ? `line ${issue.line}, column ${issue.column}` : "";
  return [issue.file, where].filter(Boolean).join(", ") + (issue.detail ? `: ${issue.detail}` : "");
}

// UnreadableBoutsNote: the per-match line. Deliberately says what was lost and
// what it affects, not just that something is wrong -- "could not be read" on
// its own does not tell an operator that their standings have moved.
export function UnreadableBoutsNote() {
  return (
    <div className="alert alert--warn data-issue data-issue--inline" role="status">
      <span aria-hidden="true">⚠</span>
      <span>
        This encounter's individual bouts could not be read from the results file, so
        it counts zero individual victories and zero points. Repair the cell, or
        re-enter the bouts here.
      </span>
    </div>
  );
}

// UnreadablePoolNote: the per-pool line, shown above the standings the missing
// bouts distort. Named for the consequence rather than the cause, because the
// consequence is what an operator has to decide about: these are the figures a
// tied pool is separated on.
export function UnreadablePoolNote({ count }) {
  return (
    <div className="alert alert--warn data-issue" role="status">
      <span aria-hidden="true">⚠</span>
      <span>
        {count === 1
          ? "One encounter in this pool is missing its individual bouts"
          : `${count} encounters in this pool are missing their individual bouts`}
        , so the IV and PW columns below are incomplete. A pool tied on wins is
        separated on exactly those figures.
      </span>
    </div>
  );
}

// bracketIssue: the located failure for bracket.json, if that is one of them.
// The recovery action only exists for the bracket, so the banner asks this
// rather than assuming the first issue is the interesting one.
export function bracketIssue(issues) {
  return (issues || []).find((i) => i && i.file === "bracket.json") || null;
}

// canRebuildBracket: whether a corrupt bracket can be rebuilt at all.
//
// A pool-fed competition keeps its DRAW in pools.csv, so the knockout structure
// can be derived again from a file that is still readable. A direct-elimination
// competition does not: there, bracket.json IS the draw, and rebuilding it from
// today's roster would not restore the tournament, it would invent a different
// one that disagrees with the bracket already printed and posted on the wall.
// The server refuses that case; this keeps the button from being offered at all,
// because an action that can only fail is worse than an explanation.
export function canRebuildBracket(competition) {
  const format = competition && competition.format;
  return format === "mixed" || format === "league" || format === "swiss";
}

// DataIssueBanner: the competition-level notice for the LOUD class, where a
// whole file will not parse and every write to it is refused.
//
// It states the consequences in the banner rather than hiding them behind the
// confirm dialog, because they are what the operator is choosing between and
// the dialog is a last gate, not a briefing.
export function DataIssueBanner({ issues, competition, onReset, resetting }) {
  const list = issues || [];
  if (list.length === 0) return null;
  const bracket = bracketIssue(list);
  const rebuildable = bracket && canRebuildBracket(competition);

  return (
    <div className="alert alert--error data-issue data-issue--banner" role="alert">
      <span aria-hidden="true">⚠</span>
      <div>
        <strong>A file for this competition could not be read.</strong>
        <ul className="data-issue__list">
          {list.map((i) => (
            <li key={i.file}><code>{dataIssueText(i)}</code></li>
          ))}
        </ul>
        <p>
          Scoring is blocked for whatever that file holds. Nothing has been
          overwritten: every write to a file that will not parse is refused, so
          it is still exactly as it was last saved.
        </p>
        <p><strong>Repair it, and this clears on its own.</strong> Open the file, fix the
          position above, and reload this page. Everything recorded in it comes back,
          results included. This is the option that loses nothing.</p>
        {bracket && !rebuildable ? (
          <p>
            There is no reset for this competition: it is a direct-elimination
            draw, so this file is the only record of who was drawn against whom.
            Rebuilding it would produce a different set of pairings rather than
            restoring these, and it would then disagree with the bracket you have
            printed. Repair the file.
          </p>
        ) : null}
        {rebuildable ? (
          <>
            <p><strong>Or reset the knockout stage</strong>, if the file cannot be repaired:</p>
            <ul className="data-issue__list">
              <li>The unreadable file is kept, renamed aside. It is never deleted.</li>
              <li>Pools, participants and pool results are untouched.</li>
              <li>Every knockout bout already fought must be re-entered from the score sheets.</li>
              <li>
                Check the rebuilt pairings against your printed bracket. The tree is
                rebuilt with the current draw algorithm, and the original one was
                inside the file that will not parse.
              </li>
            </ul>
            <button
              type="button"
              className="btn btn--danger"
              onClick={onReset}
              disabled={!!resetting}
            >
              {resetting ? "Resetting…" : "Reset the knockout stage"}
            </button>
          </>
        ) : null}
      </div>
    </div>
  );
}
