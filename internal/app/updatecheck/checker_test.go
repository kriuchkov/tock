package updatecheck

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func testClient(fn roundTripFunc) *http.Client {
	return &http.Client{Transport: fn}
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
			got, ok := CompareReleaseVersions(tc.current, tc.latest)
			if ok != tc.ok {
				t.Fatalf("expected ok=%t, got %t", tc.ok, ok)
			}
			if got != tc.want {
				t.Fatalf("expected compare result %d, got %d", tc.want, got)
			}
		})
	}
}

func TestCheckSkipsWhenDisabled(t *testing.T) {
	called := false
	client := testClient(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, nil
	})

	result, err := Check(t.Context(), client, time.Now(), State{CurrentVersion: "1.0.0"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Checked {
		t.Fatalf("expected update check to be skipped")
	}
	if called {
		t.Fatalf("expected no network request when checks are disabled")
	}
}

func TestCheckSkipsFallbackVersion(t *testing.T) {
	called := false
	client := testClient(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, nil
	})

	result, err := Check(t.Context(), client, time.Now(), State{
		CheckUpdates:   true,
		CurrentVersion: BuildVersionDev,
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Checked {
		t.Fatalf("expected update check to be skipped for dev version")
	}
	if called {
		t.Fatalf("expected no network request for fallback version")
	}
}

func TestCheckSkipsRecentCheck(t *testing.T) {
	now := time.Date(2026, time.March, 14, 12, 0, 0, 0, time.UTC)
	called := false
	client := testClient(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, nil
	})

	result, err := Check(t.Context(), client, now, State{
		CheckUpdates:   true,
		LastCheckedAt:  now.Add(-CheckInterval + time.Hour),
		CurrentVersion: "1.7.0",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Checked {
		t.Fatalf("expected update check to be skipped when interval has not elapsed")
	}
	if called {
		t.Fatalf("expected no network request when interval has not elapsed")
	}
}

func TestCheckReportsAvailableUpdate(t *testing.T) {
	now := time.Date(2026, time.March, 14, 12, 0, 0, 0, time.UTC)
	client := testClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != LatestReleaseURL {
			t.Fatalf("expected request to %s, got %s", LatestReleaseURL, req.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(`{"tag_name":"v1.8.0","html_url":"https://example.com/release"}`)),
			Header:     make(http.Header),
		}, nil
	})

	result, err := Check(t.Context(), client, now, State{
		CheckUpdates:   true,
		CurrentVersion: "1.7.14",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result.Checked {
		t.Fatalf("expected update check to run")
	}
	if !result.UpdateAvailable {
		t.Fatalf("expected update to be available")
	}
	if result.CheckedAt != now {
		t.Fatalf("expected checked time %v, got %v", now, result.CheckedAt)
	}
	if result.CurrentVersion != "1.7.14" {
		t.Fatalf("expected current version 1.7.14, got %s", result.CurrentVersion)
	}
	if result.LatestRelease.TagName != "v1.8.0" {
		t.Fatalf("expected latest tag v1.8.0, got %s", result.LatestRelease.TagName)
	}
}

func TestFetchLatestReleaseUnexpectedStatus(t *testing.T) {
	client := testClient(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       io.NopCloser(strings.NewReader("bad gateway")),
			Header:     make(http.Header),
		}, nil
	})

	_, err := FetchLatestRelease(t.Context(), client)
	if err == nil || !strings.Contains(err.Error(), "unexpected status code") {
		t.Fatalf("expected unexpected status error, got %v", err)
	}
}

func TestFetchLatestReleaseDecodeError(t *testing.T) {
	client := testClient(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("not-json")),
			Header:     make(http.Header),
		}, nil
	})

	_, err := FetchLatestRelease(t.Context(), client)
	if err == nil || !strings.Contains(err.Error(), "decode release response") {
		t.Fatalf("expected decode error, got %v", err)
	}
}
