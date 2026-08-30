package namesilo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicIPCheck(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    string
		wantErr string
	}{
		{name: "plain ipv4", status: http.StatusOK, body: "203.0.113.7", want: "203.0.113.7"},
		{name: "trailing newline", status: http.StatusOK, body: "203.0.113.7\n", want: "203.0.113.7"},
		{name: "surrounding whitespace", status: http.StatusOK, body: "  203.0.113.7 \r\n", want: "203.0.113.7"},
		{name: "ipv6", status: http.StatusOK, body: "2001:db8::1\n", want: "2001:db8::1"},
		{name: "non-200", status: http.StatusBadGateway, body: "upstream down", wantErr: "unexpected HTTP status code: 502"},
		{name: "unparsable body", status: http.StatusOK, body: "not-an-ip", wantErr: `unparsable public IP response: "not-an-ip"`},
		{name: "html error page", status: http.StatusOK, body: "<html>oops</html>", wantErr: "unparsable public IP response"},
		{name: "empty body", status: http.StatusOK, body: "", wantErr: `unparsable public IP response: ""`},
		// The response is read through a LimitReader, so an oversized body is
		// truncated mid-address rather than read into memory unbounded.
		{name: "oversized body", status: http.StatusOK, body: strings.Repeat("9", 200), wantErr: "unparsable public IP response"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			got, err := PublicIPCheck(context.Background(), srv.Client(), srv.URL)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got ip %v", tt.wantErr, got)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error %q does not contain %q", err, tt.wantErr)
				}
				if got != nil {
					t.Fatalf("expected nil ip on error, got %v", got)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.String() != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPublicIPCheckContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := PublicIPCheck(ctx, srv.Client(), srv.URL); err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestPublicIPCheckDefaults(t *testing.T) {
	// A nil client and empty URL must not panic; they fall back to a
	// timeout-bearing client and the default provider URL.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := PublicIPCheck(ctx, nil, ""); err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestNewOptions(t *testing.T) {
	c := New("key")
	if c.baseURL != DefaultBaseURL {
		t.Fatalf("default baseURL = %q, want %q", c.baseURL, DefaultBaseURL)
	}
	if c.httpClient.Timeout != defaultTimeout {
		t.Fatalf("default timeout = %v, want %v", c.httpClient.Timeout, defaultTimeout)
	}

	hc := &http.Client{}
	c = New("key", WithBaseURL("http://example.test/api/"), WithHTTPClient(hc))
	if c.baseURL != "http://example.test/api" {
		t.Fatalf("baseURL = %q, want trailing slash trimmed", c.baseURL)
	}
	if c.httpClient != hc {
		t.Fatal("WithHTTPClient did not take effect")
	}

	// Zero values must not clobber the defaults.
	c = New("key", WithBaseURL(""), WithHTTPClient(nil))
	if c.baseURL != DefaultBaseURL || c.httpClient == nil {
		t.Fatal("empty options clobbered defaults")
	}
}
