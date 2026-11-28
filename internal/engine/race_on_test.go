//go:build race

package engine

// raceEnabled says the tests run under the race detector, which slows the
// code about ten times, so a wall-clock budget measures the detector rather
// than the code (the 256-competitor draw review timings).
const raceEnabled = true
