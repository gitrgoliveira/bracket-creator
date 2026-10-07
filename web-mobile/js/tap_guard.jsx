// tap_guard.jsx: the ONE owner of "is this pointer tap the bounce of the
// previous one" (bc-dtfn).
//
// A thumb that bounces on a touchscreen delivers two complete clicks about
// 16-44ms apart. Preact renders on a microtask, so the second click always
// lands on a fresh render: a two-tap arm-then-confirm guard reads itself as
// armed and commits, and a scoring button records its value twice. Nothing in
// the click itself tells a bounce from a deliberate second tap except TIME, so
// every guard against it asks here rather than keeping its own clock.
//
// Keyboard activation is never a bounce: a click synthesized from Enter or
// Space carries detail === 0, and keyboard input is never swallowed (bc-kbrw).
//
// A leaf with no imports: every consumer ES-imports it directly, and there is
// no window mirror (the write_result.jsx pattern). useArmedConfirm and
// useOpenedTapGuard read the React global at CALL time, as ui.jsx's hooks
// do, so importing this module needs no React.

// Longer than a double tap's gap, shorter than a deliberate second tap. Two
// real ippon calls can never arrive this close: the shushin stops play for
// each one. bc-kbrw's value, now with one home.
export const TAP_BOUNCE_MS = 400;

// isPointerTap: a click from a pointer, as opposed to one synthesized from the
// keyboard (detail 0).
export const isPointerTap = (ev) => ev.detail !== 0;

const KEYLESS = "";

// withinBounce: is now inside the bounce window that opened at `at`? The one
// copy of the time test. A negative gap means the wall clock was stepped back
// (the device corrected its time), and is never a bounce: counted as one, it
// swallowed every tap until the clock caught up with the stamp.
function withinBounce(at) {
  const gap = Date.now() - at;
  return gap >= 0 && gap < TAP_BOUNCE_MS;
}

function stamps(ref) {
  if (!(ref.current instanceof Map)) ref.current = new Map();
  return ref.current;
}

// stampTap: record that a tap was ACCEPTED now. `key` scopes the stamp (one
// per side, per bout row, per match), so a tap on something else is never
// read as this one's bounce.
export function stampTap(ref, key = KEYLESS) {
  stamps(ref).set(key, Date.now());
}

// clearTap: forget an accepted tap, so the next one is never read as its
// bounce (e.g. after the operator took the mark back off).
export function clearTap(ref, key = KEYLESS) {
  stamps(ref).delete(key);
}

// tapIsBounce: is this click a pointer tap landing within TAP_BOUNCE_MS of the
// last accepted tap on the same key?
export function tapIsBounce(ref, ev, key = KEYLESS) {
  if (!isPointerTap(ev)) return false;
  const at = stamps(ref).get(key);
  return at !== undefined && withinBounce(at);
}

// acceptTap: the check-then-stamp every guarded button runs. False for a
// bounce; otherwise it records the tap as accepted and returns true. One call,
// so a site can never stamp before it checks (which would read every tap as
// its own bounce).
//   onClick={(ev) => { if (!acceptTap(ref, ev, key)) return; act(); }}
export function acceptTap(ref, ev, key = KEYLESS) {
  if (tapIsBounce(ref, ev, key)) return false;
  stampTap(ref, key);
  return true;
}

// swallowBounce: an onClickCapture handler that stops a bounce before it
// reaches any control under the container (bc-kbrw: opening a fought bout
// moves the rows under the finger, so the second tap would land on another).
export const swallowBounce = (ref, key = KEYLESS) => (ev) => {
  if (!tapIsBounce(ref, ev, key)) return;
  ev.stopPropagation();
  ev.preventDefault();
};

// useOpenedTapGuard: a layer opened by a tap (a confirm, a modal, an overlay
// editor) must not take the bounce of that tap, which lands on the fresh
// backdrop one render later (bc-cfbd). `openedRef` goes on the layer's node
// (or is called from a callback ref that already exists) and stamps when it
// mounts; `onClickCapture` goes on the backdrop and swallows a pointer bounce
// within TAP_BOUNCE_MS of that stamp. A confirm swallows every click in its
// layer; `{ backdropOnly: true }` swallows only a click on the backdrop itself,
// for an editor whose controls the operator may use at once.
//   const { openedRef, onClickCapture } = useOpenedTapGuard();
//   <div className="modal-backdrop" ref={openedRef} onClickCapture={onClickCapture}>
export function useOpenedTapGuard({ backdropOnly = false } = {}) {
  const stampRef = React.useRef(null);
  const openedRef = React.useCallback((node) => { if (node) stampTap(stampRef); }, []);
  const onClickCapture = React.useMemo(() => {
    const swallow = swallowBounce(stampRef);
    return backdropOnly ? (ev) => { if (ev.target === ev.currentTarget) swallow(ev); } : swallow;
  }, [backdropOnly]);
  return { openedRef, onClickCapture };
}

// useArmedConfirm: the two-tap commit (Finish, Finish + Start Next, End
// match). The first tap arms; a second tap commits only once the button has
// been armed for TAP_BOUNCE_MS, so the bounce of the arming tap cannot commit.
//   const { armed, setArmed, confirm } = useArmedConfirm();
//   onClick={(ev) => { if (!confirm(ev)) return; commit(); }}
// confirm(ev) arms and returns false on an unarmed button, returns false for a
// bounce, and true otherwise. setArmed(false) disarms as before, and
// setArmed(true) stamps the time. The arming time lives in a ref (null while
// disarmed), so the decision never reads a stale render closure.
export function useArmedConfirm() {
  const [armed, setArmedState] = React.useState(false);
  const armedAtRef = React.useRef(null);
  const setArmed = React.useCallback((next) => {
    armedAtRef.current = next ? Date.now() : null;
    setArmedState(!!next);
  }, []);
  const confirm = (ev) => {
    if (armedAtRef.current === null) { setArmed(true); return false; }
    if (isPointerTap(ev) && withinBounce(armedAtRef.current)) return false;
    return true;
  };
  return { armed, setArmed, confirm };
}
