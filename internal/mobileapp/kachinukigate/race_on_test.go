//go:build race

package kachinukigate

// raceEnabled says the tests run under the race detector, which makes the
// arrival-order permutation gates about ten times slower (see raceSample).
const raceEnabled = true
