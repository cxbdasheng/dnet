package ddns

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

var porkbunAPIEndpoint = "https://api.porkbun.com/api/json/v3/dns"

// Porkbun uses AccessKey=API Key and AccessSecret=Secret API Key.
type Porkbun struct{ BaseDNSProvider }

type porkbunResponse struct {
	Status  string `json:"status"`
	Records []struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		Type    string `json:"type"`
		Content string `json:"content"`
	} `json:"records"`
}

func (p *Porkbun) UpdateOrCreateRecords() []RecordResult {
	return p.updateProviderRecords(p.syncRecord)
}

func (p *Porkbun) syncRecord(recordType, value string) (bool, error) {
	zone, host, err := addressDomainParts(p.Group.Domain)
	if err != nil {
		return false, err
	}
	var response porkbunResponse
	path := "/retrieveByNameType/" + url.PathEscape(zone) + "/" + recordType
	if host != "" {
		path += "/" + url.PathEscape(host)
	}
	if err := p.request(path, nil, &response); err != nil {
		return false, err
	}
	if response.Records == nil {
		return false, fmt.Errorf("Porkbun 返回的记录列表缺失")
	}
	if len(response.Records) > 1 {
		return false, fmt.Errorf("Porkbun 存在多条同名同类型记录，请仅保留一条 DDNS 记录")
	}
	body := map[string]any{"name": host, "type": recordType, "content": value, "ttl": strconv.Itoa(addressTTL(p.Group.TTL, 600))}
	path = "/create/" + url.PathEscape(zone)
	if len(response.Records) == 1 {
		record := response.Records[0]
		name := zone
		if host != "" {
			name = host + "." + zone
		}
		if record.ID == "" || record.Name != name || record.Type != recordType {
			return false, fmt.Errorf("Porkbun 返回的记录与请求不匹配")
		}
		if providerRecordValueEqual(recordType, record.Content, value) {
			return false, nil
		}
		// Edit by ID so other records are never replaced as a side effect.
		path = "/edit/" + url.PathEscape(zone) + "/" + url.PathEscape(record.ID)
	}
	var updated porkbunResponse
	err = p.request(path, body, &updated)
	return err == nil, err
}

func (p *Porkbun) request(path string, body map[string]any, result *porkbunResponse) error {
	if body == nil {
		body = make(map[string]any)
	}
	body["apikey"] = p.Group.AccessKey
	body["secretapikey"] = p.Group.AccessSecret
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, porkbunAPIEndpoint+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	if err := addressJSONResponse(req, result); err != nil {
		return err
	}
	if result.Status != "SUCCESS" {
		return fmt.Errorf("Porkbun API 返回失败状态，请检查凭证和域名 API 权限")
	}
	return nil
}
