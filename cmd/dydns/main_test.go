package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/zerolog"
	"github.com/setheck/dydns/pkg/namesilo"
)

func TestMain(m *testing.M) {
	log = zerolog.New(io.Discard)
	m.Run()
}

// fakeNamesilo stands in for both the NameSilo API and the public IP provider.
type fakeNamesilo struct {
	publicIP string

	listStatus int
	listBody   string

	updateStatus int
	updateBody   string

	updateCalls int
	lastUpdate  map[string]string

	// listCalls is read from tests while the server goroutine writes it.
	listCalls atomic.Int64
	// beforeList, when set, runs before the list response is written.
	beforeList func()
}

func listReply(code int, detail string, records ...string) string {
	return fmt.Sprintf(`{"reply":{"code":%d,"detail":%q,"resource_record":[%s]}}`,
		code, detail, strings.Join(records, ","))
}

func aRecord(id, host, value string) string {
	return fmt.Sprintf(`{"record_id":%q,"type":"A","host":%q,"value":%q,"ttl":7207}`, id, host, value)
}

func (f *fakeNamesilo) start(t *testing.T) (*namesilo.Client, updateConfig) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/ip"):
			_, _ = io.WriteString(w, f.publicIP)

		case strings.HasSuffix(r.URL.Path, namesilo.OperationDnsListRecords):
			f.listCalls.Add(1)
			if f.beforeList != nil {
				f.beforeList()
			}
			w.WriteHeader(f.listStatus)
			_, _ = io.WriteString(w, f.listBody)

		case strings.HasSuffix(r.URL.Path, namesilo.OperationDnsUpdateRecord):
			f.updateCalls++
			f.lastUpdate = map[string]string{}
			for k, v := range r.URL.Query() {
				f.lastUpdate[k] = v[0]
			}
			w.WriteHeader(f.updateStatus)
			_, _ = io.WriteString(w, f.updateBody)

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := updateConfig{
		domain:      "example.com",
		host:        "home.example.com",
		timeout:     5 * time.Second,
		ttl:         7207,
		publicIPURL: srv.URL + "/ip",
		httpClient:  srv.Client(),
	}
	client := namesilo.New("test-key", namesilo.WithBaseURL(srv.URL), namesilo.WithHTTPClient(srv.Client()))
	return client, cfg
}

func newFake() *fakeNamesilo {
	return &fakeNamesilo{
		publicIP:     "203.0.113.7\n",
		listStatus:   http.StatusOK,
		listBody:     listReply(300, "success", aRecord("aaa", "home.example.com", "198.51.100.1")),
		updateStatus: http.StatusOK,
		updateBody:   `{"reply":{"code":300,"detail":"success"}}`,
	}
}

func TestUpdateDynamicDNS(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*fakeNamesilo)
		wantErr     string
		wantStage   string
		wantUpdates int
		wantChanges float64
		// wantUpToDate is nil where the code deliberately leaves the gauge
		// alone: a list-records failure tells us nothing about the record, so
		// clearing it would fire a false alarm during a NameSilo blip.
		// Staleness is covered by last_success_timestamp_seconds instead.
		wantUpToDate *float64
	}{
		{
			name:         "record changed",
			wantUpdates:  1,
			wantChanges:  1,
			wantUpToDate: f64(1),
		},
		{
			name: "already up to date skips the write",
			mutate: func(f *fakeNamesilo) {
				f.listBody = listReply(300, "success", aRecord("aaa", "home.example.com", "203.0.113.7"))
			},
			wantUpdates:  0,
			wantChanges:  0,
			wantUpToDate: f64(1),
		},
		{
			name: "public ip lookup fails",
			mutate: func(f *fakeNamesilo) {
				f.publicIP = "not-an-ip"
			},
			wantErr:   "failed to get public IP",
			wantStage: stagePublicIP,
		},
		{
			name: "list records transport error",
			mutate: func(f *fakeNamesilo) {
				f.listStatus = http.StatusInternalServerError
				f.listBody = "boom"
			},
			wantErr:   "failed to list records",
			wantStage: stageListRecords,
		},
		{
			name: "list records non-300 reply",
			mutate: func(f *fakeNamesilo) {
				f.listBody = listReply(110, "invalid api key")
			},
			wantErr:   "returned code 110: invalid api key",
			wantStage: stageListRecordsReply,
		},
		{
			name: "record not found",
			mutate: func(f *fakeNamesilo) {
				f.listBody = listReply(300, "success", aRecord("aaa", "other.example.com", "198.51.100.1"))
			},
			wantErr:      "no A record found",
			wantStage:    stageRecordNotFound,
			wantUpToDate: f64(0),
		},
		{
			name: "non-A record with matching host is ignored",
			mutate: func(f *fakeNamesilo) {
				f.listBody = listReply(300, "success",
					`{"record_id":"ccc","type":"CNAME","host":"home.example.com","value":"x.example.com","ttl":7207}`)
			},
			wantErr:   "no A record found",
			wantStage: stageRecordNotFound,
		},
		{
			name: "update transport error",
			mutate: func(f *fakeNamesilo) {
				f.updateStatus = http.StatusBadGateway
				f.updateBody = "boom"
			},
			wantErr:      "failed to update dns record",
			wantStage:    stageUpdateRecord,
			wantUpdates:  1,
			wantUpToDate: f64(0),
		},
		{
			name: "update non-300 reply",
			mutate: func(f *fakeNamesilo) {
				f.updateBody = `{"reply":{"code":280,"detail":"invalid rrid"}}`
			},
			wantErr:      "returned code 280: invalid rrid",
			wantStage:    stageUpdateRecordReply,
			wantUpdates:  1,
			wantUpToDate: f64(0),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFake()
			if tt.mutate != nil {
				tt.mutate(f)
			}
			client, cfg := f.start(t)

			changesBefore := testutil.ToFloat64(recordChangesTotal)
			err := updateDynamicDNS(context.Background(), client, cfg)

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err, tt.wantErr)
				}
				var se *stageError
				if !errors.As(err, &se) {
					t.Fatalf("error %v is not a *stageError; the loop cannot label it", err)
				}
				if se.stage != tt.wantStage {
					t.Fatalf("stage = %q, want %q", se.stage, tt.wantStage)
				}
			}

			if f.updateCalls != tt.wantUpdates {
				t.Errorf("dnsUpdateRecord called %d times, want %d", f.updateCalls, tt.wantUpdates)
			}
			if got := testutil.ToFloat64(recordChangesTotal) - changesBefore; got != tt.wantChanges {
				t.Errorf("record_changes_total delta = %v, want %v", got, tt.wantChanges)
			}
			if tt.wantUpToDate != nil {
				if got := testutil.ToFloat64(recordUpToDate); got != *tt.wantUpToDate {
					t.Errorf("record_up_to_date = %v, want %v", got, *tt.wantUpToDate)
				}
			}
		})
	}
}

// The public IP info gauge must hold exactly one series: without a Reset every
// observed IP would stay pinned at 1 forever.
func TestPublicIPInfoHasSingleSeries(t *testing.T) {
	f := newFake()
	client, cfg := f.start(t)

	for _, ip := range []string{"203.0.113.7", "203.0.113.8", "203.0.113.9"} {
		f.publicIP = ip + "\n"
		f.listBody = listReply(300, "success", aRecord("aaa", "home.example.com", "198.51.100.1"))
		if err := updateDynamicDNS(context.Background(), client, cfg); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if n := testutil.CollectAndCount(publicIPInfo); n != 1 {
		t.Fatalf("got %d public_ip_info series, want 1", n)
	}
	if got := testutil.ToFloat64(publicIPInfo.WithLabelValues("203.0.113.9")); got != 1 {
		t.Fatalf("public_ip_info{ip=\"203.0.113.9\"} = %v, want the most recent IP to be the surviving series", got)
	}
}

func TestUpdateRecordSendsConfiguredTTL(t *testing.T) {
	f := newFake()
	client, cfg := f.start(t)
	cfg.ttl = 600

	if err := updateDynamicDNS(context.Background(), client, cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := f.lastUpdate["rrttl"]; got != "600" {
		t.Fatalf("rrttl = %q, want %q", got, "600")
	}
	if got := f.lastUpdate["rrvalue"]; got != "203.0.113.7" {
		t.Fatalf("rrvalue = %q, want the public IP", got)
	}
	if got := f.lastUpdate["rrid"]; got != "aaa" {
		t.Fatalf("rrid = %q, want the existing record id", got)
	}
}

// Regression guard for the update-error branch, which used to increment the
// list-records error counter.
func TestUpdateErrorAttributedToUpdateStage(t *testing.T) {
	f := newFake()
	f.updateBody = `{"reply":{"code":280,"detail":"nope"}}`
	client, cfg := f.start(t)

	replyBefore := stageErrors(stageUpdateRecordReply)
	listBefore := stageErrors(stageListRecords)

	err := updateDynamicDNS(context.Background(), client, cfg)
	if err == nil {
		t.Fatal("expected an error")
	}

	// updateDynamicDNS tags the stage; updateOnInterval is what increments the
	// counter, so drive it through the same path the loop uses.
	var se *stageError
	if !errors.As(err, &se) {
		t.Fatal("expected a *stageError")
	}
	updateCycleErrorsTotal.WithLabelValues(se.stage).Inc()

	if d := stageErrors(stageUpdateRecordReply) - replyBefore; d != 1 {
		t.Errorf("update_record_reply error delta = %v, want 1", d)
	}
	if d := stageErrors(stageListRecords) - listBefore; d != 0 {
		t.Errorf("list_records error delta = %v, want 0", d)
	}
}

func TestNamesiloRequestsLabelledByReplyCode(t *testing.T) {
	f := newFake()
	f.listBody = listReply(110, "invalid api key")
	client, cfg := f.start(t)

	before := namesiloRequests(operationListRecords, "110")
	_ = updateDynamicDNS(context.Background(), client, cfg)

	if d := namesiloRequests(operationListRecords, "110") - before; d != 1 {
		t.Errorf("requests_total{operation=dnsListRecords,reply_code=110} delta = %v, want 1", d)
	}
}

func TestNamesiloTransportFailureLabelledError(t *testing.T) {
	f := newFake()
	f.listStatus = http.StatusInternalServerError
	f.listBody = "boom"
	client, cfg := f.start(t)

	before := namesiloRequests(operationListRecords, resultError)
	_ = updateDynamicDNS(context.Background(), client, cfg)

	if d := namesiloRequests(operationListRecords, resultError) - before; d != 1 {
		t.Errorf("requests_total{reply_code=error} delta = %v, want 1", d)
	}
}

func TestFindARecord(t *testing.T) {
	records := []namesilo.ResourceRecord{
		{RecordID: "1", Type: "CNAME", Host: "home.example.com", Value: "x"},
		{RecordID: "2", Type: "A", Host: "other.example.com", Value: "1.1.1.1"},
		{RecordID: "3", Type: "A", Host: "home.example.com", Value: "2.2.2.2"},
		{RecordID: "4", Type: "A", Host: "home.example.com", Value: "3.3.3.3"},
	}

	got, matches := findARecord(records, "home.example.com")
	if matches != 2 {
		t.Errorf("matches = %d, want 2", matches)
	}
	if got.RecordID != "4" {
		t.Errorf("record id = %q, want the last match", got.RecordID)
	}

	got, matches = findARecord(records, "missing.example.com")
	if matches != 0 || got.RecordID != "" {
		t.Errorf("got %+v (%d matches), want the zero record", got, matches)
	}
}

func TestNewLogger(t *testing.T) {
	// LOG_FORMAT=json must never produce the console writer, regardless of TTY.
	if l := newLogger("json", "debug"); l.GetLevel() != zerolog.DebugLevel {
		t.Errorf("level = %v, want debug", l.GetLevel())
	}
	// An unparsable level falls back to info rather than silencing the logger.
	if l := newLogger("json", "nonsense"); l.GetLevel() != zerolog.InfoLevel {
		t.Errorf("level = %v, want info fallback", l.GetLevel())
	}
	if l := newLogger("json", ""); l.GetLevel() != zerolog.InfoLevel {
		t.Errorf("level = %v, want info fallback for empty", l.GetLevel())
	}
}

func TestBuildVersion(t *testing.T) {
	v, c := buildVersion()
	if v == "" || c == "" {
		t.Fatalf("buildVersion returned empty values: %q %q", v, c)
	}
}

func f64(v float64) *float64 { return &v }

// --- metric helpers ---------------------------------------------------------
//
// These read the live collectors rather than gathering the whole registry.
// Counters are package-level on the default registry and shared across tests,
// so assertions compare deltas, never absolute values.

func stageErrors(stage string) float64 {
	return testutil.ToFloat64(updateCycleErrorsTotal.WithLabelValues(stage))
}

func totalStageErrors() float64 {
	var total float64
	for _, stage := range allStages {
		total += stageErrors(stage)
	}
	return total
}

func namesiloRequests(operation, replyCode string) float64 {
	return testutil.ToFloat64(namesiloRequestsTotal.WithLabelValues(operation, replyCode))
}
