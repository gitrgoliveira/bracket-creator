// viewer_reveal.jsx: the page a reveal link opens (bc-hnam).
//
// A competition that hides its competitors' names from the public pages can
// hand out reveal links, /reveal/<competition>#<key>: one for the whole
// competition, or one per competitor in the QR of their tag. The key rides in
// the fragment, so it never reaches a server log. takeRevealLink, which App
// calls once before its first render, keeps the key on this phone
// (reveal_key.jsx) and removes the fragment from the address bar, so the key
// is not left in the history or in a link copied from it. The page then reads
// the competition with the key (App's detail read, which sends every key the
// phone holds) and:
//   - names shown (a competition link, or a competition that does not hide
//     names): opens the competition;
//   - a per-person link: follows the competitor whose name it shows (for a
//     team, the team whose members it names) and opens the competition;
//   - nothing shown: says so (the link expired, or the organiser switched the
//     links off) and offers the competition by numbers.
// The server judges the key on every read; nothing here decides what a key
// may reveal.

import { rememberRevealKey, revealKeysFor } from './reveal_key.jsx';
import { nameHidden, numberOf } from './competitor_identity.jsx';
import { squadMemberLabel } from './squad_member_label.jsx';
import { addPlayerToWatchlist, useWatchlist } from './viewer_watchlist_core.jsx';

const { useEffect, useRef } = React;

// What the server prints for a member it cannot place (public_names.go).
const MASKED_UNKNOWN = "?";

function safeDecode(s) {
  try {
    return decodeURIComponent(s);
  } catch (_e) {
    return s;
  }
}

// takeRevealLink: when the page was opened on a reveal link, keep its key and
// take the fragment off the address bar (the path stays). Returns
// {compId, stored} for a reveal link (stored: a well-formed key was taken),
// null for any other page. Call it once, before the first render.
export function takeRevealLink(loc, hist) {
  const where = loc || (typeof window !== "undefined" ? window.location : null);
  const history = hist || (typeof window !== "undefined" ? window.history : null);
  if (!where) return null;
  const m = /^\/reveal\/([^/]+)\/?$/.exec(where.pathname || "");
  if (!m) return null;
  const compId = safeDecode(m[1]);
  const key = String(where.hash || "").replace(/^#/, "").trim().toLowerCase();
  const stored = rememberRevealKey(compId, key);
  if (where.hash && history && typeof history.replaceState === "function") {
    try {
      history.replaceState(history.state, "", `${where.pathname}${where.search || ""}`);
    } catch (_e) { /* the address bar keeps the fragment; the key is held anyway */ }
  }
  return { compId, stored };
}

// A team member's name the masked read did not replace: not empty, not the
// member's label, not the "?" of a member the mask could not place.
function memberNamed(members, teamNumber) {
  return (Array.isArray(members) ? members : []).some((mb) => {
    const name = String((mb && mb.name) || "").trim();
    if (!name || name === MASKED_UNKNOWN) return false;
    return name !== squadMemberLabel(teamNumber, mb.index);
  });
}

// revealedRecords: the competitors a masked read showed by name anyway, which
// are the ones this phone's per-person links reveal: an individual whose name
// is not their number (nameHidden), a team whose members carry names (a
// team's name always shows, so its members say whether it is revealed).
// [] for a read that hides nothing.
export function revealedRecords(detail) {
  if (!detail || !detail.namesHidden) return [];
  const cfg = detail.config || {};
  const players = (Array.isArray(detail.players) && detail.players.length ? detail.players : cfg.players) || [];
  // state.Competition.IsTeam: a team competition is kind "team" or has a team size.
  const team = cfg.kind === "team" || Number(cfg.teamSize) > 0;
  const members = detail.teamMembers || {};
  return players.filter((p) => {
    if (!p || !p.id) return false;
    return team ? memberNamed(members[p.id], numberOf(p)) : !nameHidden(p);
  });
}

// revealVerdict: what the page does with what it has. `detail` is App's read
// of the competition (null when there is none), `landing` takeRevealLink's
// answer when the page was opened on the link.
export function revealVerdict(compId, landing, detail) {
  const held = revealKeysFor(compId).length > 0;
  if (landing && landing.compId === compId && !landing.stored && !held) return { kind: "incomplete" };
  if (!detail) return { kind: "missing" };
  if (!detail.namesHidden) return { kind: "open" };
  const revealed = revealedRecords(detail);
  if (revealed.length) return { kind: "follow", revealed };
  return { kind: "nothing" };
}

export function RevealPage({ compId, landing, competition, onOpen, onHome }) {
  const [, setWatchlist] = useWatchlist();
  const verdict = revealVerdict(compId, landing, competition);
  const openedRef = useRef(false);
  useEffect(() => {
    if (openedRef.current) return;
    if (verdict.kind !== "open" && verdict.kind !== "follow") return;
    openedRef.current = true;
    if (verdict.kind === "follow") {
      setWatchlist((prev) => verdict.revealed.reduce(addPlayerToWatchlist, prev));
    }
    onOpen(compId);
  });

  if (verdict.kind === "open" || verdict.kind === "follow") {
    return <window.LoadingSpinner text="Opening the competition…" />;
  }
  const copy = {
    incomplete: {
      title: "This link is not complete",
      body: "The link or QR code did not carry its key, so names stay hidden on this phone. Scan the QR code again, or follow the competition by competitor numbers.",
    },
    nothing: {
      title: "This link shows no names now",
      body: "It may have expired, or the organiser may have switched the links off. You can still follow the competition by competitor numbers.",
    },
    missing: {
      title: "The competition could not be opened",
      body: "Check the connection and open the link again. If it still does not open, check the link with the organiser.",
    },
  }[verdict.kind];
  return (
    <div className="page" data-testid="reveal-page">
      <div className="card card--pad-lg">
        <h2>{copy.title}</h2>
        <p>{copy.body}</p>
        <div className="row" style={{ gap: 8, flexWrap: "wrap" }}>
          {verdict.kind !== "missing" && (
            <button type="button" className="btn btn--primary" onClick={() => onOpen(compId)}>Open the competition</button>
          )}
          <button type="button" className="btn" onClick={onHome}>Tournament home</button>
        </div>
      </div>
    </div>
  );
}
