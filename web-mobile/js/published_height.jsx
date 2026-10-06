// published_height.jsx: the ONE owner of "an element's height, published as a
// CSS custom property while it is mounted" (bc-tmfd).
//
// A sticky bar covers whatever the page scrolls under it, and CSS cannot read an
// element's height, so anything sized to a bar (the pinned team header sitting
// under the topbar, the scroll margin that keeps a focused control clear of the
// bars) is told the height: --topbar-stack-h by AdminTopbar, --team-pin-h and
// --team-dock-h by the team score sheet.
//
// Measured at once (a ResizeObserver first fires after paint, which would leave
// the first frame unsized), then kept current by the observer, because a bar
// grows with a connection alert, the running strip, a correction prompt or a
// banner. The property is removed when the bar goes.
//
// A leaf with no imports: every consumer ES-imports it directly.

// Publishes el's border-box height as the custom property `prop` on `host` (the
// element whose scroll or layout the bar affects) and returns the function that
// stops it, which also removes the property.
export function publishHeight(el, host, prop) {
  const publish = () => host.style.setProperty(prop, `${el.getBoundingClientRect().height}px`);
  publish();
  const ro = typeof ResizeObserver === 'function' ? new ResizeObserver(publish) : null;
  if (ro) ro.observe(el);
  return () => {
    if (ro) ro.disconnect();
    host.style.removeProperty(prop);
  };
}
