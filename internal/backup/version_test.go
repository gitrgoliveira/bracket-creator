package backup

import (
	"errors"
	"testing"
)

// A backup is restored only on the version that made it (operator ruling
// 2026-11-08): CheckAppVersion passes the same version and refuses any other,
// naming both in the sentence a person reads.
func TestCheckAppVersion(t *testing.T) {
	cases := []struct {
		name, made, running string
		ok                  bool
		detail              string
	}{
		{name: "same version", made: "v2.4.0", running: "v2.4.0", ok: true},
		{name: "surrounding space ignored", made: " v2.4.0\n", running: "v2.4.0", ok: true},
		{name: "older backup", made: "v2.3.0", running: "v2.4.0",
			detail: "This backup was made with version v2.3.0 and this app is version v2.4.0. A backup can be restored only with the version that made it, so nothing was changed."},
		{name: "newer backup", made: "v2.4.0", running: "v2.3.0",
			detail: "This backup was made with version v2.4.0 and this app is version v2.3.0. A backup can be restored only with the version that made it, so nothing was changed."},
		{name: "no version recorded", made: "", running: "v2.4.0",
			detail: "This backup does not say which version made it and this app is version v2.4.0. A backup can be restored only with the version that made it, so nothing was changed."},
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
