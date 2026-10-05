// Per-match lineup components extracted from admin_schedule.jsx (mp-d7tl).
// MatchLineupSideEditor (local), MatchLineupPanel.

import { LineupNameInput } from './admin_scoring_shared.jsx';
import { sideLookupKey } from './competitor_identity.jsx';
import { scoreRowMatchLabel } from './pool_ids.jsx';
import { squadRosterEntries, rosterWithoutPlacedElsewhere, memberPlacedElsewhere } from './lineup_resolver.jsx';
import { renameMemberFields } from './lineup_rename.jsx';
import { useLineupForm, LineupSourceLine, LineupProblem, LineupDraftNotice } from './lineup_draft.jsx';

const { useState: useStateA, useEffect: useEffectA, useMemo: useMemoA } = React;

// MatchLineupSideEditor: inline lineup editor for one team side within
// the per-match lineup panel, for a single (compId, teamId, matchId) triple. The
// lineup's state (read, draft, confirmed save, use-the-previous-match's-lineup)
// is useLineupForm's, shared with the Lineups page; the save body is this
// editor's own. `allMatches` is the competition's matches, or a function
// returning them (lineupSourceLabel calls it only for a carried lineup).
// Reuses admin_lineup.jsx's exported helpers (positionsForSize, rosterFor,
// teamIdOf) so there is no duplication of position-label / roster logic.
// The helpers are read lazily on each render so module evaluation order
// does not matter (safe in test/bundler contexts too).
export function MatchLineupSideEditor({ comp, team, match, allMatches, password, showToast }) {
  const teamSize = comp?.teamSize || 5;
  const { positionsForSize: lineupPositionsForSize, rosterFor: lineupRosterFor, teamIdOf: lineupTeamIdOf } = window.AdminLineupHelpers || {};
  const positions = (typeof lineupPositionsForSize === "function")
    ? lineupPositionsForSize(teamSize)
    : [];
  // The team's members as the PRE-SQUAD model stored them, in the roster
  // row's untyped metadata array. Kept only as a fallback tail: see
  // `suggestions` below, which leads with the squad.
  const legacyRoster = (typeof lineupRosterFor === "function")
    ? lineupRosterFor(team)
    : [];
  const teamId = (typeof lineupTeamIdOf === "function")
    ? lineupTeamIdOf(team)
    : (team?.id || team?.name || "");
  const compId = comp?.id || "";
  const matchId = match?.id || "";
  const positionKeys = positions.map(p => p.key);

  // The lineup as read and as shown, where it was saved, the unsaved-picks draft,
  // and giving the match's own lineup up: useLineupForm, shared with the Lineups
  // page. `values` is a name per position, `memberIds` the squad member id backing
  // each one (set directly when the operator picks one of the roster's numbered
  // squad entries; cleared when a position is cleared; a free name typed over it
  // is resolved through the shared resolver by save()). `lineupWarning` is the
  // composed operator-facing warning shown after a SUCCESSFUL save whose
  // squad-member attachment fell short (see doSave): a separate channel from
  // `error`, since the save did not fail.
  const form = useLineupForm({
    compId, teamId, matchId, positionKeys, password,
    matchLabel: match ? scoreRowMatchLabel(match) : "", teamName: team?.name || team?.Name,
  });
  const {
    values, setValues, memberIds, setMemberIds, memberIdsRef, setBaseline,
    error, setError, warning: lineupWarning, setWarning: setLineupWarning,
  } = form;
  // RENAME (bc-dnst, operator decision 2026-09-15): a member's name can be
  // corrected from this panel as well as from the Lineups page (the score sheet
  // stays add-only). `renamingKey` is the position whose member is being renamed;
  // the row swaps its picker for a plain input while it is set. Typing into the
  // picker itself stays a SUBSTITUTION over a named member (a different person),
  // so a correction needs this explicit door.
  const [renamingKey, setRenamingKey] = useStateA(null);
  const [renamingName, setRenamingName] = useStateA("");
  const [renameBusy, setRenameBusy] = useStateA(false);
  const [saving, setSaving] = useStateA(false);

  // bc-pnum gap closure: this team's squad, loaded once so save() can
  // resolve a typed/picked name to its member id (see doSave below) --
  // this panel's picker (LineupNameInput) stays typeable, unlike the
  // Lineups page's strict select-by-id dropdown (AdminLineup): it now
  // ALSO offers every numbered squad entry as a pickable suggestion
  // (bc-dnst, see `suggestions` below), but a freely typed name is still
  // accepted, so a name→id lookup is still needed on that path. Independent
  // of the lineup read (useLineupForm): a squad
  // fetch failure must not block loading OR saving the lineup itself, so
  // it is swallowed and the resolver (window.AdminLineupHelpers.
  // resolveMemberIdsForPositions) simply mints for every name it cannot
  // find against an empty list. bc-cse: `squadUnavailable` records that this
  // happened, so doSave's warning names the real root cause instead of
  // reporting every position the resolver then "failed" to match.
  const [squad, setSquad] = useStateA([]);
  const [squadUnavailable, setSquadUnavailable] = useStateA(false);

  // bc-pnum: the suggestion list reads the SQUAD, because that is where a
  // team's members now live. rosterFor reads the roster row's metadata array,
  // which was their home before this PR moved them out, so a team whose
  // members were entered through the squad UI arrived here looking empty --
  // the operator was told "this team has no registered members" under a full
  // squad, and had to retype every name the app already knew.
  //
  // bc-dnst (operator ruling 2026-09-15, restated in CLAUDE.md's "Team
  // Lineups & Kachinuki"): the list must show every squad slot, blank ones
  // included, by NUMBER alone (squadMemberLabel), so the operator can pick a
  // number and type a name into it -- not just the already-named members.
  // squadEntries below is therefore every member, unfiltered, in index
  // order; only the LEGACY metadata tail below still drops blanks (a bare
  // name string with no number to show is not worth suggesting twice).
  //
  // The metadata array stays as a fallback tail: names entered before the
  // squad existed, or names a squad fetch failure left this session unable
  // to see. The load-time migration folds a team's metadata into a squad
  // keyed by its PARTICIPANT ID, and skips a roster row that has no id --
  // the legacy state this PR's own data-issue notices describe -- so such a
  // team has members in metadata and nothing under its key. And the fetch
  // above can simply fail, where falling back beats suggesting nothing.
  // Migration does not clear metadata, so the fallback still has names to
  // offer in both.
  const teamNumber = team?.number || team?.Number || "";
  // Built by the shared owner (lineup_resolver.jsx), the same list the score
  // sheet's per-row picker shows: every squad slot by number, then the legacy
  // names not already on the squad. Memoised (bc-rvfx): every keystroke in
  // the Rename box re-renders this whole component (renamingName is local
  // state), which used to re-derive this list -- and the per-position roster
  // below -- on every one of those renders even though neither the squad nor
  // the lineup positions had changed.
  const suggestions = useMemoA(
    () => squadRosterEntries({ teamNumber, squad, legacyNames: legacyRoster, lineup: { positions: values } }),
    [teamNumber, squad, legacyRoster, values]
  );
  const squadEntries = useMemoA(() => suggestions.filter(e => typeof e !== "string"), [suggestions]);
  const pickedLabelFor = (posKey) => {
    const id = memberIds[posKey];
    return id ? (squadEntries.find(e => e.id === id)?.label || "") : "";
  };
  // A fixed-order lineup fields each fighter once (shared rule, see
  // rosterWithoutPlacedElsewhere): this panel's lineup-so-far is the local
  // `values` + `memberIds` state, keyed like a lineup's own maps. Precomputed
  // for every position at once, rather than once per call inside the JSX map
  // below, for the same reason `suggestions` above is memoised.
  const rostersByPosition = useMemoA(() => {
    const byKey = {};
    positions.forEach(p => { byKey[p.key] = rosterWithoutPlacedElsewhere(suggestions, { positions: values, memberIds }, p.key); });
    return byKey;
  }, [positions, suggestions, values, memberIds]);
  const rosterForPosition = (posKey) => rostersByPosition[posKey];
  // The member a position holds, when it is a squad member with a name: the
  // only case Rename applies to (a blank slot is named by typing into it).
  const namedMemberAt = (posKey) => {
    const id = memberIds[posKey];
    const mem = id ? squadEntries.find(e => e.id === id) : null;
    return mem && mem.name ? mem : null;
  };
  const startRename = (posKey) => {
    const mem = namedMemberAt(posKey);
    if (!mem) return;
    setRenamingKey(posKey);
    setRenamingName(mem.name);
    setError("");
  };
  const cancelRename = () => { setRenamingKey(null); setRenamingName(""); };
  // Mirrors the Lineups page's commitRename: the squad member is renamed
  // through the API, every position holding that id shows the new name,
  // and the lineup itself is written on the operator's next Save exactly as
  // there (the id is the identity; readers resolve the current name by it).
  const commitRename = async () => {
    const posKey = renamingKey;
    const id = memberIds[posKey];
    const name = renamingName.trim();
    if (!id || !name) { cancelRename(); return; }
    setRenameBusy(true);
    setError("");
    try {
      await window.API.renameTeamMember(compId, teamId, id, name, password);
      setSquad(sq => sq.map(m => (m && m.id === id ? { ...m, name } : m)));
      setValues(v => {
        const next = { ...v };
        // Read the CURRENT placements, not the ones this handler closed over
        // before its round trip: the pickers stay interactive while a rename
        // is in flight, so a position moved meanwhile would otherwise take the
        // new name while the position it moved to kept the old spelling.
        const ids = memberIdsRef.current;
        Object.keys(ids).forEach(key => { if (ids[key] === id) next[key] = name; });
        return next;
      });
      // The server already rewrote the stored lineup's names by id, so the
      // baseline follows: a rename alone must not make the side look edited.
      setBaseline(b => {
        const positionsNext = { ...b.positions };
        Object.keys(b.memberIds).forEach(key => { if (b.memberIds[key] === id) positionsNext[key] = name; });
        return { ...b, positions: positionsNext };
      });
      cancelRename();
    } catch (e) {
      setError(e?.message || "Failed to rename team member");
    } finally {
      setRenameBusy(false);
    }
  };
  useEffectA(() => {
    let cancelled = false;
    if (!compId || !teamId) return;
    (async () => {
      try {
        const squads = await window.API.fetchSquads(compId, password);
        if (cancelled) return;
        setSquad((squads && squads[teamId]) || []);
        setSquadUnavailable(false);
      } catch (_e) {
        if (!cancelled) setSquadUnavailable(true);
      }
    })();
    return () => { cancelled = true; };
    // `password` is a DEPENDENCY, not just a closure read, and the success
    // path CLEARS squadUnavailable. Same rule as the Lineups page's copy of
    // this effect, which carries the full rationale: the re-auth modal is a
    // SIBLING of the admin app, so a 401 here unmounts nothing and neither
    // compId nor teamId ever changes. Without both halves one 401 leaves this
    // panel's pickers empty for its whole lifetime and every typed name mints
    // a new member instead of resolving to the one already on the team.
  }, [compId, teamId, password]);

  // knownMemberIds (bc-dnst): the subset of positionsOut whose member id is
  // already known -- picked directly off the roster's numbered entries
  // (see onSelect above), never resolved by name. Those positions skip the
  // resolver entirely; only positions with NO known id still go through it.
  const doSave = async (positionsOut, successMsg = "Match lineup saved", knownMemberIds = {}) => {
    setError("");
    setLineupWarning("");
    setSaving(true);
    try {
      // bc-pnum gap closure: resolve each occupied-but-unresolved position's
      // name to a squad member id before writing. A name not on the squad is
      // a substitute typed straight into the slot; per operator ruling,
      // adding a new name in a position MINTS the member in that one step
      // (see resolveMemberIdsForPositions, admin_lineup.jsx -- the ONE
      // place this resolve/mint contract lives, shared with the inline
      // in-modal picker in admin_scoring_team.jsx). A resolve/mint failure
      // (offline venue wifi -- this panel's whole reason for existing) must
      // never block the save: the helper simply omits that position's id
      // and the write proceeds with the names alone, exactly as it
      // behaves today. bc-cse: the failure is no longer discarded either --
      // `memberFailures` carries it through to the warning shown below on
      // a successful save.
      let memberIdsOut = { ...knownMemberIds };
      let memberFailures = [];
      const positionsForResolver = {};
      Object.keys(positionsOut).forEach(key => {
        const name = (positionsOut[key] || "").trim();
        if (name && !knownMemberIds[key]) positionsForResolver[key] = name;
      });
      if (Object.keys(positionsForResolver).length > 0) {
        try {
          const resolver = window.AdminLineupHelpers?.resolveMemberIdsForPositions;
          if (typeof resolver === "function") {
            const resolved = await resolver(compId, teamId, positionsForResolver, squad, password, memberIds);
            memberIdsOut = { ...memberIdsOut, ...resolved.memberIds };
            memberFailures = resolved.failures || [];
            setSquad(resolved.squad);
          }
        } catch (_e) {
          // Defense in depth on top of the helper's own per-position mint
          // catch: even an unexpected failure IN the resolver itself must
          // not block the save. Proceed with the names alone.
        }
      }
      // One position per member (the shared predicate, bc-dnst): a typed
      // name can resolve onto a member the list never offered because it is
      // already fielded elsewhere. Refuse before writing and say where.
      for (const [key, id] of Object.entries(memberIdsOut)) {
        const otherKey = memberPlacedElsewhere(memberIdsOut, key, id);
        if (otherKey) {
          const who = (positionsOut[key] || "").trim() || "This fighter";
          const where = positions.find(p => p.key === otherKey)?.label || otherKey;
          setError(`${who} is already at ${where}.`);
          return;
        }
      }
      const hasMemberIds = Object.keys(memberIdsOut).length > 0;
      const updated = await window.API.putMatchLineup(
        compId, teamId, matchId, positionsOut, password, hasMemberIds ? memberIdsOut : undefined
      );
      // F5: a queued (offline/transient) write is NOT confirmed. Do NOT rebuild
      // the form from updated.positions (which is absent, would clear every
      // field) or show success; keep the operator's entered values and report
      // pending. The write is durable and will retry.
      if (updated && updated.queued) {
        if (typeof showToast === "function") showToast("Offline: match lineup not saved yet, will retry");
        return;
      }
      // Reflect exactly what was persisted.
      form.confirmSaved({ positions: updated.positions, memberIds: { ...memberIdsOut, ...updated.memberIds } });
      if (typeof showToast === "function") showToast(successMsg);
      const composer = window.AdminLineupHelpers?.memberIdentityWarning;
      if (typeof composer === "function") {
        setLineupWarning(composer(memberFailures, squadUnavailable));
      }
    } catch (e) {
      setError(e?.message || "Failed to save lineup");
    } finally {
      setSaving(false);
    }
  };

  const save = () => {
    // Nothing is written before the lineup was read, or while it holds no change.
    if (!form.canSave) return;
    // Strip empty positions before PUT. The handler replaces the whole
    // positions map (TeamLineup{Positions: req.Positions}), and the domain
    // validator treats an absent key the same as an explicit "": both
    // "missing". Sending explicit empties only bloats the persisted YAML.
    const positionsOut = {};
    const knownMemberIds = {};
    positions.forEach(p => {
      // Trim here too (not only at the picker's onSelect) so a Save can never
      // persist leading/trailing or whitespace-only names: matches AdminLineup.
      const v = (values[p.key] || "").trim();
      const pickedId = memberIds[p.key];
      // A picked squad entry is a real placement even when its member is
      // still unnamed (bc-dnst): keep the position so the id survives,
      // exactly like buildInlineLineupWrite (lineup_resolver.jsx). The
      // id is KNOWN (no resolver) while the box still reads the picked
      // member's own name, or nothing; a DIFFERENT name typed over the pick
      // goes through the resolver with this id as the position's current
      // member, so it renames that member (a blank one) rather than the
      // slot seeded for the position, and never the wrong fighter.
      const picked = pickedId ? squadEntries.find(e => e.id === pickedId) : null;
      const unchanged = !!picked && (!v || v.toLowerCase() === picked.name.toLowerCase());
      if (pickedId && unchanged) {
        positionsOut[p.key] = v;
        knownMemberIds[p.key] = pickedId;
      } else if (v) {
        positionsOut[p.key] = v;
      }
    });
    doSave(positionsOut, undefined, knownMemberIds);
  };

  const busy = saving || form.removing;
  // The boxes are for a lineup that was read: until then Save is off too, and the
  // problem line below says why.
  const locked = busy || !form.read;

  if (form.loading) return <div style={{ fontSize: 12, color: "var(--ink-3)" }}>Loading lineup…</div>;

  const teamName = team?.name || team?.Name || "Team";

  return (
    <div data-testid={`match-lineup-side-${teamId}`}>
      <div style={{ fontWeight: 700, fontSize: 13, marginBottom: 4 }}>{teamName}</div>

      <LineupProblem form={form} testId={`match-lineup-problem-${teamId}`} />
      <LineupSourceLine form={form} matchId={matchId} allMatches={allMatches} busy={busy} testId={`match-lineup-source-${teamId}`} />
      <LineupDraftNotice draft={form.draft} testId={`match-lineup-draft-${teamId}`} />

      {error && (
        <div className="alert alert--error" style={{ marginBottom: 8 }}>
          {error}
        </div>
      )}

      {/* Non-blocking: the save above already succeeded. Amber .alert--warn
          so it can never be mistaken for the red error banner above. */}
      {lineupWarning && (
        <div className="alert alert--warn" role="status" data-testid={`match-lineup-warning-${teamId}`} style={{ marginBottom: 8 }}>
          {lineupWarning}
        </div>
      )}

      <div style={{ display: "flex", flexDirection: "column", gap: 8, marginBottom: 12 }}>
        {positions.map(p => { const pickedLabel = pickedLabelFor(p.key); return (
          // The row is a div: a label around everything would activate its first
          // labelable descendant, the Rename button. Only the position name is a
          // label, pointed at the name box by id.
          <div key={p.key} data-testid={`match-lineup-pos-${teamId}-${p.key}`} style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 13 }}>
            {/* The picked member's number (bc-dnst) sits UNDER the position
                name, inside the label's fixed column: a picked blank slot has
                no name, so without it the box reads empty after the pick and
                the operator cannot see which slot the position holds. Stacked
                rather than beside the box so it adds no width: the box keeps
                its intrinsic width as flex basis, so a chip beside it grew the
                column's minimum past the modal and brought in a scrollbar. */}
            <span style={{ minWidth: 72, fontWeight: 600, color: "var(--ink-2)", fontSize: 12, display: "flex", flexDirection: "column", alignItems: "flex-start" }}>
              <label htmlFor={`match-lineup-name-${teamId}-${p.key}`}>{p.label}</label>
              {pickedLabel ? <span className="pmf__opt-label">{pickedLabel}</span> : null}
              {/* Rename lives in the same fixed column, so it adds no width
                  to the row (see the chip note above). */}
              {namedMemberAt(p.key) && renamingKey !== p.key ? (
                <button type="button" className="btn btn--ghost btn--sm lineup-rename-btn"
                  onClick={() => startRename(p.key)} disabled={busy || renameBusy}
                  aria-label={`Rename ${p.label} player`}>Rename</button>
              ) : null}
            </span>
            {renamingKey === p.key ? renameMemberFields({
              // stacked: this row is a fixed two-column layout that must not
              // grow WIDTH (see lineup_rename.jsx for the full rationale).
              value: renamingName,
              onChange: setRenamingName,
              onCommit: commitRename,
              onCancel: cancelRename,
              busy: renameBusy,
              ariaLabel: `Rename ${p.label} player`,
              inputStyle: { width: "100%", minWidth: 0, boxSizing: "border-box" },
              autoFocus: true,
              stacked: true,
            }) : (
            <LineupNameInput
              inputId={`match-lineup-name-${teamId}-${p.key}`}
              value={values[p.key] || ""}
              roster={rosterForPosition(p.key)}
              ariaLabel={`${p.label} player`}
              clearable={!!memberIds[p.key]}
              disabled={locked}
              onSelect={(name, entry) => {
                const trimmed = (name || "").trim();
                setValues(v => ({ ...v, [p.key]: trimmed }));
                // A picked squad entry (LineupNameInput's second onSelect
                // argument) carries its own member id: record it directly.
                // The clear button (empty name, no entry) vacates the
                // position, id included. A TYPED name (no entry) keeps the
                // id the position already holds: save() then resolves the
                // name against that member (a blank one is renamed, so the
                // name attaches to the number the operator was shown), and
                // only a position with no id at all resolves from scratch.
                setMemberIds(ids => {
                  if (entry && entry.id) return { ...ids, [p.key]: entry.id };
                  if (trimmed) return ids;
                  const next = { ...ids };
                  delete next[p.key];
                  return next;
                });
              }}
            />
            )}
          </div>
        ); })}
        {suggestions.length === 0 && (
          <div style={{ fontSize: 12, color: "var(--ink-3)", fontStyle: "italic" }}>
            This team has no registered members: type each competitor's name directly.
          </div>
        )}
      </div>

      <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
        <button type="button"
          className="btn btn--primary btn--sm"
          onClick={save}
          disabled={busy || !form.canSave}
          title={form.saveTitle}
        >
          {saving ? "Saving…" : "Save lineup"}
        </button>
      </div>
    </div>
  );
}

// MatchLineupPanel: modal overlay for per-match lineup editing. Renders
// one MatchLineupSideEditor per team side (sideA / sideB). Only shown for
// team competitions (compKind === "team" || teamSize > 0).
export function MatchLineupPanel({ match, tournament, password, showToast, onClose, variant = "modal" }) {
  const m = match;
  // Find the competition this match belongs to so we can access teamSize,
  // players (roster), etc.
  const comp = (tournament?.competitions || []).find(cc => cc.id === m.compId) || null;
  const isTeamComp = comp && (comp.kind === "team" || (comp.teamSize || 0) > 0);

  if (!isTeamComp) return null;

  // Resolve team objects from the competition's player list. comp.players
  // (loaded via /api/viewer/competitions) already carries each team's
  // metadata (member roster): no extra participants fetch is needed.
  //
  // The match's sideA/sideB are normalized to { id, name } (see
  // api_serializers.resolveSide); `id` is the participant's real UUID when
  // resolved, or "" when the backend has no UUID for that slot -- resolveSide
  // never invents an id from the team NAME. sideLookupKey (competitor_
  // identity.jsx) falls back to side.name in that "" case, which is the
  // recovery path this file needs: id over name (a real id decides, since a
  // UUID never coincidentally equals another team's display name), so
  // matchesKey's `p.id === key || p.name === key` only ever succeeds by
  // name when key itself carries no real id to offer (an id-less side).
  // The previous `(p.id || p.name) === sideId` form compared only the
  // first truthy key (the UUID), which never equals a name-keyed sideId,
  // so the roster silently failed to resolve and every dropdown showed
  // "No roster found" -- do not reintroduce that single-key form.
  const sideAKey = sideLookupKey(m.sideA);
  const sideBKey = sideLookupKey(m.sideB);
  const players = comp.players || [];
  const matchesKey = (p, key) =>
    !!key && (p.id === key || p.ID === key || p.name === key || p.Name === key);
  const teamA = players.find(p => matchesKey(p, sideAKey)) || (m.sideA && typeof m.sideA === "object" ? m.sideA : null);
  const teamB = players.find(p => matchesKey(p, sideBKey)) || (m.sideB && typeof m.sideB === "object" ? m.sideB : null);

  // A lineup carried from an earlier match names it. The competition's matches are
  // built only when a side shows such a lineup (lineupSourceLabel calls this), not
  // on every render of the panel.
  const allMatches = () => (typeof window.compMatches === "function" ? window.compMatches(comp) : []);

  const inner = (
    <>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", marginBottom: 16 }}>
          <div>
            <div className="overline">
              {comp?.name} · {m.scheduledAt || m.round || ""}
            </div>
            <h2 style={{ margin: "4px 0 0", fontSize: 20, fontWeight: 700 }}>Lineup for this match</h2>
            <div style={{ fontSize: 12, color: "var(--ink-3)", marginTop: 2 }}>
              Each team keeps the lineup of its previous match until you save a different one for this match.
            </div>
          </div>
          <button type="button" className="btn btn--ghost btn--sm" onClick={onClose}>{variant === "inline" ? "Done" : "✕ Close"}</button>
        </div>

        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 24 }}>
          <div style={{ borderRight: "1px solid var(--line)", paddingRight: 20 }}>
            <div className="overline" style={{ marginBottom: 8 }}>
              SHIRO (white)
            </div>
            {teamB ? (
              <MatchLineupSideEditor
                key={`${m.id}-side-b`}
                comp={comp}
                team={teamB}
                match={m}
                allMatches={allMatches}
                password={password}
                showToast={showToast}
              />
            ) : (
              <div style={{ color: "var(--ink-3)", fontSize: 12, fontStyle: "italic" }}>Team not found in roster.</div>
            )}
          </div>
          <div>
            <div className="overline" style={{ marginBottom: 8 }}>
              AKA (red)
            </div>
            {teamA ? (
              <MatchLineupSideEditor
                key={`${m.id}-side-a`}
                comp={comp}
                team={teamA}
                match={m}
                allMatches={allMatches}
                password={password}
                showToast={showToast}
              />
            ) : (
              <div style={{ color: "var(--ink-3)", fontSize: 12, fontStyle: "italic" }}>Team not found in roster.</div>
            )}
          </div>
        </div>
    </>
  );

  // Inline (mp-c2yr): render in-flow inside the operator console's main
  // column: no fixed overlay, no backdrop. The shiaijo page owns the
  // surrounding card; here we just provide padding + scroll.
  if (variant === "inline") {
    return <div className="scoring-panel lineup-panel--inline" aria-label="Lineup for this match">{inner}</div>;
  }

  return (
    <div style={{
      position: "fixed", inset: 0, background: "rgba(0,0,0,0.35)",
      display: "flex", alignItems: "center", justifyContent: "center",
      zIndex: 1000, padding: 16
    }}>
      <div style={{
        background: "var(--bg)", borderRadius: 8,
        boxShadow: "0 8px 32px rgba(0,0,0,0.18)", padding: 24,
        width: "100%", maxWidth: 680, maxHeight: "90vh", overflowY: "auto"
      }}>
        {inner}
      </div>
    </div>
  );
}
