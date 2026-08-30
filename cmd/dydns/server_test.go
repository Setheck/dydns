package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthAndReadiness(t *testing.T) {
	ready := &readiness{}
	handler := newHandler(ready)

	// Liveness is unconditional: a NameSilo outage must not crashloop the pod.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("/healthz before any success = %d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("/readyz before any success = %d, want 503", rec.Code)
	}

	ready.markSuccess(time.Now())

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("/readyz after a success = %d, want 200", rec.Code)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Errorf("/healthz after a success = %d, want 200", rec.Code)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	handler := newHandler(&readiness{})

	initMetrics("v1.2.3", "abc123", "example.com", "home.example.com")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("/metrics = %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	// Every metric the deployment alerts on must be exported, and the
	// pre-materialized label combinations must be present at zero so that
	// rate() and absent() work before the first event.
	want := []string{
		`dydns_build_info{commit="abc123"`,
		`dydns_target_info{domain="example.com",host="home.example.com"} 1`,
		"dydns_update_cycles_total",
		// Values are not asserted: these counters are on the shared default
		// registry, so sibling tests may have moved them. Presence of the
		// pre-materialized label combination is the point.
		`dydns_update_cycle_errors_total{stage="public_ip"}`,
		`dydns_update_cycle_errors_total{stage="record_not_found"}`,
		`dydns_update_cycle_errors_total{stage="update_record_reply"}`,
		"dydns_update_cycle_duration_seconds_bucket",
		"dydns_last_success_timestamp_seconds",
		"dydns_last_change_timestamp_seconds",
		"dydns_consecutive_failures",
		"dydns_record_up_to_date",
		"dydns_record_changes_total",
		"dydns_namesilo_request_duration_seconds_bucket",
		`dydns_public_ip_checks_total{result="success"}`,
		`dydns_public_ip_checks_total{result="error"}`,
		"dydns_public_ip_check_duration_seconds_bucket",
		// default collectors
		"go_goroutines",
	}
	for _, w := range want {
		if !strings.Contains(body, w) {
			t.Errorf("/metrics output missing %q", w)
		}
	}

	// The old, stuttering names must be gone.
	for _, gone := range []string{"dydns_dns_namesilo_info", "dydns_dns_namesilo_updates_total"} {
		if strings.Contains(body, gone) {
			t.Errorf("/metrics still exports the removed metric %q", gone)
		}
	}
}

func TestReadinessZeroValue(t *testing.T) {
	var r readiness
	if r.ready() {
		t.Fatal("zero-value readiness must not report ready")
	}
	r.markSuccess(time.Now())
	if !r.ready() {
		t.Fatal("readiness must report ready after markSuccess")
	}
}
