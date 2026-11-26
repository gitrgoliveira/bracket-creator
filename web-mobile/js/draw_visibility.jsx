// draw_visibility.jsx: the one client owner of whether a draft's draw is kept
// off the public pages, and of the words every screen says about it.
//
// A generated draw starts hidden: the public pages read the competition as
// one still in setup until the organiser taps Publish draw or starts it.
// The server stores that as the competition's `drawHidden` and judges it with
// state.Competition.DrawHiddenIn; drawHiddenFromPublic below is its twin,
// pinned to it by the shared table
// internal/mobileapp/testdata/draw_visibility_cases.json, which the Go test
// (internal/state/draw_hidden_cases_test.go) and the JS test
// (__tests__/draw_visibility.test.jsx) both read. Nothing else reads
// `.drawHidden`: the gate __tests__/draw_visibility_gate.test.jsx fails on a
// read outside this module and the two serializers that carry the field.
//
// On an organiser's read `drawHidden` is the stored value; on a viewer read
// the server stamps it from DrawHiddenIn for every audience. Either way the
// judgement here applies the same rule, so the organiser's tab and a public
// page agree on what the public sees.
//
// Pure: no React, and the one dialog it opens is window.confirmDialog (ui.jsx),
// read when it is called, so importing this module runs nothing.

import { isTeamCompetition } from './competition_shape.jsx';

// The status a draft waits in (Go's state.CompStatusDrawReady).
const DRAW_READY = "draw-ready";

// Whether the public may NOT see comp's draw now: it waits to start, it is
// stored as hidden, and the tournament is officiated (a self-run tournament's
// competitors score from the public page, so it shows every draw as soon as
// it is made). A started competition is public whatever is stored.
export function drawHiddenFromPublic(tournament, comp) {
  if (!comp || typeof comp !== "object") return false;
  if (tournament && tournament.mode === "self-run") return false;
  return comp.status === DRAW_READY && comp.drawHidden === true;
}

// Whether comp's draw is waiting to start and already public (published).
export function drawPublishedDraft(tournament, comp) {
  return !!comp && comp.status === DRAW_READY && !drawHiddenFromPublic(tournament, comp);
}

// comps without the drafts the public may not see: the same array back when
// it holds none (so a caller comparing by reference sees no change).
export function withoutHiddenDrafts(comps, tournament) {
  if (!Array.isArray(comps)) return comps;
  if (!comps.some((c) => drawHiddenFromPublic(tournament, c))) return comps;
  return comps.filter((c) => !drawHiddenFromPublic(tournament, c));
}

// Whether an event is the stream's withheld copy of a match change: a
// `match_updated` sent with no data, which is how a public stream is told
// that a hidden draft's match moved (its court, time or status) without
// being told which. A public page refetches the court positions it shows on
// it, since a hidden draft's matches still take their places on a shared
// court.
export function isWithheldMatchEvent(event) {
  return !!event && event.type === "match_updated" && event.data == null;
}

// ---- The words -------------------------------------------------------------

// The draw preview header's hint line.
export const DRAW_HIDDEN_HINT = "Hidden from the public. Publish draw or Start competition makes it public.";
export const DRAW_PUBLISHED_HINT = "Published. The public can see this draw. Changes you make show on public pages at once.";
// The link that ends the hint line: this competition's public page, opened in
// a new tab so the organiser can check what the public sees before and after
// publishing.
export const OPEN_PUBLIC_PAGE_LABEL = "Open the public page";
export function publicCompetitionPath(comp) {
  return `/competition/${encodeURIComponent(comp.id)}`;
}

// The header's buttons and their toasts.
export const PUBLISH_DRAW_LABEL = "Publish draw";
export const UNPUBLISH_DRAW_LABEL = "Unpublish";
export const DRAW_PUBLISHED_TOAST = "Draw published. The public can see it now.";
export const DRAW_UNPUBLISHED_TOAST = "Hidden again. Anyone who looked still has what they saw.";

// The dashboard card's line, beside the crossed-out eye or the open eye.
export const DRAW_HIDDEN_CARD_LINE = "Hidden from the public";
export const DRAW_PUBLIC_CARD_LINE = "Public";
// The icon each line is drawn with (icon.jsx).
export function drawVisibilityIcon(tournament, comp) {
  return drawHiddenFromPublic(tournament, comp) ? "eye-off" : "eye";
}
export function drawVisibilityCardLine(tournament, comp) {
  return drawHiddenFromPublic(tournament, comp) ? DRAW_HIDDEN_CARD_LINE : DRAW_PUBLIC_CARD_LINE;
}

// What the public pages call a hidden draft: the watchlist line and the
// competition page's card title (that page's header carries no badge for it,
// since the card already says it). The organiser's own badge keeps "Draw
// ready".
export const PUBLIC_DRAW_PREPARING = "Draw being prepared";
// The home list's status badge for the same state, short enough to stay on
// one line beside a card's details on a phone.
export const PUBLIC_DRAW_PREPARING_BADGE = "Preparing draw";

// The public card's message: "Starts at 10:30. 12 competitors entered. The
// draw shows here once it is published." The start sentence only with a start
// time; a team competition counts teams. `count` defaults to comp's roster.
export function publicDrawPreparingMessage(comp, count) {
  const n = typeof count === "number" ? count : ((comp && Array.isArray(comp.players)) ? comp.players.length : 0);
  const team = isTeamCompetition(comp);
  const noun = team ? (n === 1 ? "team" : "teams") : (n === 1 ? "competitor" : "competitors");
  const start = comp && typeof comp.startTime === "string" && comp.startTime.trim() ? `Starts at ${comp.startTime.trim()}. ` : "";
  return `${start}${n} ${noun} entered. The draw shows here once it is published.`;
}

// The Start confirm on a hidden draft: its title, then its message.
export function startHiddenDrawTitle(name) {
  return `Start ${name || ""}?`;
}
export function startHiddenDrawMessage(name) {
  return `The draw for "${name || ""}" is still hidden from the public. Starting the competition makes it public at once.`;
}

// Both Discard confirms' title, on a hidden draft and a published one.
export function discardDrawTitle(name) {
  return `Discard the draw for ${name || ""}?`;
}

// The Discard confirm on a published draft (a hidden one keeps the header's
// own message).
export function discardPublishedDrawMessage(name) {
  return `The public has already seen this draw. Discard the draw for "${name || ""}"? The pools and bracket are removed so you can edit participants and settings, and the new draw starts hidden.`;
}

// Start all's line under its list, naming the hidden draws a start makes
// visible to the public; null when it makes none visible.
export function startAllHiddenDrawsNote(tournament, comps) {
  const names = (Array.isArray(comps) ? comps : [])
    .filter((c) => drawHiddenFromPublic(tournament, c))
    .map((c) => c.name || "");
  return names.length ? `Starting makes these draws visible to the public: ${names.join(", ")}.` : null;
}

// The court console's call of a hidden draft's match: a warning, never a
// refusal.
export const CALL_ANYWAY_LABEL = "Call anyway";
export function callHiddenDrawMessage(competitionName) {
  return `The draw for ${competitionName || ""} is still hidden from the public. Calling this match shows its competitors on the public screens.`;
}

// ---- The Start confirm -----------------------------------------------------

// confirmStartHiddenDraw: the one Start confirm, asked by the draw preview
// header and the dashboard card before a start. A draft the public already
// sees (or any other competition) starts without a question: resolves true.
// A hidden one asks, and resolves the operator's answer.
export function confirmStartHiddenDraw(tournament, comp) {
  if (!drawHiddenFromPublic(tournament, comp)) return Promise.resolve(true);
  return window.confirmDialog({
    title: startHiddenDrawTitle(comp.name),
    message: startHiddenDrawMessage(comp.name),
    confirmLabel: "Start competition",
    cancelLabel: "Cancel",
  });
}
