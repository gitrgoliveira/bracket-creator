import { describe, it, expect, afterEach } from 'vitest';
import { maskSnapshotForBridge, maskPatchForBridge, boardOverlayResult, openBridge, setSnapshotProvider, _resetModuleStateForTests } from '../court_bridge.jsx';

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
        bracket: { rounds: [[{ id: 'M1', court: 'A', sideA: 'Tanaka Kenji', sideAId: 'p1', sideB: 'Winner of r1-m2', winner: 'Tanaka Kenji', winnerId: 'p1' }]] },
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
        expect(m.sideB).toBe('Winner of r1-m2');
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

    it('reads a fighting spirit award recipient as their number, "?" when no one or several hold the name', () => {
        const c = individual('hidden');
        c.config.players.push(player('p3', 'Ito Mari', 'K3'), player('p4', 'Ito Mari', 'K4'));
        c.fightingSpiritAwards = [
            { title: 'Spirit', recipientName: 'Tanaka Kenji', recipientDojo: 'Dojo' },
            { title: 'Spirit', recipientName: 'Ito Mari', recipientDojo: 'Dojo' },
            { title: 'Spirit', recipientName: 'Not On The Roster', recipientDojo: 'Dojo' },
        ];
        const [out] = maskSnapshotForBridge([c]);
        expect(out.fightingSpiritAwards.map((a) => a.recipientName)).toEqual(['K1', '?', '?']);
        expect(JSON.stringify(out)).not.toContain('Not On The Roster');
        expect(c.fightingSpiritAwards[0].recipientName).toBe('Tanaka Kenji');
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

    it('drops a team record\'s metadata, where a legacy roster held its members\' names', () => {
        const hiding = teamComp('hidden');
        hiding.config.players[0] = { ...hiding.config.players[0], metadata: ['Tanaka Kenji', 'Sato Ryo'], danGrade: 'Tanaka Kenji' };
        const [out] = maskSnapshotForBridge([hiding]);
        expect(JSON.stringify(out)).not.toContain('Tanaka');
        expect(out.config.players[0]).toMatchObject({ name: 'Dojo Alpha', metadata: [], danGrade: '' });
        expect(hiding.config.players[0].metadata).toEqual(['Tanaka Kenji', 'Sato Ryo']);
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

describe('maskSnapshotForBridge, a competitor the draw has not numbered', () => {
    it('reads "?" rather than carrying the name, in the roster and in every match naming it', () => {
        const c = individual('hidden');
        delete c.config.players[1].number;
        const [out] = maskSnapshotForBridge([c]);
        const text = JSON.stringify(out);
        expect(text).not.toContain('Suzuki');
        expect(text).not.toContain('SUZUKI');
        expect(out.config.players[1]).toMatchObject({ id: 'p2', name: '?', nameHidden: true });
        expect(out.poolMatches[0].sideB).toMatchObject({ id: 'p2', name: '?' });
        // the numbered competitor still reads by number
        expect(out.config.players[0].name).toBe('K1');
    });

    it('reads "?" for a legacy row with no id and no number, never its name', () => {
        const c = individual('hidden');
        delete c.config.players[1].id;
        delete c.config.players[1].number;
        const [out] = maskSnapshotForBridge([c]);
        expect(out.config.players[1]).toMatchObject({ name: '?', displayName: '?', nameHidden: true });
        expect(JSON.stringify(out.config.players)).not.toContain('Suzuki');
    });
});

describe('maskPatchForBridge', () => {
    const patch = (result) => ({ result });

    it('names an individual winner by number, by id or by name alone', () => {
        const c = individual('competition');
        const byId = maskPatchForBridge(patch({ id: 'M1', winner: 'Tanaka Kenji', winnerId: 'p1', sideAId: 'p1', sideBId: 'p2' }), c);
        expect(byId.result.winner).toBe('K1');
        const byName = maskPatchForBridge(patch({ id: 'M1', winner: 'Suzuki Ayumi' }), c);
        expect(byName.result.winner).toBe('K2');
        expect(JSON.stringify([byId, byName])).not.toMatch(/Tanaka|Suzuki/);
    });

    it('reads "?" for a winner no roster entry places, never the name', () => {
        const out = maskPatchForBridge(patch({ id: 'M1', winner: 'Nobody Known' }), individual('hidden'));
        expect(out.result.winner).toBe('?');
    });

    it('carries a team bout fighter and a representative by label, keeping the team names', () => {
        const c = teamComp('person');
        const out = maskPatchForBridge(patch({
            id: 'Pool A-1', sideAId: 't1', sideBId: 't2', winner: 'Dojo Alpha',
            repPlayerA: 'Sato Ryo', repPlayerB: 'Nobody Known',
            subResults: [
                { position: 1, sideA: 'Tanaka Kenji', sideAMemberId: 'm11', sideB: 'Suzuki Ayumi', sideBMemberId: 'm21', winner: 'Tanaka Kenji', winnerMemberId: 'm11' },
                { position: 2, sideA: 'Sato Ryo', sideAMemberId: 'm12', sideB: '', winner: 'Dojo Alpha' },
            ],
        }), c);
        const text = JSON.stringify(out);
        for (const real of ['Tanaka', 'Sato', 'Suzuki', 'Nobody']) expect(text).not.toContain(real);
        expect(out.result.winner).toBe('Dojo Alpha');
        expect(out.result.repPlayerA).toBe('T1.2');
        expect(out.result.repPlayerB).toBe('?');
        expect(out.result.subResults[0]).toMatchObject({ sideA: 'T1.1', sideB: 'T2.1', winner: 'T1.1' });
        // a team's own name as a bout winner stays, as the roster names it
        expect(out.result.subResults[1]).toMatchObject({ sideA: 'T1.2', winner: 'Dojo Alpha' });
    });

    it('masks a team patch that carries a representative but no bout rows, and adds no rows', () => {
        const out = maskPatchForBridge(patch({ id: 'Pool A-1', sideAId: 't1', repPlayerA: 'Sato Ryo' }), teamComp('hidden'));
        expect(out.result.repPlayerA).toBe('T1.2');
        expect('subResults' in out.result).toBe(false);
    });

    it('masks every entry of a results list', () => {
        const out = maskPatchForBridge({ results: [{ id: 'M1', winner: 'Tanaka Kenji', winnerId: 'p1' }, null] }, individual('hidden'));
        expect(out.results[0].winner).toBe('K1');
        expect(out.results[1]).toBe(null);
    });

    it('passes a competition that shows its names, or a self-run tournament, through by reference', () => {
        const p = patch({ id: 'M1', winner: 'Tanaka Kenji' });
        expect(maskPatchForBridge(p, individual(''))).toBe(p);
        expect(maskPatchForBridge(p, individual('hidden'), { mode: 'self-run' })).toBe(p);
    });

    it('does not change the patch it was handed', () => {
        const p = patch({ id: 'M1', winner: 'Tanaka Kenji', winnerId: 'p1' });
        maskPatchForBridge(p, individual('hidden'));
        expect(p.result.winner).toBe('Tanaka Kenji');
    });
});

describe('a patch published on the court channel', () => {
    class Channel {
        static last = null;
        postMessage(data) { Channel.last = data; }
        close() {}
    }
    const orig = global.BroadcastChannel;
    afterEach(() => {
        global.BroadcastChannel = orig;
        Channel.last = null;
        _resetModuleStateForTests();
    });

    function publishWith(comps, compId, payload) {
        global.BroadcastChannel = Channel;
        _resetModuleStateForTests();
        const b = openBridge();
        setSnapshotProvider(() => comps, () => ({ mode: 'officiated' }));
        b.publish('patch', 'A', compId, payload);
        return Channel.last;
    }

    it('leaves the tab masked for a competition that hides its names', () => {
        const sent = publishWith([individual('competition')], 'c1', { result: { id: 'M1', winner: 'Tanaka Kenji' } });
        expect(sent.payload.result.winner).toBe('K1');
    });

    it('is sent as it is by a competition that shows its names', () => {
        const sent = publishWith([individual('')], 'c1', { result: { id: 'M1', winner: 'Tanaka Kenji' } });
        expect(sent.payload.result.winner).toBe('Tanaka Kenji');
    });

    it('is not sent for a competition the tab cannot place', () => {
        const sent = publishWith([individual('hidden')], 'other', { result: { id: 'M1', winner: 'Tanaka Kenji' } });
        expect(sent).toBe(null);
    });

    it('is sent as it is by a tab holding no tournament', () => {
        global.BroadcastChannel = Channel;
        _resetModuleStateForTests();
        openBridge().publish('patch', 'A', 'c1', { result: { id: 'M1', winner: 'Tanaka Kenji' } });
        expect(Channel.last.payload.result.winner).toBe('Tanaka Kenji');
    });
});
