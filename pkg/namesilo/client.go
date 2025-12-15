package namesilo

import (
	"io"
	"net"
	"net/http"
	"net/url"
)

const (
	nameSiloHost = "www.namesilo.com"
	apiBase      = "api"
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
	TTL      string `json:"ttl"`
	Distance any    `json:"distance"` // distance seems to be string or int
}

type Client struct {
	httpClient *http.Client

	apiKey string
}

func New(apiKey string) *Client {
	return &Client{
		httpClient: http.DefaultClient,
		apiKey:     apiKey,
	}
}

func (c *Client) baseParams() map[string]string {
	return map[string]string{
		"version": "1",
		"type":    "json",
		"key":     c.apiKey,
	}
}

func PublicIPCheck() (net.IP, error) {
	u := &url.URL{
		Scheme: "https",
		Host:   "ifconfig.me",
		Path:   "ip",
	}

	resp, err := http.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return net.ParseIP(string(raw)), nil
}
