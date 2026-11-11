package cli

import "testing"

func TestParseTarget(t *testing.T) {
	for in, want := range map[string]struct {
		host string
		port int
		ok   bool
	}{
		"5432":           {"", 5432, true},
		"localhost:8080": {"", 8080, true},
		"127.0.0.1:3000": {"127.0.0.1", 3000, true},
		"[::1]:3000":     {"::1", 3000, true},
		"127.0.0.5:1":    {"127.0.0.5", 1, true},
		"10.0.0.5:80":    {"", 0, false},
		"example.com:80": {"", 0, false},
		"0":              {"", 0, false},
		"99999":          {"", 0, false},
		"abc":            {"", 0, false},
	} {
		h, p, err := parseTarget(in)
		if (err == nil) != want.ok || h != want.host || p != want.port {
			t.Errorf("parseTarget(%q) = %q %d %v, want %+v", in, h, p, err, want)
		}
	}
}
