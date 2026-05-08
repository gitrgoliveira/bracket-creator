import { describe, it, expect } from 'vitest';
import { toBackendMatchResult, normalizeMatch, buildPlayerMap, normalizePlayer } from '../api.js';

describe('API Utils', () => {
  describe('toBackendMatchResult', () => {
    it('should translate UI patch to backend shape', () => {
      const match = { sideA: { name: 'Player A' }, sideB: { name: 'Player B' } };
      const patch = {
        winner: { name: 'Player A' },
        status: 'complete',
        score: { type: 'ippon', fouls: { a: 1, b: 0 } },
        ipponsA: ['M', 'K'],
        ipponsB: []
      };
      
      const result = toBackendMatchResult(patch, match);
      expect(result.winner).toBe('Player A');
      expect(result.ipponsA).toEqual(['M', 'K']);
      expect(result.hansokuA).toBe(1);
      expect(result.status).toBe('completed');
    });

    it('should handle hikiwake decision', () => {
      const match = { sideA: 'A', sideB: 'B' };
      const patch = { status: 'complete', score: { type: 'hikiwake' } };
      const result = toBackendMatchResult(patch, match);
      expect(result.decision).toBe('hikewake');
    });
  });

  describe('normalizeMatch', () => {
    it('should normalize string sides to objects using playerMap', () => {
      const playerMap = { 'Alice': { id: 'Alice', name: 'Alice', dojo: 'Dojo A' } };
      const match = { sideA: 'Alice', sideB: 'Bob', status: 'scheduled' };
      const norm = normalizeMatch(match, playerMap);
      
      expect(norm.sideA).toEqual({ id: 'Alice', name: 'Alice', dojo: 'Dojo A' });
      expect(norm.sideB).toEqual({ id: 'Bob', name: 'Bob' }); // Fallback if not in map
    });

    it('should build score object from ippons for pool matches', () => {
      const match = { 
        sideA: 'A', sideB: 'B', winner: 'A', 
        status: 'completed', ipponsA: ['M'], ipponsB: [] 
      };
      const norm = normalizeMatch(match, {});
      expect(norm.score).toEqual({
        type: 'ippon',
        winnerPts: 1,
        loserPts: 0,
        ippons: ['M']
      });
    });
  });

  describe('buildPlayerMap', () => {
    it('should build a map from competition players', () => {
      const comp = {
        players: [
          { Name: 'Alice', Dojo: 'Dojo A', Seed: 1 },
          { name: 'Bob', dojo: 'Dojo B' }
        ]
      };
      const map = buildPlayerMap(comp);
      expect(map['Alice']).toEqual({ id: 'Alice', name: 'Alice', dojo: 'Dojo A', seed: 1 });
      expect(map['Bob']).toEqual({ id: 'Bob', name: 'Bob', dojo: 'Dojo B', seed: 0 });
    });
  });

  describe('normalizePlayer', () => {
    it('should normalize Go-style player to frontend shape', () => {
      const p = { Name: 'Alice', Dojo: 'Dojo A', Seed: 2 };
      expect(normalizePlayer(p)).toEqual({
        name: 'Alice',
        displayName: '',
        dojo: 'Dojo A',
        seed: 2,
        number: '',
        tag: ''
      });
    });
  });
});
