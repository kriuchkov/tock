package cli

import (
	"runtime/debug"
	"testing"
)

func TestResolveBuildMetadataUsesModuleVersion(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{
			Version: "v1.8.0",
		},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "8793fd5163d58ad8a089ee812da5d13009623af8"},
			{Key: "vcs.time", Value: "2026-03-20T19:05:44Z"},
		},
	}

	metadata := resolveBuildMetadata(buildMetadata{
		version: "dev",
		commit:  "none",
		date:    "unknown",
	}, info)

	if metadata.version != "1.8.0" {
		t.Fatalf("expected version 1.8.0, got %q", metadata.version)
	}

	if metadata.commit != "8793fd5163d58ad8a089ee812da5d13009623af8" {
		t.Fatalf("unexpected commit %q", metadata.commit)
	}

	if metadata.date != "2026-03-20T19:05:44Z" {
		t.Fatalf("unexpected date %q", metadata.date)
	}
}

func TestResolveBuildMetadataNormalizesPseudoVersion(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{
			Version: "v1.8.1-0.20260320192201-a0df8eaad4e2+dirty",
		},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "a0df8eaad4e2824a383528dd0a1e5ed3fb9c145a"},
			{Key: "vcs.time", Value: "2026-03-20T19:22:01Z"},
			{Key: "vcs.modified", Value: "true"},
		},
	}

	metadata := resolveBuildMetadata(buildMetadata{
		version: "dev",
		commit:  "none",
		date:    "unknown",
	}, info)

	if metadata.version != "1.8.1-0.20260320192201-a0df8eaad4e2+dirty" {
		t.Fatalf("expected version 1.8.1-0.20260320192201-a0df8eaad4e2+dirty, got %q", metadata.version)
	}
}

func TestResolveBuildMetadataPreservesReleaseBuildMetadata(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{
			Version: "v1.8.0+dirty",
		},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "8793fd5163d58ad8a089ee812da5d13009623af8"},
			{Key: "vcs.time", Value: "2026-03-20T19:05:44Z"},
			{Key: "vcs.modified", Value: "true"},
		},
	}

	metadata := resolveBuildMetadata(buildMetadata{
		version: "dev",
		commit:  "none",
		date:    "unknown",
	}, info)

	if metadata.version != "1.8.0+dirty" {
		t.Fatalf("expected version 1.8.0+dirty, got %q", metadata.version)
	}
}

func TestResolveBuildMetadataFallsBackToRevisionForDevel(t *testing.T) {
	info := &debug.BuildInfo{
		Main: debug.Module{
			Version: "(devel)",
		},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "a0df8eaad4e2b6ef65fa5cb4cf0be2dbb8d74f18"},
			{Key: "vcs.time", Value: "2026-03-20T19:22:01Z"},
			{Key: "vcs.modified", Value: "true"},
		},
	}

	metadata := resolveBuildMetadata(buildMetadata{
		version: "dev",
		commit:  "none",
		date:    "unknown",
	}, info)

	if metadata.version != "dev" {
		t.Fatalf("expected dev, got %q", metadata.version)
	}

	if metadata.commit != "a0df8eaad4e2b6ef65fa5cb4cf0be2dbb8d74f18" {
		t.Fatalf("unexpected commit %q", metadata.commit)
	}

	if metadata.date != "2026-03-20T19:22:01Z" {
		t.Fatalf("unexpected date %q", metadata.date)
	}
}

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
