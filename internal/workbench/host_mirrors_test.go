package workbench

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sylvinus/agent-vm/internal/cage"
)

// Each candidate is served by a fixture whose throughput differs; the
// recommendation follows the measured rates, not a fixed order.
func TestProbeDebianMirrorsRecommendation(t *testing.T) {
	delay := map[string]time.Duration{}
	served := map[string]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.Split(r.URL.Path, "/")[1]
		served[id] = r.URL.Path
		if id == "ustc" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		chunk := make([]byte, 64<<10)
		for i := 0; i < 8; i++ {
			time.Sleep(delay[id])
			if _, err := w.Write(chunk); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	}))
	defer server.Close()
	original := debianMirrorCandidates
	defer func() { debianMirrorCandidates = original }()
	debianMirrorCandidates = nil
	for _, c := range original {
		debianMirrorCandidates = append(debianMirrorCandidates, struct{ ID, Name, URL string }{c.ID, c.Name, server.URL + "/" + c.ID})
	}
	// Overseas exit: the official CDN is fastest; keep the official-first default.
	delay["nju"], delay["official"] = 40*time.Millisecond, 0
	result := probeDebianMirrors(context.Background(), server.Client(), "trixie", "arm64")
	if len(result.Samples) != 3 || result.Recommended != "auto" {
		t.Fatalf("%+v", result)
	}
	for _, sample := range result.Samples {
		switch sample.ID {
		case "ustc":
			if sample.Error != "HTTP 503" || sample.BytesPerSecond != 0 {
				t.Fatalf("unavailable mirror not reported: %+v", sample)
			}
		default:
			if sample.Error != "" || sample.Bytes != 8*64<<10 || sample.BytesPerSecond <= 0 {
				t.Fatalf("sample not measured: %+v", sample)
			}
		}
	}
	if !strings.HasSuffix(served["official"], "/dists/trixie/main/binary-arm64/Packages.gz") {
		t.Fatalf("wrong index path: %s", served["official"])
	}
	// Domestic connection: a mirror clearly faster than the CDN is recommended.
	delay["nju"], delay["official"] = 0, 60*time.Millisecond
	if result = probeDebianMirrors(context.Background(), server.Client(), "trixie", "arm64"); result.Recommended != "domestic" {
		t.Fatalf("faster mirror not recommended: %+v", result)
	}
	// A marginal difference is noise; the default stays.
	delay["nju"], delay["official"] = 20*time.Millisecond, 22*time.Millisecond
	if result = probeDebianMirrors(context.Background(), server.Client(), "trixie", "arm64"); result.Recommended != "auto" {
		t.Fatalf("marginal difference changed the default: %+v", result)
	}
}

func TestProbeDebianMirrorsWithoutAnyReachableSource(t *testing.T) {
	original := debianMirrorCandidates
	defer func() { debianMirrorCandidates = original }()
	debianMirrorCandidates = []struct{ ID, Name, URL string }{{"official", "x", "http://127.0.0.1:1"}, {"nju", "y", "http://127.0.0.1:1"}}
	client := &http.Client{Timeout: time.Second}
	result := probeDebianMirrors(context.Background(), client, "trixie", "amd64")
	if result.Recommended != "auto" || len(result.Samples) != 2 || result.Samples[0].Error == "" {
		t.Fatalf("%+v", result)
	}
}

func TestDebianMirrorPreferenceValidation(t *testing.T) {
	settings := func(value string) Settings {
		return Settings{Preferences: Preferences{Keyboard: "mac", DebianMirror: value}, DNSURL: "https://1.1.1.1/dns-query", Timezone: "Etc/UTC", Locale: "en_US.UTF-8", BrowserLanguage: "en-US"}
	}
	for _, value := range []string{"", "auto", "official", "domestic"} {
		if _, _, err := proposedSettings(settings(value), cage.DefaultConfig(), t.TempDir()); err != nil {
			t.Fatalf("%q rejected: %v", value, err)
		}
	}
	if _, _, err := proposedSettings(settings("fastest"), cage.DefaultConfig(), t.TempDir()); err == nil {
		t.Fatal("invalid mirror preference accepted")
	}
	if script := cage.DebianPrepareScriptFor("fastest"); strings.Contains(script, "export "+cage.DebianMirrorEnv) || script != cage.DebianPrepareScript {
		t.Fatal("unknown preference exported to the guest")
	}
	if script := cage.DebianPrepareScriptFor("domestic"); !strings.HasPrefix(script, "#!/bin/sh\nexport "+cage.DebianMirrorEnv+"=domestic\n") {
		t.Fatalf("preference not exported: %q", script[:80])
	}
}

// Opt-in: measure the real candidates through the proxy of a Cage profile.
// CAGE_MIRROR_PROBE_CONFIG=~/.claude-cage/config.json go test -run RealProxy -v
func TestProbeDebianMirrorsThroughRealProxy(t *testing.T) {
	path := os.Getenv("CAGE_MIRROR_PROBE_CONFIG")
	if path == "" {
		t.Skip("set CAGE_MIRROR_PROBE_CONFIG to a Cage config.json")
	}
	cfg, err := cage.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	egress, err := cage.NewEgress(cfg)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{Proxy: nil, DialContext: egress.DialContext, DisableKeepAlives: true}}
	result := probeDebianMirrors(context.Background(), client, "trixie", debianGuestArch())
	for _, sample := range result.Samples {
		t.Logf("%-12s %-22s %8.0f KB/s  %7d bytes  %s", sample.ID, sample.Host, sample.BytesPerSecond/1024, sample.Bytes, sample.Error)
	}
	t.Logf("recommended: %s", result.Recommended)
}
