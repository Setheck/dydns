package namesilo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/google/go-querystring/query"
)

const (
	// DefaultBaseURL is the NameSilo API root, including the version-less /api path.
	DefaultBaseURL = "https://www.namesilo.com/api"
	// DefaultPublicIPURL returns the caller's public IP as a bare string body.
	DefaultPublicIPURL = "https://ifconfig.me/ip"

	defaultTimeout = 15 * time.Second

	// maxErrorBodySnippet bounds how much of an unexpected response body ends up
	// in an error message.
	maxErrorBodySnippet = 256
	// maxPublicIPBody is generous for an IPv6 address plus a trailing newline.
	maxPublicIPBody = 64
)

type Request struct {
	Operation string `json:"operation"`
	IP        string `json:"ip"`
}

type Reply struct {
	Code            int              `json:"code"`
	Detail          string           `json:"detail"`
	ResourceRecords []ResourceRecord `json:"resource_record"`
}

type ResourceRecord struct {
	RecordID string `json:"record_id"`
	Type     string `json:"type"`
	Host     string `json:"host"`
	Value    string `json:"value"`
	TTL      int    `json:"ttl"`
	Distance any    `json:"distance"` // distance seems to be string or int
}

type Client struct {
	httpClient *http.Client
	baseURL    string

	apiKey string
}

// Option customizes a Client.
type Option func(*Client)

// WithHTTPClient replaces the default client, e.g. to share an instrumented transport.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithBaseURL points the client at an alternate API root, mainly for tests.
func WithBaseURL(u string) Option {
	return func(c *Client) {
		if u != "" {
			c.baseURL = strings.TrimSuffix(u, "/")
		}
	}
}

func New(apiKey string, opts ...Option) *Client {
	c := &Client{
		httpClient: &http.Client{Timeout: defaultTimeout},
		baseURL:    DefaultBaseURL,
		apiKey:     apiKey,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// get issues an API call and decodes the JSON response into out. params is
// encoded via its `url` struct tags.
func (c *Client) get(ctx context.Context, operation string, params, out any) error {
	values, err := query.Values(params)
	if err != nil {
		return fmt.Errorf("%s: encode parameters: %w", operation, err)
	}
	values.Set("version", "1")
	values.Set("type", "json")
	values.Set("key", c.apiKey)

	u := c.baseURL + "/" + operation + "?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("%s: build request: %w", operation, err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", operation, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: unexpected HTTP status code: %d: %s",
			operation, resp.StatusCode, bodySnippet(resp.Body))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s: decode response: %w", operation, err)
	}
	return nil
}

func bodySnippet(r io.Reader) string {
	b, err := io.ReadAll(io.LimitReader(r, maxErrorBodySnippet))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// PublicIPCheck resolves the caller's public IP. A nil hc falls back to a client
// with a default timeout; an empty rawURL falls back to DefaultPublicIPURL.
func PublicIPCheck(ctx context.Context, hc *http.Client, rawURL string) (net.IP, error) {
	if hc == nil {
		hc = &http.Client{Timeout: defaultTimeout}
	}
	if rawURL == "" {
		rawURL = DefaultPublicIPURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status code: %d: %s",
			resp.StatusCode, bodySnippet(resp.Body))
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxPublicIPBody))
	if err != nil {
		return nil, err
	}

	body := strings.TrimSpace(string(raw))
	ip := net.ParseIP(body)
	if ip == nil {
		return nil, fmt.Errorf("unparsable public IP response: %q", body)
	}
	return ip, nil
}
