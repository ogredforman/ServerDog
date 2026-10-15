package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		path         string
		wantStatus   int
		wantCache    string
		wantBodyPart string
	}{
		{"index", http.MethodGet, "/", http.StatusOK, "public, max-age=60, s-maxage=300", "Not affiliated with or endorsed by Bulkhead or Wardogs"},
		{"index HEAD", http.MethodHead, "/", http.StatusOK, "public, max-age=60, s-maxage=300", ""},
		{"unknown path", http.MethodGet, "/nope", http.StatusNotFound, "no-store", ""},
		{"index POST", http.MethodPost, "/", http.StatusNotFound, "no-store", ""},
	}
	h := Handler()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			if got := rec.Header().Get("Cache-Control"); got != tt.wantCache {
				t.Errorf("Cache-Control = %q, want %q", got, tt.wantCache)
			}
			if !strings.Contains(rec.Body.String(), tt.wantBodyPart) {
				t.Errorf("body does not contain %q", tt.wantBodyPart)
			}
		})
	}
}
