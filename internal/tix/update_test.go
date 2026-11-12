package tix

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestNewerRelease(t *testing.T) {
	tests := []struct {
		latest, running string
		want            bool
	}{
		{"v1.2.4", "v1.2.3", true},
		{"v1.10.0", "v1.9.9", true},
		{"v2.0.0", "v1.99.99", true},
		{"v1.2.3", "v1.2.3", false},
		{"v1.2.2", "v1.2.3", false},
		{"v1.3.0", "dev", false},
		{"v1.3.0", "v0.0.0-20260101000000-abcdef123456", false},
		{"v1.3.0-rc.1", "v1.2.0", false},
		{"", "v1.2.0", false},
	}
	for _, tt := range tests {
		t.Run(tt.latest+" vs "+tt.running, func(t *testing.T) {
			if got := NewerRelease(tt.latest, tt.running); got != tt.want {
				t.Errorf("NewerRelease(%q, %q) = %v, want %v", tt.latest, tt.running, got, tt.want)
			}
		})
	}
}

func TestUpdateStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "update.json")
	empty, err := ReadUpdateState(path)
	if err != nil {
		t.Fatalf("reading missing state: %v", err)
	}
	if empty != (UpdateState{}) {
		t.Fatalf("missing state = %+v, want zero", empty)
	}
	want := UpdateState{CheckedAt: time.Date(2026, 11, 12, 0, 0, 0, 0, time.UTC), Latest: "v1.2.3", Notified: "v1.2.2"}
	if err := WriteUpdateState(path, want); err != nil {
		t.Fatalf("writing state: %v", err)
	}
	got, err := ReadUpdateState(path)
	if err != nil {
		t.Fatalf("reading state: %v", err)
	}
	if !got.CheckedAt.Equal(want.CheckedAt) || got.Latest != want.Latest || got.Notified != want.Notified {
		t.Errorf("state = %+v, want %+v", got, want)
	}
}

func TestFetchLatest(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    string
		wantErr bool
	}{
		{"release", http.StatusOK, `{"Version":"v1.4.0","Time":"2026-11-01T00:00:00Z"}`, "v1.4.0", false},
		{"not semver", http.StatusOK, `{"Version":"v1.4.0; rm -rf ~"}`, "", true},
		{"pseudo-version", http.StatusOK, `{"Version":"v0.0.0-20260101000000-abcdef123456"}`, "", true},
		{"server error", http.StatusInternalServerError, ``, "", true},
		{"bad json", http.StatusOK, `nope`, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				fmt.Fprint(w, tt.body)
			}))
			defer srv.Close()
			got, err := FetchLatest(context.Background(), srv.URL)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("version = %q, want %q", got, tt.want)
			}
		})
	}
}
