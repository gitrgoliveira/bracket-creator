// SponsorStrip — renders a horizontal row of sponsor logos. Used on the
// viewer home and on the /display TV / lobby surfaces. See mp-c38.
//
// Props:
//   sponsors  Array<{name, file, link?}> from tournament.sponsors
//   variant   "viewer" | "lobby" | "tv"
//
// The viewer variant wraps logos with a link in <a target="_blank"
// rel="noopener noreferrer">. The lobby and tv variants are passive
// (TVs have no mouse; lobby touch installs shouldn't focus-trap on a
// sponsor logo) — they always render a bare <img>.
//
// Hidden when sponsors is empty or undefined.

function SponsorStrip({ sponsors, variant }) {
  if (!sponsors || sponsors.length === 0) return null;
  const v = variant || "viewer";
  const interactive = v === "viewer";

  return (
    <div
      className={"sponsor-strip sponsor-strip--" + v}
      role="complementary"
      aria-label="Sponsors"
    >
      {sponsors.map((s) => {
        // Defensive: hide a broken logo rather than letting one missing
        // file blow out the row layout. The handler returns 404 if the
        // file is gone; the rest of the strip stays intact.
        const img = (
          <img
            src={"/api/sponsors/" + s.file}
            alt={s.name || "Sponsor"}
            className="sponsor-strip__logo"
            onError={(e) => { e.currentTarget.style.display = "none"; }}
          />
        );
        // Key on s.file (server-generated, unique per upload) so React
        // doesn't reuse the wrong DOM node when a sponsor is deleted —
        // index keys would let sponsor N+1's image flash into sponsor
        // N's slot before the next render flush.
        if (interactive && s.link) {
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
