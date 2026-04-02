package cli

import (
	"testing"
)

func TestCompareReleaseVersions(t *testing.T) {
	testCases := []struct {
		name    string
		current string
		latest  string
		want    int
		ok      bool
	}{
		{name: "older release", current: "1.7.14", latest: "1.8.0", want: -1, ok: true},
		{name: "same release with metadata", current: "1.8.0+dirty", latest: "v1.8.0", want: 0, ok: true},
		{
			name:    "pseudo version newer than older release",
			current: "1.8.1-0.20260320192201-a0df8eaad4e2+dirty",
			latest:  "1.8.0",
			want:    1,
			ok:      true,
		},
		{
			name:    "pseudo version older than final release",
			current: "1.8.1-0.20260320192201-a0df8eaad4e2+dirty",
			latest:  "1.8.1",
			want:    -1,
			ok:      true,
		},
		{name: "devel build", current: "dev", latest: "1.8.0", want: 0, ok: false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := compareReleaseVersions(tc.current, tc.latest)
			if ok != tc.ok {
				t.Fatalf("expected ok=%t, got %t", tc.ok, ok)
			}
			if got != tc.want {
				t.Fatalf("expected compare result %d, got %d", tc.want, got)
			}
		})
	}
}
