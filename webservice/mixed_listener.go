package webservice

import (
	"bufio"
	"crypto/tls"
	"net"
	"sync"
	"time"
)

type peekConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *peekConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

type acceptResult struct {
	conn net.Conn
	err  error
}

// Classify in bounded workers so an idle client cannot block all accepts.
type mixedListener struct {
	net.Listener
	config    *tls.Config
	once      sync.Once
	closeOnce sync.Once
	done      chan struct{}
	ready     chan acceptResult
}

func newMixedListener(l net.Listener, c *tls.Config) *mixedListener {
	return &mixedListener{Listener: l, config: c, done: make(chan struct{}), ready: make(chan acceptResult)}
}
func (l *mixedListener) Accept() (net.Conn, error) {
	l.once.Do(func() { go l.run() })
	select {
	case <-l.done:
		return nil, net.ErrClosed
	case r := <-l.ready:
		return r.conn, r.err
	}
}
func (l *mixedListener) Close() error {
	var err error
	l.closeOnce.Do(func() { close(l.done); err = l.Listener.Close() })
	return err
}
func (l *mixedListener) run() {
	slots := make(chan struct{}, 128)
	for {
		select {
		case slots <- struct{}{}:
		case <-l.done:
			return
		}
		c, err := l.Listener.Accept()
		if err != nil {
			<-slots
			select {
			case l.ready <- acceptResult{err: err}:
			case <-l.done:
			}
			return
		}
		go func() {
			defer func() { <-slots }()
			c.SetReadDeadline(time.Now().Add(10 * time.Second))
			reader := bufio.NewReader(c)
			first, err := reader.Peek(1)
			if err != nil {
				c.Close()
				return
			}
			c.SetReadDeadline(time.Time{})
			var conn net.Conn = &peekConn{Conn: c, reader: reader}
			if first[0] == 0x16 {
				conn = tls.Server(conn, l.config)
			}
			select {
			case l.ready <- acceptResult{conn: conn}:
			case <-l.done:
				conn.Close()
			}
		}()
	}
}
