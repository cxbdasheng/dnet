package ddns

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cxbdasheng/dnet/helper"
	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
)

// updateProviderRecords is shared by the DNSLA and Porkbun adapters.
// syncRecord returns whether a remote write was made. An unchanged remote value
// still refreshes the local cache, but must not emit a success webhook.
func (b *BaseDNSProvider) updateProviderRecords(syncRecord func(string, string) (bool, error)) []RecordResult {
	if b.Group == nil {
		return nil
	}
	valid := filterValidRecords(b.Group, b.Caches)
	if b.Group.Domain == "" || b.Group.AccessKey == "" || b.Group.AccessSecret == "" {
		return createErrorResults(valid, InitFailed, "配置不完整（需要 API Key 和 API Secret）")
	}
	for _, vr := range valid {
		if vr.record.Type == RecordTypeCNAME && len(valid) > 1 {
			return createErrorResults(valid, UpdatedFailed, "CNAME 记录不能与同名其他记录同时配置")
		}
	}
	results := make([]RecordResult, 0, len(valid))
	for _, vr := range valid {
		record, cache := vr.record, vr.cache
		if record.Type != RecordTypeA && record.Type != RecordTypeAAAA && record.Type != RecordTypeCNAME && record.Type != RecordTypeTXT {
			results = append(results, RecordResult{RecordType: record.Type, Status: UpdatedFailed,
				ErrorMessage: "不支持的记录类型", ShouldWebhook: shouldSendWebhook(cache, UpdatedFailed)})
			continue
		}
		value, result, ok := getCurrentValue(b.GetServiceName(), record, cache)
		if !ok {
			results = append(results, result)
			continue
		}
		// Only address records use dynamic IP caches, even if a hand-edited
		// CNAME/TXT configuration contains a stale IPType field.
		staticRecord := *record
		if record.Type == RecordTypeCNAME || record.Type == RecordTypeTXT {
			staticRecord.IPType = ""
			record = &staticRecord
		}
		if skip, skipped := checkDynamicCache(b.GetServiceName(), record, cache, value, &result); skip {
			results = append(results, skipped)
			continue
		}
		changed, err := syncRecord(record.Type, value)
		if err != nil {
			result.Status = UpdatedFailed
			result.ErrorMessage = err.Error()
			result.ShouldWebhook = shouldSendWebhook(cache, UpdatedFailed)
			helper.Error(helper.LogTypeDDNS, "[%s] [%s] DNS 更新失败: %v", b.GetServiceName(), record.Type, err)
		} else if changed {
			finalizeSuccess(b.GetServiceName(), record, cache, value, &result)
		} else {
			if IsDynamicType(record.IPType) {
				cache.UpdateDynamicIP(getCacheKey(record.IPType, record.Value, record.Regex), value)
			}
			cache.HasRun = true
			cache.TimesFailed = 0
			cache.forcedNoChange = false
			cache.ResetTimes()
			result.Status = UpdatedNothing
			result.ShouldWebhook = false
		}
		results = append(results, result)
	}
	return results
}

// addressDomainParts handles public suffixes such as co.uk, IDNs and apex names.
func addressDomainParts(domain string) (zone, host string, err error) {
	domain = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domain)), ".")
	wildcard := strings.HasPrefix(domain, "*.")
	// DNS owner names may contain underscores (e.g. _acme-challenge).
	domain, err = idna.New(idna.MapForLookup(), idna.StrictDomainName(false)).ToASCII(strings.TrimPrefix(domain, "*."))
	if err != nil {
		return "", "", fmt.Errorf("无效域名: %w", err)
	}
	zone, err = publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil {
		return "", "", fmt.Errorf("无法确定根域名: %w", err)
	}
	if domain != zone {
		host = strings.TrimSuffix(domain, "."+zone)
	}
	if wildcard {
		if host == "" {
			host = "*"
		} else {
			host = "*." + host
		}
	}
	return zone, host, nil
}

func providerRecordValueEqual(recordType, existing, desired string) bool {
	if recordType == RecordTypeCNAME {
		return strings.EqualFold(strings.TrimSuffix(existing, "."), strings.TrimSuffix(desired, "."))
	}
	// TXT data is case sensitive; quotes and whitespace are part of the value.
	return existing == desired
}

func addressTTL(raw string, minimum int) int {
	raw = strings.ToLower(strings.TrimSpace(raw))
	seconds, err := strconv.Atoi(raw)
	if err != nil {
		if duration, parseErr := time.ParseDuration(raw); parseErr == nil {
			seconds = int(duration / time.Second)
		}
	}
	if seconds <= 0 {
		seconds = 600
	}
	if seconds < minimum {
		seconds = minimum
	}
	return seconds
}

func addressJSONResponse(req *http.Request, result any) error {
	req.Header.Set("Content-Type", "application/json")
	resp, err := helper.CreateHTTPClient().Do(req)
	if err != nil {
		return fmt.Errorf("DNS API 请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("DNS API HTTP 状态码: %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(result); err != nil {
		return fmt.Errorf("DNS API 响应格式无效: %w", err)
	}
	return nil
}
