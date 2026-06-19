// viewer_utils.jsx — utility helpers extracted from viewer.jsx (mp-pxxc step 1).
// Pure file split: no behaviour change.
//
// Sharing model: files are compiled individually by esbuild and loaded as
// ordered <script type="module"> tags. This file MUST be listed in index.html
// BEFORE viewer_watchlist.js and viewer.js.

// TermV — kendo-glossary tooltip wrapper. Lazy lookup so the script
// load order between glossary.jsx and viewer.jsx doesn't matter.
// U1 / glossary.md.
export function TermV(props) {
  if (typeof window !== 'undefined' && window.Term) {
    return React.createElement(window.Term, props, props.children);
  }
  return React.createElement('span', null, props.children);
}

export function competitionKindLabel(c) {
  const base = c.kind === "team" ? "Teams" : "Individual";
  if (c.gender === "M") return `${base} · Men`;
  if (c.gender === "F") return `${base} · Women`;
  return base;
}

export const poolLabel = (m) => m.compFormat === "league" ? m.compName : m.poolName;
// Publish to window immediately so viewer_watchlist.js (loaded after this file)
// can read window.poolLabel at render time.
window.poolLabel = poolLabel;

// Lazy window proxy for the shared DD-MM-YYYY date comparator (defined in
// data.js, loaded before viewer_*.js). Kept here so both viewer_schedule.jsx
// and viewer_home.jsx import a single source rather than re-declaring it.
export const compareDmy = (a, b) => window.compareDmy(a, b);

// Private helper — also kept in viewer.jsx for the usages there.
const isPoolDaihyosenID = id => id.includes('-DH-');

export function compMatches(c) {
  const out = [];

  // Setup status: no public data yet — draw is admin-only preview.
  // draw-ready is allowed through: pool/bracket structure is already
  // persisted and returned by handlers_viewer.go unconditionally, so
  // spectators can see the pool draw before the first match is called.
  if (!c.status || c.status === "setup") return out;

  const POOL_ID_RE = /^(.+?)(?:-DH-\d+|-TB-\d+|-\d+)$/;
  const rawPoolMatches = c.poolMatches || (c.pools ? c.pools.flatMap(p => p.matches.map(m => ({ ...m, phase: "pool", poolName: p.name, phaseName: p.name }))) : []);
  // Pool-daihyosen matches ("Pool X-DH-N") are representative bouts scored as
  // individual matches even in team competitions — override compKind and teamSize
  // so all isTeam checks (compKind === "team" || teamSize > 0) evaluate false,
  // routing to the individual ScoreEditorModal and rendering individual match UI.
  // Flat poolMatches from the viewer API don't carry phase/poolName; derive them
  // from the match ID (e.g. "Pool A-0" → poolName "Pool A") when absent.
  rawPoolMatches.forEach(m => {
    const isDH = isPoolDaihyosenID(m.id || "");
    const derivedPool = m.poolName || (POOL_ID_RE.exec(m.id || "") || [])[1] || "";
    out.push({ phase: "pool", poolName: derivedPool, phaseName: derivedPool, ...m, compId: c.id, compName: c.name, compFormat: c.format, compKind: isDH ? "" : c.kind, teamSize: isDH ? 0 : c.teamSize });
  });

  // mp-9dz: a preview bracket on a mixed source carries pool-origin
  // placeholders ("Pool A-1st") with assigned scheduled times. It must
  // NOT contribute to the global match list that feeds Find-My-Matches /
  // Watchlist / schedule / TV displays — those treat every bracket match
  // as a real, scheduled bout. The viewer aggregate endpoint already
  // strips it server-side; this is defense-in-depth for older caches and
  // any code path that bypasses the aggregator.
  const isPreviewBracket = !!(c.bracket && c.bracket.preview);
  const rounds = (!isPreviewBracket && c.bracket && c.bracket.rounds) ? c.bracket.rounds : (!isPreviewBracket ? (c.bracket || []) : []);
  rounds.forEach((round, ri) => round.forEach((m) => out.push({
    ...m,
    phase: "bracket",
    round: window.roundLabel(ri, rounds.length),
    phaseName: window.roundLabel(ri, rounds.length),
    // Raw 0-based round index alongside the display label so consumers
    // (useTeamLineups) need not parse the label — now a bracket-size string
    // ("Round 16") that a "Round N"→N-1 parse would misread as round 15.
    roundIndex: ri,
    compId: c.id,
    compName: c.name,
    compFormat: c.format,
    compKind: c.kind,
    teamSize: c.teamSize
  })));
  return out;
}

export function tournamentMatches(t) {
  // draw-ready comps contribute their (unscored) draw matches so the
  // "Find my matches" / watchlist / full-schedule views can surface a
  // spectator's upcoming bout once the draw is published. These never
  // appear in the home NOW/Up-next strips: those gate on runningCompIds,
  // which excludes draw-ready (a draw-ready comp is not running).
  return (t.competitions || [])
    .filter(c => c.status && c.status !== "setup")
    .flatMap(compMatches);
}

// Next match to be played in this competition (running first, else first scheduled in time order)
export function currentMatchOf(c) {
  const ms = compMatches(c);
  const running = ms.find((m) => m.status === "running" && window.hasBothSides(m));
  if (running) return running;
  const sched = ms.filter((m) => m.status === "scheduled" && window.hasBothSides(m));
  sched.sort((a, b) => (a.scheduledAt || "99:99").localeCompare(b.scheduledAt || "99:99"));
  return sched[0] || null;
}

export const isHttpURL = (u) => /^https?:\/\//i.test(u);

// linkBase returns the configured publicURL (mp-s1gl) for the given tournament,
// falling back to the current frame's origin when publicURL is not set. This is
// the single source of truth for building externally-shareable links (QR codes,
// viewer share links) so operators can set one canonical URL regardless of what
// address the admin browser is using.
// Guard against opaque-origin contexts (sandboxed iframes) where
// window.location.origin is the literal string "null". Falling back to
// window.location.origin in that case would produce "null/register/..." links,
// so return an empty string instead so callers get a relative path.
export const linkBase = (t) => {
  if (t && t.publicURL) return t.publicURL;
  const origin = window.location.origin;
  return (!origin || origin === "null") ? "" : origin;
};

// isNonPublicOrigin returns true when the given origin looks like a device-local
// or LAN address that won't be reachable by remote attendees. Used to warn the
// operator when publicURL is unset and the fallback origin would produce
// unworkable QR codes / share links (mp-s1gl).
//
// Treats falsy / "null" (opaque-origin sandboxed iframe) as non-public so the
// warning degrades gracefully and never renders "null/register/..." links.
export const isNonPublicOrigin = (origin) => {
  if (!origin || origin === "null") return true;
  let host, hasPort = false;
  try {
    const u = new URL(origin);
    host = u.hostname;
    hasPort = u.port !== "";
  } catch { return true; }
  if (host === "localhost" || host === "0.0.0.0") return true;
  if (host === "::1" || host === "[::1]") return true;  // IPv6 loopback
  if (host.endsWith(".local")) return true;
  if (/^127\./.test(host)) return true;
  if (/^10\./.test(host)) return true;
  if (/^192\.168\./.test(host)) return true;
  if (/^172\.(1[6-9]|2\d|3[01])\./.test(host)) return true;
  if (hasPort) return true;
  return false;
};

// TournamentInfo renders optional public tournament info (mp-ef3) as a
// read-only card on the viewer home page. Returns null when no info fields
// are set so the card is invisible for tournaments that haven't filled them in.
export function TournamentInfo({ tournament }) {
  const t = tournament;
  if (!t.venueAddress && !t.venueMapURL && !t.openingTime && !t.closingTime && !t.awardsNote && !t.rulesURL && !t.infoNotes && !(t.contacts && t.contacts.length > 0)) return null;

  const contactLink = (value) => {
    if (!value) return value;
    if (isHttpURL(value)) return <a href={value} className="tournament-info__link" target="_blank" rel="noopener noreferrer">{value.replace(/^https?:\/\//i, "")}</a>;
    // Only build a mailto: href for a well-formed single-address email. A loose
    // value.includes("@") would turn arbitrary operator-entered strings (e.g.
    // "javascript:alert(1)@x" or values with whitespace) into a mailto href;
    // anything that fails this check renders as plain text instead.
    if (/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(value)) return <a href={"mailto:" + value} className="tournament-info__link">{value}</a>;
    if (/^\+?[\d\s()-]+$/.test(value)) return <a href={"tel:" + value.replace(/[\s()-]/g, "")} className="tournament-info__link">{value}</a>;
    return value;
  };

  return (
    <div className="tournament-info">
      <div className="tournament-info__title">Tournament Info</div>
      <dl className="tournament-info__grid">
        {(t.venueAddress || t.venueMapURL) && <>
          <dt className="tournament-info__label">Venue</dt>
          <dd className="tournament-info__value">
            {t.venueAddress}
            {t.venueMapURL && isHttpURL(t.venueMapURL) && <>{t.venueAddress ? " " : ""}<a href={t.venueMapURL} className="tournament-info__link" target="_blank" rel="noopener noreferrer">View map ↗</a></>}
          </dd>
        </>}
        {(t.openingTime || t.closingTime) && <>
          <dt className="tournament-info__label">Times</dt>
          <dd className="tournament-info__value">
            {t.openingTime && t.closingTime ? `${t.openingTime} – ${t.closingTime}` : t.openingTime ? `Opens ${t.openingTime}` : `Closes ${t.closingTime}`}
          </dd>
        </>}
        {t.awardsNote && <>
          <dt className="tournament-info__label">Awards</dt>
          <dd className="tournament-info__value">{t.awardsNote}</dd>
        </>}
        {t.rulesURL && <>
          <dt className="tournament-info__label">Rules</dt>
          <dd className="tournament-info__value">{isHttpURL(t.rulesURL) ? <a href={t.rulesURL} className="tournament-info__link" target="_blank" rel="noopener noreferrer">{t.rulesURL.replace(/^https?:\/\//i, "")}</a> : t.rulesURL}</dd>
        </>}
        {t.infoNotes && <>
          <dt className="tournament-info__label">Notes</dt>
          <dd className="tournament-info__value">{t.infoNotes}</dd>
        </>}
        {t.contacts && t.contacts.length > 0 && <>
          <dt className="tournament-info__label">Contact</dt>
          <dd className="tournament-info__value">
            {t.contacts.map((ct, i) => (
              <div key={i} className="tournament-info__contact">
                {ct.label && <span className="tournament-info__contact-label">{ct.label}:</span>}
                {" "}{contactLink(ct.value)}
              </div>
            ))}
          </dd>
        </>}
      </dl>
    </div>
  );
}
