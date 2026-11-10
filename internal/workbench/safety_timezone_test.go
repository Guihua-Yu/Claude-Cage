package workbench

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/sylvinus/agent-vm/internal/cage"
)

func intp(v int) *int { return &v }

// A correct TZ variable must not hide a stale /etc/localtime or missing tzdata:
// the offset programs observe is part of the guest settings evidence.
func TestLinuxGuestSettingsRequireEffectiveTimezone(t *testing.T) {
	cfg := cage.DefaultConfig()
	cfg.Timezone = "Asia/Shanghai"
	good := guestObservation{TZ: cfg.Timezone, SystemTimezone: cfg.Timezone, Lang: cfg.Locale, LCAll: cfg.Locale, LocaleValid: true,
		LocaltimeTarget: "/usr/share/zoneinfo/" + cfg.Timezone, UTCOffsetExpected: intp(28800), UTCOffsetActual: intp(28800)}
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
