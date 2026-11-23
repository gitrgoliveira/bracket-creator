import { describe, it, expect } from 'vitest';
import { tournamentRefusal } from '../document_versions.jsx';
import { PUBLIC_NAMES_APPLY_FAILED } from '../public_names_fields.jsx';

// bc-hnam: a tournament save the server answered 500 with the competitions its
// new default could not reach is told apart from any other failure, so the form
// sends the confirmation again for the rest.
describe('tournamentRefusal: a default that did not reach every competition', () => {
    it('carries the code and the competitions a 500 lists', () => {
        const e = tournamentRefusal({ error: 'Save again to retry.', competitions: ['Men', 'Women'] }, 500);
        expect(e.code).toBe(PUBLIC_NAMES_APPLY_FAILED);
        expect(e.competitions).toEqual(['Men', 'Women']);
        expect(e.message).toBe('Save again to retry.');
    });

    it('leaves a 500 that lists no competitions as a plain failure', () => {
        const e = tournamentRefusal({ error: 'internal error' }, 500);
        expect(e.code).toBeUndefined();
    });
});
