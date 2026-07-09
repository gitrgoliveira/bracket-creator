// SponsorStrip: renders a vertical stack of full-width sponsor banners on the
// viewer home page ONLY. See mp-c38. The /display TV and lobby surfaces
// intentionally do NOT show sponsors (organiser decision: keep the wall
// boards focused on match info), so there is no variant prop. Banner footprint
// (max width + max height) is capped responsively in CSS so an arbitrary
// user-uploaded image can never dominate the viewer page.
//
// Props:
//   sponsors  Array<{name, file, link?}> from tournament.sponsors
//
// A sponsor with a link is wrapped in <a target="_blank"
// rel="noopener noreferrer">; without one it renders a bare <img>.
//
// Hidden when sponsors is empty or undefined.

function SponsorStrip({ sponsors }) {
  if (!sponsors || sponsors.length === 0) return null;

  return (
    <div
      className="sponsor-strip sponsor-strip--viewer"
      role="complementary"
      aria-label="Sponsors"
    >
      {sponsors.map((s) => {
        // Key on s.file (server-generated, unique per upload) so React
        // doesn't reuse the wrong DOM node when a sponsor is deleted:
        // index keys would let sponsor N+1's image flash into sponsor
        // N's slot before the next render flush.
        //
        // onError hides the *wrapper* element (a or span), not just the
        // img. Hiding only the img leaves the wrapper as an empty flex
        // item that still occupies a gap in the row layout. Climbing to
        // the parentElement removes both the gap and the broken image.
        // If that was the LAST visible banner, collapse the whole strip too,
        // otherwise the container's border-top + padding render as a
        // mysterious empty band when every sponsor image fails to load.
        const handleError = (e) => {
          const wrapper = e.currentTarget.parentElement;
          if (!wrapper) return;
          wrapper.style.display = "none";
          const strip = wrapper.parentElement;
          if (strip && ![...strip.children].some((c) => c.style.display !== "none")) {
            strip.style.display = "none";
          }
        };
        const img = (
          <img
            src={"/api/sponsors/" + s.file}
            alt={s.name || "Sponsor"}
            className="sponsor-strip__logo"
            loading="lazy"
            onError={handleError}
          />
        );
        if (s.link) {
          return (
            <a
              key={s.file}
              href={s.link}
              target="_blank"
              rel="noopener noreferrer"
              className="sponsor-strip__link"
              title={s.name}
            >
              {img}
            </a>
          );
        }
        return (
          <span key={s.file} className="sponsor-strip__item" title={s.name}>
            {img}
          </span>
        );
      })}
    </div>
  );
}

window.SponsorStrip = SponsorStrip;

// ES export for the vitest suite. Mirrors admin_announcement.jsx pattern.
export { SponsorStrip };
