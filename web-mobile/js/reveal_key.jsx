// reveal_key.jsx: the ONE owner of the reveal keys this phone holds (bc-hnam).
//
// A competition that hides its competitors' names from the public pages can
// give out reveal links, /reveal/<competition>#<key>: one link for the whole
// competition (it shows every name in it), or one per competitor, printed in
// the QR of their tag (it shows that competitor's own name, a team's members
// for a team). The key rides in the fragment, so it never reaches a server
// log; viewer_reveal.jsx takes it from there, hands it to rememberRevealKey
// and removes the fragment from the address bar.
//
// The phone remembers a key (operator decision Q-storage): every public read
// sends the keys held here as X-Reveal-Key (api_client_requests.jsx
// readHeaders), and the server judges them on every read, so a key past its
// expiry, or one whose links the organiser switched off, simply reveals
// nothing. Nothing here judges a key beyond its form. Losing the store (a
// private window, storage cleared or blocked) only hides names again, so the
// store is a convenience: every access is guarded, and a key taken while
// storage is unavailable is still held for the life of the page.
//
// A leaf: it imports nothing, so any module may import it.

const LS_REVEAL_KEYS = "bc_reveal_keys";

// The server's own bounds (internal/mobileapp/public_names.go): a key is 64
// lowercase hex characters (an HMAC-SHA256), a competition id is what
// state.ValidateCompetitionID accepts, and a header past 32 entries is cut
// there. The oldest key goes first when a 33rd is taken.
const KEY_RE = /^[0-9a-f]{64}$/;
const COMPETITION_ID_RE = /^[A-Za-z0-9_-]{1,64}$/;
export const MAX_REVEAL_KEYS = 32;

// The keys held, oldest first, as {compId, key}. Read from storage once, then
// kept here; null until the first read.
let _held = null;
const _listeners = new Set();

export function isRevealKey(key) {
  return typeof key === "string" && KEY_RE.test(key);
}

export function isRevealCompetitionId(compId) {
  return typeof compId === "string" && COMPETITION_ID_RE.test(compId);
}

// The well-formed entries of a stored value, without repeats, capped.
function _wellFormed(list) {
  const out = [];
  const seen = new Set();
  for (const e of Array.isArray(list) ? list : []) {
    if (!e || !isRevealCompetitionId(e.compId) || !isRevealKey(e.key)) continue;
    const id = `${e.compId}:${e.key}`;
    if (seen.has(id)) continue;
    seen.add(id);
    out.push({ compId: e.compId, key: e.key });
  }
  return out.slice(-MAX_REVEAL_KEYS);
}

function _read() {
  if (_held) return _held;
  let stored = null;
  try {
    const raw = window.localStorage.getItem(LS_REVEAL_KEYS);
    stored = raw ? JSON.parse(raw) : null;
  } catch (_e) { /* storage unavailable or unreadable: nothing held */ }
  _held = _wellFormed(stored);
  return _held;
}

function _tell() {
  for (const fn of _listeners) {
    try { fn(); } catch (err) { console.error("reveal key listener failed:", err); }
  }
}

// rememberRevealKey keeps compId's key on this phone. True when the key is
// well formed (held now, whether or not it was before); false, holding
// nothing, for a malformed one. A key the phone already holds is kept where
// it is.
export function rememberRevealKey(compId, key) {
  if (!isRevealCompetitionId(compId) || !isRevealKey(key)) return false;
  const held = _read();
  if (held.some((e) => e.compId === compId && e.key === key)) return true;
  _held = _wellFormed([...held, { compId, key }]);
  try {
    window.localStorage.setItem(LS_REVEAL_KEYS, JSON.stringify(_held));
  } catch (_e) { /* storage unavailable: held for this page only */ }
  _tell();
  return true;
}

// The keys held for one competition.
export function revealKeysFor(compId) {
  return _read().filter((e) => e.compId === compId).map((e) => e.key);
}

// The X-Reveal-Key header value for every key held, "<compId>:<key>,...", or
// "" when none is.
export function revealKeysHeader() {
  return _read().map((e) => `${e.compId}:${e.key}`).join(",");
}

// onRevealKeysChange calls fn whenever the keys held change, here or in
// another tab of this browser. Returns the unsubscribe.
export function onRevealKeysChange(fn) {
  _listeners.add(fn);
  return () => { _listeners.delete(fn); };
}

// Another tab took a key: read the store again on the next ask.
if (typeof window !== "undefined" && typeof window.addEventListener === "function") {
  window.addEventListener("storage", (e) => {
    if (e && e.key !== LS_REVEAL_KEYS && e.key !== null) return;
    _held = null;
    _tell();
  });
}
