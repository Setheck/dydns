package namesilo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"

	"github.com/google/go-querystring/query"
)

type DnsListRecordsParameters struct {
	Domain string `url:"domain"`
}
type DnsListRecordsResponse struct {
	Request Request `json:"request"`
	Reply   Reply   `json:"reply"`
}

func (c *Client) DnsListRecords(ctx context.Context, params DnsListRecordsParameters) (*DnsListRecordsResponse, error) {
	values, err := query.Values(params)
	if err != nil {
		return nil, err
	}
	for k, v := range c.baseParams() {
		values.Set(k, v)
	}
	u := &url.URL{
		Scheme:   "https",
		Host:     nameSiloHost,
		Path:     path.Join(apiBase, "dnsListRecords"),
		RawQuery: values.Encode(),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	default:
		return nil, fmt.Errorf("unexpected HTTP status code: %d", resp.StatusCode)
	}

	result := &DnsListRecordsResponse{}
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return nil, err
	}

	return result, nil
}

type DnsUpdateRecordParameters struct {
	Domain  string `url:"domain"`
	RRID    string `url:"rrid"`
	RRHost  string `url:"rrhost"`
	RRValue string `url:"rrvalue"`
	RRTTL   string `url:"rrttl"`
}
type DnsUpdateRecordResponse struct {
	Request Request `json:"request"`
	Reply   Reply   `json:"reply"`
}

func (c *Client) DnsUpdateRecord(ctx context.Context, params DnsUpdateRecordParameters) (*DnsUpdateRecordResponse, error) {
	values, err := query.Values(params)
	if err != nil {
		return nil, err
	}
	for k, v := range c.baseParams() {
		values.Set(k, v)
	}
	u := &url.URL{
		Scheme:   "https",
		Host:     nameSiloHost,
		Path:     path.Join(apiBase, "dnsUpdateRecord"),
		RawQuery: values.Encode(),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	default:
		return nil, fmt.Errorf("unexpected HTTP status code: %d", resp.StatusCode)
	}

	result := &DnsUpdateRecordResponse{}
	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return nil, err
	}

	return result, nil
}
