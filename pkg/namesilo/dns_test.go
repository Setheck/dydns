package namesilo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const listRecordsBody = `{
  "request": {"operation": "dnsListRecords", "ip": "203.0.113.7"},
  "reply": {
    "code": 300,
    "detail": "success",
    "resource_record": [
      {"record_id": "aaa", "type": "A", "host": "home.example.com", "value": "198.51.100.1", "ttl": 7207, "distance": 0},
      {"record_id": "bbb", "type": "MX", "host": "example.com", "value": "mail.example.com", "ttl": 3600, "distance": "10"}
    ]
  }
}`

// newTestClient serves body/status and captures the query of the last request.
func newTestClient(t *testing.T, status int, body string) (*Client, *url.Values) {
	t.Helper()

	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	c := New("test-key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	return c, &gotQuery
}

func TestDnsListRecords(t *testing.T) {
	c, gotQuery := newTestClient(t, http.StatusOK, listRecordsBody)

	resp, err := c.DnsListRecords(context.Background(), DnsListRecordsParameters{Domain: "example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]string{
		"version": "1",
		"type":    "json",
		"key":     "test-key",
		"domain":  "example.com",
	}
	for k, v := range want {
		if got := gotQuery.Get(k); got != v {
			t.Errorf("query %q = %q, want %q", k, got, v)
		}
	}

	if resp.Reply.Code != ReplyCodeSuccess {
		t.Errorf("reply code = %d, want %d", resp.Reply.Code, ReplyCodeSuccess)
	}
	if n := len(resp.Reply.ResourceRecords); n != 2 {
		t.Fatalf("got %d records, want 2", n)
	}
	rec := resp.Reply.ResourceRecords[0]
	if rec.RecordID != "aaa" || rec.Type != "A" || rec.Value != "198.51.100.1" || rec.TTL != 7207 {
		t.Errorf("unexpected first record: %+v", rec)
	}
	// distance comes back as an int here and a string in the MX record; both
	// must decode without error, which is why the field is `any`.
	if resp.Reply.ResourceRecords[1].Distance != "10" {
		t.Errorf("distance = %v, want \"10\"", resp.Reply.ResourceRecords[1].Distance)
	}
}

func TestDnsListRecordsOperationPath(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(listRecordsBody))
	}))
	defer srv.Close()
	c := New("k", WithBaseURL(srv.URL+"/api"), WithHTTPClient(srv.Client()))

	if _, err := c.DnsListRecords(context.Background(), DnsListRecordsParameters{Domain: "example.com"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := "/api/" + OperationDnsListRecords; gotPath != want {
		t.Fatalf("path = %q, want %q", gotPath, want)
	}
}

func TestDnsUpdateRecord(t *testing.T) {
	body := `{"request":{"operation":"dnsUpdateRecord"},"reply":{"code":300,"detail":"success"}}`
	c, gotQuery := newTestClient(t, http.StatusOK, body)

	resp, err := c.DnsUpdateRecord(context.Background(), DnsUpdateRecordParameters{
		Domain:  "example.com",
		RRID:    "aaa",
		RRHost:  "home.example.com",
		RRValue: "203.0.113.7",
		RRTTL:   "7207",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]string{
		"version": "1",
		"type":    "json",
		"key":     "test-key",
		"domain":  "example.com",
		"rrid":    "aaa",
		"rrhost":  "home.example.com",
		"rrvalue": "203.0.113.7",
		"rrttl":   "7207",
	}
	for k, v := range want {
		if got := gotQuery.Get(k); got != v {
			t.Errorf("query %q = %q, want %q", k, got, v)
		}
	}
	if resp.Reply.Code != ReplyCodeSuccess {
		t.Errorf("reply code = %d, want %d", resp.Reply.Code, ReplyCodeSuccess)
	}
}

func TestRequestErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		wantErr string
	}{
		{
			name:    "non-200 includes status and body snippet",
			status:  http.StatusInternalServerError,
			body:    "internal error",
			wantErr: "unexpected HTTP status code: 500: internal error",
		},
		{
			name:    "malformed json",
			status:  http.StatusOK,
			body:    "{not json",
			wantErr: "decode response",
		},
		{
			name:    "html error page",
			status:  http.StatusOK,
			body:    "<html><body>maintenance</body></html>",
			wantErr: "decode response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newTestClient(t, tt.status, tt.body)

			_, err := c.DnsListRecords(context.Background(), DnsListRecordsParameters{Domain: "example.com"})
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err, tt.wantErr)
			}
			// Errors are prefixed with the operation for attributability.
			if !strings.Contains(err.Error(), OperationDnsListRecords) {
				t.Fatalf("error %q missing operation prefix", err)
			}
		})
	}
}

// A non-300 reply code is a valid HTTP response; the client surfaces it rather
// than turning it into an error, so callers can label it in metrics.
func TestNonSuccessReplyCodeIsNotAnError(t *testing.T) {
	body := `{"reply":{"code":110,"detail":"invalid api key"}}`
	c, _ := newTestClient(t, http.StatusOK, body)

	resp, err := c.DnsListRecords(context.Background(), DnsListRecordsParameters{Domain: "example.com"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Reply.Code != 110 || resp.Reply.Detail != "invalid api key" {
		t.Fatalf("unexpected reply: %+v", resp.Reply)
	}
}

func TestRequestHonoursContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := New("k", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.DnsListRecords(ctx, DnsListRecordsParameters{Domain: "example.com"}); err == nil {
		t.Fatal("expected error from cancelled context")
	}
}
