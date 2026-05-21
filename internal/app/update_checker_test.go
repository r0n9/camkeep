package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestUpdateCheckerStableVersionUpdateAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got == "" {
			t.Fatal("expected User-Agent header")
		}
		w.Header().Set("ETag", `"release-1"`)
		_, _ = w.Write([]byte(`{
			"tag_name": "v1.2.4",
			"html_url": "https://github.com/r0n9/camkeep/releases/tag/v1.2.4",
			"published_at": "2026-05-21T00:00:00Z"
		}`))
	}))
	defer server.Close()

	checker := newUpdateChecker("v1.2.3", server.URL, time.Hour, server.Client())
	checker.Check(context.Background())

	snapshot := checker.Snapshot()
	if snapshot.Channel != "stable" {
		t.Fatalf("expected stable channel, got %q", snapshot.Channel)
	}
	if !snapshot.UpdateAvailable {
		t.Fatal("expected update to be available")
	}
	if snapshot.LatestStableVersion != "v1.2.4" {
		t.Fatalf("expected latest v1.2.4, got %q", snapshot.LatestStableVersion)
	}
	if snapshot.ReleaseURL == "" {
		t.Fatal("expected release url")
	}
}

func TestUpdateCheckerDevVersionDoesNotMarkStableUpdate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"tag_name": "v9.9.9",
			"html_url": "https://github.com/r0n9/camkeep/releases/tag/v9.9.9",
			"published_at": "2026-05-21T00:00:00Z"
		}`))
	}))
	defer server.Close()

	checker := newUpdateChecker("v1.3.0-dev", server.URL, time.Hour, server.Client())
	checker.Check(context.Background())

	snapshot := checker.Snapshot()
	if snapshot.Channel != "dev" {
		t.Fatalf("expected dev channel, got %q", snapshot.Channel)
	}
	if snapshot.UpdateAvailable {
		t.Fatal("expected dev version to skip stable update flag")
	}
	if snapshot.LatestStableVersion != "v9.9.9" {
		t.Fatalf("expected latest stable version to be retained, got %q", snapshot.LatestStableVersion)
	}
	if snapshot.Message == "" {
		t.Fatal("expected explanatory message")
	}
}

func TestParseVersionChannels(t *testing.T) {
	cases := []struct {
		version string
		channel string
		stable  bool
	}{
		{version: "v1.2.3", channel: "stable", stable: true},
		{version: "1.2.3", channel: "stable", stable: true},
		{version: "dev", channel: "dev", stable: false},
		{version: "dev-abc123", channel: "dev", stable: false},
		{version: "v1.2.3-dev", channel: "dev", stable: false},
		{version: "test-abc123", channel: "test", stable: false},
		{version: "local-build", channel: "custom", stable: false},
	}

	for _, tc := range cases {
		t.Run(tc.version, func(t *testing.T) {
			got := parseVersion(tc.version)
			if got.channel != tc.channel || got.stable != tc.stable {
				t.Fatalf("parseVersion(%q) = channel %q stable %v, want channel %q stable %v", tc.version, got.channel, got.stable, tc.channel, tc.stable)
			}
		})
	}
}
