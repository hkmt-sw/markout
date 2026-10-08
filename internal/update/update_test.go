package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewer(t *testing.T) {
	tests := []struct {
		latest, current string
		want            bool
	}{
		{"v1.2.0", "v1.1.9", true},
		{"v1.1.10", "v1.1.9", true},
		{"v2.0.0", "v1.9.9", true},
		{"1.2.0", "v1.1.0", true},
		{"v1.1.2", "v1.1.2", false},
		{"v1.1.1", "v1.1.2", false},
		{"v1.2.0", "dev", false},
		{"v1.2.0", "v1.1.2-3-gabc123-dirty", false},
		{"garbage", "v1.0.0", false},
		// A release is newer than its candidates, and a candidate than the one before
		{"v1.8.0", "v1.8.0-rc2", true},
		{"v1.8.0-rc2", "v1.8.0-rc1", true},
		{"v1.8.0-rc1", "v1.8.0", false},
		{"v1.8.0-rc1", "v1.7.9", true},
		{"v1.7.9", "v1.8.0-rc1", false},
	}
	for _, tt := range tests {
		if got := Newer(tt.latest, tt.current); got != tt.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", tt.latest, tt.current, got, tt.want)
		}
	}
}

func TestIsRelease(t *testing.T) {
	for v, want := range map[string]bool{"v1.2.3": true, "1.2.3": true, "dev": false, "": false,
		"v1.2": false, "v1.2.3-rc1": true, "v1.2.3-rc0": false, "v1.2.3-rc": false, "v1.2.3-rc1-2-gabc": false,
		"v1.2.3-beta1": false, "v1.1.2-3-gabc123": false, "v01.2.3": false} {
		if got := IsRelease(v); got != want {
			t.Errorf("IsRelease(%q) = %v, want %v", v, got, want)
		}
	}
}

func TestLatest(t *testing.T) {
	var userAgent string
	body, status := `{"tag_name": "v1.4.0", "name": "x"}`, http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userAgent = r.Header.Get("User-Agent")
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	defer srv.Close()

	got, err := Latest(context.Background(), srv.URL, "v1.1.2")
	if err != nil || got != "v1.4.0" {
		t.Fatalf("Latest = %q, %v", got, err)
	}
	if userAgent != "markout/v1.1.2" {
		t.Errorf("User-Agent = %q", userAgent)
	}

	// Anything that is not a release tag, and any failure, is an error rather
	// than a version the user would be told to install.
	body = `{"tag_name": "<script>"}`
	if got, err := Latest(context.Background(), srv.URL, "v1.1.2"); err == nil {
		t.Errorf("accepted a malformed tag: %q", got)
	}
	body, status = `{"message": "rate limited"}`, http.StatusForbidden
	if _, err := Latest(context.Background(), srv.URL, "v1.1.2"); err == nil {
		t.Error("accepted an error response")
	}
}
