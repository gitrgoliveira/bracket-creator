package backup

import (
	"errors"
	"testing"
)

// A backup is restored on the version that made it or a later one, never on
// an earlier one (operator ruling 2026-11-08). Versions with no order (a
// branch name, a short sha, "dev", a -dirty build) restore each other only
// when they match, and no backup before OldestRestorableVersion restores.
func TestRestorableBy(t *testing.T) {
	cases := []struct {
		name, made, running string
		ok                  bool
	}{
		{"same release", "v2.4.0", "v2.4.0", true},
		{"surrounding space ignored", " v2.4.0\n", "v2.4.0", true},
		{"older release on a later one", "v2.4.0", "v2.5.0", true},
		{"later release on an earlier one", "v2.5.0", "v2.4.0", false},
		{"patch order", "v2.4.1", "v2.4.0", false},
		{"major outranks minor", "v2.9.0", "v3.0.0", true},
		{"numbers, not text", "v2.10.0", "v2.9.0", false},
		{"release on a build after it", "v2.4.0", "v2.4.0-12-gabc1234", true},
		// git-describe is commits AFTER the tag, the reverse of semver precedence.
		{"build after a release on that release", "v2.4.0-12-gabc1234", "v2.4.0", false},
		{"earlier build on a later one", "v2.4.0-3-gdef5678", "v2.4.0-12-gabc1234", true},
		{"later build on an earlier one", "v2.4.0-12-gabc1234", "v2.4.0-3-gdef5678", false},
		{"build on the next release", "v2.4.0-12-gabc1234", "v2.5.0", true},
		{"dirty build has no order", "v2.4.0-dirty", "v2.4.0", false},
		{"release on a dirty build", "v2.4.0", "v2.4.0-12-gabc1234-dirty", false},
		{"same dirty build", "v2.4.0-12-gabc1234-dirty", "v2.4.0-12-gabc1234-dirty", true},
		{"the oldest restorable version", "v2.3.0", "v2.4.0", true},
		{"a build after the oldest", "v2.3.0-5-g1234567", "v2.4.0", true},
		{"before the oldest", "v2.2.9", "v2.4.0", false},
		{"leading v optional", "2.4.0", "v2.4.0", true},
		{"same branch build", "ship/schedule-estimator-page", "ship/schedule-estimator-page", true},
		{"release on a branch build", "v2.4.0", "ship/schedule-estimator-page", false},
		{"branch build on a release", "main", "v2.4.0", false},
		{"short sha on a release", "abc1234", "v2.4.0", false},
		{"dev on dev", "dev", "dev", true},
		{"no version recorded", "", "v2.4.0", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := RestorableBy(tc.made, tc.running); got != tc.ok {
				t.Errorf("RestorableBy(%q, %q) = %v, want %v", tc.made, tc.running, got, tc.ok)
			}
		})
	}
}

// CheckAppVersion passes what RestorableBy allows and refuses the rest,
// naming both versions in the sentence a person reads.
func TestCheckAppVersion(t *testing.T) {
	cases := []struct {
		name, made, running string
		ok                  bool
		detail              string
	}{
		{name: "same version", made: "v2.4.0", running: "v2.4.0", ok: true},
		{name: "older backup", made: "v2.4.0", running: "v2.5.0", ok: true},
		{name: "newer backup", made: "v2.5.0", running: "v2.4.0",
			detail: "This backup was made with version v2.5.0 and this app is version v2.4.0. A backup can be restored only with the version that made it or a later one, so nothing was changed."},
		{name: "versions with no order", made: "main", running: "v2.4.0",
			detail: "This backup was made with version main and this app is version v2.4.0. These two versions cannot be compared. A backup can be restored only with the version that made it or a later one, so nothing was changed."},
		{name: "before the oldest", made: "v2.2.0", running: "v2.4.0",
			detail: "This backup was made with version v2.2.0 and this app is version v2.4.0. This app can bring a backup up to date only from version v2.3.0 on, so nothing was changed."},
		{name: "no version recorded", made: "", running: "v2.4.0",
			detail: "This backup does not say which version made it and this app is version v2.4.0. A backup can be restored only with the version that made it or a later one, so nothing was changed."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckAppVersion(Manifest{AppVersion: tc.made}, tc.running)
			if tc.ok {
				if err != nil {
					t.Fatalf("CheckAppVersion(%q, %q) = %v, want nil", tc.made, tc.running, err)
				}
				return
			}
			var be *Error
			if !errors.As(err, &be) {
				t.Fatalf("CheckAppVersion(%q, %q) = %v, want *Error", tc.made, tc.running, err)
			}
			if be.Code != CodeVersionMismatch {
				t.Errorf("code = %q, want %q", be.Code, CodeVersionMismatch)
			}
			if be.Detail != tc.detail {
				t.Errorf("detail = %q\nwant     %q", be.Detail, tc.detail)
			}
		})
	}
}

// The floor must itself be an ordered version, or beforeOldest would compare
// every backup against a zero order and refuse none.
func TestOldestRestorableVersionIsOrdered(t *testing.T) {
	if _, ok := versionOrder(OldestRestorableVersion); !ok {
		t.Fatalf("OldestRestorableVersion %q has no release order", OldestRestorableVersion)
	}
}
