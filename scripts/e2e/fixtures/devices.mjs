// The two devices every journey runs on. playwright.config.mjs builds its
// projects from these, and a journey that opens a SECOND context (the public
// viewer beside the operator's iPad) spreads the same constant into
// browser.newContext(), so a project and a hand-made context cannot drift.

// iPad Air, landscape: the court table's device. hasTouch + isMobile is what
// makes `(pointer: coarse)` match, measured, so the 44px tap-floor rules in
// styles.css apply as they do on the device. fixtures/test.mjs re-checks it
// for every operator page and falls back to CDP touch emulation if a future
// Chromium stops honouring these two.
export const OPERATOR_DEVICE = {
  viewport: { width: 1180, height: 820 },
  hasTouch: true,
  isMobile: true,
  deviceScaleFactor: 1,
};

// A phone, portrait: a spectator or a competitor following the draw.
export const PUBLIC_DEVICE = {
  viewport: { width: 390, height: 844 },
  hasTouch: true,
  isMobile: true,
  deviceScaleFactor: 1,
};
