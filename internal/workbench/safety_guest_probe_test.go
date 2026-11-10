package workbench

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/sylvinus/agent-vm/internal/cage"
)

func intp(v int) *int { return &v }

func linuxObservation(cfg cage.Config) guestObservation {
	return guestObservation{TZ: cfg.Timezone, SystemTimezone: cfg.Timezone, Lang: cfg.Locale, LCAll: cfg.Locale, LocaleValid: true,
		LocaltimeTarget: "/usr/share/zoneinfo/" + cfg.Timezone, UTCOffsetExpected: intp(28800), UTCOffsetActual: intp(28800)}
}

// A correct TZ variable must not hide a stale /etc/localtime or missing tzdata:
// the offset programs observe is part of the guest settings evidence.
func TestLinuxGuestSettingsRequireEffectiveTimezone(t *testing.T) {
	cfg := cage.DefaultConfig()
	cfg.Timezone = "Asia/Shanghai"
	good := linuxObservation(cfg)
	if !guestSettingsMatch(good, cfg) {
		t.Fatalf("matching observation rejected: %s", linuxGuestSettingsMismatch(good, cfg))
	}
	cases := map[string]func(*guestObservation){
		"offset mismatch":      func(v *guestObservation) { v.UTCOffsetActual = intp(0) },
		"offset unread":        func(v *guestObservation) { v.UTCOffsetExpected = nil },
		"stale localtime link": func(v *guestObservation) { v.LocaltimeTarget = "/usr/share/zoneinfo/Etc/UTC" },
		"tz variable":          func(v *guestObservation) { v.TZ = "Etc/UTC" },
		"system timezone":      func(v *guestObservation) { v.SystemTimezone = "" },
		"locale invalid":       func(v *guestObservation) { v.LocaleValid = false },
		"lang":                 func(v *guestObservation) { v.Lang = "C.UTF-8" },
	}
	for name, mutate := range cases {
		v := good
		mutate(&v)
		if guestSettingsMatch(v, cfg) {
			t.Errorf("%s accepted", name)
		}
		if linuxGuestSettingsMismatch(v, cfg) == "" {
			t.Errorf("%s has no explanation", name)
		}
	}
	// A regular /etc/localtime file leaves the decision to the offset comparison.
	v := good
	v.LocaltimeTarget = ""
	if !guestSettingsMatch(v, cfg) {
		t.Fatalf("regular localtime file rejected: %s", linuxGuestSettingsMismatch(v, cfg))
	}
}

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

func TestInterfaceInventoryIsBounded(t *testing.T) {
	if interfaceInventory(nil) != "" {
		t.Fatal("empty inventory produced text")
	}
	text := interfaceInventory([]string{"a", "b", "c", "d", "e", "f", "g", "h"})
	if !strings.Contains(text, "f") || strings.Contains(text, "g") || !strings.HasSuffix(text, "…") {
		t.Fatalf("inventory not truncated to six names: %q", text)
	}
}

// The VSOCK observation is reference evidence: it never decides the grant.
func TestHostChannelCheckIsReferenceOnly(t *testing.T) {
	for vsock, state := range map[string]cage.CheckState{"unsupported": cage.CheckPassed, "refused": cage.CheckPassed, "connected": cage.CheckFailed, "": cage.CheckUnknown, "other": cage.CheckUnknown} {
		check := hostChannelCheck(vsock)
		if check.Critical || check.ID != "host_channel" || check.State != state || check.Evidence == "" {
			t.Errorf("%q: %+v", vsock, check)
		}
	}
	report := cage.SafetyReport{Policy: cage.SafetyPolicyVersion, ID: "test", RunID: "run", CheckID: "check", Configuration: "config", CreatedAt: time.Now()}
	for _, id := range []string{"egress_policy", "proxy_transport", "dns_transport", "guest_settings", "browser_settings", "filesystem_boundary", "management_boundary", "fail_closed", "udp_policy", "ipv6_policy", "exit_consistency"} {
		report.Checks = append(report.Checks, cage.SafetyCheck{ID: id, Critical: true, State: cage.CheckPassed, Evidence: "verified", CheckedAt: report.CreatedAt})
	}
	report.Checks = append(report.Checks, hostChannelCheck("connected"))
	if err := report.Authorizes("run", "config", time.Now()); err != nil {
		t.Fatalf("reference check blocked the grant: %v", err)
	}
}

func TestLinuxProbeObservationDecodes(t *testing.T) {
	data := `{"tz":"Asia/Shanghai","lang":"en_US.UTF-8","lc_all":"en_US.UTF-8","system_timezone":"Asia/Shanghai","locale_valid":true,
"localtime_target":"/usr/share/zoneinfo/Asia/Shanghai","utc_offset_actual":28800,"utc_offset_expected":28800,
"public_connected":false,"management_connected":false,"link_local_connected":false,"proxy_endpoint_connected":false,
"ipv6_connected":false,"ipv6_default_routes":0,"ipv6_global_addresses":0,"interfaces":["eth0"],
"udp_attempted":true,"dns_udp_attempted":true,"dns_tcp_connected":false,"dot_connected":false,
"resolvers":{"count":2,"confined":true},"vsock":"refused","public_dns_answered":false,"private_file":"unreachable","shared_file":true}`
	var v guestObservation
	if err := json.Unmarshal([]byte(data), &v); err != nil {
		t.Fatal(err)
	}
	cfg := cage.DefaultConfig()
	cfg.Timezone, cfg.Locale = "Asia/Shanghai", "en_US.UTF-8"
	if !guestSettingsMatch(v, cfg) {
		t.Fatalf("probe output rejected: %s", linuxGuestSettingsMismatch(v, cfg))
	}
	if issue, _ := dnsBypassIssue(v, true); issue != "" || hostReachIssue(v) != "" || v.VSock != "refused" || len(v.Interfaces) != 1 || !v.Resolvers.Confined {
		t.Fatalf("probe output misread: %+v (%s)", v, issue)
	}
}

// The embedded probe and the Go decoder must agree on the offline stage; the
// environment stage makes no network connections.
func TestSafetyProbeEnvironmentStageReportsEffectiveTimezone(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not installed")
	}
	input, _ := json.Marshal(map[string]any{"stage": "environment", "timezone": "Asia/Shanghai"})
	cmd := exec.Command(python, "-I", "-c", guestSafetyProbe, base64.StdEncoding.EncodeToString(input))
	cmd.Env = append(os.Environ(), "TZ=Asia/Shanghai", "LANG=en_US.UTF-8", "LC_ALL=en_US.UTF-8")
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	var v guestObservation
	if err = json.Unmarshal(output, &v); err != nil {
		t.Fatalf("probe output %q: %v", output, err)
	}
	if v.TZ != "Asia/Shanghai" || v.UTCOffsetExpected == nil || *v.UTCOffsetExpected != 28800 || v.UTCOffsetActual == nil || *v.UTCOffsetActual != 28800 {
		t.Fatalf("effective timezone not reported: %s", output)
	}
	if strings.Contains(string(output), "public_connected") {
		t.Fatalf("environment stage ran network probes: %s", output)
	}
	cmd = exec.Command(python, "-I", "-c", guestSafetyProbe, base64.StdEncoding.EncodeToString(input))
	cmd.Env = append(os.Environ(), "TZ=Etc/UTC")
	if output, err = cmd.Output(); err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	if err = json.Unmarshal(output, &v); err != nil || v.UTCOffsetActual == nil || *v.UTCOffsetActual != 0 || v.UTCOffsetExpected == nil || *v.UTCOffsetExpected != 28800 {
		t.Fatalf("offset mismatch not observable: %s %v", output, err)
	}
}
