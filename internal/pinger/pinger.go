// Package pinger implements concurrent ICMP ping and TCP-ping (tcping)
// probing with per-target statistics and per-probe history.
//
// The ICMP backend is platform specific: on Windows it uses the Win32
// IcmpSendEcho / Icmp6SendEcho2 API (no administrator rights required),
// on other platforms it falls back to raw/udp ICMP sockets via x/net.
package pinger

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Mode selects how a test run is terminated.
type Mode int

const (
	// ModeCount runs a fixed number of probes per target.
	ModeCount Mode = iota
	// ModeDuration runs probes until the given duration elapsed.
	ModeDuration
	// ModeContinuous keeps probing until Stop is called.
	ModeContinuous
)

// continuousRingSize is the probe history kept per target in ModeContinuous.
// Because the run is unbounded the ring scrolls, retaining only the most
// recent probes for the status grid, the latency chart and the log export.
const continuousRingSize = 2048

// Kind selects the probe protocol.
type Kind string

const (
	KindICMP Kind = "ICMP"
	KindTCP  Kind = "TCP"
)

// ErrTimeout is reported when no reply arrived in time.
var ErrTimeout = errors.New("请求超时")

// Target describes one host/port to probe.
type Target struct {
	Raw  string // original text, keeps a user supplied label
	Host string
	Port int // 0 for ICMP
	Kind Kind
}

// Label returns a human readable name of the target.
func (t Target) Label() string {
	if t.Raw != "" {
		return t.Raw
	}
	return t.String()
}

func (t Target) String() string {
	if t.Kind == KindTCP {
		if ip := net.ParseIP(t.Host); ip != nil && ip.To4() == nil {
			return net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
		}
		return t.Host + ":" + strconv.Itoa(t.Port)
	}
	return t.Host
}

// ProtocolMode controls how the protocol is derived from a target line.
type ProtocolMode int

const (
	// ProtocolAuto uses TCP when a port was given, ICMP otherwise.
	ProtocolAuto ProtocolMode = iota
	// ProtocolICMP forces ICMP for every target.
	ProtocolICMP
	// ProtocolTCP forces TCP for every target.
	ProtocolTCP
)

// ParseOptions configures ParseTarget.
type ParseOptions struct {
	DefaultPort int          // port used for TCP targets without explicit port
	Protocol    ProtocolMode // how to choose ICMP vs TCP
}

// ParseTarget parses a single target line. Supported syntax:
//
//	1.1.1.1
//	example.com
//	1.1.1.1:443
//	example.com:80
//	[2001:db8::1]:443
//	tcp://example.com:443
//	icmp://example.com
//	# comment
func ParseTarget(line string, opt ParseOptions) (Target, error) {
	s := strings.TrimSpace(line)
	if s == "" {
		return Target{}, errors.New("空的目标")
	}
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if s == "" {
		return Target{}, errors.New("空的目标")
	}

	proto := opt.Protocol
	switch {
	case strings.HasPrefix(s, "tcp://"):
		proto, s = ProtocolTCP, s[len("tcp://"):]
	case strings.HasPrefix(s, "icmp://"):
		proto, s = ProtocolICMP, s[len("icmp://"):]
	case strings.HasPrefix(s, "ping://"):
		proto, s = ProtocolICMP, s[len("ping://"):]
	}
	if s == "" {
		return Target{}, errors.New("缺少主机名")
	}

	host, port := s, 0
	if h, p, err := net.SplitHostPort(s); err == nil {
		n, perr := strconv.Atoi(p)
		if perr != nil {
			return Target{}, fmt.Errorf("端口无效: %q", p)
		}
		host, port = h, n
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return Target{}, errors.New("缺少主机名")
	}
	if port < 0 || port > 65535 {
		return Target{}, fmt.Errorf("端口超出范围: %d", port)
	}

	kind := KindICMP
	switch proto {
	case ProtocolICMP:
		kind, port = KindICMP, 0
	case ProtocolTCP:
		kind = KindTCP
	case ProtocolAuto:
		if port > 0 {
			kind = KindTCP
		} else {
			kind = KindICMP
		}
	}
	if kind == KindTCP && port == 0 {
		port = opt.DefaultPort
	}
	if kind == KindTCP && (port <= 0 || port > 65535) {
		return Target{}, fmt.Errorf("%s: TCP 模式需要有效端口 (1-65535)", host)
	}

	return Target{Raw: strings.TrimSpace(line), Host: host, Port: port, Kind: kind}, nil
}

// MaxExpand is the maximum number of hosts a single subnet or range
// expression may expand to.
const MaxExpand = 65536

// ExpandTarget parses one input line into one or more targets. In addition to
// the single-host syntax understood by ParseTarget it understands network
// sweeps:
//
//	192.168.1.0/24                IPv4 网段（自动跳过网络号与广播地址）
//	192.168.1.0/24:22             网段 + TCPing 端口
//	tcp://192.168.1.0/24:22       强制 TCP 的网段扫描
//	192.168.1.1-254              最后一个字节的范围
//	192.168.1.1-192.168.1.254    完整地址范围
//	2001:db8::/120               IPv6 网段
//
// Any other line is delegated to ParseTarget.
func ExpandTarget(line string, opt ParseOptions) ([]Target, error) {
	s := strings.TrimSpace(line)
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if s == "" {
		return nil, errors.New("空的目标")
	}

	proto := opt.Protocol
	switch {
	case strings.HasPrefix(s, "tcp://"):
		proto, s = ProtocolTCP, s[len("tcp://"):]
	case strings.HasPrefix(s, "icmp://"):
		proto, s = ProtocolICMP, s[len("icmp://"):]
	case strings.HasPrefix(s, "ping://"):
		proto, s = ProtocolICMP, s[len("ping://"):]
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("缺少主机名")
	}

	expr, port, hasPort := splitPortSuffix(s)

	if _, ipnet, err := net.ParseCIDR(expr); err == nil {
		return expandCIDR(expr, ipnet, port, hasPort, proto, opt)
	}
	if lo, hi, ok := parseIPv4Range(expr); ok {
		return expandIPv4Range(expr, lo, hi, port, hasPort, proto, opt)
	}

	// Single host: defer to ParseTarget so all of its syntax keeps working.
	t, err := ParseTarget(line, opt)
	if err != nil {
		return nil, err
	}
	return []Target{t}, nil
}

// splitPortSuffix splits a trailing ":port" from an address expression. It is
// only meaningful when the remaining expression parses as a subnet or range;
// otherwise the caller falls back to ParseTarget on the original line.
func splitPortSuffix(s string) (expr string, port int, ok bool) {
	if strings.HasPrefix(s, "[") {
		return s, 0, false // [ipv6]:port is handled by ParseTarget
	}
	i := strings.LastIndexByte(s, ':')
	if i < 0 || i == len(s)-1 {
		return s, 0, false
	}
	n, err := strconv.Atoi(s[i+1:])
	if err != nil {
		return s, 0, false
	}
	return s[:i], n, true
}

// decideKind resolves the protocol mode and requested port into a concrete
// probe kind and the effective port.
func decideKind(proto ProtocolMode, port int, hasPort bool, opt ParseOptions) (Kind, int, error) {
	if !hasPort {
		port = 0
	}
	switch proto {
	case ProtocolICMP:
		return KindICMP, 0, nil
	case ProtocolTCP:
		if port == 0 {
			port = opt.DefaultPort
		}
		if port <= 0 || port > 65535 {
			return "", 0, errors.New("TCP 模式需要有效端口 (1-65535)")
		}
		return KindTCP, port, nil
	default: // ProtocolAuto
		if port > 0 {
			if port > 65535 {
				return "", 0, fmt.Errorf("端口超出范围: %d", port)
			}
			return KindTCP, port, nil
		}
		return KindICMP, 0, nil
	}
}

func ip4ToU32(ip net.IP) uint32 {
	ip = ip.To4()
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

func u32ToIP4(v uint32) net.IP {
	return net.IPv4(byte(v>>24), byte(v>>16), byte(v>>8), byte(v)).To4()
}

// parseIPv4Range recognises "a.b.c.d-a.b.c.e", "a.b.c.d-e" and "a.b.c.d-n".
func parseIPv4Range(s string) (lo, hi uint32, ok bool) {
	i := strings.IndexByte(s, '-')
	if i <= 0 || i == len(s)-1 {
		return 0, 0, false
	}
	left := strings.TrimSpace(s[:i])
	right := strings.TrimSpace(s[i+1:])
	a := net.ParseIP(left)
	if a == nil || a.To4() == nil {
		return 0, 0, false
	}
	lo = ip4ToU32(a)
	if b := net.ParseIP(right); b != nil && b.To4() != nil {
		hi = ip4ToU32(b)
	} else {
		n, err := strconv.Atoi(right)
		if err != nil || n < 0 || n > 255 {
			return 0, 0, false
		}
		hi = lo&0xffffff00 | uint32(n)
	}
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi, true
}

func expandIPv4Range(expr string, lo, hi uint32, port int, hasPort bool, proto ProtocolMode, opt ParseOptions) ([]Target, error) {
	kind, eport, err := decideKind(proto, port, hasPort, opt)
	if err != nil {
		return nil, err
	}
	n := int(hi-lo) + 1
	if n > MaxExpand {
		return nil, fmt.Errorf("范围过大：%s 会展开为 %d 个地址（上限 %d）", expr, n, MaxExpand)
	}
	out := make([]Target, 0, n)
	for v := lo; ; v++ {
		out = append(out, Target{Host: u32ToIP4(v).String(), Port: eport, Kind: kind})
		if v == hi {
			break
		}
	}
	return out, nil
}

func expandCIDR(expr string, ipnet *net.IPNet, port int, hasPort bool, proto ProtocolMode, opt ParseOptions) ([]Target, error) {
	kind, eport, err := decideKind(proto, port, hasPort, opt)
	if err != nil {
		return nil, err
	}
	ones, bits := ipnet.Mask.Size()
	if bits == 0 {
		return nil, fmt.Errorf("无效的网段: %s", expr)
	}
	hostBits := uint(bits - ones)

	var total uint64
	if hostBits >= 64 {
		total = ^uint64(0)
	} else {
		total = uint64(1) << hostBits
	}

	usable := total
	skipEdges := bits == 32 && hostBits >= 2 // IPv4 网络号与广播地址不探测
	if skipEdges {
		usable -= 2
	}
	if usable > MaxExpand {
		return nil, fmt.Errorf("网段过大：%s 会展开为 %d 个地址（上限 %d），请缩小范围", expr, usable, MaxExpand)
	}

	out := make([]Target, 0, usable)
	if bits == 32 {
		v := ip4ToU32(ipnet.IP)
		if skipEdges {
			v++
		}
		for i := uint64(0); i < usable; i++ {
			out = append(out, Target{Host: u32ToIP4(v).String(), Port: eport, Kind: kind})
			v++
		}
		return out, nil
	}

	ip := make(net.IP, net.IPv6len)
	copy(ip, ipnet.IP.To16())
	for i := uint64(0); i < usable; i++ {
		out = append(out, Target{Host: ip.String(), Port: eport, Kind: kind})
		incIP(ip)
	}
	return out, nil
}

// incIP increments a 16 byte IP address in big-endian order.
func incIP(ip net.IP) {
	for i := len(ip) - 1; i >= 0; i-- {
		ip[i]++
		if ip[i] != 0 {
			return
		}
	}
}

// LocalSubnets returns the IPv4 subnets of the up, non-loopback interfaces in
// CIDR notation, for example "192.168.1.0/24".
func LocalSubnets() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := n.IP.To4()
			if ip4 == nil {
				continue
			}
			cidr := (&net.IPNet{IP: ip4.Mask(n.Mask), Mask: n.Mask}).String()
			if seen[cidr] {
				continue
			}
			seen[cidr] = true
			out = append(out, cidr)
		}
	}
	return out
}

// Config holds one test run configuration.
type Config struct {
	Mode        Mode
	Count       int           // used with ModeCount
	Duration    time.Duration // used with ModeDuration
	Interval    time.Duration // delay between probes of the same target
	Timeout     time.Duration // per probe timeout
	Concurrency int           // max simultaneously running targets
	PayloadSize int           // ICMP payload size in bytes
}

// normalize fills in sane defaults.
func (c Config) normalize() Config {
	if c.Interval <= 0 {
		c.Interval = time.Second
	}
	if c.Timeout <= 0 {
		c.Timeout = 2 * time.Second
	}
	if c.Concurrency <= 0 {
		c.Concurrency = 100
	}
	if c.PayloadSize <= 0 {
		c.PayloadSize = 32
	}
	if c.Mode == ModeCount && c.Count <= 0 {
		c.Count = 1
	}
	if c.Mode == ModeDuration && c.Duration <= 0 {
		c.Duration = time.Second
	}
	return c
}

// Probe is the outcome of a single measurement.
type Probe struct {
	Seq int
	At  time.Time
	RTT time.Duration
	TTL int // reply hop limit when known, 0 otherwise
	OK  bool
	Err string
}

// TargetStatus is a point in time view of one target.
type TargetStatus struct {
	Target
	IP      net.IP
	Sent    int
	Recv    int
	Fail    int
	MinRTT  time.Duration
	MaxRTT  time.Duration
	AvgRTT  time.Duration
	LastRTT time.Duration
	LastOK  bool
	LastErr string
	Running bool
	Done    bool
	Elapsed time.Duration
	Started time.Time
}

// Loss returns the packet loss ratio in percent.
func (s TargetStatus) Loss() float64 {
	if s.Sent == 0 {
		return 0
	}
	return float64(s.Sent-s.Recv) / float64(s.Sent) * 100
}

// Engine runs probes for a set of targets.
type Engine struct {
	cfg     Config
	targets []Target

	mu     sync.Mutex
	st     []TargetStatus
	rings  []*ring
	rsum   []time.Duration
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates an engine. Call Start to begin probing.
func New(cfg Config, targets []Target) *Engine {
	cfg = cfg.normalize()
	e := &Engine{
		cfg:     cfg,
		targets: targets,
		st:      make([]TargetStatus, len(targets)),
		rings:   make([]*ring, len(targets)),
		rsum:    make([]time.Duration, len(targets)),
	}
	size := ringSize(cfg)
	for i, t := range targets {
		e.st[i].Target = t
		e.rings[i] = newRing(size)
	}
	return e
}

func ringSize(cfg Config) int {
	n := cfg.Count
	switch cfg.Mode {
	case ModeDuration:
		n = int(cfg.Duration/cfg.Interval) + 2
	case ModeContinuous:
		// Unbounded run: keep a rolling window of recent probes for the grid,
		// the chart and the detailed log export.
		n = continuousRingSize
	}
	if n < 16 {
		n = 16
	}
	if n > 20000 {
		n = 20000
	}
	return n
}

// Start launches the probing goroutines.
func (e *Engine) Start() {
	e.ctx, e.cancel = context.WithCancel(context.Background())

	sem := make(chan struct{}, e.cfg.Concurrency)
	for i := range e.targets {
		e.wg.Add(1)
		go func(idx int) {
			defer e.wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			e.runTarget(idx)
		}(i)
	}
}

// Stop cancels a running test.
func (e *Engine) Stop() {
	if e.cancel != nil {
		e.cancel()
	}
}

// Wait blocks until all target goroutines finished.
func (e *Engine) Wait() { e.wg.Wait() }

// Running reports whether the test is still in progress.
func (e *Engine) Running() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i := range e.st {
		if !e.st[i].Done {
			return true
		}
	}
	return false
}

// Config returns the normalized configuration.
func (e *Engine) Config() Config { return e.cfg }

// Snapshot returns a copy of all target statuses.
func (e *Engine) Snapshot() []TargetStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]TargetStatus, len(e.st))
	copy(out, e.st)
	return out
}

// Probes returns the recorded probe history of one target, oldest first.
func (e *Engine) Probes(i int) []Probe {
	e.mu.Lock()
	defer e.mu.Unlock()
	if i < 0 || i >= len(e.rings) {
		return nil
	}
	return e.rings[i].all()
}

func (e *Engine) runTarget(idx int) {
	ctx := e.ctx
	t := e.targets[idx]

	e.mu.Lock()
	e.st[idx].Started = time.Now()
	e.st[idx].Running = true
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.st[idx].Running = false
		e.st[idx].Done = true
		e.st[idx].Elapsed = time.Since(e.st[idx].Started)
		e.mu.Unlock()
	}()

	var (
		ip     net.IP
		icmpS  *icmpSession
		setErr error
	)
	if t.Kind == KindICMP {
		ip, setErr = resolveHost(ctx, t.Host)
		e.mu.Lock()
		e.st[idx].IP = ip
		e.mu.Unlock()
		if setErr == nil {
			if s, err := newICMPSession(); err == nil {
				icmpS = s
				defer icmpS.Close()
			} else {
				setErr = err
			}
		}
	} else {
		// best effort, only for display
		if v, err := resolveHost(ctx, t.Host); err == nil {
			ip = v
			e.mu.Lock()
			e.st[idx].IP = ip
			e.mu.Unlock()
		}
	}

	deadline := time.Time{}
	if e.cfg.Mode == ModeDuration {
		deadline = time.Now().Add(e.cfg.Duration)
	}

	for seq := 1; ; seq++ {
		if ctx.Err() != nil {
			return
		}
		if e.cfg.Mode == ModeCount && seq > e.cfg.Count {
			return
		}
		if e.cfg.Mode == ModeDuration && time.Now().After(deadline) {
			return
		}

		began := time.Now()
		p := Probe{Seq: seq, At: began}

		switch {
		case setErr != nil:
			p.Err = setErr.Error()
		case t.Kind == KindICMP:
			if ip == nil {
				p.Err = "域名解析失败"
			} else {
				rtt, ttl, err := icmpS.Ping(ctx, ip, e.cfg.Timeout, e.cfg.PayloadSize)
				if err != nil {
					p.Err = err.Error()
				} else {
					p.OK, p.RTT, p.TTL = true, rtt, ttl
				}
			}
		case t.Kind == KindTCP:
			rtt, err := tcpProbe(ctx, t.Host, t.Port, e.cfg.Timeout)
			if err != nil {
				p.Err = err.Error()
			} else {
				p.OK, p.RTT = true, rtt
			}
		}

		e.record(idx, p)

		if wait := e.cfg.Interval - time.Since(began); wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}
}

func (e *Engine) record(idx int, p Probe) {
	e.mu.Lock()
	defer e.mu.Unlock()

	st := &e.st[idx]
	st.Sent++
	if p.OK {
		st.Recv++
		st.LastRTT = p.RTT
		if st.MinRTT == 0 || p.RTT < st.MinRTT {
			st.MinRTT = p.RTT
		}
		if p.RTT > st.MaxRTT {
			st.MaxRTT = p.RTT
		}
		e.rsum[idx] += p.RTT
		st.AvgRTT = e.rsum[idx] / time.Duration(st.Recv)
	} else {
		st.Fail++
	}
	st.LastOK = p.OK
	st.LastErr = p.Err
	if !st.Started.IsZero() {
		st.Elapsed = time.Since(st.Started)
	}
	if len(e.st) > 0 {
		e.rings[idx].add(p)
	}
}

func resolveHost(ctx context.Context, host string) (net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return ip, nil
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %v", host, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("解析 %s 失败: 无记录", host)
	}
	// prefer IPv4 to match the "classic" ping behaviour
	for _, a := range addrs {
		if a.IP.To4() != nil {
			return a.IP, nil
		}
	}
	return addrs[0].IP, nil
}

func tcpProbe(ctx context.Context, host string, port int, timeout time.Duration) (time.Duration, error) {
	d := net.Dialer{Timeout: timeout}
	began := time.Now()
	c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return 0, ErrTimeout
		}
		return 0, err
	}
	rtt := time.Since(began)
	c.Close()
	return rtt, nil
}

// ring is a fixed size circular buffer of probes.
type ring struct {
	buf  []Probe
	next int
	n    int
}

func newRing(size int) *ring {
	if size < 1 {
		size = 1
	}
	return &ring{buf: make([]Probe, size)}
}

func (r *ring) add(p Probe) {
	r.buf[r.next] = p
	r.next = (r.next + 1) % len(r.buf)
	if r.n < len(r.buf) {
		r.n++
	}
}

func (r *ring) all() []Probe {
	if r.n == 0 {
		return nil
	}
	out := make([]Probe, 0, r.n)
	if r.n < len(r.buf) {
		return append(out, r.buf[:r.n]...)
	}
	out = append(out, r.buf[r.next:]...)
	out = append(out, r.buf[:r.next]...)
	return out
}
