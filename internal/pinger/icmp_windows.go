//go:build windows

package pinger

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	modIphlpapi = syscall.NewLazyDLL("iphlpapi.dll")

	procIcmpCreateFile  = modIphlpapi.NewProc("IcmpCreateFile")
	procIcmpCloseHandle = modIphlpapi.NewProc("IcmpCloseHandle")
	procIcmpSendEcho    = modIphlpapi.NewProc("IcmpSendEcho")
	procIcmp6CreateFile = modIphlpapi.NewProc("Icmp6CreateFile")
	procIcmp6SendEcho2  = modIphlpapi.NewProc("Icmp6SendEcho2")
)

// Values from iphlpapi.h
const (
	ipSuccess             = 0
	ipBuffTooSmall        = 11001
	ipDestNetUnreachable  = 11002
	ipDestHostUnreachable = 11003
	ipDestProtUnreachable = 11004
	ipDestPortUnreachable = 11005
	ipNoResources         = 11006
	ipBadOption           = 11007
	ipHardwareError       = 11008
	ipPacketTooBig        = 11009
	ipReqTimedOut         = 11010
	ipBadRequest          = 11011
	ipBadRoute            = 11012
	ipTtlExpired          = 11013
	ipTtlExpiredTransmit  = 11014
	ipBadDestination      = 11015
	ipGeneralFailure      = 11050
)

// ipOptionInformation mirrors the Win32 IP_OPTION_INFORMATION.
type ipOptionInformation struct {
	TTL         uint8
	TOS         uint8
	Flags       uint8
	OptionsSize uint8
	OptionsData uintptr
}

// icmpEchoReply mirrors the Win32 ICMP_ECHO_REPLY.
type icmpEchoReply struct {
	Address       uint32
	Status        uint32
	RoundTripTime uint32
	DataSize      uint16
	Reserved      uint16
	Data          uintptr
	Options       ipOptionInformation
}

// sockaddrIn6 mirrors the Win32 SOCKADDR_IN6.
type sockaddrIn6 struct {
	Family   uint16
	Port     uint16
	FlowInfo uint32
	Addr     [16]byte
	ScopeID  uint32
}

// icmpv6EchoReply mirrors the Win32 ICMPV6_ECHO_REPLY.
type icmpv6EchoReply struct {
	Address       sockaddrIn6
	Status        uint32
	RoundTripTime uint32
	DataSize      uint16
	Reserved      uint16
	Data          uintptr
	Options       ipOptionInformation
}

const afInet6 = 23

// icmpSession holds the reusable ICMP handles of one probing goroutine.
type icmpSession struct {
	mu sync.Mutex
	h4 uintptr
	h6 uintptr
}

func newICMPSession() (*icmpSession, error) {
	return &icmpSession{}, nil
}

func (s *icmpSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h4 != 0 {
		procIcmpCloseHandle.Call(s.h4)
		s.h4 = 0
	}
	if s.h6 != 0 {
		procIcmpCloseHandle.Call(s.h6)
		s.h6 = 0
	}
}

func (s *icmpSession) handle4() (uintptr, error) {
	if s.h4 != 0 {
		return s.h4, nil
	}
	h, _, errno := procIcmpCreateFile.Call()
	if h == 0 || h == ^uintptr(0) {
		return 0, fmt.Errorf("IcmpCreateFile 失败: %v", errno)
	}
	s.h4 = h
	return h, nil
}

func (s *icmpSession) handle6() (uintptr, error) {
	if s.h6 != 0 {
		return s.h6, nil
	}
	if err := procIcmp6CreateFile.Find(); err != nil {
		return 0, errors.New("当前系统不支持 IPv6 ICMP")
	}
	h, _, errno := procIcmp6CreateFile.Call()
	if h == 0 || h == ^uintptr(0) {
		return 0, fmt.Errorf("Icmp6CreateFile 失败: %v", errno)
	}
	s.h6 = h
	return h, nil
}

// Ping sends one echo request and returns the measured round trip time and,
// when the platform reports it, the reply hop limit (TTL).
func (s *icmpSession) Ping(ctx context.Context, ip net.IP, timeout time.Duration, payloadSize int) (time.Duration, int, error) {
	if dl, ok := ctx.Deadline(); ok {
		if d := time.Until(dl); d < timeout {
			timeout = d
		}
	}
	if timeout <= 0 {
		return 0, 0, ErrTimeout
	}
	if payloadSize < 8 {
		payloadSize = 8
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if ctx.Err() != nil {
		return 0, 0, ctx.Err()
	}

	if ip4 := ip.To4(); ip4 != nil {
		return s.ping4(ip4, timeout, payloadSize)
	}
	return s.ping6(ip, timeout, payloadSize)
}

func (s *icmpSession) ping4(ip4 net.IP, timeout time.Duration, payloadSize int) (time.Duration, int, error) {
	h, err := s.handle4()
	if err != nil {
		return 0, 0, err
	}

	payload := make([]byte, payloadSize)
	binary.BigEndian.PutUint64(payload, uint64(time.Now().UnixNano()))

	replySize := int(unsafe.Sizeof(icmpEchoReply{})) + payloadSize + 8
	reply := make([]byte, replySize)

	// IcmpSendEcho expects DestinationAddress as an IPAddr (ULONG) in network
	// byte order, exactly as inet_addr() returns it. On Windows (little-endian)
	// that means reinterpreting the four network-order bytes as a native
	// uint32, i.e. binary.LittleEndian. Using BigEndian here byte-swaps the
	// address (only palindromic addresses such as 8.8.8.8 would still work).
	ret, _, errno := procIcmpSendEcho.Call(
		h,
		uintptr(binary.LittleEndian.Uint32(ip4)),
		uintptr(unsafe.Pointer(&payload[0])),
		uintptr(uint16(payloadSize)),
		0, // no request options
		uintptr(unsafe.Pointer(&reply[0])),
		uintptr(replySize),
		uintptr(timeout.Milliseconds()),
	)
	runtime.KeepAlive(payload)
	runtime.KeepAlive(reply)

	if ret == 0 {
		return 0, 0, icmpError(errno)
	}
	r := (*icmpEchoReply)(unsafe.Pointer(&reply[0]))
	if r.Status != ipSuccess {
		return 0, 0, fmt.Errorf("%s", statusText(r.Status))
	}
	return time.Duration(r.RoundTripTime) * time.Millisecond, int(r.Options.TTL), nil
}

func (s *icmpSession) ping6(ip net.IP, timeout time.Duration, payloadSize int) (time.Duration, int, error) {
	h, err := s.handle6()
	if err != nil {
		return 0, 0, err
	}

	var dst sockaddrIn6
	dst.Family = afInet6
	copy(dst.Addr[:], ip.To16())

	payload := make([]byte, payloadSize)
	binary.BigEndian.PutUint64(payload, uint64(time.Now().UnixNano()))

	replySize := int(unsafe.Sizeof(icmpv6EchoReply{})) + payloadSize + 8
	reply := make([]byte, replySize)

	ret, _, errno := procIcmp6SendEcho2.Call(
		h,
		0, // Event
		0, // ApcRoutine
		0, // ApcContext
		0, // SourceAddress (let the stack choose)
		uintptr(unsafe.Pointer(&dst)),
		uintptr(unsafe.Pointer(&payload[0])),
		uintptr(uint16(payloadSize)),
		0, // no request options
		uintptr(unsafe.Pointer(&reply[0])),
		uintptr(replySize),
		uintptr(timeout.Milliseconds()),
	)
	runtime.KeepAlive(payload)
	runtime.KeepAlive(reply)

	if ret == 0 {
		return 0, 0, icmpError(errno)
	}
	r := (*icmpv6EchoReply)(unsafe.Pointer(&reply[0]))
	if r.Status != ipSuccess {
		return 0, 0, fmt.Errorf("%s", statusText(r.Status))
	}
	return time.Duration(r.RoundTripTime) * time.Millisecond, int(r.Options.TTL), nil
}

func icmpError(err error) error {
	errno, ok := err.(syscall.Errno)
	if !ok || errno == 0 {
		if err == nil {
			return errors.New("ICMP 请求失败")
		}
		return err
	}
	if errno == syscall.Errno(ipReqTimedOut) {
		return ErrTimeout
	}
	return errors.New(statusText(uint32(errno)))
}

func statusText(status uint32) string {
	switch status {
	case ipSuccess:
		return "成功"
	case ipBuffTooSmall:
		return "接收缓冲区太小"
	case ipDestNetUnreachable:
		return "目标网络不可达"
	case ipDestHostUnreachable:
		return "目标主机不可达"
	case ipDestProtUnreachable:
		return "目标协议不可达"
	case ipDestPortUnreachable:
		return "目标端口不可达"
	case ipNoResources:
		return "资源不足"
	case ipBadOption:
		return "无效的 IP 选项"
	case ipHardwareError:
		return "网卡硬件错误"
	case ipPacketTooBig:
		return "数据包过大"
	case ipReqTimedOut:
		return "请求超时"
	case ipBadRequest:
		return "无效的请求"
	case ipBadRoute:
		return "路由错误"
	case ipTtlExpired, ipTtlExpiredTransmit:
		return "TTL 超时"
	case ipBadDestination:
		return "目标地址无效"
	case ipGeneralFailure:
		return "一般性故障"
	default:
		return fmt.Sprintf("ICMP 错误 (代码 %d)", status)
	}
}
