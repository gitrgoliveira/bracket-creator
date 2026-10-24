//go:build race

package mobileapp

// raceEnabled says the tests run under the race detector, which makes the
// arrival-order permutation gates about ten times slower (see raceSample).
const raceEnabled = true
