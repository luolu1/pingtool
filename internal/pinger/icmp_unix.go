//go:build !windows

package pinger

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

// This backend only exists so the engine can be exercised on non Windows
// systems during development. The shipped tool targets Windows.

type icmpSession struct {
	mu   sync.Mutex
	v4   *icmp.PacketConn
	v6   *icmp.PacketConn
	id   int
	dead bool
}

func newICMPSession() (*icmpSession, error) {
	return &icmpSession{id: os.Getpid() & 0xffff}, nil
}

func (s *icmpSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.v4 != nil {
		s.v4.Close()
		s.v4 = nil
	}
	if s.v6 != nil {
		s.v6.Close()
		s.v6 = nil
	}
	s.dead = true
}

func listen(networks []string, addr string) (*icmp.PacketConn, string, error) {
	var lastErr error
	for _, n := range networks {
		c, err := icmp.ListenPacket(n, addr)
		if err == nil {
			return c, n, nil
		}
		lastErr = err
	}
	return nil, "", lastErr
}

func (s *icmpSession) conn4() (*icmp.PacketConn, error) {
	if s.v4 != nil {
		return s.v4, nil
	}
	c, _, err := listen([]string{"ip4:icmp", "udp4"}, "0.0.0.0")
	if err != nil {
		return nil, fmt.Errorf("无法创建 ICMP 套接字: %v", err)
	}
	s.v4 = c
	return c, nil
}

func (s *icmpSession) conn6() (*icmp.PacketConn, error) {
	if s.v6 != nil {
		return s.v6, nil
	}
	c, _, err := listen([]string{"ip6:ipv6-icmp", "udp6"}, "::")
	if err != nil {
		return nil, fmt.Errorf("无法创建 IPv6 ICMP 套接字: %v", err)
	}
	s.v6 = c
	return c, nil
}

func (s *icmpSession) Ping(ctx context.Context, ip net.IP, timeout time.Duration, payloadSize int) (time.Duration, int, error) {
	if payloadSize < 8 {
		payloadSize = 8
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dead {
		return 0, 0, errors.New("会话已关闭")
	}

	is4 := ip.To4() != nil
	var (
		conn *icmp.PacketConn
		err  error
		typ  icmp.Type
		ptyp icmp.Type
	)
	if is4 {
		conn, err = s.conn4()
		typ, ptyp = ipv4.ICMPTypeEcho, ipv4.ICMPTypeEchoReply
	} else {
		conn, err = s.conn6()
		typ, ptyp = ipv6.ICMPTypeEchoRequest, ipv6.ICMPTypeEchoReply
	}
	if err != nil {
		return 0, 0, err
	}

	payload := make([]byte, payloadSize)
	copy(payload, []byte("pingtool"))
	msg := icmp.Message{
		Type: typ,
		Code: 0,
		Body: &icmp.Echo{ID: s.id, Seq: 1, Data: payload},
	}
	wire, err := msg.Marshal(nil)
	if err != nil {
		return 0, 0, err
	}

	dst := &net.IPAddr{IP: ip}
	deadline := time.Now().Add(timeout)
	if err := conn.SetDeadline(deadline); err != nil {
		return 0, 0, err
	}
	began := time.Now()
	if _, err := conn.WriteTo(wire, dst); err != nil {
		return 0, 0, fmt.Errorf("发送失败: %v", err)
	}

	buf := make([]byte, 1500)
	for {
		if ctx.Err() != nil {
			return 0, 0, ctx.Err()
		}
		if time.Now().After(deadline) {
			return 0, 0, ErrTimeout
		}
		n, peer, err := conn.ReadFrom(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				return 0, 0, ErrTimeout
			}
			return 0, 0, err
		}
		proto := 1
		if !is4 {
			proto = 58
		}
		parsed, err := icmp.ParseMessage(proto, buf[:n])
		if err != nil {
			continue
		}
		if parsed.Type != ptyp {
			continue
		}
		if peer.String() != dst.String() {
			continue
		}
		return time.Since(began), 0, nil
	}
}
