package helper

import (
	"context"
	"fmt"
	"net"
	"time"
)

const (
	dnsLookupRoundTimeout = 3 * time.Second
	internetRetryInterval = 5 * time.Second
)

var (
	internetProbeHosts = []string{
		"alidns.aliyuncs.com",
		"dnspod.tencentcloudapi.com",
		"api.cloudflare.com",
	}
	backupDNSServers = []string{
		"223.5.5.5",
		"114.114.114.114",
		"119.29.29.29",
	}
)

type waitInternetDeps struct {
	lookupHost      func(context.Context, *net.Resolver, string) ([]string, error)
	newResolver     func(string) (*net.Resolver, error)
	currentResolver func() *net.Resolver
	setResolver     func(*net.Resolver)
	sleep           func(context.Context, time.Duration) error
	probeHosts      []string
	backupDNS       []string
	lookupTimeout   time.Duration
	retryInterval   time.Duration
}

// WaitInternet 等待基本 DNS 解析能力恢复。调用方通过 ctx 控制最长等待时间。
func WaitInternet(ctx context.Context, customDNS string) error {
	deps := waitInternetDeps{
		lookupHost: func(ctx context.Context, resolver *net.Resolver, host string) ([]string, error) {
			return resolver.LookupHost(ctx, host)
		},
		newResolver:     newDNSResolver,
		currentResolver: currentResolver,
		setResolver:     setApplicationResolver,
		sleep:           sleepContext,
		probeHosts:      internetProbeHosts,
		backupDNS:       backupDNSServers,
		lookupTimeout:   dnsLookupRoundTimeout,
		retryInterval:   internetRetryInterval,
	}
	return waitInternet(ctx, customDNS, deps)
}

func waitInternet(ctx context.Context, customDNS string, deps waitInternetDeps) error {
	if len(deps.probeHosts) == 0 {
		return fmt.Errorf("没有可用的网络探测域名")
	}

	resolver := deps.currentResolver()
	if customDNS != "" {
		customResolver, err := deps.newResolver(customDNS)
		if err != nil {
			return fmt.Errorf("自定义 DNS 配置无效: %w", err)
		}
		resolver = customResolver
		deps.setResolver(resolver)
		Info(LogTypeNetwork, "使用自定义 DNS: %s", customDNS)
	}

	failedOnce := false
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("等待网络连接: %w", err)
		}
		if probeResolver(ctx, resolver, deps) {
			if failedOnce {
				Info(LogTypeNetwork, "网络连接已恢复")
			}
			return nil
		}
		failedOnce = true

		if customDNS == "" {
			fallbackResolver, dnsServer, ok := findBackupResolver(ctx, deps)
			if ok {
				deps.setResolver(fallbackResolver)
				Info(LogTypeNetwork, "系统 DNS 不可用，使用备用 DNS: %s", dnsServer)
				return nil
			}
		}

		Warn(LogTypeNetwork, "网络暂不可用，%s 后重试", deps.retryInterval)
		if err := deps.sleep(ctx, deps.retryInterval); err != nil {
			return fmt.Errorf("等待网络连接: %w", err)
		}
	}
}

func probeResolver(ctx context.Context, resolver *net.Resolver, deps waitInternetDeps) bool {
	lookupCtx, cancel := context.WithTimeout(ctx, deps.lookupTimeout)
	defer cancel()

	results := make(chan bool, len(deps.probeHosts))
	for _, host := range deps.probeHosts {
		go func(host string) {
			addresses, err := deps.lookupHost(lookupCtx, resolver, host)
			results <- err == nil && len(addresses) > 0
		}(host)
	}

	for range deps.probeHosts {
		select {
		case ok := <-results:
			if ok {
				return true
			}
		case <-lookupCtx.Done():
			return false
		}
	}
	return false
}

func findBackupResolver(ctx context.Context, deps waitInternetDeps) (*net.Resolver, string, bool) {
	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type result struct {
		resolver *net.Resolver
		server   string
		ok       bool
	}
	results := make(chan result, len(deps.backupDNS))
	for _, server := range deps.backupDNS {
		go func(server string) {
			resolver, err := deps.newResolver(server)
			if err != nil {
				results <- result{}
				return
			}
			results <- result{resolver: resolver, server: server, ok: probeResolver(probeCtx, resolver, deps)}
		}(server)
	}

	for range deps.backupDNS {
		select {
		case candidate := <-results:
			if candidate.ok {
				return candidate.resolver, candidate.server, true
			}
		case <-ctx.Done():
			return nil, "", false
		}
	}
	return nil, "", false
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
