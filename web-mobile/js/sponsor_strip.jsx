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

  return React.createElement(
    "div",
    { className: "sponsor-strip sponsor-strip--" + v, role: "complementary", "aria-label": "Sponsors" },
    sponsors.map((s, i) => {
      const img = React.createElement("img", {
        key: "img-" + i,
        src: "/api/sponsors/" + s.file,
        alt: s.name || "Sponsor",
        className: "sponsor-strip__logo",
        // Defensive: hide a broken logo rather than letting one missing
        // file blow out the row layout. The handler returns 404 if the
        // file is gone; the rest of the strip stays intact.
        onError: (e) => { e.currentTarget.style.display = "none"; },
      });
      if (interactive && s.link) {
        return React.createElement(
          "a",
          {
            key: "a-" + i,
            href: s.link,
            target: "_blank",
            rel: "noopener noreferrer",
            className: "sponsor-strip__link",
            title: s.name,
          },
          img,
        );
      }
      return React.createElement(
        "span",
        { key: "span-" + i, className: "sponsor-strip__item", title: s.name },
        img,
      );
    }),
  );
}

window.SponsorStrip = SponsorStrip;

// ES export for the vitest suite. Mirrors admin_announcement.jsx pattern.
export { SponsorStrip };
