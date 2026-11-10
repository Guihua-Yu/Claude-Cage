package cage

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDebianMirrorFailureFallbackAndPreference(t *testing.T) {
	old := "https://mirrors.nju.edu.cn/debian\tpriority:1\nhttps://mirrors.ustc.edu.cn/debian\tpriority:2\nhttps://deb.debian.org/debian\tpriority:3\n"
	for _, mode := range []string{"reachable", "bookworm", "tls-failure", "official-failure", "nju-failure", "official", "custom", "old", "reboot", "invalid", "domestic", "env-domestic", "env-invalid"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			mirrors := filepath.Join(dir, "mirrors")
			os.MkdirAll(mirrors, 0755)
			list := filepath.Join(mirrors, "debian.list")
			content := "https://deb.debian.org/debian\n"
			if mode == "custom" {
				content = "https://custom.invalid/debian\n"
			}
			if mode == "old" {
				content = old
			}
			if mode == "reboot" {
				content = "# Cage managed Debian mirrors\nhttps://deb.debian.org/debian\tpriority:1\n"
			}
			os.WriteFile(list, []byte(content), 0644)
			os.WriteFile(filepath.Join(mirrors, "debian-security.list"), []byte("https://deb.debian.org/debian-security\n"), 0644)
			preference := filepath.Join(dir, "preference")
			if mode == "official" || mode == "invalid" || mode == "domestic" {
				os.WriteFile(preference, []byte(mode+"\n"), 0644)
			}
			for name, body := range map[string]string{
				"sudo": "#!/bin/sh\nexec \"$@\"\n",
				"apt-helper": `#!/bin/sh
printf '%s\n' "$*" >> "$FIXTURE/probes"
[ "$MODE" != tls-failure ] || exit 1
case "$*" in
 *deb.debian.org*) [ "$MODE" != nju-failure ] && [ "$MODE" != official-failure ] || exit 1;;
 *mirrors.nju*) [ "$MODE" != nju-failure ] || exit 1;;
esac
exit 0
`,
				"timeout": "#!/bin/sh\nshift\nexec \"$@\"\n",
			} {
				os.WriteFile(filepath.Join(dir, name), []byte(body), 0755)
			}
			script := strings.ReplaceAll(DebianMirrorsScript, "/etc/apt/mirrors", mirrors)
			script = strings.ReplaceAll(script, "/etc/claude-cage/debian-mirror", preference)
			script = strings.ReplaceAll(script, "/usr/lib/apt/apt-helper", filepath.Join(dir, "apt-helper"))
			suite := "trixie"
			if mode == "bookworm" {
				suite = "bookworm"
			}
			osRelease := filepath.Join(dir, "os-release")
			if err := os.WriteFile(osRelease, []byte("VERSION_CODENAME="+suite+"\n"), 0644); err != nil {
				t.Fatal(err)
			}
			script = strings.ReplaceAll(script, "/etc/os-release", osRelease)
			run := func() ([]byte, error) {
				cmd := exec.Command("sh", "-c", script)
				cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "FIXTURE="+dir, "MODE="+mode)
				switch mode {
				case "env-domestic":
					cmd.Env = append(cmd.Env, DebianMirrorEnv+"=domestic")
				case "env-invalid":
					cmd.Env = append(cmd.Env, DebianMirrorEnv+"=fastest")
				}
				return cmd.CombinedOutput()
			}
			out, err := run()
			if mode == "invalid" || mode == "env-invalid" {
				if err == nil {
					t.Fatal("invalid preference accepted")
				}
				return
			}
			if err != nil {
				t.Fatalf("%v %s", err, out)
			}
			data, _ := os.ReadFile(list)
			got := string(data)
			switch mode {
			case "custom":
				if got != content {
					t.Fatal("custom source overwritten")
				}
			case "official", "tls-failure", "reachable", "bookworm", "old", "reboot":
				if strings.Contains(got, "mirrors.") || !strings.Contains(got, "deb.debian.org") {
					t.Fatal(got)
				}
			case "nju-failure":
				if strings.Contains(got, "mirrors.nju") || !strings.Contains(got, "mirrors.ustc") {
					t.Fatal(got)
				}
			default:
				// official-failure, domestic and env-domestic: mirrors first, official last.
				if !strings.HasPrefix(got, "# Cage managed Debian mirrors\nhttps://mirrors.nju") || !strings.HasSuffix(strings.TrimSpace(got), "https://deb.debian.org/debian\tpriority:3") {
					t.Fatal(got)
				}
			}
			if mode == "domestic" || mode == "env-domestic" {
				if probes, _ := os.ReadFile(filepath.Join(dir, "probes")); strings.Contains(string(probes), "deb.debian.org") {
					t.Fatal("domestic preference probed the official source first")
				}
			}
			if mode == "env-domestic" {
				// The host's measured choice is kept for later runs without the variable.
				if saved, _ := os.ReadFile(preference); strings.TrimSpace(string(saved)) != "domestic" {
					t.Fatalf("host choice not persisted: %q", saved)
				}
			}
			if mode == "official" {
				if probes, _ := os.ReadFile(filepath.Join(dir, "probes")); len(probes) > 0 {
					t.Fatal("official preference contacted mirrors")
				}
			}
			if mode == "reachable" || mode == "bookworm" {
				probes, err := os.ReadFile(filepath.Join(dir, "probes"))
				if err != nil || !strings.Contains(string(probes), "/dists/"+suite+"/InRelease") || !strings.Contains(string(probes), "/dists/"+suite+"-security/InRelease") {
					t.Fatalf("wrong distribution probes: %s, %v", probes, err)
				}
				if strings.Contains(string(probes), "mirrors.") {
					t.Fatal("reachable official source still contacted domestic mirrors")
				}
			}
			if out, err := run(); err != nil {
				t.Fatalf("repeat: %v %s", err, out)
			}
			after, _ := os.ReadFile(list)
			if string(after) != got {
				t.Fatal("preference changed on repeated setup")
			}
		})
	}
}
