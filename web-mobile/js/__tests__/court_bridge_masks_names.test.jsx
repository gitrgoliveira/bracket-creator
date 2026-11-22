import { describe, it, expect } from 'vitest';
import { maskSnapshotForBridge, boardOverlayResult } from '../court_bridge.jsx';

const player = (id, name, number) => ({ id, name, number, displayName: name.toUpperCase(), dojo: 'Dojo' });

function individual(publicNames) {
    const a = player('p1', 'Tanaka Kenji', 'K1');
    const b = player('p2', 'Suzuki Ayumi', 'K2');
    return {
        id: 'c1',
        name: 'Open',
        publicNames,
        courts: ['A'],
        config: { players: [a, b] },
        poolMatches: [
            { id: 'Pool A-1', court: 'A', sideA: { id: 'p1', name: 'Tanaka Kenji' }, sideB: { id: 'p2', name: 'Suzuki Ayumi' }, winner: { id: 'p1', name: 'Tanaka Kenji' } },
        ],
        bracket: { rounds: [[{ id: 'M1', court: 'A', sideA: 'Tanaka Kenji', sideAId: 'p1', sideB: 'Winner of Pool A-2', winner: 'Tanaka Kenji', winnerId: 'p1' }]] },
    };
}

describe('maskSnapshotForBridge', () => {
    it('carries a hiding competition by number and id, never by name', () => {
        const [out] = maskSnapshotForBridge([individual('competition')]);
        const text = JSON.stringify(out);
        expect(text).not.toContain('Tanaka');
        expect(text).not.toContain('Suzuki');
        expect(text).not.toContain('TANAKA');
        expect(out.namesHidden).toBe(true);
        expect(out.name).toBe('Open');
        const [p1] = out.config.players;
        expect(p1).toMatchObject({ id: 'p1', name: 'K1', number: 'K1', nameHidden: true, displayName: 'K1' });
        expect(out.poolMatches[0].sideA).toMatchObject({ id: 'p1', name: 'K1', nameHidden: true });
        expect(out.poolMatches[0].winner.name).toBe('K1');
        const m = out.bracket.rounds[0][0];
        expect(m.sideA).toBe('K1');
        expect(m.winner).toBe('K1');
        // a placeholder no roster entry can place stays as it is
        expect(m.sideB).toBe('Winner of Pool A-2');
    });

    it('leaves a competition that shows its names untouched, by reference', () => {
        const shown = individual('');
        const [out] = maskSnapshotForBridge([shown]);
        expect(out).toBe(shown);
    });

    it('does not change the organiser copy it was handed', () => {
        const hiding = individual('person');
        maskSnapshotForBridge([hiding]);
        expect(hiding.config.players[0].name).toBe('Tanaka Kenji');
    });

    it('names two competitors sharing a name by id, not by the shared name', () => {
        const c = individual('hidden');
        c.config.players.push(player('p3', 'Tanaka Kenji', 'K3'));
        c.bracket.rounds[0][0].sideAId = 'p3';
        const [out] = maskSnapshotForBridge([c]);
        expect(out.bracket.rounds[0][0].sideA).toBe('K3');
        expect(JSON.stringify(out)).not.toContain('Tanaka');
    });

    it('shows a self-run tournament as it is, whatever the competition says', () => {
        const hiding = individual('hidden');
        const [out] = maskSnapshotForBridge([hiding], { mode: 'self-run' });
        expect(out).toBe(hiding);
        const [masked] = maskSnapshotForBridge([hiding], { mode: 'officiated' });
        expect(masked.namesHidden).toBe(true);
    });

    it('passes a non-array through', () => {
        expect(maskSnapshotForBridge(null)).toBe(null);
    });
});

function teamComp(publicNames) {
    const t1 = { id: 't1', name: 'Dojo Alpha', number: 'T1', dojo: 'Alpha' };
    const t2 = { id: 't2', name: 'Dojo Beta', number: 'T2', dojo: 'Beta' };
    return {
        id: 'ct',
        name: 'Teams',
        kind: 'team',
        teamSize: 3,
        publicNames,
        config: { players: [t1, t2] },
        teamMembers: {
            t1: [{ id: 'm11', name: 'Tanaka Kenji', index: 1 }, { id: 'm12', name: 'Sato Ryo', index: 2 }],
            t2: [{ id: 'm21', name: 'Suzuki Ayumi', index: 1 }],
        },
        poolMatches: [
            {
                id: 'Pool A-1', court: 'A',
                sideA: { id: 't1', name: 'Dojo Alpha' }, sideB: { id: 't2', name: 'Dojo Beta' },
                repPlayerA: 'Sato Ryo', repPlayerB: 'Nobody Known',
                subResults: [
                    { position: 1, sideA: 'Tanaka Kenji', sideAMemberId: 'm11', sideB: 'Suzuki Ayumi', sideBMemberId: 'm21', winner: 'Tanaka Kenji', winnerMemberId: 'm11' },
                    { position: 2, sideA: 'Sato Ryo', sideAMemberId: 'm12', sideB: '', winner: 'Dojo Alpha' },
                ],
            },
        ],
    };
}

describe('maskSnapshotForBridge, team competitions', () => {
    it('keeps the team names and carries every team member by label', () => {
        const [out] = maskSnapshotForBridge([teamComp('person')]);
        const text = JSON.stringify(out);
        for (const real of ['Tanaka', 'Sato', 'Suzuki', 'Nobody']) expect(text).not.toContain(real);
        expect(out.namesHidden).toBe(true);
        expect(out.config.players.map((p) => p.name)).toEqual(['Dojo Alpha', 'Dojo Beta']);
        const m = out.poolMatches[0];
        expect(m.sideA.name).toBe('Dojo Alpha');
        expect(out.teamMembers.t1.map((x) => x.name)).toEqual(['T1.1', 'T1.2']);
        expect(out.teamMembers.t2[0].name).toBe('T2.1');
        expect(m.subResults[0]).toMatchObject({ sideA: 'T1.1', sideB: 'T2.1', winner: 'T1.1', sideAMemberId: 'm11' });
        // a team's own name in a row stays, an empty side stays empty
        expect(m.subResults[1]).toMatchObject({ sideA: 'T1.2', sideB: '', winner: 'Dojo Alpha' });
        expect(m.repPlayerA).toBe('T1.2');
        expect(m.repPlayerB).toBe('?');
    });

    it('leaves a team competition that shows its names untouched', () => {
        const shown = teamComp('');
        expect(maskSnapshotForBridge([shown])[0]).toBe(shown);
    });

    it('does not change the organiser copy it was handed', () => {
        const hiding = teamComp('hidden');
        maskSnapshotForBridge([hiding]);
        expect(hiding.teamMembers.t1[0].name).toBe('Tanaka Kenji');
        expect(hiding.poolMatches[0].subResults[0].sideA).toBe('Tanaka Kenji');
    });
});

describe('boardOverlayResult', () => {
    it('names the sides by id only', () => {
        const r = boardOverlayResult({ id: 'M1', court: 'A', sideA: { id: 'p1', name: 'Tanaka Kenji' }, sideB: { id: 'p2', name: 'Suzuki Ayumi' }, status: 'running' });
        expect(r.sideAId).toBe('p1');
        expect(r.sideBId).toBe('p2');
        expect(JSON.stringify(r)).not.toContain('Tanaka');
        expect(JSON.stringify(r)).not.toContain('Suzuki');
    });
});
