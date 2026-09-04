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
//
// NOTE ON ARIA. The three notices carry no live-region role. They describe a
// condition that PERSISTS until someone repairs a file, and they sit in reading
// order beside the rows they are about. Every other `role="status"` in this app
// is on TRANSIENT state -- the pending-write banner, the reconnect pill, the
// loading page -- which is what the role is for; and a row-level note announced
// away from its row ("this encounter's bouts could not be read") names no
// encounter, so it tells a screen-reader user less than silence does. The
// competition banner below is the one exception and keeps `role="alert"`: it
// appears to say scoring is blocked, which is worth interrupting for.

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
    <div className="alert alert--warn data-issue data-issue--inline">
      <span aria-hidden="true">⚠</span>
      <span>
        This encounter's individual bouts could not be read from the results file, so
        it counts zero individual victories and zero points. Repair the cell, or
        re-enter the bouts here.
      </span>
    </div>
  );
}

// UnreadableEditorNote: the same fact, said to the one person who can act on
// it immediately.
//
// The other notices explain a consequence to someone reading standings. This
// one is read by an operator who has just opened the encounter and is looking
// at empty bout rows, so it answers their actual question ("did this never get
// scored?") and names the repair they are already one step away from. Saving
// from here writes real sub-bouts, which supersedes the retained cell and
// clears the warning everywhere.
export function UnreadableEditorNote() {
  return (
    <div className="alert alert--warn data-issue data-issue--editor">
      <span aria-hidden="true">⚠</span>
      <span>
        The bouts recorded for this encounter could not be read from the results
        file, so the rows below start empty. They are not lost: the unreadable
        text is still in the file. Entering the bouts here replaces it and clears
        this warning.
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
    <div className="alert alert--warn data-issue">
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

// bracketRecoveryKind: what a corrupt bracket.json can actually be recovered
// with, which is not one question but two, and the answer decides every word
// below as well as whether a button is offered.
//
//   "rebuild"  MIXED only. Its knockout is drawn from pools.csv, which is still
//              readable, so the tree can be laid out again by the same builder
//              and the finished pools re-seeded into it.
//   "discard"  LEAGUE and SWISS. They never draw a bracket, so any bracket.json
//              they hold is left over and unused. Moving it aside unblocks the
//              competition and costs nothing, because nothing was in it.
//   "none"     Everything else, INCLUDING an unrecognised or missing format.
//              Those take the draw pipeline's default branch and get a
//              standalone knockout bracket, so the file IS the draw. Rebuilding
//              it from today's roster would not restore the tournament, it would
//              invent a different one that disagrees with the bracket already
//              printed and posted on the wall. The server refuses this; the
//              console does not offer it, because an action that can only fail
//              is worse than an explanation.
//
// Mirrors engine.CompetitionDrawsBracket + engine.CompetitionRebuildableFromPools
// and is pinned against the same shared table they are,
// internal/engine/testdata/format_draws_bracket.json, so a new format cannot be
// added without answering this here too. Do NOT hand-extend the list: the
// unrecognised-format case is exactly the half a hand-written check gets wrong,
// and it cost this function its first version.
export const BRACKET_RECOVERY_REBUILD = "rebuild";
export const BRACKET_RECOVERY_DISCARD = "discard";
export const BRACKET_RECOVERY_NONE = "none";

export function bracketRecoveryKind(competition) {
  const format = (competition && competition.format) || "";
  if (format === "mixed") return BRACKET_RECOVERY_REBUILD;
  if (format === "league" || format === "swiss") return BRACKET_RECOVERY_DISCARD;
  return BRACKET_RECOVERY_NONE;
}

// The banner button, the confirm dialog and the success toast are the same voice
// as the banner and answer to the same two questions, so they are written here
// rather than at the call site.
//
// buttonLabel and confirmLabel are deliberately NOT the same string. The banner
// CTA names the whole action because it stands alone on the page; the dialog's
// label sits under a sentence that has just named it, where repeating the
// article reads clumsily. Sharing one string collapsed the banner CTA to the
// dialog's register.
//
// The toast takes the server's own `rebuilt`, not the kind we predicted: the
// server decides, and a message that guessed would eventually tell an operator a
// stage was rebuilt when it was not.
export function bracketResetPrompt(kind, name) {
  if (kind === BRACKET_RECOVERY_DISCARD) {
    return {
      message: `Move the unreadable bracket file for "${name}" aside? This competition has no `
        + `knockout stage, so the file is left over and unused. It is kept, renamed aside, and `
        + `no results are affected.`,
      buttonLabel: "Move the file aside",
      busyLabel: "Moving…",
      confirmLabel: "Move the file aside",
    };
  }
  return {
    message: `Reset the knockout stage for "${name}"? The unreadable file is kept, renamed aside, `
      + `but the knockout results inside it will no longer be used and must be re-entered.`,
    buttonLabel: "Reset the knockout stage",
    busyLabel: "Resetting…",
    confirmLabel: "Reset knockout stage",
  };
}

export function bracketResetToast(quarantinedAs, rebuilt) {
  return rebuilt
    ? `Knockout stage reset. The unreadable file is kept as ${quarantinedAs}`
    : `The unreadable file is kept as ${quarantinedAs}. Nothing was rebuilt: this competition `
      + `has no knockout stage.`;
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
  // Only ask the format question when the bracket is the broken file: a corrupt
  // pool-matches.csv has no reset, whatever the format.
  const kind = bracket ? bracketRecoveryKind(competition) : BRACKET_RECOVERY_NONE;

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
        {bracket && kind === BRACKET_RECOVERY_NONE ? (
          <p>
            There is no reset for this competition: its bracket was drawn
            directly, so this file is the only record of who was drawn against
            whom. Rebuilding it would produce a different set of pairings rather
            than restoring these, and it would then disagree with the bracket you
            have printed. Repair the file.
          </p>
        ) : null}
        {kind === BRACKET_RECOVERY_DISCARD ? (
          <>
            <p>
              <strong>Or move the file aside</strong>, if it cannot be repaired. This
              competition has no knockout stage, so this file is left over and unused:
            </p>
            <ul className="data-issue__list">
              <li>The unreadable file is kept, renamed aside. It is never deleted.</li>
              <li>Nothing is rebuilt, because there is no knockout stage to rebuild.</li>
              <li>Participants, standings and every result you have recorded are untouched.</li>
            </ul>
          </>
        ) : null}
        {kind === BRACKET_RECOVERY_REBUILD ? (
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
          </>
        ) : null}
        {/* One button for both recoverable kinds: they run the same endpoint and
            differ only in what it will find to do, so the label follows the kind
            rather than each branch growing its own copy of the control. */}
        {kind !== BRACKET_RECOVERY_NONE ? (
          <button
            type="button"
            className="btn btn--danger"
            onClick={onReset}
            disabled={!!resetting}
          >
            {resetting ? bracketResetPrompt(kind).busyLabel : bracketResetPrompt(kind).buttonLabel}
          </button>
        ) : null}
      </div>
    </div>
  );
}
