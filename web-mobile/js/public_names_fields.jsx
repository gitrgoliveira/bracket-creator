// public_names_fields.jsx: the rendered controls and the words of the
// "competitor names on the public pages" option (bc-hnam, bc-anon), shared by
// the tournament settings and the Add competition form (admin_setup.jsx), the
// competition settings (admin_competition_settings.jsx) and the participant
// list (admin_participants.jsx). Those three modules each have their own
// script tag, so none may import another: this leaf is the one place they meet,
// ES-imported and never script-tagged, like competition_fields.jsx.
//
import { PillGroup } from './competition_fields.jsx';
import { FORMAT_SWISS, isTeamCompetition, tournamentShowsEveryName } from './competition_shape.jsx';
import { useStableId } from './stable_id.jsx';
import { andList } from './list_phrase.jsx';

// ---------------------------------------------------------------------------
// Competitor names on the public pages (bc-hnam, bc-anon). One option per
// competition, and a tournament default that new competitions start with:
//   ""          names shown (the default)
//   person      numbers; a competitor's own link shows that competitor's name
//   competition numbers; one competition link shows every name
//   hidden      numbers; no link, only the signed-in organiser sees names
// The server owns the rules (state.PublicNames*, handlers_reveal_links.go);
// these are the client's copy of the words and of the two predicates, shared by
// the tournament settings, the Add competition form and the competition
// settings so the three cannot word one choice three ways.
export const LABEL_PUBLIC_NAMES = "Competitor names on public pages";
export const PUBLIC_NAMES_OPTIONS = [
  { value: "", label: "Show names" },
  { value: "person", label: "Numbers, a link per person" },
  { value: "competition", label: "Numbers, one competition link" },
  { value: "hidden", label: "Numbers only" },
];
export const PUBLIC_NAMES_HINTS = {
  "": "Everyone sees competitor names.",
  person: "Spectators see competitor numbers. A competitor's own link, on the QR code of their printed tag or copied from the Participants list, shows that competitor's name only.",
  competition: "Spectators see competitor numbers. One link or QR code for the whole competition, shown at the venue, shows every name in it.",
  hidden: "Spectators see competitor numbers and there is no link. Only the signed-in organiser sees names.",
};
export const HINT_PUBLIC_NAMES_TEAM = "Team names stay visible. The names of the team members are hidden.";
export const HINT_PUBLIC_NAMES_DOJO = "Dojo names stay visible, and so do organiser screens and exports.";
export const HINT_PUBLIC_NAMES_SWISS = "Showing numbers instead of names is not available for Swiss competitions yet, because a Swiss draw gives competitors no numbers to show.";
export const HINT_PUBLIC_NAMES_APPLY = "Saving a change here also sets every competition that is not Swiss to this choice, after you confirm the list. Each competition can still be changed on its own afterwards.";
export const WARN_PUBLIC_NAMES_FREE_TEXT = "Anything typed in the announcement box or the awards note is shown to everyone exactly as typed. Do not put competitor names in them.";
// Said where a reveal link is copied while the tournament has no Public URL:
// the link is built from this device's own address, which other phones may
// not be able to open.
export const WARN_REVEAL_LINK_NO_PUBLIC_URL = "It points at this device's own address, which other phones may not reach. Set a Public URL in the tournament details to fix that.";
export const LABEL_REVEAL_UNTIL = "Links stop working at";
export const HINT_REVEAL_UNTIL = "Leave empty to stop the links one hour after the tournament's closing time on its last day (at 01:00 the next morning when no closing time is set). Switching links off, or moving this time later, makes the same links work again.";

export function publicNamesHides(v) {
  return v === "person" || v === "competition" || v === "hidden";
}
export function publicNamesHasLinks(v) {
  return v === "person" || v === "competition";
}
// Whether the option applies to this tournament at all: not to one that shows
// every name (a self-run one, tournamentShowsEveryName).
export function publicNamesApplies(tournament) {
  return !tournamentShowsEveryName(tournament);
}
export function publicNamesLabel(v) {
  const o = PUBLIC_NAMES_OPTIONS.find((x) => x.value === (v || ""));
  return o ? o.label : "Show names";
}
// Whether any competition of the tournament hides names right now: what the
// free-text warning waits on.
export function anyCompetitionHidesNames(tournament) {
  if (!publicNamesApplies(tournament)) return false;
  return (tournament && tournament.competitions || []).some((c) => c && c.format !== FORMAT_SWISS && publicNamesHides(c.publicNames));
}
// The competitions that taking the tournament default `value` would change:
// every one whose option differs, Swiss left out (the server's list).
export function publicNamesApplyTargets(competitions, value) {
  return (competitions || []).filter((c) => c && c.format !== FORMAT_SWISS && (c.publicNames || "") !== (value || ""));
}
export function publicNamesApplyMessage(targets, value) {
  const n = targets.length;
  const names = andList(targets.map((c) => `${c.name} (now: ${publicNamesLabel(c.publicNames)})`));
  return `Saving this changes ${n === 1 ? "1 competition" : `${n} competitions`} to "${publicNamesLabel(value)}": ${names}. Each one can still be changed on its own afterwards.`;
}
// The server's code for a save whose changed default would change competitions
// the save has not confirmed. It lists them as `competitions`, which can differ
// from the form's own list (one was added or changed on another device).
export const PUBLIC_NAMES_APPLY_CONFIRM = "public_names_apply_confirm";
// The code a save is refused with once its settings landed but some competitions
// could not be set to the new default (the server's 500, which lists them as
// `competitions`). The default now equals the stored one, so the form has to
// send the confirmation again by itself to finish the rest.
export const PUBLIC_NAMES_APPLY_FAILED = "public_names_apply_failed";
// The operator's yes to setting every listed competition to `value`; the
// promise answers true or false. Shared by the form's own question and the one
// the server's list asks.
export function confirmPublicNamesApply(targets, value) {
  return window.confirmDialog({
    title: "Change these competitions?",
    message: publicNamesApplyMessage(targets, value),
    confirmLabel: "Change them all",
    danger: publicNamesHides(value),
  });
}

// The link a competition's QR code and the Copy button carry:
// <public address>/reveal/<competition>#<key>. The key rides in the fragment,
// so it never reaches a server log.
export function revealLinkUrl(base, compId, key) {
  return `${String(base || "").replace(/\/+$/, "")}/reveal/${encodeURIComponent(compId)}#${key}`;
}
// "2026-11-22T18:00" read as the people at the venue say it.
export function formatRevealExpiry(expiresAt) {
  return String(expiresAt || "").replace("T", " at ");
}
// The organiser's read of a competition's reveal links (keys and expiry).
// Organiser only, so it sends the password; the answer is never cached.
export async function fetchRevealLinks(compId, password) {
  const res = await fetch(`/api/competitions/${encodeURIComponent(compId)}/reveal-links`, {
    headers: { "X-Tournament-Password": password || "" },
  });
  if (!res.ok) {
    const err = await res.json().catch(() => ({}));
    throw new Error(err.error || `Could not read the reveal links (status ${res.status}).`);
  }
  return res.json();
}

// copyText puts text on the clipboard. The same two-step copy as ui.jsx's
// copyToClipboard (which is not exported): the Clipboard API where the page is
// a secure context, and a selected textarea on a plain-http venue LAN.
export async function copyText(text) {
  if (navigator.clipboard && navigator.clipboard.writeText) {
    return navigator.clipboard.writeText(text);
  }
  const ta = document.createElement("textarea");
  ta.value = text;
  ta.style.position = "fixed";
  ta.style.opacity = "0";
  document.body.appendChild(ta);
  ta.select();
  try {
    if (!document.execCommand("copy")) throw new Error("copy failed");
  } finally {
    document.body.removeChild(ta);
  }
}

// printRevealPoster prints the competition's QR code on a page of its own: a
// hidden frame holds the page, so nothing else of the console prints with it.
export function printRevealPoster({ title, url, expiresAt }) {
  const canvas = document.createElement("canvas");
  window.renderQR(canvas, url, { moduleSize: 10, quietZone: 4 });
  const esc = (t) => String(t).replace(/[&<>"]/g, (ch) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[ch]));
  const until = expiresAt ? `<p>Works until ${esc(formatRevealExpiry(expiresAt))}.</p>` : "";
  const frame = document.createElement("iframe");
  frame.setAttribute("aria-hidden", "true");
  frame.style.cssText = "position:fixed;right:0;bottom:0;width:0;height:0;border:0";
  frame.srcdoc = `<!doctype html><html><head><meta charset="utf-8"><title>${esc(title)}</title>`
    + `<style>body{font-family:system-ui,sans-serif;text-align:center;margin:0;padding:24mm 12mm}`
    + `h1{font-size:28pt;margin:0 0 6mm}p{font-size:16pt;margin:4mm 0}img{width:130mm;max-width:100%;image-rendering:pixelated}</style></head>`
    + `<body><h1>${esc(title)}</h1><p>Scan to see the competitor names.</p><img alt="QR code" src="${canvas.toDataURL("image/png")}">${until}</body></html>`;
  frame.onload = () => {
    try {
      frame.contentWindow.focus();
      frame.contentWindow.print();
    } finally {
      setTimeout(() => frame.remove(), 60000);
    }
  };
  document.body.appendChild(frame);
}

// RevealQrSheet: the competition link as a QR code, with Print and Copy link.
// The sheet itself is the shared ShareLinkModal; only the wording and the
// Print button are this surface's own.
export function RevealQrSheet({ compName, url, expiresAt, onClose, showToast }) {
  const title = `Competition link: ${compName}`;
  return (
    <window.ShareLinkModal
      title={title}
      url={url}
      onClose={onClose}
      onCopy={(ok) => {
        if (!showToast) return;
        if (ok) showToast("Competition link copied!");
        else showToast("Copy failed; select the link above manually", "error");
      }}
    >
      {(showQR) => (
        <>
          <p className="share-link__note">
            {showQR
              ? "Anyone who scans this code or opens the link sees every competitor's name in this competition."
              : "Anyone who opens this link sees every competitor's name in this competition."}
            {expiresAt ? ` It works until ${formatRevealExpiry(expiresAt)}.` : ""}
          </p>
          {showQR && (
            <button type="button" className="btn" onClick={() => printRevealPoster({ title: compName, url, expiresAt })}>Print the QR code</button>
          )}
        </>
      )}
    </window.ShareLinkModal>
  );
}

// PublicNamesFields: the option picker, the notes that go with it, and (when a
// competition screen hands over the expiry) the time its links stop working.
// `format`/`kind`/`teamSize` are the competition's (absent for the tournament
// default).
// `children` carries what only the saved competition can show (its link).
export function PublicNamesFields({ tournament, format, kind, teamSize, value, onChange, revealUntil, onRevealUntilChange, applyNote, children }) {
  const untilId = useStableId("reveal-until");
  // A self-run tournament always shows names, so it has no such setting to show.
  if (!publicNamesApplies(tournament)) return null;
  if (format === FORMAT_SWISS) {
    return (
      <div className="field">
        <label className="field__label">{LABEL_PUBLIC_NAMES}</label>
        <div className="field__hint" data-testid="public-names-swiss">{HINT_PUBLIC_NAMES_SWISS}</div>
        {publicNamesHides(value) && (
          <div className="public-names__fix">
            <div className="field__hint field__hint--warn">This competition is set to show numbers, which the server refuses for Swiss.</div>
            <button type="button" className="btn btn--sm" onClick={() => onChange("")}>Show names for this competition</button>
          </div>
        )}
      </div>
    );
  }
  const isTeam = isTeamCompetition({ kind, teamSize });
  const hides = publicNamesHides(value);
  const hint = `${PUBLIC_NAMES_HINTS[value || ""] || ""}${hides ? ` ${HINT_PUBLIC_NAMES_DOJO}` : ""}${hides && isTeam ? ` ${HINT_PUBLIC_NAMES_TEAM}` : ""}`.trim();
  return (
    <>
      <PillGroup label={LABEL_PUBLIC_NAMES} options={PUBLIC_NAMES_OPTIONS} value={value || ""} onChange={onChange} hint={hint} />
      {applyNote && <div className="field__hint public-names__note">{applyNote}</div>}
      {hides && (
        <div className="field__hint field__hint--warn public-names__note" role="note" data-testid="public-names-free-text-warning">{WARN_PUBLIC_NAMES_FREE_TEXT}</div>
      )}
      {onRevealUntilChange && publicNamesHasLinks(value) && (
        <div className="field">
          <label className="field__label" htmlFor={untilId}>{LABEL_REVEAL_UNTIL}<span className="field__label-optional"> (optional)</span></label>
          <div className="public-names__until">
            <input id={untilId} className="input" type="datetime-local" value={revealUntil || ""} onChange={(e) => onRevealUntilChange(e.target.value)} />
            {revealUntil ? <button type="button" className="btn btn--sm" onClick={() => onRevealUntilChange("")}>Clear</button> : null}
          </div>
          <div className="field__hint">{HINT_REVEAL_UNTIL}</div>
        </div>
      )}
      {children}
    </>
  );
}

