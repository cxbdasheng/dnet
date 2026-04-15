package bootstrap

import (
	"testing"

	"github.com/cxbdasheng/dnet/config"
	"github.com/cxbdasheng/dnet/ddns"
)

func TestProcessDDNSServices_ReusesCachesWhenGroupOrderChanges(t *testing.T) {
	prevForceCompare := ddns.ForceCompareGlobal
	defer func() {
		ddns.ForceCompareGlobal = prevForceCompare
	}()

	runner := &Runner{}
	firstConf := &config.Config{
		DDNSConfig: config.DDNSConfig{
			DDNSEnabled: true,
			DDNS: []config.DNSGroup{
				{
					ID:      "group-1",
					Domain:  "a.example.com",
					Service: ddns.ProviderAliDNS,
					Records: []config.DNSRecord{
						{Type: ddns.RecordTypeA, Value: "1.2.3.4"},
					},
				},
				{
					ID:      "group-2",
					Domain:  "b.example.com",
					Service: ddns.ProviderAliDNS,
					Records: []config.DNSRecord{
						{Type: ddns.RecordTypeTXT, Value: "hello"},
					},
				},
			},
		},
	}

	ddns.ForceCompareGlobal = true
	runner.processDDNSServices(firstConf)

	group1Key := buildDDNSCacheKey(&firstConf.DDNSConfig.DDNS[0], &firstConf.DDNSConfig.DDNS[0].Records[0])
	group2Key := buildDDNSCacheKey(&firstConf.DDNSConfig.DDNS[1], &firstConf.DDNSConfig.DDNS[1].Records[0])

	group1Cache := runner.ddnsCaches[group1Key]
	group2Cache := runner.ddnsCaches[group2Key]
	if group1Cache == nil || group2Cache == nil {
		t.Fatal("expected caches to be created for both records")
	}

	group1Cache.HasRun = true
	group2Cache.Times = 2

	secondConf := &config.Config{
		DDNSConfig: config.DDNSConfig{
			DDNSEnabled: true,
			DDNS: []config.DNSGroup{
				firstConf.DDNSConfig.DDNS[1],
				firstConf.DDNSConfig.DDNS[0],
			},
		},
	}

	ddns.ForceCompareGlobal = false
	runner.processDDNSServices(secondConf)

	if got := runner.ddnsCaches[group1Key]; got != group1Cache {
		t.Fatal("group-1 cache should be reused after group reorder")
	}
	if !runner.ddnsCaches[group1Key].HasRun {
		t.Fatal("group-1 cache state should be preserved after group reorder")
	}
	if got := runner.ddnsCaches[group2Key]; got != group2Cache {
		t.Fatal("group-2 cache should be reused after group reorder")
	}
	if runner.ddnsCaches[group2Key].Times != 2 {
		t.Fatal("group-2 cache state should be preserved after group reorder")
	}
}

func TestProcessDDNSServices_RebindsCachesWhenValidRecordsChange(t *testing.T) {
	prevForceCompare := ddns.ForceCompareGlobal
	defer func() {
		ddns.ForceCompareGlobal = prevForceCompare
	}()

	runner := &Runner{}
	firstConf := &config.Config{
		DDNSConfig: config.DDNSConfig{
			DDNSEnabled: true,
			DDNS: []config.DNSGroup{
				{
					ID:      "group-1",
					Domain:  "records.example.com",
					Service: ddns.ProviderAliDNS,
					Records: []config.DNSRecord{
						{Type: ddns.RecordTypeA, Value: "1.2.3.4"},
						{Type: ddns.RecordTypeAAAA, Value: ""},
						{Type: ddns.RecordTypeTXT, Value: "v=spf1"},
					},
				},
			},
		},
	}

	ddns.ForceCompareGlobal = true
	runner.processDDNSServices(firstConf)

	aKey := buildDDNSCacheKey(&firstConf.DDNSConfig.DDNS[0], &firstConf.DDNSConfig.DDNS[0].Records[0])
	txtKey := buildDDNSCacheKey(&firstConf.DDNSConfig.DDNS[0], &firstConf.DDNSConfig.DDNS[0].Records[2])

	aCache := runner.ddnsCaches[aKey]
	txtCache := runner.ddnsCaches[txtKey]
	if aCache == nil || txtCache == nil {
		t.Fatal("expected caches to be created for active records")
	}

	aCache.HasRun = true
	txtCache.Times = 1

	secondConf := &config.Config{
		DDNSConfig: config.DDNSConfig{
			DDNSEnabled: true,
			DDNS: []config.DNSGroup{
				{
					ID:      "group-1",
					Domain:  "records.example.com",
					Service: ddns.ProviderAliDNS,
					Records: []config.DNSRecord{
						{Type: ddns.RecordTypeA, Value: ""},
						{Type: ddns.RecordTypeAAAA, Value: "::1"},
						{Type: ddns.RecordTypeTXT, Value: "v=spf1"},
					},
				},
			},
		},
	}

	ddns.ForceCompareGlobal = false
	runner.processDDNSServices(secondConf)

	aaaaKey := buildDDNSCacheKey(&secondConf.DDNSConfig.DDNS[0], &secondConf.DDNSConfig.DDNS[0].Records[1])
	aaaaCache := runner.ddnsCaches[aaaaKey]
	if aaaaCache == nil {
		t.Fatal("expected cache to be created for newly active AAAA record")
	}
	if aaaaCache == aCache {
		t.Fatal("newly active AAAA record should not reuse the old A record cache")
	}
	if aaaaCache.HasRun {
		t.Fatal("newly active AAAA record should start with a fresh cache state")
	}

	if got := runner.ddnsCaches[txtKey]; got != txtCache {
		t.Fatal("unchanged TXT record should keep its existing cache")
	}
	if runner.ddnsCaches[txtKey].Times != 1 {
		t.Fatal("unchanged TXT record should preserve cache state")
	}
	if _, exists := runner.ddnsCaches[aKey]; exists {
		t.Fatal("cache for inactive A record should be removed")
	}
}
