// dialog_focus.jsx: the ONE owner of focus going into a layer that opens over the
// page and back to the control that opened it (WCAG 2.4.3, the ARIA dialog pattern).
// The shared Modal (ui.jsx), the court console's confirms, the at-court lineup
// panel and the calendar's move dialog take it from here; DialogHost (ui.jsx),
// which also locks the page's scroll, keeps its own move in and back but asks
// keepTabInside, the one rule for Tab inside a layer.
//
// A leaf with no imports: every consumer ES-imports it directly, and there is no
// window mirror (the tap_guard.jsx pattern). The hook reads the React global at CALL
// time, so importing this module needs no React.

// useDialogFocus: while `open`, focus goes into the layer (a control marked
// autofocus, else its first button, whose Enter is a safe cancel or close), and
// when the layer closes or goes away it goes back to the control that had it as the
// layer opened. `boxRef` is the layer's node.
//   const boxRef = React.useRef(null);
//   useDialogFocus(boxRef, open);
//   <div ref={boxRef} role="dialog" aria-modal="true">
// Neither move scrolls the page (preventScroll): the layer opens over the control that
// was pressed, which is in view. The move in runs on a 0ms timer, as DialogHost's: a
// focus moved during the commit is reset. A layer that already holds focus by then
// keeps it. A control marked autofocus is focused here because Preact only writes
// the attribute, and a browser acts on it only while no control holds focus (the
// opener does): the registration desk's Edit dialog puts the cursor in its name box.
// Focus is not given back to the page itself (a tap on a touchscreen focuses
// nothing) or to a control that has left the page.
// While the layer is open, Tab and Shift+Tab stay inside it (keepTabInside), so a
// keyboard user cannot reach the page the layer covers. Only while focus is in the
// layer: focus in a layer stacked above it (a confirm opened from it) is that
// layer's to keep.
export function useDialogFocus(boxRef, open = true) {
  React.useEffect(() => {
    if (!open) return undefined;
    const opener = document.activeElement;
    const onKeyDown = (e) => {
      const box = boxRef.current;
      if (box && box.contains(document.activeElement)) keepTabInside(box, e);
    };
    document.addEventListener("keydown", onKeyDown, true);
    const timer = setTimeout(() => {
      const box = boxRef.current;
      if (!box || box.contains(document.activeElement)) return;
      const first = box.querySelector("[autofocus]") || box.querySelector("button");
      if (first) first.focus({ preventScroll: true });
    }, 0);
    return () => {
      clearTimeout(timer);
      document.removeEventListener("keydown", onKeyDown, true);
      if (opener && opener !== document.body && document.contains(opener)) opener.focus({ preventScroll: true });
    };
  }, [boxRef, open]);
}

const TABBABLE = 'button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])';

// keepTabInside: on a Tab keydown, wraps focus from the layer's last control to its
// first (Shift+Tab: first, or the layer itself, to last), and holds it when the
// layer has no control. A disabled or hidden control is skipped, as the browser
// skips it.
export function keepTabInside(box, e) {
  if (e.key !== "Tab") return;
  const f = [...box.querySelectorAll(TABBABLE)].filter((el) => !el.disabled && el.offsetParent !== null);
  if (f.length === 0) { e.preventDefault(); return; }
  const first = f[0], last = f[f.length - 1], active = document.activeElement;
  if (e.shiftKey && (active === first || active === box)) { e.preventDefault(); last.focus(); }
  else if (!e.shiftKey && active === last) { e.preventDefault(); first.focus(); }
}
