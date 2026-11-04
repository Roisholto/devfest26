package application

import (
	"net/http/httptest"
	"strings"
	"testing"

	"devfest26/internal/config"
)

func TestApprovalRequiresExplicitBoolean(t *testing.T) {
	s := &Server{Config: config.Config{Prefix: "participant-main"}}
	for _, body := range []string{`{"proposal_id":"one"}`, `{"proposal_id":"one","approve":"yes"}`, `{"proposal_id":"one","approve":null}`, `{"approve":true}`, `{"proposal_id":"one","approve":true,"amount":10}`} {
		r := httptest.NewRequest("POST", "/requests/participant-main-test/decision", strings.NewReader(body))
		r.Header.Set("Idempotency-Key", "test")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatalf("accepted invalid decision %s: %d", body, w.Code)
		}
	}
}
func TestEnvironmentIsolation(t *testing.T) {
	s := &Server{Config: config.Config{Prefix: "participant-main"}}
	r := httptest.NewRequest("GET", "/requests/presenter-main-other", nil)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatalf("cross-environment request returned %d", w.Code)
	}
}
