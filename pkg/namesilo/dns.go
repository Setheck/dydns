package namesilo

import "context"

// NameSilo API operations.
const (
	OperationDnsListRecords  = "dnsListRecords"
	OperationDnsUpdateRecord = "dnsUpdateRecord"
)

// ReplyCodeSuccess is NameSilo's "success" reply code.
const ReplyCodeSuccess = 300

type DnsListRecordsParameters struct {
	Domain string `url:"domain"`
}
type DnsListRecordsResponse struct {
	Request Request `json:"request"`
	Reply   Reply   `json:"reply"`
}

func (c *Client) DnsListRecords(ctx context.Context, params DnsListRecordsParameters) (*DnsListRecordsResponse, error) {
	result := &DnsListRecordsResponse{}
	if err := c.get(ctx, OperationDnsListRecords, params, result); err != nil {
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
	result := &DnsUpdateRecordResponse{}
	if err := c.get(ctx, OperationDnsUpdateRecord, params, result); err != nil {
		return nil, err
	}
	return result, nil
}
