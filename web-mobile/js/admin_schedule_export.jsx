// AdminExport card extracted from admin_schedule.jsx (mp-d7tl).

import { shownMessage } from './write_result.jsx';
import { competitionHasRecordedResult } from './recorded_result.jsx';
import { downloadBlob } from './blob_download.jsx';
import { canShuffleUnseeded } from './competition_shape.jsx';

const { useState } = React;

// Before the draw the server answers the template door with the blank layout
// (engine.blankPlaceCount sizes it: the larger of the expected number and
// everyone registered, checked in or not).
export const PRE_DRAW_NOTICE = "No draw yet: until the draw is generated, only a blank template can be downloaded. Its places are empty (no names, seeds or numbers), as many as the larger of the expected number of participants (set in Settings) and everyone registered.";

// The never-shuffled warning (state.Competition.RosterShuffled unset). The
// draw places competitors in list order, so the order they were entered in
// decides where they are placed; the remedy differs once a draw exists.
export const NOT_SHUFFLED_BEFORE_DRAW = "The competitor list has never been shuffled. The draw places competitors in the order of the list, so the order they were entered in decides who meets whom. Tap Shuffle unseeded on Participants before you generate the draw.";
export const NOT_SHUFFLED_DRAWN = "The competitor list was never shuffled before this draw, so the draw placed competitors in the order they were entered. To change that: discard the draw, tap Shuffle unseeded on Participants, then generate the draw again.";

// notShuffledWarning is the warning's sentence, or null when there is none to
// give: in setup or draw-ready only (once a result exists a new draw is no
// longer the remedy), never for Swiss (no export), and only while a shuffle
// could do something (canShuffleUnseeded).
export function notShuffledWarning({ status, format, rosterShuffled, players }) {
  if (rosterShuffled || format === "swiss") return null;
  if (!canShuffleUnseeded(players)) return null;
  if (!status || status === "setup") return NOT_SHUFFLED_BEFORE_DRAW;
  if (status === "draw-ready") return NOT_SHUFFLED_DRAWN;
  return null;
}

export function AdminExport({ c, t, password, showToast, poolMatches, bracket }) {
  // The competition detail nests its real fields (roster, format, courts,
  // pool settings) under `c.config`; some other call sites pass an already
  // flattened object. Read from `c.config` when present, else from `c`, so
  // this works in both shapes.
  const cfg = c.config || c;
  const cid = c.id || cfg.id;
  const url = `${(window.linkBase || (() => window.location.origin))(t)}/competition/${cid}`;

  // Every status but setup has a stored draw (the negation of state.CanGenerateDraw).
  const status = c.status ?? cfg.status;
  const hasDraw = !!status && status !== "setup";
  // The results workbook is offered once a result is recorded (the engine's
  // result gate, mirrored in recorded_result.jsx): before that it holds nothing
  // the blank template does not.
  const hasResult = competitionHasRecordedResult(poolMatches, bracket);
  const shuffleWarning = notShuffledWarning({
    status,
    format: cfg.format,
    rosterShuffled: c.rosterShuffled ?? cfg.rosterShuffled,
    players: cfg.players || c.players,
  });

  const [resultsBusy, setResultsBusy] = useState(false);
  const [templateBusy, setTemplateBusy] = useState(false);
  // The last template download's failure, said on the tab beside the button
  // (a pre-draw layout that cannot be sized names its fix there), cleared by
  // the next try.
  const [templateError, setTemplateError] = useState("");

  const notify = (msg, kind) => {
    if (showToast) showToast(msg, kind);
    else if (kind === "error") alert(msg);
  };

  // downloadResults pulls the RESULTS-populated workbook (played scores,
  // standings, winners, decisions) from the admin-gated export-results
  // endpoint. Unlike downloadXlsx below (the blank template), this reflects
  // what actually happened in the live app.
  const downloadResults = async () => {
    if (resultsBusy) return;
    setResultsBusy(true);
    try {
      downloadBlob(await window.API.exportResults(cid, password), `results-${cid}.xlsx`);
      notify("Results workbook downloaded.");
    } catch (err) {
      notify(shownMessage(err, "Results export failed: "), "error");
    } finally {
      setResultsBusy(false);
    }
  };

  const downloadXlsx = async () => {
    if (templateBusy) return;
    setTemplateBusy(true);
    setTemplateError("");
    try {
      // Swiss has no static-bracket generator; exporting it as a round-robin
      // pool would hand the operator a structurally wrong workbook. Block it
      // with a clear message instead (live results live in the scoring view).
      if (cfg.format === "swiss") {
        throw new Error("Swiss competitions can't be exported to a static Excel bracket: use the live scoring view to track Swiss rounds.");
      }

      // The server answers with the stored draw, or, before the draw, with the
      // blank layout the draw would print, its places empty.
      const blob = await window.API.exportDrawTemplate(cid, password);
      downloadBlob(blob, `bracket-${cid}.xlsx`);
    } catch (err) {
      // Said on the tab, not in a passing alert, so the fix it names stays in
      // view while the operator goes to make it. A request that got no answer
      // says the one "not sent" sentence (write_result.jsx).
      setTemplateError(shownMessage(err, "Export failed: "));
    } finally {
      setTemplateBusy(false);
    }
  };

  const copyUrl = async () => {
    try {
      await navigator.clipboard.writeText(url);
      alert("URL copied to clipboard!");
    } catch (err) {
      alert("Failed to copy URL: " + (err?.message || err));
    }
  };

  return (
    <div className="row">
      <div className="card">
        <div className="card__title" style={{ marginBottom: 8 }}>Export {c.name}</div>
        <div className="card__sub" style={{ marginBottom: 14 }}>Download this competition as an Excel workbook.</div>
        {!hasDraw && cfg.format !== "swiss" && (
          <div className="alert alert--info" style={{ marginBottom: 14 }} data-testid="export-pre-draw-notice">
            {PRE_DRAW_NOTICE}
          </div>
        )}
        {shuffleWarning && (
          <div className="alert alert--warn" style={{ marginBottom: 14 }} data-testid="export-not-shuffled">
            {shuffleWarning}
          </div>
        )}
        {hasResult && (
          <>
            <button type="button" className="btn btn--primary btn--full" onClick={downloadResults} disabled={resultsBusy}>
              {resultsBusy ? "Preparing…" : "Download results (.xlsx)"}
            </button>
            <div className="field__hint" style={{ marginTop: 8, marginBottom: 14 }}>Pool standings and brackets with the played scores, winners, and decisions (Kiken / Fus. / DH / Ht) filled in.</div>
          </>
        )}
        <button type="button" className="btn btn--full" onClick={downloadXlsx} disabled={templateBusy}>
          {templateBusy ? "Preparing…" : "Download blank template (.xlsx)"}
        </button>
        <div className="field__hint" style={{ marginTop: 8 }}>
          Empty pool draws, matches, and brackets with linked formulas, for scoring by hand.
        </div>
        {templateError && (
          <div className="alert alert--error" role="alert" style={{ marginTop: 8 }} data-testid="export-template-error">
            {templateError}
          </div>
        )}
      </div>
      <div className="card">
        <div className="card__title" style={{ marginBottom: 8 }}>Public viewer link</div>
        <div className="card__sub" style={{ marginBottom: 14 }}>Players & spectators see this competition&apos;s bracket, schedule and results.</div>
        <div style={{ display: "flex", gap: 8 }}>
          <input className="input" value={url} readOnly style={{ flex: 1 }} />
          <button type="button" className="btn" onClick={copyUrl}>Copy</button>
        </div>
      </div>
    </div>
  );
}
