//go:build race

package racesample

// Enabled says the tests run under the race detector, which makes the
// arrival-order permutation gates about ten times slower (see Sample).
const Enabled = true
