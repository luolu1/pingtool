package main

import "testing"

func TestEnsureExt(t *testing.T) {
	cases := []struct{ in, ext, want string }{
		{"报告", ".csv", "报告.csv"},
		{"ping-result", ".csv", "ping-result.csv"},
		{"ping-result.csv", ".csv", "ping-result.csv"},
		{"ping-result.CSV", ".csv", "ping-result.CSV"},
		{"ping.log", ".txt", "ping.log"},
		{"dir.d/report", ".csv", "dir.d/report.csv"},
		{"report.", ".csv", "report.csv"},
		{"archive.tar", ".csv", "archive.tar"},
		{"", ".txt", ""},
	}
	for _, c := range cases {
		if got := ensureExt(c.in, c.ext); got != c.want {
			t.Errorf("ensureExt(%q, %q) = %q, want %q", c.in, c.ext, got, c.want)
		}
	}
}
