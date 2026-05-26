// Tests for buildRoster's check-in gate (mp-wkm).
// Defense-in-depth: when checkInEnabled is false, checkedIn must never be
// true in the roster so that badge rendering sites don't need to re-check
// the competition-level flag.
import { describe, it, expect } from 'vitest';
import { buildRoster } from '../viewer.jsx';

describe('buildRoster check-in gating', () => {
  it('masks checkedIn=true when checkInEnabled is false', () => {
    const comps = [{
      id: 'c1',
      checkInEnabled: false,
      players: [{ id: 'p1', name: 'Alice', checkedIn: true }],
    }];
    const roster = buildRoster(comps);
    expect(roster).toHaveLength(1);
    expect(roster[0].checkedIn).toBe(false);
  });

  it('preserves checkedIn=true when checkInEnabled is true', () => {
    const comps = [{
      id: 'c1',
      checkInEnabled: true,
      players: [{ id: 'p1', name: 'Alice', checkedIn: true }],
    }];
    const roster = buildRoster(comps);
    expect(roster[0].checkedIn).toBe(true);
  });

  it('preserves checkedIn=false regardless of checkInEnabled', () => {
    const comps = [
      { id: 'c1', checkInEnabled: true, players: [{ id: 'p1', name: 'Alice', checkedIn: false }] },
      { id: 'c2', checkInEnabled: false, players: [{ id: 'p2', name: 'Bob', checkedIn: false }] },
    ];
    const roster = buildRoster(comps);
    expect(roster.find(p => p.id === 'p1').checkedIn).toBe(false);
    expect(roster.find(p => p.id === 'p2').checkedIn).toBe(false);
  });

  it('surfaces checkedIn=true if any check-in-enabled competition has the player checked in', () => {
    const comps = [
      { id: 'c1', checkInEnabled: false, players: [{ id: 'p1', name: 'Alice', checkedIn: true }] },
      { id: 'c2', checkInEnabled: true, players: [{ id: 'p1', name: 'Alice', checkedIn: true }] },
    ];
    const roster = buildRoster(comps);
    expect(roster).toHaveLength(1);
    expect(roster[0].checkedIn).toBe(true);
  });

  it('deduplicates players across competitions', () => {
    const comps = [
      { id: 'c1', checkInEnabled: false, players: [{ id: 'p1', name: 'Alice', checkedIn: false }] },
      { id: 'c2', checkInEnabled: false, players: [{ id: 'p1', name: 'Alice', checkedIn: false }] },
    ];
    const roster = buildRoster(comps);
    expect(roster).toHaveLength(1);
  });

  it('upgrades checkedIn from false to true when a later enabled comp has the player checked in', () => {
    const comps = [
      { id: 'c1', checkInEnabled: true, players: [{ id: 'p1', name: 'Alice', checkedIn: false }] },
      { id: 'c2', checkInEnabled: true, players: [{ id: 'p1', name: 'Alice', checkedIn: true }] },
    ];
    const roster = buildRoster(comps);
    expect(roster).toHaveLength(1);
    expect(roster[0].checkedIn).toBe(true);
  });

  it('handles missing or empty competitions gracefully', () => {
    expect(buildRoster(null)).toEqual([]);
    expect(buildRoster([])).toEqual([]);
    expect(buildRoster([{ id: 'c1', checkInEnabled: true, players: [] }])).toEqual([]);
  });

  it('skips null/missing player entries', () => {
    const comps = [{
      id: 'c1',
      checkInEnabled: true,
      players: [null, undefined, { id: 'p1', name: 'Alice', checkedIn: true }],
    }];
    const roster = buildRoster(comps);
    expect(roster).toHaveLength(1);
    expect(roster[0].id).toBe('p1');
  });

  it('treats missing checkedIn field as false even when checkInEnabled is true', () => {
    const comps = [{
      id: 'c1',
      checkInEnabled: true,
      players: [{ id: 'p1', name: 'Alice' }],
    }];
    const roster = buildRoster(comps);
    expect(roster[0].checkedIn).toBe(false);
  });
});
