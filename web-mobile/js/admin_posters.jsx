// admin_posters.jsx: the Posters page (/admin/posters, bc-qrps). The organiser
// picks a poster (the viewer home or one court), sets its heading, its
// call-to-action line, where the logo goes and whether the sponsors show, sees
// the change in the preview at once, saves it with the tournament and prints.
//
// What a poster IS lives in posters_model.jsx and how it looks in
// poster_layout.jsx; this page holds only the draft and the two doors out of it:
//   - Save: nothing is sent while the organiser types or switches posters. One
//     tap sends the whole `posters` block as one settings save (onSave, which is
//     AdminApp's updateTournament with its quiet, onRefusal and onSettled
//     options) against the version last read or saved, so one editing session
//     is one reload on the other devices. A refusal keeps the draft on screen.
//   - Print: prints the draft, saved or not, through the browser's own dialog
//     over the print sheet in css/posters.css. It never saves and never asks.
// Leaving with unsaved changes asks first (the unsaved-entries warning), and a
// reload or a closed tab gets the browser's own prompt.

import { Poster } from "./poster_layout.jsx";
import {
  POSTER_RULES,
  defaultHeading,
  logoPlacement,
  normalisePosters,
  posterAddress,
  posterChoices,
  posterList,
  postersChanged,
  postersTooLong,
  posterFlags,
  withChoicesOnEveryCourt,
  textLength,
  trimText,
  withChoice,
} from "./posters_model.jsx";
import { isNonPublicOrigin, linkBase } from "./viewer_utils.jsx";
import { confirmDiscardUnsaved, whenConfirmed } from "./unsaved_entries.jsx";
import { Icon } from "./icon.jsx";
import { useLatestRef } from "./latest_ref.jsx";

const { useState, useEffect, useRef } = React;

export const LEAVE_POSTERS_ANYWAY = "Leave Posters anyway?";
export const POSTERS_CHANGED_ELSEWHERE =
  "Not saved: the tournament settings were changed on another device. Your poster text is still here.";
export const POSTER_STATUS = Object.freeze({ unsaved: "Unsaved changes", saving: "Saving", saved: "Saved" });

// The class the print sheet keys on, and how long Print waits for the images of
// the posters to decode before it opens the dialog anyway (a missing sponsor
// file must never hold Print).
export const PRINTING_CLASS = "printing-posters";
export const PRINT_DECODE_WAIT_MS = 3000;

// decodeImages: resolves once every <img> under `node` has decoded or failed,
// or after `ms`, whichever is first.
export function decodeImages(node, ms) {
  const imgs = node ? Array.from(node.querySelectorAll("img")) : [];
  const all = Promise.all(imgs.map((img) => Promise.resolve().then(() => img.decode()).catch(() => {})));
  let timer;
  const bound = new Promise((resolve) => { timer = setTimeout(resolve, ms); });
  return Promise.race([all, bound]).finally(() => clearTimeout(timer));
}

// Counter: the characters a box holds against its maximum. An empty box says
// what prints instead (`empty`), since nothing typed is not nothing printed.
function Counter({ id, text, max, empty }) {
  const n = textLength(text);
  const over = n - max;
  return (
    <div id={id} className={over > 0 ? "field__hint field__hint--error" : "field__hint"} data-testid={id}>
      {over > 0
        ? `${n} of ${max} characters: ${over} too many, so it cannot be saved or printed. Shorten it.`
        : n === 0 && empty
          ? `${empty} Up to ${max} characters.`
          : `${n} of ${max} characters`}
    </div>
  );
}

// The Print all question: how many sheets, the address the codes open, a
// warning when phones at the venue may not reach it, and the advice to test one.
export function printAllMessage(count, base, unreachable) {
  return [
    `This prints ${count} A4 sheets, one poster on each.`,
    `The codes open pages under ${base}.`,
    unreachable ? "No public address is set, so phones at the venue may not reach it." : "",
    "Print one poster and scan it with a phone first, to check the code opens the right page.",
  ].filter(Boolean).join(" ");
}

// AdminPostersPage
//   tournament       the tournament (kept fresh by the app)
//   onSave(patch, opts)  the settings save (AdminApp's updateTournament)
//   onBack, onEditTournament, onLogout, onViewerMode  the page's exits
export function AdminPostersPage({ tournament, onSave, onBack, onEditTournament, onLogout, onViewerMode }) {
  const t = tournament || {};
  const AdminTopbar = window.AdminTopbar;
  const Breadcrumbs = window.Breadcrumbs;

  // The draft is the page's own: read from the tournament once, then never
  // derived from it again, so a copy the app takes back after a refusal does not
  // wipe what was typed.
  const [draft, setDraft] = useState(() => normalisePosters(t.posters));
  // The block as last read or saved: what "unsaved" is measured against.
  const [savedBlock, setSavedBlock] = useState(() => normalisePosters(t.posters));
  // The block the server is known to hold: what a refused or discarded save
  // turns the page back to.
  const storedRef = useRef(savedBlock);
  const [saving, setSaving] = useState(false);
  const [refusal, setRefusal] = useState(null);
  const [selectedKey, setSelectedKey] = useState("viewer");
  // The print in progress: its posters and the print root they are drawn into.
  const [printJob, setPrintJob] = useState(null);
  // Fixed when the page opens: a logo replaced before then never prints stale
  // from a cache (logoURL).
  const [logoKey] = useState(() => Date.now());

  const mountedRef = useRef(true);
  useEffect(() => () => { mountedRef.current = false; }, []);
  // The version the next save is made against: the one read, then each landed
  // save's answer.
  const versionRef = useRef(t.tournamentVersion);
  const tournamentRef = useLatestRef(t);
  const saveSeqRef = useRef(0);
  const printingRef = useRef(false);
  const leavingRef = useRef(false);

  const dirty = postersChanged(draft, savedBlock);
  const dirtyRef = useLatestRef(dirty);

  // A reload or a closed tab with unsaved changes gets the browser's prompt.
  useEffect(() => {
    if (!dirty) return undefined;
    const ask = (e) => {
      if (leavingRef.current) return;
      e.preventDefault();
      e.returnValue = "";
    };
    window.addEventListener("beforeunload", ask);
    return () => window.removeEventListener("beforeunload", ask);
  }, [dirty]);

  // Every way off the page asks first while something is unsaved; with nothing
  // unsaved it goes in the same tick.
  // The topbar's running-match chips ask the same question through its
  // `beforeLeave`: `true` at once, or a promise of the operator's answer.
  const confirmLeave = () => (dirtyRef.current
    ? confirmDiscardUnsaved(true, LEAVE_POSTERS_ANYWAY).then((ok) => ok && mountedRef.current)
    : true);
  const guarded = (go) => (...args) => {
    if (typeof go !== "function") return;
    whenConfirmed(confirmLeave(), () => { leavingRef.current = true; go(...args); });
  };

  const list = posterList(t);
  const selected = list.find((p) => p.key === selectedKey) || list[0];
  const draftT = { ...t, posters: draft };
  const choices = posterChoices(draftT, selected);
  const lp = logoPlacement(draftT, selected, choices);
  const sponsorsOffered = Array.isArray(t.sponsors) && t.sponsors.some((s) => s && s.file);
  const address = posterAddress(t, selected);
  const showAddressWarning = !String(t.publicURL || "").trim() && isNonPublicOrigin(window.location.origin);

  const tooLong = postersTooLong(draft);
  // Each poster's markers on the picker, and the posters a too-long text sits on.
  const flags = new Map(list.map((p) => [p.key, posterFlags(draft, savedBlock, p)]));
  const overLabels = list.filter((p) => flags.get(p.key).tooLong).map((p) => p.label);

  const change = (patch) => setDraft((d) => withChoice(d, selected, patch, t.courts));

  // save: the whole draft as one settings save. `again` (after a refusal made
  // against an older copy) sends it at the version of the copy the app now
  // holds, so the other device's other settings are kept and only the poster
  // block is the operator's.
  const save = async (again) => {
    if (tooLong) return;
    const block = normalisePosters(draft);
    if (!again && !postersChanged(block, savedBlock)) { setRefusal(null); return; }
    let turnedBack = false;
    if (again) versionRef.current = tournamentRef.current.tournamentVersion;
    const id = ++saveSeqRef.current;
    setRefusal(null);
    setSaving(true);
    const latest = () => id === saveSeqRef.current;
    // A refused or discarded save puts back the block the server holds, but
    // only the newest save does: an older one's answer must not undo a newer
    // save that is still out.
    const turnBack = () => {
      turnedBack = true;
      if (latest()) setSavedBlock(storedRef.current);
    };
    try {
      await onSave({ tournamentVersion: versionRef.current, posters: block }, {
        onRefusal: (e) => {
          if (!mountedRef.current) return false;
          turnBack();
          if (latest()) setSaving(false);
          setRefusal(e && e.code === "tournament_changed"
            ? { changed: true, message: POSTERS_CHANGED_ELSEWHERE }
            : { changed: false, message: "Not saved: " + ((e && e.message) || "the server refused the poster text.") });
          return true;
        },
        onSettled: (saved) => {
          if (!mountedRef.current) return;
          if (saved) {
            storedRef.current = block;
            if (saved.tournamentVersion) versionRef.current = saved.tournamentVersion;
          } else {
            turnBack();
          }
          if (latest()) setSaving(false);
        },
      });
      // The save is in the outbox, which sends it whenever the link allows, so
      // it is no longer an unsaved entry: leaving does not warn about it. A
      // refusal or a discard later turns the block back (above).
      if (mountedRef.current && !turnedBack) setSavedBlock(block);
    } catch (e) {
      if (!mountedRef.current) return;
      if (latest()) setSaving(false);
      setRefusal({ changed: false, message: "Not saved: " + ((e && e.message) || "the save could not be made.") });
    }
  };

  // Print: the chosen posters, from the draft, into a print root of their own
  // under <body>, which the print sheet alone shows on paper. Nothing prints
  // while a text is over its limit (it would not fit the poster).
  const print = (posters) => {
    if (printingRef.current || tooLong) return;
    printingRef.current = true;
    const node = document.createElement("div");
    node.className = "poster-print-root";
    document.body.appendChild(node);
    document.body.classList.add(PRINTING_CLASS);
    setPrintJob({ node, posters });
  };
  // Every shiaijo the same: this court poster's choices copied onto the other
  // court posters of the draft, asked first because it replaces what they hold.
  const courtCount = list.filter((p) => p.kind === "court").length;
  const [applyNote, setApplyNote] = useState("");
  useEffect(() => { setApplyNote(""); }, [selectedKey]);
  const applyToEveryCourt = async () => {
    const { others } = withChoicesOnEveryCourt(draft, selected, list, t.courts);
    if (others.length === 0) { setApplyNote("Every shiaijo poster already has these choices."); return; }
    const ok = await window.confirmDialog({
      title: "Use these choices on every shiaijo?",
      message: `The heading, call to action, logo place and sponsors of ${selected.label} replace those on ${others.map((p) => p.label).join(", ")}. Nothing is saved until you tap Save.`,
      confirmLabel: "Use on every shiaijo",
    });
    if (!ok || !mountedRef.current) return;
    setDraft((d) => withChoicesOnEveryCourt(d, selected, list, t.courts).posters);
    setApplyNote(`Copied to ${others.map((p) => p.label).join(", ")}.`);
  };

  // Print all asks first: it is many sheets, and every code on them opens the
  // address the page names.
  const printAll = async () => {
    if (printingRef.current || tooLong) return;
    const ok = await window.confirmDialog({
      title: "Print all posters?",
      message: printAllMessage(list.length, linkBase(t).replace(/\/+$/, ""), showAddressWarning),
      confirmLabel: "Print all",
    });
    if (ok && mountedRef.current) print(list);
  };
  useEffect(() => {
    if (!printJob) return undefined;
    const { node } = printJob;
    let live = true;
    const finish = () => { if (mountedRef.current) setPrintJob(null); };
    window.addEventListener("afterprint", finish);
    decodeImages(node, PRINT_DECODE_WAIT_MS).then(() => { if (live) window.print(); });
    return () => {
      live = false;
      window.removeEventListener("afterprint", finish);
      document.body.classList.remove(PRINTING_CLASS);
      if (node && node.parentNode) node.parentNode.removeChild(node);
      printingRef.current = false;
    };
  }, [printJob]);

  const status = saving ? POSTER_STATUS.saving : dirty ? POSTER_STATUS.unsaved : POSTER_STATUS.saved;
  const idBase = "poster-" + selected.key.replace(/[^A-Za-z0-9]/g, "-");
  const logoValue = choices.logo === "above" || !lp.centreAvailable ? "above" : "centre";

  return (
    <div className="app">
      {AdminTopbar && <AdminTopbar onLogout={guarded(onLogout)} onViewerMode={guarded(onViewerMode)} beforeLeave={confirmLeave} tournament={t} />}
      <div className="page page--wide posters-page">
        {Breadcrumbs && (
          <Breadcrumbs items={[{ label: t.name || "Tournament", onClick: guarded(onBack) }, { label: "Posters" }]} />
        )}
        <div className="page-head">
          <div>
            <h1 className="page-head__title">Posters</h1>
            <div className="page-head__sub">QR-code posters for the venue: one for the viewer home and one for each shiaijo.</div>
          </div>
          <div className="page-head__actions">
            <button type="button" className="btn btn--ghost" onClick={guarded(onBack)}>Back</button>
          </div>
        </div>

        {showAddressWarning && (
          <div className="alert alert--warn poster-no-print" role="note" data-testid="posters-address-warning">
            No public address is set, so the codes open this device&apos;s address ({window.location.origin}), which phones at the venue may not reach.{" "}
            <button type="button" className="btn btn--sm" onClick={guarded(onEditTournament)}>Set it in Edit details</button>
          </div>
        )}

        <div className="seg posters-picker" role="group" aria-label="Poster">
          {list.map((p) => (
            <button
              key={p.key}
              type="button"
              className={p.key === selected.key ? "is-active" : ""}
              aria-pressed={p.key === selected.key}
              onClick={() => setSelectedKey(p.key)}
            >
              {p.label}
              {flags.get(p.key).tooLong ? (
                <span className="posters-mark posters-mark--error" data-testid={"posters-mark-" + p.key}>
                  <span aria-hidden="true">!</span><span className="sr-only"> (over a limit)</span>
                </span>
              ) : flags.get(p.key).changed ? (
                <span className="posters-mark" data-testid={"posters-mark-" + p.key}>
                  <span aria-hidden="true">•</span><span className="sr-only"> (unsaved)</span>
                </span>
              ) : null}
            </button>
          ))}
        </div>

        <div className="posters-grid">
          <div className="card">
            <div className="card__body">
              <div className="posters-save-row">
                <button type="button" className="btn btn--primary" disabled={tooLong} onClick={() => save(!!(refusal && refusal.changed))}>
                  {refusal && refusal.changed ? "Save again" : "Save"}
                </button>
                <span role="status" aria-live="polite" data-testid="posters-status">{status}</span>
                {tooLong && (
                  <span className="posters-too-long" data-testid="posters-too-long">
                    Over a limit on {overLabels.join(", ")}: shorten it to save or print.
                  </span>
                )}
              </div>
              <h2 className="posters-editing">{selected.label} poster</h2>
              {refusal && (
                <div className="alert alert--error" role="alert" data-testid="posters-refusal">{refusal.message}</div>
              )}

              <div className="field">
                <label className="field__label" htmlFor={idBase + "-heading"}>Heading</label>
                <input
                  id={idBase + "-heading"}
                  className="input"
                  value={choices.heading}
                  placeholder={defaultHeading(selected)}
                  aria-describedby={idBase + "-heading-count"}
                  onChange={(e) => change({ heading: e.target.value })}
                />
                <Counter id={idBase + "-heading-count"} text={trimText(choices.heading)} max={POSTER_RULES.headingMaxLength} empty={`Prints \u201c${defaultHeading(selected)}\u201d.`} />
              </div>

              <div className="field">
                <label className="field__label" htmlFor={idBase + "-cta"}>Call to action <span className="field__label-optional">(optional)</span></label>
                <input
                  id={idBase + "-cta"}
                  className="input"
                  value={choices.callToAction}
                  placeholder="For example: Scan for live results"
                  aria-describedby={idBase + "-cta-count"}
                  onChange={(e) => change({ callToAction: e.target.value })}
                />
                <Counter id={idBase + "-cta-count"} text={trimText(choices.callToAction)} max={POSTER_RULES.callToActionMaxLength} empty="Nothing prints here." />
              </div>

              {lp.offered && (
                <fieldset key={selected.key} className="field posters-logo-field">
                  <legend className="field__label">Logo</legend>
                  <label className="checkbox">
                    <input
                      type="radio"
                      name={idBase + "-logo"}
                      value="centre"
                      checked={logoValue === "centre"}
                      disabled={!lp.centreAvailable}
                      onChange={() => change({ logo: "" })}
                    />
                    Logo in the centre of the code
                  </label>
                  <label className="checkbox">
                    <input
                      type="radio"
                      name={idBase + "-logo"}
                      value="above"
                      checked={logoValue === "above"}
                      onChange={() => change({ logo: "above" })}
                    />
                    Logo above the code
                  </label>
                  {lp.centreRefusedSentence && <div className="field__hint field__hint--warn">{lp.centreRefusedSentence}</div>}
                </fieldset>
              )}

              {sponsorsOffered && (
                <div className="field">
                  <label className="checkbox">
                    <input type="checkbox" checked={!choices.hideSponsors} onChange={(e) => change({ hideSponsors: !e.target.checked })} />
                    Show sponsors
                  </label>
                </div>
              )}

              {selected.kind === "court" && courtCount > 1 && (
                <div className="field posters-apply">
                  <button type="button" className="btn btn--sm" onClick={applyToEveryCourt}>Use these choices on every shiaijo</button>
                  <span className="field__hint" role="status" aria-live="polite" data-testid="posters-apply-note">{applyNote}</span>
                </div>
              )}

              <div className="field__hint posters-branding" data-testid="posters-branding">
                {lp.offered || sponsorsOffered
                  ? "The logo and the sponsor images come from the Branding and Sponsors sections of Edit details."
                  : "To put a logo or sponsor images on the posters, add them in the Branding and Sponsors sections of Edit details."}{" "}
                <button type="button" className="btn btn--sm" onClick={guarded(onEditTournament)}>Open Edit details</button>
              </div>

              <div className="field posters-address" data-testid="posters-address">
                <div className="field__label">The code opens</div>
                <code className="posters-address__url">{address}</code>
              </div>

              <div className="posters-print-row">
                <button type="button" className="btn" onClick={() => print([selected])} disabled={!!printJob || tooLong}>
                  <Icon name="printer" /> Print this poster
                </button>
                <button type="button" className="btn" onClick={printAll} disabled={!!printJob || tooLong}>
                  <Icon name="printer" /> Print all posters
                </button>
              </div>
              <div className="field__hint posters-print-hint">Print one poster and scan it with a phone before printing them all. Prints on A4; for A3, choose A3 paper and scale to fit in the print dialog. The colours print without turning on background graphics.</div>
            </div>
          </div>

          <div className="posters-preview" data-testid="posters-preview">
            <Poster tournament={draftT} poster={selected} choices={choices} logoKey={logoKey} />
          </div>
        </div>
      </div>
      {printJob && window.ReactDOM.createPortal(
        printJob.posters.map((p) => (
          <Poster key={p.key} tournament={draftT} poster={p} choices={posterChoices(draftT, p)} logoKey={logoKey} />
        )),
        printJob.node,
      )}
    </div>
  );
}
