package forward

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/cxbdasheng/dnet/helper"
)

// A connected target socket per source keeps reply routing and datagram boundaries intact.
// Queues are bounded, including while DNS resolution is pending; overload drops packets.
type udpSession struct {
	ctx      context.Context
	cancel   context.CancelFunc
	packets  chan []byte
	activity atomic.Int64
}

// Caller holds e.mu. Configuration changes invalidate existing UDP mappings.
func (e *entry) clearUDPSessions() {
	for key, session := range e.sessions {
		session.cancel()
		delete(e.sessions, key)
	}
}

func (e *entry) receiveUDP() {
	defer e.wg.Done()
	defer func() { e.mu.Lock(); e.listening = false; e.clearUDPSessions(); e.mu.Unlock() }()
	buffer := make([]byte, 65535)
	for {
		n, source, err := e.packet.ReadFrom(buffer)
		if err != nil {
			if e.ctx.Err() == nil {
				e.fail(fmt.Errorf("UDP 接收失败: %w", err))
			}
			return
		}
		e.mu.Lock()
		r := e.rule
		if e.ctx.Err() != nil {
			e.mu.Unlock()
			return
		}
		if !allowed(source, r) {
			e.mu.Unlock()
			helper.RuleLog(helper.LogLevelDEBUG, r.ID, "[%s] UDP 来源不在白名单: %s", r.Name, source)
			continue
		}
		key := source.String()
		session := e.sessions[key]
		if session == nil {
			if len(e.sessions) >= r.MaxConnections {
				e.mu.Unlock()
				helper.RuleLog(helper.LogLevelDEBUG, r.ID, "[%s] UDP 会话数达到上限", r.Name)
				continue
			}
			ctx, cancel := context.WithCancel(e.ctx)
			session = &udpSession{ctx: ctx, cancel: cancel, packets: make(chan []byte, 16)}
			session.activity.Store(time.Now().UnixNano())
			e.sessions[key] = session
			e.wg.Add(1)
			go e.relayUDP(source, r, session)
		}
		select {
		case session.packets <- append([]byte{}, buffer[:n]...):
		default:
			helper.RuleLog(helper.LogLevelDEBUG, r.ID, "[%s] UDP 队列已满，丢弃数据报", r.Name)
		}
		e.mu.Unlock()
	}
}

func (e *entry) relayUDP(source net.Addr, r Rule, session *udpSession) {
	defer e.wg.Done()
	defer session.cancel()
	defer func() {
		e.mu.Lock()
		if e.sessions[source.String()] == session {
			delete(e.sessions, source.String())
		}
		e.mu.Unlock()
	}()
	target, err := (&net.Dialer{Timeout: time.Duration(r.DialTimeoutSec) * time.Second}).DialContext(session.ctx, "udp", net.JoinHostPort(r.TargetHost, strconv.Itoa(r.TargetPort)))
	if err != nil {
		if session.ctx.Err() == nil {
			e.fail(fmt.Errorf("UDP 目标连接失败: %w", err))
		}
		return
	}
	defer target.Close()
	remote := target.RemoteAddr().(*net.UDPAddr)
	local := target.LocalAddr().(*net.UDPAddr)
	if remote.Port == r.ListenPort && (remote.IP.Equal(net.ParseIP(r.ListenAddress)) || (net.ParseIP(r.ListenAddress).IsUnspecified() && remote.IP.Equal(local.IP))) {
		e.fail(fmt.Errorf("UDP 目标指向转发入口"))
		return
	}
	helper.RuleLog(helper.LogLevelDEBUG, r.ID, "[%s] UDP 会话建立 [来源=%s, 目标=%s]", r.Name, source, target.RemoteAddr())
	defer helper.RuleLog(helper.LogLevelDEBUG, r.ID, "[%s] UDP 会话关闭 [来源=%s]", r.Name, source)
	replies := make(chan error, 1)
	go func() {
		buffer := make([]byte, 65535)
		for {
			n, err := target.Read(buffer)
			if err != nil {
				replies <- err
				return
			}
			if session.ctx.Err() != nil {
				replies <- session.ctx.Err()
				return
			}
			written, err := e.packet.WriteTo(buffer[:n], source)
			if err != nil {
				replies <- err
				return
			}
			e.download.Add(int64(written))
			session.activity.Store(time.Now().UnixNano())
		}
	}()
	// Always join the reader, even on cancellation or expiry.
	readerDone := false
	defer func() {
		target.Close()
		if !readerDone {
			<-replies
		}
	}()
	idle := time.Duration(r.IdleTimeoutSec) * time.Second
	if idle == 0 {
		idle = 60 * time.Second
	}
	ticker := time.NewTicker(min(idle, time.Second))
	defer ticker.Stop()
	for {
		select {
		case <-session.ctx.Done():
			return
		case err := <-replies:
			readerDone = true
			if session.ctx.Err() == nil {
				e.fail(fmt.Errorf("UDP 回包失败: %w", err))
			}
			return
		case <-ticker.C:
			if time.Since(time.Unix(0, session.activity.Load())) >= idle {
				return
			}
		case packet := <-session.packets:
			if session.ctx.Err() != nil {
				return
			}
			target.SetWriteDeadline(time.Now().Add(time.Duration(r.DialTimeoutSec) * time.Second))
			n, err := target.Write(packet)
			if err != nil {
				if session.ctx.Err() == nil {
					e.fail(fmt.Errorf("UDP 转发失败: %w", err))
				}
				return
			}
			e.upload.Add(int64(n))
			session.activity.Store(time.Now().UnixNano())
		}
	}
}
