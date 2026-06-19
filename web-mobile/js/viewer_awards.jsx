// Awards components extracted from viewer.jsx (mp-pxxc step 6).
// Pure file split — no behaviour change.

import { WinnerBadge } from './viewer_standings.jsx';
import { competitionKindLabel } from './viewer_utils.jsx';

const { useState, useMemo, useRef: useRefV, useEffect } = React;

// deriveAwards returns up to four placements for the closing ceremony per
// FIK convention: 1st, 2nd, and two 3rds (semi-final losers — no bronze match).
// Returns [] when no podium data exists yet.
// `nameToPlayer` is an optional Map(name → {name, dojo}) to enrich bracket
// entries with dojo info; missing names fall back to {name, dojo: ""}.
// Bracket match fields (sideA/sideB/winner) may be either plain strings (raw
// backend payload) or normalized objects ({id, name, dojo}) as produced by
// normalizeMatch() in api_serializers.jsx — both shapes are handled.
// `standings` may be either a flat array (Swiss-shape) or an object keyed by
// pool name (pools/league shape).
export function deriveAwards(bracket, standings, pools, nameToPlayer) {
  // Extract the name string from a match field that may be a string (raw
  // backend payload) or a normalized object ({id, name, dojo}) produced by
  // normalizeMatch() in api_serializers.jsx.
  const toName = (v) => (v && typeof v === "object" ? v.name || "" : v || "");

  // Enrich a player field with dojo info. If the field is already a normalized
  // object with a dojo, use it directly; otherwise fall back to nameToPlayer.
  const lookup = (v) => {
    const name = toName(v);
    if (!name) return null;
    const fromField = v && typeof v === "object" ? v.dojo : null;
    const p = nameToPlayer && nameToPlayer.get(name);
    return { name, dojo: fromField || (p && p.dojo) || "" };
  };

  // Bracket-based: final + semi-finals. Only used when the final has a
  // decided winner; if it doesn't, fall through to standings below so a
  // pools-only competition that has a TBD-placeholder bracket still shows
  // the podium from its pool standings.
  if (bracket && bracket.rounds && bracket.rounds.length > 0) {
    const finalRound = bracket.rounds[bracket.rounds.length - 1];
    const sfRound = bracket.rounds[bracket.rounds.length - 2] || [];
    const final = finalRound[0];
    if (final && final.winner) {
      const winnerName = toName(final.winner);
      const champion = final.winner;
      const runnerUp = winnerName === toName(final.sideA) ? final.sideB : final.sideA;
      const thirds = sfRound
        .map((m) => {
          if (!m.winner) return null;
          return toName(m.winner) === toName(m.sideA) ? m.sideB : m.sideA;
        })
        .filter(Boolean);
      const slot = (place, side) => {
        const r = lookup(side);
        return r ? { place, ...r } : null;
      };
      return [
        slot(1, champion),
        slot(2, runnerUp),
        slot(3, thirds[0]),
        slot(3, thirds[1]),
      ].filter(Boolean);
    }
  }

  // Standings-based fallback. Two payload shapes are supported:
  //   - Swiss/`/swiss/standings`: a flat array of standings rows.
  //   - Pools/league: an object keyed by poolName → array of rows.
  // We take the top four from the (single) leaderboard; for pools-only with
  // multiple pools we use the first pool (consistent with PoolsViewer's
  // leagueWinner pick).
  let list = null;
  if (Array.isArray(standings)) {
    list = standings;
  } else if (standings && pools && pools.length > 0) {
    list = standings[pools[0].poolName] || [];
  }
  if (list && list.length > 0) {
    const slice = list.slice(0, 4).map((s, i) => ({
      place: i < 3 ? i + 1 : 3,
      name: s.player?.name || "",
      dojo: s.player?.dojo || "",
    }));
    return slice.filter((e) => e.name);
  }

  return [];
}

// bracketHasDecidedFinal: true iff the bracket's last round has a decided final.
export function bracketHasDecidedFinal(bracket) {
  if (!bracket || !bracket.rounds || bracket.rounds.length === 0) return false;
  const finalRound = bracket.rounds[bracket.rounds.length - 1];
  const final = finalRound && finalRound[0];
  return !!(final && final.winner);
}

// resolveCompetitionAwards: the single source of truth for a competition's
// podium. Mixed (Pools + Knockout) is a single competition whose knockout fills
// in place, so its podium is derived from its OWN bracket (the KNOCKOUT result,
// 1/2/3/3 — never pool standings) once the final is decided.
// Returns { state, podium } where state is one of:
//   'final'       — podium is the final result
//   'in-progress' — knockout not yet decided (podium [])
//   'skip'        — linked-playoffs shell (sourceCompID set); caller excludes from results
// fetchers = { fetchCompetitionDetails(id), swissStandings(id)|null }
export async function resolveCompetitionAwards(comp, fetchers) {
  // A linked-playoffs shell (legacy split-comp layout carrying sourceCompID)
  // derives its podium from its source comp, never standalone — drop it so it
  // doesn't appear twice in the results summary. buildAllWinnersPublic filters
  // state==="skip". (The current data model no longer emits sourceCompID, so
  // this is defensive parity with the documented behaviour.)
  if (comp && comp.sourceCompID) {
    return { state: "skip", podium: [] };
  }
  const fmt = comp && comp.format;
  const ntpFrom = (players) => {
    const m = new Map();
    (players || []).forEach((p) => { if (p && p.name) m.set(p.name, p); });
    return m;
  };
  if (fmt === "mixed" || fmt === "playoffs") {
    // Mixed is now a SINGLE competition: its knockout bracket fills in place as
    // pools finish (no separate "- Playoffs" comp). Both mixed and standalone
    // playoffs derive their podium from their OWN bracket once the final is
    // decided; until then the knockout is still in progress.
    const d = await fetchers.fetchCompetitionDetails(comp.id);
    if (bracketHasDecidedFinal(d.bracket)) {
      return { state: "final", podium: deriveAwards(d.bracket, null, null, ntpFrom(d.players)) };
    }
    return { state: "in-progress", podium: [] };
  }
  // league / swiss (and any standings-based) → standings
  const d = await fetchers.fetchCompetitionDetails(comp.id);
  let standings = d.standings;
  if (fmt === "swiss" && fetchers.swissStandings) {
    try { standings = await fetchers.swissStandings(comp.id); } catch (_) { /* keep detail standings */ }
  }
  return { state: "final", podium: deriveAwards(null, standings, d.pools, ntpFrom(d.players)) };
}

const PLACE_STYLE = {
  1: { icon: "🏆", label: "1st Place", accent: "var(--gold, #d4af37)" },
  2: { icon: "🥈", label: "2nd Place", accent: "var(--silver, #c0c0c0)" },
  3: { icon: "🥉", label: "3rd Place", accent: "var(--bronze, #cd7f32)" },
};

export function AwardsView({ c, bracket, standings, pools, players }) {
  const containerRef = useRefV(null);
  const [isFs, setIsFs] = useState(false);
  const isLeague = c?.format === "league";
  const isMixed = c?.format === "mixed";
  // Swiss standings aren't part of the competition-detail payload — they live
  // behind /swiss/standings. Fetch them here when the format is swiss so the
  // Awards tab works for Swiss competitions too.
  const [swissStandings, setSwissStandings] = useState(null);
  // koAwards: undefined = not yet loaded, object = { state, awards } for mixed comps.
  const [koAwards, setKoAwards] = useState(undefined);

  React.useEffect(() => {
    const onFsChange = () => setIsFs(!!document.fullscreenElement);
    document.addEventListener("fullscreenchange", onFsChange);
    return () => document.removeEventListener("fullscreenchange", onFsChange);
  }, []);

  React.useEffect(() => {
    if (c?.format !== "swiss" || !window.API?.swissStandings) return;
    let cancelled = false;
    window.API.swissStandings(c.id)
      .then((data) => { if (!cancelled) setSwissStandings(Array.isArray(data) ? data : []); })
      .catch(() => { if (!cancelled) setSwissStandings([]); });
    return () => { cancelled = true; };
  }, [c?.id, c?.format, c?.swissCurrentRound]);

  // For mixed (pools+knockout) comps, delegate to resolveCompetitionAwards so
  // this view and the All Winners modal share the same resolver (no drift).
  React.useEffect(() => {
    if (!isMixed) return;
    setKoAwards(undefined);
    if (!window.API?.fetchCompetitionDetails) {
      setKoAwards({ state: "in-progress", awards: [] });
      return;
    }
    let cancelled = false;
    const fetchers = { fetchCompetitionDetails: window.API.fetchCompetitionDetails, swissStandings: null };
    resolveCompetitionAwards(c, fetchers)
      .then(({ state, podium }) => {
        if (!cancelled) setKoAwards({ state, awards: podium });
      })
      .catch(() => {
        if (!cancelled) setKoAwards({ state: "in-progress", awards: [] });
      });
    return () => { cancelled = true; };
  }, [c?.id, c?.format]);

  const nameToPlayer = useMemo(() => {
    const m = new Map();
    (players || []).forEach((p) => {
      if (p && p.name) m.set(p.name, p);
    });
    return m;
  }, [players]);

  const effectiveStandings = c?.format === "swiss" ? swissStandings : standings;
  const isSwissLoading = c?.format === "swiss" && swissStandings === null;
  const baseAwards = useMemo(
    () => deriveAwards(bracket, effectiveStandings, pools, nameToPlayer),
    [bracket, effectiveStandings, pools, nameToPlayer]
  );

  // Determine the awards array and state to use for rendering.
  let awards, resolvedState;
  if (isMixed) {
    if (koAwards === undefined) {
      // Still loading — show loading spinner below.
      awards = [];
      resolvedState = "loading";
    } else {
      awards = koAwards.awards;
      resolvedState = koAwards.state;
    }
  } else {
    awards = baseAwards;
    resolvedState = "final";
  }

  const leagueWinner = isLeague ? (awards.find(a => a.place === 1) || null) : null;

  const toggleFs = () => {
    const el = containerRef.current;
    if (!el) return;
    if (document.fullscreenElement) {
      document.exitFullscreen().catch(() => {});
    } else if (el.requestFullscreen) {
      el.requestFullscreen().catch(() => {});
    }
  };

  if (isSwissLoading || resolvedState === "loading") {
    return (
      <div className="empty" data-testid="awards-loading">
        <div className="icon">🏆</div>
        <h3>Loading final standings…</h3>
      </div>
    );
  }

  if (awards.length === 0) {
    const fsAwards = c?.fightingSpiritAwards;
    const hasFsAwards = fsAwards && fsAwards.length > 0;
    if (isMixed && resolvedState === "in-progress") {
      return (
        <div>
          <div className="empty" data-testid="awards-in-progress">
            <div className="icon">🏆</div>
            <h3>Knockout in progress</h3>
          </div>
          {hasFsAwards && <FightingSpiritSection fsAwards={fsAwards} isFs={isFs} />}
        </div>
      );
    }
    return (
      <div>
        <div className="empty" data-testid="awards-empty">
          <div className="icon">🏆</div>
          <h3>Final standings not yet available</h3>
        </div>
        {hasFsAwards && <FightingSpiritSection fsAwards={fsAwards} isFs={isFs} />}
      </div>
    );
  }

  const champion = awards.find(a => a.place === 1) || null;
  const second = awards.find(a => a.place === 2) || null;
  const thirds = awards.filter(a => a.place === 3);

  // Shared header (title + place count + fullscreen toggle) — identical for the
  // league and champion-hero layouts.
  const header = (
    <div className="awards__header">
      <div>
        <div className={`section-title awards__title${isFs ? " awards__title--fs" : ""}`}>
          {c?.name ? `${c.name} — Awards` : "Awards"}
        </div>
        <div className={isFs ? "awards__subtitle--fs" : "awards__subtitle"}>
          Closing ceremony · {awards.length} place{awards.length === 1 ? "" : "s"}
        </div>
      </div>
      <button className="btn btn--sm" onClick={toggleFs} data-testid="awards-fullscreen">
        {isFs ? "Exit fullscreen" : "Fullscreen"}
      </button>
    </div>
  );

  // League format: keep WinnerBadge above and fall back to the classic podium
  // row layout — the hero card doesn't fit the league ceremony well.
  if (isLeague) {
    return (
      <div
        ref={containerRef}
        className="awards"
        data-testid="awards-view"
        style={{
          background: isFs ? "var(--bg)" : "transparent",
          padding: isFs ? 40 : 0,
          minHeight: isFs ? "100vh" : "auto",
        }}
      >
        {header}
        {leagueWinner && <WinnerBadge name={leagueWinner.name} isFs={isFs} testId="league-winner-badge" marginBottom={16} />}
        <div className="podium" style={isFs ? { gap: 24, fontSize: 18 } : null}>
          {awards.map((a, idx) => {
            const style = PLACE_STYLE[a.place] || PLACE_STYLE[3];
            return (
              <div
                key={`${a.place}-${a.name}-${idx}`}
                className={`podium-step podium-step--${a.place}`}
                data-testid={`awards-place-${a.place}-${idx}`}
                style={{ borderTop: `4px solid ${style.accent}` }}
              >
                <div style={{ fontSize: isFs ? 56 : 28 }}>{style.icon}</div>
                <div className="place" style={{ fontSize: isFs ? 18 : 12 }}>{style.label}</div>
                <div className="name" style={{ fontSize: isFs ? 28 : 16 }}>{a.name}</div>
                {a.dojo && (
                  <div className="dojo" style={{ fontSize: isFs ? 16 : 12, color: "var(--ink-3)" }}>{a.dojo}</div>
                )}
              </div>
            );
          })}
        </div>
        {/* Fighting Spirit awards — distinct section, independent of the podium */}
        <FightingSpiritSection fsAwards={c?.fightingSpiritAwards} isFs={isFs} />
      </div>
    );
  }

  // mp-8jbo: Champion-hero podium layout for non-league competitions.
  // 1st place → large gold hero card (top, full width).
  // 2nd place → single centered card in its own row.
  // 3rd places → side-by-side equal-width cards in their own row (kendo has two
  // joint 3rds — both beaten semi-finalists; no 4th, no bronze match).
  return (
    <div
      ref={containerRef}
      className="awards"
      data-testid="awards-view"
      style={{
        background: isFs ? "var(--bg)" : "transparent",
        padding: isFs ? 40 : 0,
        minHeight: isFs ? "100vh" : "auto",
      }}
    >
      {header}

      {/* 1st place — champion hero */}
      {champion && (
        <div
          className="awards-hero"
          data-testid={`awards-place-1-${awards.indexOf(champion)}`}
          style={isFs ? { padding: 32, marginBottom: 20 } : null}
        >
          <div className="awards-hero__crown" style={isFs ? { fontSize: 48 } : null}>{PLACE_STYLE[1].icon}</div>
          <div className="awards-hero__eyebrow" style={isFs ? { fontSize: 16 } : null}>Champion</div>
          <div className="awards-hero__name" style={isFs ? { fontSize: 34 } : null}>{champion.name}</div>
          {champion.dojo && (
            <div className="awards-hero__dojo" style={isFs ? { fontSize: 18 } : null}>{champion.dojo}</div>
          )}
        </div>
      )}

      {/* 2nd place — single centered card */}
      {second && (
        <div className="awards-row awards-row--center">
          <div
            className="podium-step podium-step--2 place--eq"
            data-testid={`awards-place-2-${awards.indexOf(second)}`}
            style={isFs ? { fontSize: 18, padding: "20px 24px" } : null}
          >
            <div style={{ fontSize: isFs ? 40 : 22 }}>{PLACE_STYLE[2].icon}</div>
            <div className="place" style={{ fontSize: isFs ? 16 : 12 }}>{PLACE_STYLE[2].label}</div>
            <div className="name" style={{ fontSize: isFs ? 24 : 16 }}>{second.name}</div>
            {second.dojo && (
              <div className="dojo" style={{ fontSize: isFs ? 14 : 12 }}>{second.dojo}</div>
            )}
          </div>
        </div>
      )}

      {/* 3rd places — side-by-side (kendo: two joint 3rds, no bronze match) */}
      {thirds.length > 0 && (
        <div className="awards-row">
          {thirds.map((a, idx) => (
            <div
              key={`3-${a.name}-${idx}`}
              className="podium-step podium-step--3 place--eq"
              data-testid={`awards-place-3-${awards.indexOf(a)}`}
              style={isFs ? { fontSize: 16, padding: "16px 20px" } : null}
            >
              <div style={{ fontSize: isFs ? 36 : 20 }}>{PLACE_STYLE[3].icon}</div>
              <div className="place" style={{ fontSize: isFs ? 14 : 11 }}>{PLACE_STYLE[3].label}</div>
              <div className="name" style={{ fontSize: isFs ? 20 : 14 }}>{a.name}</div>
              {a.dojo && (
                <div className="dojo" style={{ fontSize: isFs ? 13 : 11 }}>{a.dojo}</div>
              )}
            </div>
          ))}
        </div>
      )}

      {/* Fighting Spirit awards — distinct section, independent of the podium */}
      <FightingSpiritSection fsAwards={c?.fightingSpiritAwards} isFs={isFs} />
    </div>
  );
}

// FightingSpiritSection renders the 🔥 Fighting Spirit subsection inside
// AwardsView. Distinct from the placement podium — never merged into
// deriveAwards/podium logic. Renders nothing when the list is empty/absent.
export function FightingSpiritSection({ fsAwards, isFs }) {
  if (!fsAwards || fsAwards.length === 0) return null;
  return (
    <div
      style={{ marginTop: 24, borderTop: "1px solid var(--line)", paddingTop: 16 }}
      data-testid="fighting-spirit-section"
    >
      <div
        className="section-title"
        style={{ margin: "0 0 12px", fontSize: isFs ? 22 : 15 }}
      >
        🔥 Fighting Spirit
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: 8 }}>
        {fsAwards.map((a, idx) => (
          <div
            key={idx}
            style={{
              padding: isFs ? "14px 20px" : "10px 14px",
              borderRadius: 8,
              background: "var(--accent-soft, #fff7ed)",
              border: "1px solid var(--accent-warm, #fb923c)",
              display: "flex",
              flexDirection: "column",
              gap: 2,
            }}
            data-testid={`fighting-spirit-award-${idx}`}
          >
            <div style={{ fontSize: isFs ? 13 : 11, fontWeight: 600, color: "var(--ink-3)", textTransform: "uppercase", letterSpacing: "0.04em" }}>
              {a.title}
            </div>
            <div style={{ fontSize: isFs ? 22 : 16, fontWeight: 700, color: "var(--ink-1)" }}>
              {a.recipientName}
            </div>
            {a.recipientDojo && (
              <div style={{ fontSize: isFs ? 14 : 12, color: "var(--ink-3)" }}>
                {a.recipientDojo}
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// buildAllWinnersPublic — public-viewer equivalent of admin_shell's
// buildAllWinners. Thin orchestrator: filter completed comps (excluding linked
// playoffs shells whose sourceCompID marks them as driven by a parent mixed
// competition), resolve each through resolveCompetitionAwards.
// Exported to window so AllWinnersView and tests can reach it.
// ---------------------------------------------------------------------------
export async function buildAllWinnersPublic(comps, fetchers) {
  const completed = (comps || []).filter((c) => c.status === "completed" && !c.sourceCompID);
  const results = await Promise.all(
    completed.map(async (comp) => {
      try {
        const { state, podium } = await resolveCompetitionAwards(comp, fetchers);
        return { comp, state, podium };
      } catch (err) {
        return { comp, state: "error", podium: [], error: err?.message || String(err) };
      }
    })
  );
  return results;
}

// ---------------------------------------------------------------------------
// AllWinnersView — full-page public results summary.  Mirrors the admin
// AllWinnersModal rendering but as a full-page view (not a modal), matching
// the ViewerSchedule / GlossaryPage page pattern. Props: { tournament, onBack, tweaks }.
// ---------------------------------------------------------------------------
export function AllWinnersView({ tournament, onBack, tweaks }) {
  const comps = (tournament && tournament.competitions) || [];
  const [viewState, setViewState] = useState({ loading: true, results: [], error: null });

  // Stable signature of every comp's id:status — triggers refetch when any
  // competition completes or its knockout resolves (same pattern as admin modal).
  const compsSig = comps.map((c) => `${c.id}:${c.status}`).join("|");

  useEffect(() => {
    let cancelled = false;
    setViewState((s) => ({ ...s, loading: true }));
    buildAllWinnersPublic(comps, {
      fetchCompetitionDetails: window.API.fetchCompetitionDetails.bind(window.API),
      swissStandings: window.API.swissStandings ? window.API.swissStandings.bind(window.API) : null,
    }).then((results) => {
      if (!cancelled) setViewState({ loading: false, results, error: null });
    }).catch((err) => {
      if (!cancelled) setViewState({ loading: false, results: [], error: err?.message || String(err) });
    });
    return () => { cancelled = true; };
  }, [compsSig]);

  return (
    <div className="viewer">
      <div className="viewer__shell">
        <div className="viewer__head">
          <button className="viewer__back" onClick={onBack} aria-label="Back">←</button>
          <div className="viewer__title-block">
            <div className="viewer__eyebrow">{tournament && tournament.name}</div>
            <div className="viewer__title">Results</div>
            <div className="viewer__sub">All competition placings</div>
          </div>
        </div>
        <div className="viewer__body">
          {viewState.loading && (
            <div style={{ textAlign: "center", color: "var(--ink-3)", padding: "24px 0" }} data-testid="all-winners-loading">
              Loading results…
            </div>
          )}
          {!viewState.loading && viewState.error && (
            <div style={{ color: "var(--red)", padding: "8px 0" }} data-testid="all-winners-error">
              Failed to load results: {viewState.error}
            </div>
          )}
          {!viewState.loading && !viewState.error && viewState.results.length === 0 && (
            <div className="empty" data-testid="all-winners-empty">
              <div className="icon">🏅</div>
              <h3>No completed competitions</h3>
              <div style={{ fontSize: 13 }}>Check back once competitions have finished.</div>
            </div>
          )}
          {!viewState.loading && viewState.results.map(({ comp, state: compState, podium, error: compErr }) => (
            <div key={comp.id} className="card" style={{ padding: "12px 16px", marginBottom: 12 }} data-testid={`all-winners-card-${comp.id}`}>
              <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 8 }}>
                <div style={{ fontWeight: 700, fontSize: 15 }}>{comp.name}</div>
                <div style={{ fontSize: 12, color: "var(--ink-3)", background: "var(--surface-2, #f0f0f0)", borderRadius: 4, padding: "1px 6px" }}>
                  {competitionKindLabel(comp)}
                </div>
              </div>
              {compErr && (
                <div style={{ fontSize: 13, color: "var(--red)" }} data-testid={`all-winners-error-${comp.id}`}>
                  Could not load results: {compErr}
                </div>
              )}
              {!compErr && compState === "in-progress" && (
                <div style={{ fontSize: 13, color: "var(--ink-3)" }} data-testid={`all-winners-inprogress-${comp.id}`}>
                  Knockout in progress
                </div>
              )}
              {!compErr && compState === "final" && podium.length === 0 && (
                <div style={{ fontSize: 13, color: "var(--ink-3)" }}>No results yet</div>
              )}
              {!compErr && compState === "final" && podium.map((a, idx) => {
                const style = PLACE_STYLE[a.place] || PLACE_STYLE[3];
                return (
                  <div
                    key={`${a.place}-${a.name}-${idx}`}
                    style={{ display: "flex", alignItems: "center", gap: 10, padding: "6px 0", borderBottom: idx < podium.length - 1 ? "1px solid var(--line)" : "none" }}
                    data-testid={`all-winners-place-${comp.id}-${a.place}-${idx}`}
                  >
                    <span style={{ fontSize: 22 }}>{style.icon}</span>
                    <span style={{ fontSize: 12, color: "var(--ink-3)", minWidth: 60 }}>{style.label}</span>
                    <div style={{ minWidth: 0 }}>
                      <div style={{ fontWeight: 600, fontSize: 14 }}>{a.name}</div>
                      {tweaks.showDojo && a.dojo && <div style={{ fontSize: 12, color: "var(--ink-3)" }}>{a.dojo}</div>}
                    </div>
                  </div>
                );
              })}
              {/* Fighting Spirit awards: an independent annotation (not the
                  placement podium), shown for any competition that has them
                  regardless of podium state. Self-guards when empty. */}
              <FightingSpiritSection fsAwards={comp.fightingSpiritAwards} isFs={false} />
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

window.buildAllWinnersPublic = buildAllWinnersPublic;
window.AllWinnersView = AllWinnersView;
