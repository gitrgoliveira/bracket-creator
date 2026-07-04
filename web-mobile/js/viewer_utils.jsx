// viewer_utils.jsx: utility helpers extracted from viewer.jsx (mp-pxxc step 1).
// Pure file split: no behaviour change.
//
// Sharing model: esbuild compiles each .jsx to dist/.js individually (no bundle);
// the server rewrites `/dist/X.jsx` → the compiled `X.js`, so ES `import "./X.jsx"`
// specifiers resolve in the browser. viewer.js is the SOLE viewer entry script in
// index.html and imports this module: do NOT give it its own <script type="module">
// tag, or the browser fetches it under a second URL (.js?v=N vs .jsx) and evaluates
// it twice (double-load; same class as mp-zd1v).
//
// pool_ids.jsx is a leaf module (no imports, no side effects), so importing it
// here does not introduce a load-order dependency or a double-eval risk.
import { isSupplementaryBout } from './pool_ids.jsx';

// TermV: kendo-glossary tooltip wrapper. Lazy lookup so the script
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

// leagueAwareLabel: the single source of truth for the pool-vs-league heading.
// A league is one round-robin table spanning the whole roster, so calling it
// "Pool A" is wrong; show "League table" instead. Non-league formats fall back
// to the pool name (or the given fallback when that's empty).
export const leagueAwareLabel = (compFormat, poolName, fallback = "") =>
  compFormat === "league" ? "League table" : (poolName || fallback);
export const poolLabel = (m) => leagueAwareLabel(m.compFormat, m.poolName);
// Publish to window so viewer_watchlist.js (and the admin scoring/shiaijo/pools
// surfaces) can read these at RENDER time without an explicit import. Load order
// is irrelevant: each consumer reads the global when it renders, by which point
// viewer.js: which evaluates this module: has run.
if (typeof window !== 'undefined') {
  window.poolLabel = poolLabel;
  window.leagueAwareLabel = leagueAwareLabel;
}

// Lazy window proxy for the shared DD-MM-YYYY date comparator. window.compareDmy
// is attached by admin_helpers.jsx, whose <script> tag loads AFTER viewer.js:
// safe because this proxy is only invoked at render time (after every module tag
// has executed). Kept here so both viewer_schedule.jsx and viewer_home.jsx import
// a single source rather than re-declaring it.
export const compareDmy = (a, b) => window.compareDmy(a, b);

export function compMatches(c) {
  const out = [];

  // Setup status: no public data yet: draw is admin-only preview.
  // draw-ready is allowed through: pool/bracket structure is already
  // persisted and returned by handlers_viewer.go unconditionally, so
  // spectators can see the pool draw before the first match is called.
  if (!c.status || c.status === "setup") return out;

  const POOL_ID_RE = /^(.+?)(?:-DH-\d+|-TB-\d+|-\d+)$/;
  const rawPoolMatches = c.poolMatches || (c.pools ? c.pools.flatMap(p => (p.matches || []).map(m => ({ ...m, phase: "pool", poolName: p.poolName || p.name, phaseName: p.poolName || p.name }))) : []);
  // Pool supplementary bouts (daihyosen "Pool X-DH-N" and tiebreaker
  // "Pool X-TB-N") are representative bouts scored as individual matches even in
  // team competitions: override compKind/teamSize/compEngi so all isTeam checks
  // (compKind === "team" || teamSize > 0) evaluate false, routing to the
  // individual ScoreEditorModal and rendering individual match UI.
  // Flat poolMatches from the viewer API don't carry phase/poolName; derive them
  // from the match ID (e.g. "Pool A-0" → poolName "Pool A") when absent.
  rawPoolMatches.forEach(m => {
    const isRepBout = isSupplementaryBout(m.id || "");
    const derivedPool = m.poolName || (POOL_ID_RE.exec(m.id || "") || [])[1] || "";
    // Spread `...m` FIRST, then the derived fields: flat viewer-API poolMatches
    // omit (or null) phase/poolName/phaseName, so if `...m` came last it would
    // clobber the derived pool name with undefined. derivedPool already prefers
    // m.poolName when present, so putting it last is both safe and authoritative.
    out.push({ ...m, phase: "pool", poolName: derivedPool, phaseName: derivedPool, compId: c.id, compName: c.name, compFormat: c.format, compKind: isRepBout ? "" : c.kind, teamSize: isRepBout ? 0 : c.teamSize, compEngi: isRepBout ? false : !!c.engi });
  });

  // mp-9dz: a preview bracket on a mixed source carries pool-origin
  // placeholders ("Pool A-1st") with assigned scheduled times. It must
  // NOT contribute to the global match list that feeds Find-My-Matches /
  // Watchlist / schedule / TV displays: those treat every bracket match
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
    // (useTeamLineups) need not parse the label: now a bracket-size string
    // ("Round 16") that a "Round N"→N-1 parse would misread as round 15.
    roundIndex: ri,
    compId: c.id,
    compName: c.name,
    compFormat: c.format,
    compKind: c.kind,
    teamSize: c.teamSize,
    compEngi: !!c.engi,
  })));

  // Bronze (3rd-place) playoff: a sibling of bracket.rounds (naginata only),
  // so the rounds loop above does not cover it. Surface it as a first-class
  // bracket match so it appears in the shiaijo court queue / find-my-matches /
  // schedule alongside the final (the two are conventionally run on the same
  // shiaijo). roundIndex sits one past the final so groupQueueMatches gives it
  // its own "3rd Place" group rather than folding it into the final's.
  const bronze = !isPreviewBracket && c.bracket && c.bracket.thirdPlaceMatch;
  if (bronze) {
    out.push({
      ...bronze,
      phase: "bracket",
      round: "3rd Place",
      phaseName: "3rd Place",
      roundIndex: rounds.length,
      compId: c.id,
      compName: c.name,
      compFormat: c.format,
      compKind: c.kind,
      teamSize: c.teamSize,
      compEngi: !!c.engi,
    });
  }

  // Add poolPosition (1-based) and poolCount (total RR matches in this pool)
  // to each pool match so the eyebrow and queue rows can show "Match N of M"
  // (AC2). Matches are ordered by their natural sort within the pool group;
  // poolCount is the number of REGULAR round-robin matches with the same
  // poolName, which equals N*(N-1)/2 for a pool of N players. Tiebreak
  // ("-TB-") and pool-daihyosen ("-DH-") bouts are NOT part of the RR schedule,
  // so they are excluded here: counting them would inflate poolCount and shift
  // "Match N of M" for the regular bouts. Such bouts simply get no position.
  const NON_RR_ID_RE = /-(?:TB|DH)-\d+$/;
  // Null-prototype: poolName is user-controlled, so a pool literally named
  // "__proto__"/"constructor" must not pollute the map or collide with
  // inherited keys (matches the Object.create(null) pattern used elsewhere).
  const poolMatchesByPool = Object.create(null);
  for (const m of out) {
    if (m.phase !== "pool") continue;
    if (NON_RR_ID_RE.test(m.id || "")) continue;
    const pn = m.poolName || "";
    if (!poolMatchesByPool[pn]) poolMatchesByPool[pn] = [];
    poolMatchesByPool[pn].push(m);
  }
  for (const pms of Object.values(poolMatchesByPool)) {
    const count = pms.length;
    pms.forEach((m, i) => {
      m.poolPosition = i + 1;
      m.poolCount = count;
    });
  }

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
    // check would turn arbitrary operator-entered strings into a mailto href:
    // a leading-dot domain ("a@.evil.com"), or mailto query/fragment injection
    // ("foo@x.com?bcc=attacker@evil.org" pre-fills the visitor's mail client).
    // The label-based pattern excludes whitespace, a second "@", a leading-dot
    // domain, and the ?#&= chars; anything that fails renders as plain text.
    // Classes don't overlap the literal ".", so there is no catastrophic backtracking.
    if (/^[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}$/.test(value)) return <a href={"mailto:" + value} className="tournament-info__link">{value}</a>;
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
          <dd className="tournament-info__value">{isHttpURL(t.rulesURL) ? <a href={t.rulesURL} className="tournament-info__link" target="_blank" rel="noopener noreferrer">{t.rulesURL.replace(/^https?:\/\//i, "")}</a>: t.rulesURL}</dd>
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
