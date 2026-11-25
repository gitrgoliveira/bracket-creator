// poster_layout.jsx: the ONE layout of a venue poster (bc-qrps), used for the
// preview on the Posters page and for the print sheet. What a poster is lives in
// posters_model.jsx; the styles, sizes in cqw so the preview and the paper are
// the same layout at any width, live in css/posters.css.
//
// The QR code is an SVG drawn from the module matrix qr.jsx computes: black
// squares on white with a quiet zone, ONE path for the dark modules and crisp
// edges, whatever the page colours are. A centre logo is an HTML <img> laid over
// a white pad of whole modules (the modules under the pad are left out of the
// path), not an SVG <image>: an <img> can be decode()d before the print dialog
// opens, and an SVGImageElement cannot.

import { qrFits, qrMatrix } from "./qr.jsx";
import {
  CODE_TOO_LONG_SENTENCE,
  POSTER_QUIET_MODULES,
  POSTER_SIZES,
  bandTextColour,
  logoPadModules,
  logoPlacement,
  logoURL,
  paperInkColour,
  posterAddress,
  posterChoices,
  posterPrimary,
  posterSponsors,
  textLength,
} from "./posters_model.jsx";

// The path of the dark modules, as horizontal runs, in a viewBox that holds the
// quiet zone. Modules inside the pad (a square of `pad` whole modules, centred)
// are left out so the logo sits on bare white.
export function codePath({ size, modules }, pad) {
  const q = POSTER_QUIET_MODULES;
  const start = pad > 0 ? Math.floor((size - pad) / 2) : size;
  const end = start + pad;
  const inPad = (r, c) => pad > 0 && r >= start && r < end && c >= start && c < end;
  let d = "";
  for (let r = 0; r < size; r++) {
    let c = 0;
    while (c < size) {
      if (modules[r * size + c] !== 1 || inPad(r, c)) { c++; continue; }
      const from = c;
      while (c < size && modules[r * size + c] === 1 && !inPad(r, c)) c++;
      d += "M" + (from + q) + " " + (r + q) + "h" + (c - from) + "v1h-" + (c - from) + "z";
    }
  }
  return d;
}

// The size class of a piece of text: a long heading or call to action is set
// smaller, so the longest the server allows still fits the page.
function sizeClass(text, shortMax, mediumMax) {
  const n = textLength(text);
  return n <= shortMax ? "short" : n <= mediumMax ? "medium" : "long";
}

const pct = (n) => Math.round(n * 1000) / 1000 + "%";

function PosterCode({ address, placement, level, fits, logoKey }) {
  if (!fits) {
    return (
      <div className="poster__code poster__code--warning">
        <p className="poster__code-warning" role="note">{CODE_TOO_LONG_SENTENCE}</p>
      </div>
    );
  }
  const matrix = qrMatrix(address, { level });
  const q = POSTER_QUIET_MODULES;
  const total = matrix.size + 2 * q;
  const pad = placement === "centre" ? logoPadModules(matrix.size) : 0;
  const start = Math.floor((matrix.size - pad) / 2) + q;
  // The logo sits a module inside its pad, so the pad shows a white edge.
  const inset = 1;
  return (
    <div className="poster__code" data-level={level} data-size={matrix.size} data-pad={pad}>
      <svg
        className="poster__code-svg"
        viewBox={"0 0 " + total + " " + total}
        role="img"
        aria-label={"QR code for " + address}
        shapeRendering="crispEdges"
      >
        <rect width={total} height={total} fill="#fff" />
        <path d={codePath(matrix, pad)} fill="#000" />
      </svg>
      {pad > 0 && (
        <img
          className="poster__logo-centre"
          src={logoURL(logoKey)}
          alt=""
          style={{
            left: pct(((start + inset) / total) * 100),
            top: pct(((start + inset) / total) * 100),
            width: pct(((pad - 2 * inset) / total) * 100),
            height: pct(((pad - 2 * inset) / total) * 100),
          }}
        />
      )}
    </div>
  );
}

// Poster: one poster, at whatever width its container gives it (aspect ratio of
// A4 portrait).
//   tournament  the tournament (name, theme, courts, sponsors, publicURL, posters)
//   poster      one entry of posterList(tournament)
//   choices     posterChoices(...), or a draft's; read from the tournament when absent
//   logoKey     cache key for the logo address (see logoURL)
// A sponsor file that fails to load would print as a broken-image icon: hide it,
// and the whole strip when it was the last one showing (as SponsorStrip does).
// The strip is keyed on its file list, so a new list remounts it clean.
function hideFailedSponsor(e) {
  const logo = e.currentTarget;
  logo.style.display = "none";
  const strip = logo.parentElement;
  if (strip && [...strip.children].every((c) => c.style.display === "none")) {
    strip.style.display = "none";
  }
}

export function Poster({ tournament, poster, choices, logoKey }) {
  const t = tournament || {};
  const c = choices || posterChoices(t, poster);
  const isCourt = poster.kind === "court";
  const address = posterAddress(t, poster);
  const lp = logoPlacement(t, poster, c);
  const primary = posterPrimary(t);
  const sponsors = posterSponsors(t, c);
  const fits = qrFits(address, { level: lp.level });
  const logoAbove = lp.placement === "above";

  const style = {
    "--poster-primary": primary,
    "--poster-ink": paperInkColour(primary),
    "--poster-band-ink": bandTextColour(primary),
    "--poster-code": (isCourt ? POSTER_SIZES.courtCode : POSTER_SIZES.viewerCode) + "cqw",
    "--poster-letter": POSTER_SIZES.courtLetter + "cqw",
    "--poster-logo-above": (isCourt ? POSTER_SIZES.courtLogoAbove : POSTER_SIZES.viewerLogoAbove) + "cqw",
  };

  return (
    <section
      className={"poster poster--" + poster.kind}
      data-poster={poster.key}
      data-placement={lp.placement}
      aria-label={poster.label + " poster"}
      style={style}
    >
      <div className="poster__sheet">
        <header className="poster__band">
          <span className="poster__tournament">{t.name}</span>
        </header>
        <div className="poster__body">
          {isCourt && (
            <div className="poster__court">
              <span className="poster__court-label">Shiaijo</span>
              <span className="poster__court-letter">{poster.letter}</span>
            </div>
          )}
          <div className="poster__titles">
            <h2 className={"poster__heading poster__heading--" + sizeClass(c.headingText, 28, 52)}>{c.headingText}</h2>
            {c.callToActionText && (
              <p className={"poster__cta poster__cta--" + sizeClass(c.callToActionText, 50, 80)}>{c.callToActionText}</p>
            )}
          </div>
          {logoAbove && lp.offered && (
            <img className="poster__logo-above" src={logoURL(logoKey)} alt="Tournament logo" />
          )}
          <PosterCode address={address} placement={lp.placement} level={lp.level} fits={fits} logoKey={logoKey} />
          <p className="poster__address">{address}</p>
        </div>
        {sponsors.length > 0 && (
          <div
            key={sponsors.map((s) => s.file).join("|")}
            className="poster__sponsors"
            role="complementary"
            aria-label="Sponsors"
            data-count={sponsors.length}
            style={{ "--poster-sponsors": sponsors.length }}
          >
            {sponsors.map((s) => (
              <img
                key={s.file}
                className="poster__sponsor"
                src={"/api/sponsors/" + s.file}
                alt={s.name || "Sponsor"}
                onError={hideFailedSponsor}
              />
            ))}
          </div>
        )}
      </div>
    </section>
  );
}
