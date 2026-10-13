package main

import (
	"net"
	"strings"
	"testing"
	"time"

	"pingtool/internal/pinger"
)

func TestWriteProbeLogICMP(t *testing.T) {
	s := pinger.TargetStatus{
		Target: pinger.Target{Raw: "192.168.95.60", Host: "192.168.95.60", Kind: pinger.KindICMP},
		IP:     net.ParseIP("192.168.95.60"),
		Sent:   3,
		Recv:   2,
		Fail:   1,
		MinRTT: 748 * time.Microsecond,
		AvgRTT: 1739 * time.Microsecond,
		MaxRTT: 2730 * time.Microsecond,
	}
	probes := []pinger.Probe{
		{Seq: 1, OK: true, RTT: 2730 * time.Microsecond, TTL: 64},
		{Seq: 2, OK: true, RTT: 748 * time.Microsecond, TTL: 64},
		{Seq: 3, OK: false, Err: "请求超时"},
	}

	var sb strings.Builder
	writeProbeLog(&sb, s, probes, pinger.Config{PayloadSize: 56}, "root@host:~#")
	out := sb.String()

	for _, want := range []string{
		"==== 192.168.95.60   [ICMP] ====",
		"root@host:~# ping 192.168.95.60",
		"PING 192.168.95.60 (192.168.95.60) 56(84) bytes of data.",
		"56 bytes from 192.168.95.60: icmp_seq=1 ttl=64 time=2.73 ms",
		"56 bytes from 192.168.95.60: icmp_seq=2 ttl=64 time=0.75 ms",
		"Request timeout for icmp_seq=3 (请求超时)",
		"--- 192.168.95.60 ping statistics ---",
		"3 packets transmitted, 2 received, 33.33% packet loss",
		"rtt min/avg/max = 0.75/1.74/2.73 ms",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestWriteProbeLogTCP(t *testing.T) {
	s := pinger.TargetStatus{
		Target: pinger.Target{Raw: "www.baidu.com:443", Host: "www.baidu.com", Port: 443, Kind: pinger.KindTCP},
		Sent:   3,
		Recv:   2,
		Fail:   1,
		MinRTT: 41 * time.Millisecond,
		AvgRTT: 48 * time.Millisecond,
		MaxRTT: 55 * time.Millisecond,
	}
	probes := []pinger.Probe{
		{Seq: 1, OK: true, RTT: 55 * time.Millisecond},
		{Seq: 2, OK: true, RTT: 41 * time.Millisecond},
		{Seq: 3, OK: false, Err: "connection refused"},
	}

	var sb strings.Builder
	writeProbeLog(&sb, s, probes, pinger.Config{}, "root@host:~#")
	out := sb.String()

	for _, want := range []string{
		"==== www.baidu.com:443   [TCP] ====",
		"root@host:~# tcping www.baidu.com 443",
		"port 443 open, seq=1 time=55.00 ms",
		"port 443 open, seq=2 time=41.00 ms",
		"port 443 closed, seq=3 (connection refused)",
		"--- www.baidu.com:443 tcping statistics ---",
		"3 probes transmitted, 2 successful, 33.33% failed",
		"rtt min/avg/max = 41.00/48.00/55.00 ms",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestModeText(t *testing.T) {
	cases := []struct {
		cfg  pinger.Config
		want string
	}{
		{pinger.Config{Mode: pinger.ModeCount, Count: 5}, "按包数 5"},
		{pinger.Config{Mode: pinger.ModeDuration, Duration: 10 * time.Minute}, "10m"},
		{pinger.Config{Mode: pinger.ModeContinuous}, "持续"},
	}
	for _, c := range cases {
		if got := modeText(c.cfg); !strings.Contains(got, c.want) {
			t.Errorf("modeText(mode=%v) = %q, want contains %q", c.cfg.Mode, got, c.want)
		}
	}
}
