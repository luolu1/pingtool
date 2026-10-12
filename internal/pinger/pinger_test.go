package pinger

import (
	"net"
	"testing"
	"time"
)

func TestParseTarget(t *testing.T) {
	cases := []struct {
		line string
		opt  ParseOptions
		host string
		port int
		kind Kind
	}{
		{"8.8.8.8", ParseOptions{Protocol: ProtocolAuto}, "8.8.8.8", 0, KindICMP},
		{"example.com # dns", ParseOptions{Protocol: ProtocolAuto}, "example.com", 0, KindICMP},
		{"1.1.1.1:443", ParseOptions{Protocol: ProtocolAuto}, "1.1.1.1", 443, KindTCP},
		{"example.com:80", ParseOptions{Protocol: ProtocolAuto}, "example.com", 80, KindTCP},
		{"[2001:db8::1]:443", ParseOptions{Protocol: ProtocolAuto}, "2001:db8::1", 443, KindTCP},
		{"2001:db8::1", ParseOptions{Protocol: ProtocolAuto}, "2001:db8::1", 0, KindICMP},
		{"tcp://example.com:8443", ParseOptions{Protocol: ProtocolICMP}, "example.com", 8443, KindTCP},
		{"icmp://example.com:443", ParseOptions{Protocol: ProtocolTCP}, "example.com", 0, KindICMP},
		{"example.com", ParseOptions{Protocol: ProtocolTCP, DefaultPort: 22}, "example.com", 22, KindTCP},
		{"1.2.3.4", ParseOptions{Protocol: ProtocolTCP, DefaultPort: 3389}, "1.2.3.4", 3389, KindTCP},
	}
	for _, c := range cases {
		got, err := ParseTarget(c.line, c.opt)
		if err != nil {
			t.Fatalf("ParseTarget(%q) error: %v", c.line, err)
		}
		if got.Host != c.host || got.Port != c.port || got.Kind != c.kind {
			t.Errorf("ParseTarget(%q) = %s/%d/%s, want %s/%d/%s",
				c.line, got.Host, got.Port, got.Kind, c.host, c.port, c.kind)
		}
	}

	for _, bad := range []string{"", "   ", "#comment", "host:99999", "tcp://"} {
		if _, err := ParseTarget(bad, ParseOptions{Protocol: ProtocolAuto}); err == nil {
			t.Errorf("ParseTarget(%q) expected error", bad)
		}
	}
}

func TestExpandTarget(t *testing.T) {
	cases := []struct {
		line  string
		opt   ParseOptions
		hosts []string
		port  int
		kind  Kind
	}{
		// /30 keeps the two usable addresses, drops network + broadcast.
		{"192.168.1.0/30", ParseOptions{Protocol: ProtocolAuto}, []string{"192.168.1.1", "192.168.1.2"}, 0, KindICMP},
		// /31 is point-to-point, both addresses are usable.
		{"192.168.1.0/31", ParseOptions{Protocol: ProtocolAuto}, []string{"192.168.1.0", "192.168.1.1"}, 0, KindICMP},
		// /32 is a single host.
		{"192.168.1.7/32", ParseOptions{Protocol: ProtocolAuto}, []string{"192.168.1.7"}, 0, KindICMP},
		// Range shorthand and full range.
		{"10.0.0.1-4", ParseOptions{Protocol: ProtocolAuto}, []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4"}, 0, KindICMP},
		{"10.0.0.10-10.0.0.12", ParseOptions{Protocol: ProtocolAuto}, []string{"10.0.0.10", "10.0.0.11", "10.0.0.12"}, 0, KindICMP},
		// Reverse order is normalised.
		{"10.0.0.4-2", ParseOptions{Protocol: ProtocolAuto}, []string{"10.0.0.2", "10.0.0.3", "10.0.0.4"}, 0, KindICMP},
		// Subnet + port becomes a TCP sweep.
		{"192.168.1.0/30:22", ParseOptions{Protocol: ProtocolAuto}, []string{"192.168.1.1", "192.168.1.2"}, 22, KindTCP},
		{"tcp://192.168.1.0/30:443", ParseOptions{Protocol: ProtocolAuto}, []string{"192.168.1.1", "192.168.1.2"}, 443, KindTCP},
		// icmp:// forces ICMP even with a port suffix.
		{"icmp://192.168.1.0/30:22", ParseOptions{Protocol: ProtocolTCP, DefaultPort: 22}, []string{"192.168.1.1", "192.168.1.2"}, 0, KindICMP},
		// Range with a forced TCP default port.
		{"10.0.0.1-2", ParseOptions{Protocol: ProtocolTCP, DefaultPort: 3389}, []string{"10.0.0.1", "10.0.0.2"}, 3389, KindTCP},
		// IPv6 subnet (no network/broadcast trimming).
		{"2001:db8::/126", ParseOptions{Protocol: ProtocolAuto}, []string{"2001:db8::", "2001:db8::1", "2001:db8::2", "2001:db8::3"}, 0, KindICMP},
		// Plain hosts still fall through to ParseTarget.
		{"8.8.8.8", ParseOptions{Protocol: ProtocolAuto}, []string{"8.8.8.8"}, 0, KindICMP},
		{"example.com:80", ParseOptions{Protocol: ProtocolAuto}, []string{"example.com"}, 80, KindTCP},
		{"my-host.example.com", ParseOptions{Protocol: ProtocolAuto}, []string{"my-host.example.com"}, 0, KindICMP},
	}
	for _, c := range cases {
		got, err := ExpandTarget(c.line, c.opt)
		if err != nil {
			t.Fatalf("ExpandTarget(%q) error: %v", c.line, err)
		}
		if len(got) != len(c.hosts) {
			t.Fatalf("ExpandTarget(%q) returned %d targets, want %d (%v)", c.line, len(got), len(c.hosts), got)
		}
		for i, want := range c.hosts {
			if got[i].Host != want || got[i].Port != c.port || got[i].Kind != c.kind {
				t.Errorf("ExpandTarget(%q)[%d] = %s/%d/%s, want %s/%d/%s",
					c.line, i, got[i].Host, got[i].Port, got[i].Kind, want, c.port, c.kind)
			}
		}
	}

	// A whole class A is refused instead of allocating millions of targets.
	if _, err := ExpandTarget("10.0.0.0/8", ParseOptions{Protocol: ProtocolAuto}); err == nil {
		t.Errorf("ExpandTarget(10.0.0.0/8) expected error")
	}
	if _, err := ExpandTarget("2001:db8::/64", ParseOptions{Protocol: ProtocolAuto}); err == nil {
		t.Errorf("ExpandTarget(2001:db8::/64) expected error")
	}
}

func TestLocalSubnets(t *testing.T) {
	// The result is environment dependent, just make sure it never panics
	// and only yields parseable CIDRs.
	for _, s := range LocalSubnets() {
		if _, _, err := net.ParseCIDR(s); err != nil {
			t.Errorf("LocalSubnets returned invalid CIDR %q: %v", s, err)
		}
	}
}

func TestEngineTCPSuccess(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port

	// a port that is guaranteed closed on localhost
	closed, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := closed.Addr().(*net.TCPAddr).Port
	closed.Close()

	tg := []Target{
		{Raw: "open", Host: "127.0.0.1", Port: port, Kind: KindTCP},
		{Raw: "closed", Host: "127.0.0.1", Port: closedPort, Kind: KindTCP},
	}
	e := New(Config{Mode: ModeCount, Count: 3, Interval: 20 * time.Millisecond, Timeout: time.Second}, tg)
	e.Start()
	e.Wait()

	snap := e.Snapshot()
	if snap[0].Sent != 3 || snap[0].Recv != 3 {
		t.Errorf("open port: sent=%d recv=%d, want 3/3", snap[0].Sent, snap[0].Recv)
	}
	if snap[1].Recv != 0 || snap[1].Fail != 3 {
		t.Errorf("closed port: recv=%d fail=%d, want 0/3", snap[1].Recv, snap[1].Fail)
	}
	if got := len(e.Probes(0)); got != 3 {
		t.Errorf("history len = %d, want 3", got)
	}
	if snap[0].AvgRTT <= 0 {
		t.Errorf("avg rtt should be > 0, got %v", snap[0].AvgRTT)
	}
}

func TestEngineICMPLoopback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}
	tg := []Target{{Raw: "lo", Host: "127.0.0.1", Kind: KindICMP}}
	e := New(Config{Mode: ModeCount, Count: 2, Interval: 20 * time.Millisecond, Timeout: time.Second}, tg)
	e.Start()
	e.Wait()

	snap := e.Snapshot()
	if snap[0].Sent != 2 {
		t.Fatalf("sent=%d want 2", snap[0].Sent)
	}
	if snap[0].Recv == 0 {
		t.Skipf("ICMP loopback not available here: %s", snap[0].LastErr)
	}
	t.Logf("icmp loopback rtt avg=%v", snap[0].AvgRTT)
}

func TestEngineDurationMode(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port

	tg := []Target{{Host: "127.0.0.1", Port: port, Kind: KindTCP}}
	e := New(Config{Mode: ModeDuration, Duration: 300 * time.Millisecond, Interval: 50 * time.Millisecond, Timeout: time.Second}, tg)
	began := time.Now()
	e.Start()
	e.Wait()
	elapsed := time.Since(began)

	if elapsed < 250*time.Millisecond {
		t.Errorf("duration mode returned too early: %v", elapsed)
	}
	snap := e.Snapshot()
	if snap[0].Sent < 4 {
		t.Errorf("expected at least 4 probes in 300ms/50ms, got %d", snap[0].Sent)
	}
}
