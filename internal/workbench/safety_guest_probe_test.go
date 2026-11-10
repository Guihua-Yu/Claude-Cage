package workbench

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sylvinus/agent-vm/internal/cage"
)

func TestGuestProxyEndpointUsesGatewayForHostProxies(t *testing.T) {
	cfg := cage.DefaultConfig()
	for _, tc := range []struct {
		proxy, gateway, host string
		port                 int
	}{
		{"socks5://127.0.0.1:17896", "192.168.5.2", "192.168.5.2", 17896},
		{"http://0.0.0.0:7890", "10.88.0.1", "10.88.0.1", 7890},
		{"http://[::1]:7890", "192.168.5.2", "192.168.5.2", 7890},
		{"http://203.0.113.9:8080", "192.168.5.2", "203.0.113.9", 8080},
		{"http://192.168.1.20:7890", "192.168.5.2", "192.168.1.20", 7890},
		{"http://127.0.0.1", "192.168.5.2", "", 0},
	} {
		cfg.ProxyURL = tc.proxy
		host, port := guestProxyEndpoint(cfg, tc.gateway)
		if host != tc.host || port != tc.port {
			t.Errorf("%s via %s: got %s:%d, want %s:%d", tc.proxy, tc.gateway, host, port, tc.host, tc.port)
		}
	}
}

func TestHostReachAndDNSBypassIssues(t *testing.T) {
	var v guestObservation
	if issue := hostReachIssue(v); issue != "" {
		t.Fatalf("empty observation reported host reach: %s", issue)
	}
	// Windows and macOS probes do not report the Linux-only fields; they add no demands there.
	if issue, _ := dnsBypassIssue(v, false); issue != "" {
		t.Fatalf("non-Linux observation demanded Linux evidence: %s", issue)
	}
	if issue, confirmed := dnsBypassIssue(v, true); issue == "" || confirmed {
		t.Fatalf("missing Linux DNS evidence must be unknown, not passed or failed: %q %t", issue, confirmed)
	}
	v.DNSUDPAttempted = true
	v.Resolvers = resolverObservation{Count: 2, Confined: true}
	if issue, _ := dnsBypassIssue(v, true); issue != "" {
		t.Fatalf("confined resolvers rejected: %s", issue)
	}
	v.Resolvers.Confined = false
	if issue, confirmed := dnsBypassIssue(v, true); !confirmed || !strings.Contains(issue, "解析器") {
		t.Fatalf("resolver outside the environment not confirmed: %q %t", issue, confirmed)
	}
	v.Resolvers = resolverObservation{Count: -1}
	if issue, confirmed := dnsBypassIssue(v, true); issue == "" || confirmed {
		t.Fatalf("unreadable resolver configuration must stay unknown: %q %t", issue, confirmed)
	}
	for name, mutate := range map[string]func(*guestObservation){
		"tcp/53":  func(v *guestObservation) { v.DNSTCPConnected = true },
		"dot/853": func(v *guestObservation) { v.DoTConnected = true },
	} {
		w := guestObservation{DNSUDPAttempted: true, Resolvers: resolverObservation{Count: 1, Confined: true}}
		mutate(&w)
		for _, linux := range []bool{true, false} {
			if issue, confirmed := dnsBypassIssue(w, linux); !confirmed || issue == "" {
				t.Errorf("%s reached (linux=%t) not confirmed: %q", name, linux, issue)
			}
		}
	}
	if issue := hostReachIssue(guestObservation{LinkLocalConnected: true}); !strings.Contains(issue, "169.254.169.254") {
		t.Fatalf("link-local reach not reported: %q", issue)
	}
	if issue := hostReachIssue(guestObservation{ProxyEndpointConnected: true}); !strings.Contains(issue, "代理端口") {
		t.Fatalf("proxy endpoint reach not reported: %q", issue)
	}
}

// The Linux probe's boundary output decodes into the observation the checks read.
func TestLinuxProbeObservationDecodes(t *testing.T) {
	data := `{"tz":"Etc/UTC","lang":"en_US.UTF-8","lc_all":"en_US.UTF-8","system_timezone":"Etc/UTC","locale_valid":true,
"public_connected":false,"management_connected":false,"link_local_connected":false,"proxy_endpoint_connected":false,
"ipv6_connected":false,"ipv6_default_routes":0,"ipv6_global_addresses":0,
"udp_attempted":true,"dns_udp_attempted":true,"dns_tcp_connected":false,"dot_connected":false,
"resolvers":{"count":2,"confined":true},"public_dns_answered":false,"private_file":"unreachable","shared_file":true}`
	var v guestObservation
	if err := json.Unmarshal([]byte(data), &v); err != nil {
		t.Fatal(err)
	}
	if issue, _ := dnsBypassIssue(v, true); issue != "" || hostReachIssue(v) != "" || !v.Resolvers.Confined || v.IPv6GlobalAddresses != 0 {
		t.Fatalf("probe output misread: %+v (%s)", v, issue)
	}
}
