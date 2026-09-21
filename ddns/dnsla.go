package ddns

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

var dnslaAPIEndpoint = "https://api.dns.la/api"

// DNSLA uses AccessKey and AccessSecret for HTTP Basic authentication.
type DNSLA struct{ BaseDNSProvider }

type dnslaRecord struct {
	ID   string `json:"id"`
	Host string `json:"host"`
	Type int    `json:"type"`
	Data string `json:"data"`
}

type dnslaResponse struct {
	Code int `json:"code"`
	Data *struct {
		ID      string        `json:"id"`
		Domain  string        `json:"domain"`
		Total   int           `json:"total"`
		Results []dnslaRecord `json:"results"`
	} `json:"data"`
}

func (d *DNSLA) UpdateOrCreateRecords() []RecordResult {
	return d.updateProviderRecords(d.syncRecord)
}

func (d *DNSLA) syncRecord(recordType, value string) (bool, error) {
	zone, host, err := addressDomainParts(d.Group.Domain)
	if err != nil {
		return false, err
	}
	if host == "" {
		host = "@"
	}
	typeID, ok := map[string]int{RecordTypeA: 1, RecordTypeAAAA: 28, RecordTypeCNAME: 5, RecordTypeTXT: 16}[recordType]
	if !ok {
		return false, fmt.Errorf("DNSLA 不支持的记录类型: %s", recordType)
	}
	// The official record endpoints require a domain ID, not a domain name.
	var domain dnslaResponse
	if err := d.request(http.MethodGet, "/domain?"+url.Values{"domain": {zone}}.Encode(), nil, &domain); err != nil {
		return false, err
	}
	if domain.Data == nil || domain.Data.ID == "" || !strings.EqualFold(strings.TrimSuffix(domain.Data.Domain, "."), zone) {
		return false, fmt.Errorf("DNSLA 未返回匹配的域名 ID")
	}
	query := url.Values{"domainId": {domain.Data.ID}, "host": {host}, "type": {strconv.Itoa(typeID)}, "pageIndex": {"1"}, "pageSize": {"100"}}
	var response dnslaResponse
	if err := d.request(http.MethodGet, "/recordList?"+query.Encode(), nil, &response); err != nil {
		return false, err
	}
	if response.Data == nil {
		return false, fmt.Errorf("DNSLA 返回的记录列表缺失")
	}
	// Never choose an arbitrary record when multiple routing lines/values exist.
	if response.Data.Total > 1 || len(response.Data.Results) > 1 {
		return false, fmt.Errorf("DNSLA 存在多条同名同类型记录，请仅保留一条 DDNS 记录")
	}
	body := map[string]any{"host": host, "type": typeID, "data": value, "ttl": addressTTL(d.Group.TTL, 1)}
	method := http.MethodPost
	if len(response.Data.Results) == 1 {
		record := response.Data.Results[0]
		if record.ID == "" || record.Host != host || record.Type != typeID {
			return false, fmt.Errorf("DNSLA 返回的记录与请求不匹配")
		}
		if providerRecordValueEqual(recordType, record.Data, value) {
			return false, nil
		}
		method = http.MethodPut
		body["id"] = record.ID
	} else {
		if response.Data.Total != 0 {
			return false, fmt.Errorf("DNSLA 返回的记录列表不完整")
		}
		body["domainId"] = domain.Data.ID
	}
	var updated dnslaResponse
	err = d.request(method, "/record", body, &updated)
	return err == nil, err
}

func (d *DNSLA) request(method, path string, body any, result *dnslaResponse) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequest(method, dnslaAPIEndpoint+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.SetBasicAuth(d.Group.AccessKey, d.Group.AccessSecret)
	if err := addressJSONResponse(req, result); err != nil {
		return err
	}
	if result.Code != 200 {
		return fmt.Errorf("DNSLA API 错误 (code=%d)", result.Code)
	}
	return nil
}
