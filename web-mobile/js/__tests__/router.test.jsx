import { describe, it, expect } from 'vitest';
import { parseSearch } from '../router.jsx';

// parseSearch: the single canonical query-string parser in router.jsx, exposed
// via the ES export and window.AppRouter. app.jsx (parseCourtFromSearch) and
// router's own useQuery both delegate to it, so it is worth covering directly.

describe('parseSearch', () => {
    it('returns an empty object for empty / too-short input', () => {
        expect(parseSearch('')).toEqual({});
        expect(parseSearch('?')).toEqual({});
        expect(parseSearch(undefined)).toEqual({});
        expect(parseSearch(null)).toEqual({});
    });

    it('parses a single key=value pair, stripping a leading ?', () => {
        expect(parseSearch('?court=A')).toEqual({ court: 'A' });
    });

    it('parses without a leading ?', () => {
        expect(parseSearch('court=B')).toEqual({ court: 'B' });
    });

    it('parses multiple pairs', () => {
        expect(parseSearch('?court=A&overlay=true&position=top')).toEqual({
            court: 'A', overlay: 'true', position: 'top',
        });
    });

    it('maps a key with no value to an empty string', () => {
        expect(parseSearch('?flag')).toEqual({ flag: '' });
        expect(parseSearch('?a=1&flag&b=2')).toEqual({ a: '1', flag: '', b: '2' });
    });

    it('percent-decodes keys and values', () => {
        expect(parseSearch('?name=Akira%20Tanaka')).toEqual({ name: 'Akira Tanaka' });
        expect(parseSearch('?a%20b=c')).toEqual({ 'a b': 'c' });
    });

    it('keeps the last value when a key repeats', () => {
        expect(parseSearch('?court=A&court=B')).toEqual({ court: 'B' });
    });

    it('skips empty segments from doubled separators', () => {
        expect(parseSearch('?court=A&&overlay=1')).toEqual({ court: 'A', overlay: '1' });
    });
});
