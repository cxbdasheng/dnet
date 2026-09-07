package helper

import (
	"context"
	"errors"
	"fmt"
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newWaitInternetTestDeps() waitInternetDeps {
	return waitInternetDeps{
		newResolver: func(server string) (*net.Resolver, error) {
			return &net.Resolver{}, nil
		},
		currentResolver: func() *net.Resolver { return &net.Resolver{} },
		setResolver:     func(*net.Resolver) {},
		sleep: func(ctx context.Context, _ time.Duration) error {
			return ctx.Err()
		},
		probeHosts:    []string{"one.example", "two.example"},
		backupDNS:     []string{"backup-one", "backup-two"},
		lookupTimeout: time.Second,
		retryInterval: time.Millisecond,
	}
}

func TestWaitInternetSystemResolverSuccess(t *testing.T) {
	deps := newWaitInternetTestDeps()
	var lookupCount atomic.Int32
	deps.lookupHost = func(_ context.Context, _ *net.Resolver, host string) ([]string, error) {
		lookupCount.Add(1)
		if host == "two.example" {
			return []string{"192.0.2.1"}, nil
		}
		return nil, errors.New("lookup failed")
	}
	deps.newResolver = func(string) (*net.Resolver, error) {
		t.Fatal("不应创建备用 resolver")
		return nil, nil
	}

	if err := waitInternet(context.Background(), "", deps); err != nil {
		t.Fatalf("waitInternet() error = %v", err)
	}
	if lookupCount.Load() == 0 {
		t.Fatal("未执行 DNS 探测")
	}
}

func TestWaitInternetUsesWorkingBackup(t *testing.T) {
	deps := newWaitInternetTestDeps()
	systemResolver := &net.Resolver{}
	backupOne := &net.Resolver{}
	backupTwo := &net.Resolver{}
	deps.currentResolver = func() *net.Resolver { return systemResolver }
	deps.newResolver = func(server string) (*net.Resolver, error) {
		switch server {
		case "backup-one":
			return backupOne, nil
		case "backup-two":
			return backupTwo, nil
		default:
			return nil, fmt.Errorf("unexpected server %q", server)
		}
	}
	deps.lookupHost = func(_ context.Context, resolver *net.Resolver, _ string) ([]string, error) {
		if resolver == backupTwo {
			return []string{"198.51.100.1"}, nil
		}
		return nil, errors.New("lookup failed")
	}
	var selected *net.Resolver
	deps.setResolver = func(resolver *net.Resolver) { selected = resolver }

	if err := waitInternet(context.Background(), "", deps); err != nil {
		t.Fatalf("waitInternet() error = %v", err)
	}
	if selected != backupTwo {
		t.Fatalf("selected resolver = %p, want backupTwo %p", selected, backupTwo)
	}
}

func TestWaitInternetCustomDNSOnly(t *testing.T) {
	deps := newWaitInternetTestDeps()
	customResolver := &net.Resolver{}
	var created []string
	deps.newResolver = func(server string) (*net.Resolver, error) {
		created = append(created, server)
		if server != "custom-dns" {
			t.Fatalf("不应创建备用 resolver: %s", server)
		}
		return customResolver, nil
	}
	deps.lookupHost = func(_ context.Context, resolver *net.Resolver, _ string) ([]string, error) {
		if resolver != customResolver {
			t.Fatal("探测未使用自定义 resolver")
		}
		return []string{"203.0.113.1"}, nil
	}
	var selected *net.Resolver
	deps.setResolver = func(resolver *net.Resolver) { selected = resolver }

	if err := waitInternet(context.Background(), "custom-dns", deps); err != nil {
		t.Fatalf("waitInternet() error = %v", err)
	}
	if selected != customResolver {
		t.Fatal("未发布自定义 resolver")
	}
	if !reflect.DeepEqual(created, []string{"custom-dns"}) {
		t.Fatalf("created resolvers = %v", created)
	}
}

func TestWaitInternetInvalidCustomDNS(t *testing.T) {
	deps := newWaitInternetTestDeps()
	invalidErr := errors.New("invalid DNS")
	deps.newResolver = func(string) (*net.Resolver, error) {
		return nil, invalidErr
	}
	deps.lookupHost = func(context.Context, *net.Resolver, string) ([]string, error) {
		t.Fatal("无效配置不应执行 lookup")
		return nil, nil
	}

	err := waitInternet(context.Background(), "bad", deps)
	if !errors.Is(err, invalidErr) {
		t.Fatalf("error = %v, want invalidErr", err)
	}
	if got := err.Error(); got != "自定义 DNS 配置无效: invalid DNS" {
		t.Fatalf("error = %q", got)
	}
}

func TestWaitInternetCustomDNSTimesOutWithoutFallback(t *testing.T) {
	deps := newWaitInternetTestDeps()
	customResolver := &net.Resolver{}
	var created []string
	deps.newResolver = func(server string) (*net.Resolver, error) {
		created = append(created, server)
		return customResolver, nil
	}
	deps.lookupHost = func(context.Context, *net.Resolver, string) ([]string, error) {
		return nil, errors.New("lookup failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	deps.sleep = func(context.Context, time.Duration) error {
		cancel()
		return context.Canceled
	}

	err := waitInternet(ctx, "custom-dns", deps)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if !reflect.DeepEqual(created, []string{"custom-dns"}) {
		t.Fatalf("自定义 DNS 失败后发生了 fallback: %v", created)
	}
}

func TestWaitInternetAllResolversFailUntilCanceled(t *testing.T) {
	deps := newWaitInternetTestDeps()
	deps.lookupHost = func(context.Context, *net.Resolver, string) ([]string, error) {
		return nil, errors.New("lookup failed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	var sleepCount int
	deps.sleep = func(context.Context, time.Duration) error {
		sleepCount++
		cancel()
		return context.Canceled
	}

	err := waitInternet(ctx, "", deps)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if sleepCount != 1 {
		t.Fatalf("sleep count = %d, want 1", sleepCount)
	}
}

func TestProbeResolverRejectsEmptyResult(t *testing.T) {
	deps := newWaitInternetTestDeps()
	deps.lookupHost = func(context.Context, *net.Resolver, string) ([]string, error) {
		return nil, nil
	}
	if probeResolver(context.Background(), &net.Resolver{}, deps) {
		t.Fatal("空 lookup 结果不应被视为成功")
	}
}

func TestSleepContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	if err := sleepContext(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("sleepContext() error = %v", err)
	}
	if time.Since(started) > 100*time.Millisecond {
		t.Fatal("取消后未立即返回")
	}
}

func TestProbeResolverConcurrentLookups(t *testing.T) {
	deps := newWaitInternetTestDeps()
	started := make(chan struct{}, len(deps.probeHosts))
	release := make(chan struct{})
	var once sync.Once
	deps.lookupHost = func(context.Context, *net.Resolver, string) ([]string, error) {
		started <- struct{}{}
		<-release
		return []string{"192.0.2.1"}, nil
	}

	done := make(chan bool, 1)
	go func() { done <- probeResolver(context.Background(), &net.Resolver{}, deps) }()
	for range deps.probeHosts {
		select {
		case <-started:
		case <-time.After(time.Second):
			once.Do(func() { close(release) })
			t.Fatal("探测域名未并发查询")
		}
	}
	once.Do(func() { close(release) })
	if !<-done {
		t.Fatal("并发 lookup 成功后返回 false")
	}
}
