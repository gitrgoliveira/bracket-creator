package state

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
)

// CorruptFileError says that a competition file on disk could not be parsed,
// and WHERE. It exists because the alternative the operator used to get was a
// generic HTTP 500: scoring simply stopped, with the cause visible only in the
// server log of a laptop nobody is watching mid event.
//
// The tool's storage is deliberately shaped for people to read and repair
// (docs/architecture/data-model.md section 1), which only works if a wrong edit
// says what is wrong and where. Line and Column are 1-based and are 0 when the
// parser could not place the fault.
//
// This is the LOUD class of malformed data, the one that fails the load: a
// competition whose bracket.json will not parse cannot be scored, and both
// bracket write paths abort before saving, so the file on disk is left exactly
// as the operator left it. That is deliberate. The QUIET class -- a single
// malformed cell that degrades to its documented default -- does not produce
// this error; it keeps its own per-match channel (MatchResult.SubResultsRaw and
// SubResultsUnreadable) so one bad cell cannot stop a tournament.
type CorruptFileError struct {
	// File is the competition-relative filename, e.g. "bracket.json".
	File   string
	Line   int
	Column int
	// Detail is the underlying parser's own message, which names the offending
	// byte ("invalid character 'x' after object key:value pair"). Safe to show
	// an operator: it describes syntax, not competitor data.
	Detail string
	Err    error
}

func (e *CorruptFileError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s is corrupt at line %d, column %d: %s", e.File, e.Line, e.Column, e.Detail)
	}
	return fmt.Sprintf("%s is corrupt: %s", e.File, e.Detail)
}

func (e *CorruptFileError) Unwrap() error { return e.Err }

// AsCorruptFile reports whether err is (or wraps) a CorruptFileError.
func AsCorruptFile(err error) (*CorruptFileError, bool) {
	var c *CorruptFileError
	if errors.As(err, &c) {
		return c, true
	}
	return nil, false
}

// corruptJSON converts a json decode failure into a located CorruptFileError.
// Both error types carry a byte OFFSET rather than a position, so the offset is
// resolved against the bytes that produced it: a line and column is what an
// operator can act on in the editor they already have open.
func corruptJSON(file string, raw []byte, err error) error {
	if err == nil {
		return nil
	}
	// ONLY a genuine parse failure becomes a CorruptFileError. Anything else --
	// an I/O failure, a programming error in the destination type -- is not
	// corruption, must not tell an operator to go and repair their file, and
	// must not have its message forwarded: Detail is returned to the CLIENT, and
	// an I/O error's message carries the absolute path that internalError exists
	// to keep out of a response body.
	c := &CorruptFileError{File: file, Detail: err.Error(), Err: err}
	var syn *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case errors.As(err, &syn):
		c.Line, c.Column = offsetToLineColumn(raw, syn.Offset)
	case errors.As(err, &typ):
		c.Line, c.Column = offsetToLineColumn(raw, typ.Offset)
	default:
		return err
	}
	return c
}

// corruptCSV converts a csv PARSE failure into a located CorruptFileError.
// encoding/csv already reports a line and column, so there is no offset to
// resolve. Any other failure is returned untouched.
func corruptCSV(file string, err error) error {
	if err == nil {
		return nil
	}
	// Parse failures only, for the reason given on corruptJSON: a read error
	// mid file is not something a text editor fixes, and its message names the
	// path on disk.
	var pe *csv.ParseError
	if !errors.As(err, &pe) {
		return err
	}
	c := &CorruptFileError{File: file, Line: pe.Line, Column: pe.Column, Detail: err.Error(), Err: err}
	// pe.Err is the bare reason ("extraneous or missing \" in quoted-field");
	// pe.Error() prefixes it with the position this struct already carries.
	if pe.Err != nil {
		c.Detail = pe.Err.Error()
	}
	return c
}

// offsetToLineColumn resolves a 0-based byte offset into a 1-based line and
// column. An offset past the end (json reports one for a truncated document,
// which is what half a hand edit looks like) resolves to the final position
// rather than to nothing, so a truncation still points somewhere useful.
func offsetToLineColumn(raw []byte, offset int64) (line, column int) {
	if offset < 0 {
		return 0, 0
	}
	if offset > int64(len(raw)) {
		offset = int64(len(raw))
	}
	line, column = 1, 1
	for _, b := range raw[:offset] {
		if b == '\n' {
			line++
			column = 1
			continue
		}
		column++
	}
	return line, column
}
