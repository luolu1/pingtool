// Detailed plain text export. These helpers are deliberately free of any
// platform dependency (no walk, no Windows API) so the exported ping style log
// can be unit tested on the host platform.

package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"pingtool/internal/pinger"
)

// writeProbeLog appends one target section with every probe plus a ping style
// statistics footer.
func writeProbeLog(sb *strings.Builder, s pinger.TargetStatus, probes []pinger.Probe, cfg pinger.Config, prompt string) {
	if s.Kind == pinger.KindTCP {
		fmt.Fprintf(sb, "==== %s   [TCP] ====\n", s.Label())
		fmt.Fprintf(sb, "%s tcping %s %d\n", prompt, s.Host, s.Port)
	} else {
		fmt.Fprintf(sb, "==== %s   [ICMP] ====\n", s.Label())
		fmt.Fprintf(sb, "%s ping %s\n", prompt, s.Host)
		host := s.Host
		if s.IP != nil {
			host = fmt.Sprintf("%s (%s)", s.Host, s.IP)
		}
		fmt.Fprintf(sb, "PING %s %d(%d) bytes of data.\n", host, cfg.PayloadSize, cfg.PayloadSize+28)
	}

	for _, p := range probes {
		switch {
		case s.Kind == pinger.KindTCP && p.OK:
			fmt.Fprintf(sb, "port %d open, seq=%d time=%s ms\n", s.Port, p.Seq, msText(p.RTT))
		case s.Kind == pinger.KindTCP:
			fmt.Fprintf(sb, "port %d closed, seq=%d (%s)\n", s.Port, p.Seq, probeErrText(p))
		case p.OK && p.TTL > 0:
			fmt.Fprintf(sb, "%d bytes from %s: icmp_seq=%d ttl=%d time=%s ms\n",
				cfg.PayloadSize, s.IP, p.Seq, p.TTL, msText(p.RTT))
		case p.OK:
			fmt.Fprintf(sb, "%d bytes from %s: icmp_seq=%d time=%s ms\n",
				cfg.PayloadSize, s.IP, p.Seq, msText(p.RTT))
		default:
			fmt.Fprintf(sb, "Request timeout for icmp_seq=%d (%s)\n", p.Seq, probeErrText(p))
		}
	}

	if s.Kind == pinger.KindTCP {
		fmt.Fprintf(sb, "\n--- %s:%d tcping statistics ---\n", s.Host, s.Port)
		fmt.Fprintf(sb, "%d probes transmitted, %d successful, %.2f%% failed\n", s.Sent, s.Recv, s.Loss())
	} else {
		fmt.Fprintf(sb, "\n--- %s ping statistics ---\n", s.Host)
		fmt.Fprintf(sb, "%d packets transmitted, %d received, %.2f%% packet loss\n", s.Sent, s.Recv, s.Loss())
	}
	if s.Recv > 0 {
		fmt.Fprintf(sb, "rtt min/avg/max = %s/%s/%s ms\n", msText(s.MinRTT), msText(s.AvgRTT), msText(s.MaxRTT))
	}
	sb.WriteString("\n")
}

func probeErrText(p pinger.Probe) string {
	if p.Err == "" {
		return "请求超时"
	}
	return p.Err
}

func modeText(cfg pinger.Config) string {
	if cfg.Mode == pinger.ModeDuration {
		return fmt.Sprintf("按时长 %s", cfg.Duration)
	}
	return fmt.Sprintf("按包数 %d", cfg.Count)
}

// localPrompt mimics a shell prompt such as user@host:~# .
func localPrompt() string {
	user := os.Getenv("USERNAME")
	if user == "" {
		user = "root"
	}
	host, _ := os.Hostname()
	if host == "" {
		host = "localhost"
	}
	return fmt.Sprintf("%s@%s:~#", user, host)
}

func msText(d time.Duration) string {
	return fmt.Sprintf("%.2f", float64(d)/float64(time.Millisecond))
}
