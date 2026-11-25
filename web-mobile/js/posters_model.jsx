// posters_model.jsx: the ONE owner of what a venue poster IS (bc-qrps).
//
// A poster is the viewer-home page or one court's display page, with a QR code
// that opens it, a band with the tournament name, a heading the organiser may
// overwrite, an optional call-to-action line, an optional logo (in the code's
// centre or above it) and a sponsor strip. This module holds every rule about
// those: the list, the address each code encodes, the stored choices with their
// defaults, where the logo can go, the contrast colour of the band and the edits
// of the stored `posters` block. It draws nothing (poster_layout.jsx does) and
// holds no state, so the Posters page, the preview and the print sheet cannot
// disagree about a poster.
//
// Not a leaf: it imports viewer_utils.jsx (linkBase), qr.jsx (what fits) and
// admin_branding.jsx (the default primary colour), so it loads after all three.

import { linkBase } from "./viewer_utils.jsx";
import { qrFits, qrLogoPadShare } from "./qr.jsx";
import { BRANDING_DEFAULTS } from "./admin_branding.jsx";

// The rules the server owns (internal/state/posters.go), copied here because the
// browser cannot read the Go package. internal/state/testdata/poster_rules.json
// is the shared table: posters_model.test.jsx pins every value below to it, and
// TestPosterRules_SharedTable pins the same table to the Go side.
export const POSTER_RULES = Object.freeze({
  headingMaxLength: 80,
  callToActionMaxLength: 120,
  logoValues: Object.freeze(["", "centre", "above"]),
  maxCourtEntries: 32,
  defaultHeadings: Object.freeze({ viewer: "Follow the tournament live", courtPrefix: "Shiaijo " }),
});

// Sizes in cqw, a hundredth of the poster's width, so the preview and the paper
// are one layout at any width and A3 "fit to page" scales without reflow. The
// court letter is the largest element of a court poster (taller than its code),
// and the court code stays over 8 cm wide on A4.
export const POSTER_SIZES = Object.freeze({
  viewerCode: 62,
  courtCode: 44,
  courtLetter: 46,
  viewerLogoAbove: 15,
  courtLogoAbove: 6.5,
});

// A code is drawn with this many white modules around it (the QR standard's quiet
// zone), whatever the page colour is.
export const POSTER_QUIET_MODULES = 4;

// Where a centre logo's white pad may reach, as a share of the code's side. 0.4
// is 16% of the area, under the 20% ceiling, so a logo is never the reason a
// code stops scanning; qrLogoPadShare (qr.jsx) lowers it where a short code
// has no codewords to spare.
const CENTRE_PAD_SIDE = 0.4;

export const CENTRE_TOO_LONG_SENTENCE =
  "This address is too long to carry a logo in the middle of the code. The logo goes above the code instead. A shorter public address in the tournament settings fixes it.";

export const CODE_TOO_LONG_SENTENCE =
  "This address is too long for a QR code. A shorter public address in the tournament settings fixes it.";

const textOf = (s) => (typeof s === "string" ? s : "");

// textLength counts characters the way the server does (runes, not UTF-16
// units), so the counters beside the boxes agree with its 400.
export const textLength = (s) => [...textOf(s)].length;

// posterList: the viewer-home poster, then one per court of the tournament, in
// the tournament's order. A court that is no longer in the tournament is not
// listed (its stored choices stay in the block and return with the court).
export function posterList(t) {
  const courts = Array.isArray(t && t.courts) ? t.courts : [];
  const list = [{ kind: "viewer", key: "viewer", label: "Viewer home" }];
  const seen = new Set();
  for (const raw of courts) {
    const letter = textOf(raw).trim();
    if (!letter || seen.has(letter)) continue;
    seen.add(letter);
    list.push({ kind: "court", key: "court:" + letter, letter, label: "Shiaijo " + letter });
  }
  return list;
}

// posterAddress: the page the code opens, always public. linkBase is the public
// URL or this browser's origin; trailing slashes go, as the server trims them.
// Nothing else is ever appended, so no poster can carry a reveal key (FR13).
export function posterAddress(t, poster) {
  const base = linkBase(t).replace(/\/+$/, "");
  if (poster.kind === "court") return base + "/display?court=" + encodeURIComponent(poster.letter);
  return base + "/viewer";
}

export function defaultHeading(poster) {
  return poster.kind === "court"
    ? POSTER_RULES.defaultHeadings.courtPrefix + poster.letter
    : POSTER_RULES.defaultHeadings.viewer;
}

function storedChoices(posters, poster) {
  if (!posters) return {};
  if (poster.kind === "court") return (posters.courts && posters.courts[poster.letter]) || {};
  return posters.viewer || {};
}

// posterChoices: the stored choices with the defaults filled in. `heading` and
// `callToAction` are what is stored (for the boxes); `headingText` and
// `callToActionText` are what prints (trimmed, the heading falling back to its
// default when empty).
export function posterChoices(t, poster) {
  const s = storedChoices(t && t.posters, poster);
  const heading = textOf(s.heading);
  const callToAction = textOf(s.callToAction);
  return {
    heading,
    callToAction,
    headingText: heading.trim() || defaultHeading(poster),
    callToActionText: callToAction.trim(),
    logo: s.logo === "centre" || s.logo === "above" ? s.logo : "",
    hideSponsors: !!s.hideSponsors,
  };
}

// The sponsors a poster prints: none when the organiser switched them off.
export function posterSponsors(t, choices) {
  if (choices && choices.hideSponsors) return [];
  const list = Array.isArray(t && t.sponsors) ? t.sponsors : [];
  return list.filter((s) => s && s.file);
}

// codeFits: does the address fit a plain (level M) code at all?
export const codeFits = (address) => qrFits(address);

// logoPlacement: where the logo goes, and at which level the code is encoded.
//   no logo uploaded          -> offered false, no logo, level M
//   centre (chosen or default) and the address fits level H -> centre, level H
//   otherwise                 -> above, level M, and when Centre was refused
//                                for length, the sentence that says why
// `centreAvailable` tells the page whether to offer Centre at all.
export function logoPlacement(t, poster, choices) {
  const offered = !!(t && t.theme && t.theme.hasLogo);
  if (!offered) {
    return { offered: false, placement: "none", level: "M", centreAvailable: false, centreRefusedSentence: "" };
  }
  const c = choices || posterChoices(t, poster);
  const centreAvailable = qrFits(posterAddress(t, poster), { level: "H" });
  const placement = c.logo !== "above" && centreAvailable ? "centre" : "above";
  return {
    offered,
    placement,
    level: placement === "centre" ? "H" : "M",
    centreAvailable,
    centreRefusedSentence: centreAvailable ? "" : CENTRE_TOO_LONG_SENTENCE,
  };
}

// logoPadModules: the side, in whole modules, of the white pad behind a centre
// logo. Odd, so it sits centred on the odd-sided grid, and never over the share
// qrLogoPadShare allows for that size.
export function logoPadModules(size) {
  const cap = Math.min(
    Math.floor(CENTRE_PAD_SIDE * size),
    Math.floor(Math.sqrt(qrLogoPadShare(size)) * size),
  );
  return cap % 2 === 0 ? cap - 1 : cap;
}

// The band's colour: the tournament's primary colour, or the app's own.
export const posterPrimary = (t) => (t && t.theme && t.theme.primaryColor) || BRANDING_DEFAULTS.primaryColor;

// WCAG relative luminance of a #rgb or #rrggbb colour; null when not one.
function luminance(hex) {
  let h = textOf(hex).trim().replace(/^#/, "");
  if (/^[0-9a-f]{3}$/i.test(h)) h = h.replace(/./g, "$&$&");
  if (!/^[0-9a-f]{6}$/i.test(h)) return null;
  const lin = [0, 2, 4].map((i) => {
    const v = parseInt(h.slice(i, i + 2), 16) / 255;
    return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
  });
  return 0.2126 * lin[0] + 0.7152 * lin[1] + 0.0722 * lin[2];
}

export const BAND_INK_LIGHT = "#ffffff";
export const BAND_INK_DARK = "#111111";

// bandTextColour: white or near-black, whichever reads better on `hex` (the
// higher WCAG contrast ratio). A value that is not a colour reads as the dark
// default band, so white.
export function bandTextColour(hex) {
  const l = luminance(hex);
  if (l === null) return BAND_INK_LIGHT;
  const onWhite = 1.05 / (l + 0.05);
  const onDark = (l + 0.05) / (luminance(BAND_INK_DARK) + 0.05);
  return onWhite >= onDark ? BAND_INK_LIGHT : BAND_INK_DARK;
}

// logoURL: the one logo address a poster draws. The logo route is cached and the
// tournament's version does not change when a logo is replaced, so the page
// passes `key`, a value fixed when it opened (the page reloads the tournament
// then), and a replaced logo never prints stale.
export function logoURL(key) {
  const base = "/api/branding/logo";
  return key === undefined || key === null || key === "" ? base : base + "?v=" + encodeURIComponent(String(key));
}

// ---- editing the stored block ----------------------------------------------

// One entry cleaned: empty fields dropped, a logo only when it is a real value.
// `trim` also trims the text (what is saved); a draft keeps it as typed so a
// space between two words is not eaten while the organiser types.
function cleanEntry(e, trim) {
  const out = {};
  const entry = e || {};
  for (const field of ["heading", "callToAction"]) {
    const v = textOf(entry[field]);
    if (v.trim()) out[field] = trim ? v.trim() : v;
  }
  if (entry.logo === "centre" || entry.logo === "above") out.logo = entry.logo;
  if (entry.hideSponsors) out.hideSponsors = true;
  return out;
}

const isEmptyEntry = (e) => Object.keys(e).length === 0;

function rebuild(posters, trim) {
  const cur = posters || {};
  const next = {};
  const viewer = cleanEntry(cur.viewer, trim);
  if (!isEmptyEntry(viewer)) next.viewer = viewer;
  const courts = {};
  for (const [letter, entry] of Object.entries(cur.courts || {})) {
    const clean = cleanEntry(entry, trim);
    if (!isEmptyEntry(clean)) courts[letter] = clean;
  }
  if (Object.keys(courts).length > 0) next.courts = courts;
  return next;
}

// normalisePosters: the block as the server stores it: text trimmed, empty
// entries dropped. ALWAYS an object (`{}` when nothing is left): a PUT whose
// `posters` is undefined or null drops the key from the JSON, and the server
// would then carry the old block back.
export const normalisePosters = (posters) => rebuild(posters, true);

// postersChanged: do two blocks differ once normalised? Trailing spaces alone
// are not a change.
export function postersChanged(a, b) {
  return JSON.stringify(sortKeys(normalisePosters(a))) !== JSON.stringify(sortKeys(normalisePosters(b)));
}

function sortKeys(v) {
  if (!v || typeof v !== "object") return v;
  return Object.fromEntries(Object.keys(v).sort().map((k) => [k, sortKeys(v[k])]));
}

// withChoice: the new `posters` block with `patch` laid over one poster's
// entry. ALWAYS an object, `{}` when every choice is cleared. Entries for
// courts that left the tournament are kept (they come back with the court), up
// to the server's bound; past it the stale ones go first. `courts` is the
// tournament's current court list, which says which entries are stale.
export function withChoice(posters, poster, patch, courts) {
  const next = rebuild(posters, false);
  if (poster.kind === "viewer") {
    const viewer = cleanEntry({ ...next.viewer, ...patch }, false);
    if (isEmptyEntry(viewer)) delete next.viewer;
    else next.viewer = viewer;
    return next;
  }
  const map = { ...next.courts };
  const entry = cleanEntry({ ...map[poster.letter], ...patch }, false);
  if (isEmptyEntry(entry)) delete map[poster.letter];
  else map[poster.letter] = entry;
  if (Object.keys(map).length > POSTER_RULES.maxCourtEntries) {
    const live = new Set((Array.isArray(courts) ? courts : []).map(textOf));
    for (const letter of Object.keys(map)) {
      if (Object.keys(map).length <= POSTER_RULES.maxCourtEntries) break;
      if (letter !== poster.letter && !live.has(letter)) delete map[letter];
    }
  }
  if (Object.keys(map).length > 0) next.courts = map;
  else delete next.courts;
  return next;
}
