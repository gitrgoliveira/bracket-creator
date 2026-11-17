// The API object is the spread of its domain pieces (api_client.jsx). A key two
// pieces both define is silently overwritten by the later spread, so a member
// could vanish, or answer with another domain's method, without any error. This
// pins that the pieces share no key and that API is exactly their union, member
// for member. It does not pin the names: adding or moving a member needs no edit
// here.
import { describe, it, expect } from 'vitest';
import { API } from '../api_client.jsx';
import { tournamentApi } from '../api_client_tournament.jsx';
import { competitionsApi } from '../api_client_competitions.jsx';
import { matchesApi } from '../api_client_matches.jsx';
import { teamsApi } from '../api_client_teams.jsx';
import { outboxApi } from '../api_client_outbox.jsx';
import { eventsApi } from '../api_client_events.jsx';

const PIECES = { tournamentApi, competitionsApi, matchesApi, teamsApi, outboxApi, eventsApi };

describe('the API pieces', () => {
  it('each is a non-empty plain object', () => {
    for (const [name, piece] of Object.entries(PIECES)) {
      expect(Object.getPrototypeOf(piece), name).toBe(Object.prototype);
      expect(Object.keys(piece).length, name).toBeGreaterThan(0);
    }
  });

  it('share no key', () => {
    const owner = new Map();
    const shared = [];
    for (const [name, piece] of Object.entries(PIECES)) {
      for (const key of Object.keys(piece)) {
        if (owner.has(key)) shared.push(`${key}: ${owner.get(key)} and ${name}`);
        else owner.set(key, name);
      }
    }
    expect(shared).toEqual([]);
  });

  it('make up API exactly, each member the piece\'s own value', () => {
    const union = Object.values(PIECES).flatMap((piece) => Object.keys(piece));
    expect(Object.keys(API).sort()).toEqual([...union].sort());
    for (const piece of Object.values(PIECES)) {
      for (const [key, value] of Object.entries(piece)) expect(API[key], key).toBe(value);
    }
  });

  it('are what window.API is', () => {
    expect(window.API).toBe(API);
  });
});
