package workbench

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/sylvinus/agent-vm/internal/cage"
)

// Debian APT sources the guest may use. The probe downloads a package index
// from each through the user's proxy, because the guest reaches them the same
// way: the guest's own probe only tells whether a source answers, not whether
// a domestic mirror is several times faster than the official CDN (a direct
// domestic connection) or several times slower (an overseas exit).
var debianMirrorCandidates = []struct{ ID, Name, URL string }{
	{"nju", "南京大学镜像", "https://mirrors.nju.edu.cn/debian"},
	{"ustc", "中国科学技术大学镜像", "https://mirrors.ustc.edu.cn/debian"},
	{"official", "Debian 官方源（CDN）", "https://deb.debian.org/debian"},
}

const (
	mirrorProbeBudget = 5 * time.Second
	mirrorProbeBytes  = 4 << 20
)

type mirrorSample struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Host           string  `json:"host"`
	Bytes          int64   `json:"bytes"`
	BytesPerSecond float64 `json:"bytes_per_second"`
	Error          string  `json:"error,omitempty"`
}

type mirrorProbe struct {
	Samples     []mirrorSample `json:"samples"`
	Recommended string         `json:"recommended"`
	Preference  string         `json:"preference"`
	Decided     bool           `json:"decided"`
}

func debianGuestArch() string {
	if runtime.GOARCH == "arm64" {
		return "arm64"
	}
	return "amd64"
}

// domesticAdvantage is how much faster the best domestic mirror must measure
// before the dialog recommends it over the official-first default.
const domesticAdvantage = 1.5

// probeDebianMirrors measures each candidate one after another so the samples
// do not compete for the same proxy connection. A sample ends at the byte or
// time budget; the rate counts only bytes actually received.
func probeDebianMirrors(ctx context.Context, client *http.Client, suite, arch string) mirrorProbe {
	result := mirrorProbe{Recommended: "auto"}
	var official, domestic float64
	for _, candidate := range debianMirrorCandidates {
		sample := mirrorSample{ID: candidate.ID, Name: candidate.Name}
		if u, err := url.Parse(candidate.URL); err == nil {
			sample.Host = u.Hostname()
		}
		sample.Bytes, sample.BytesPerSecond, sample.Error = sampleMirror(ctx, client, candidate.URL+"/dists/"+suite+"/main/binary-"+arch+"/Packages.gz")
		result.Samples = append(result.Samples, sample)
		switch {
		case sample.Error != "":
		case candidate.ID == "official":
			official = sample.BytesPerSecond
		case sample.BytesPerSecond > domestic:
			domestic = sample.BytesPerSecond
		}
		if ctx.Err() != nil {
			break
		}
	}
	if domestic > 0 && domestic >= official*domesticAdvantage {
		result.Recommended = "domestic"
	}
	return result
}

func sampleMirror(ctx context.Context, client *http.Client, target string) (int64, float64, string) {
	ctx, cancel := context.WithTimeout(ctx, mirrorProbeBudget+2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, 0, "无效的镜像地址"
	}
	req.Header.Set("User-Agent", "Cage-mirror-probe/1")
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, "连接失败"
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	var received int64
	buffer := make([]byte, 64<<10)
	deadline := start.Add(mirrorProbeBudget)
	for received < mirrorProbeBytes && time.Now().Before(deadline) {
		n, err := resp.Body.Read(buffer)
		received += int64(n)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			if received == 0 {
				return 0, 0, "读取中断"
			}
			break
		}
	}
	elapsed := time.Since(start).Seconds()
	if elapsed < 0.2 {
		elapsed = 0.2
	}
	if received == 0 {
		return 0, 0, "没有收到数据"
	}
	return received, float64(received) / elapsed, ""
}

func (s *Server) hostDebianMirrors(w http.ResponseWriter, r *http.Request) {
	cfg, err := loadConfig(s.configPath)
	if err != nil {
		requestError(w, err)
		return
	}
	if err = cfg.RequireManagedProxy(s.profile); err != nil {
		http.Error(w, "请先应用代理节点，再测量软件源速度。", http.StatusConflict)
		return
	}
	egress, err := cage.NewEgress(cfg)
	if err != nil {
		requestError(w, err)
		return
	}
	transport := &http.Transport{Proxy: nil, DialContext: egress.DialContext, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("mirror redirect refused") }}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(len(debianMirrorCandidates))*(mirrorProbeBudget+3*time.Second))
	defer cancel()
	result := probeDebianMirrors(ctx, client, "trixie", debianGuestArch())
	s.mu.Lock()
	result.Preference = s.preferences.DebianMirror
	s.mu.Unlock()
	result.Decided = result.Preference != ""
	if result.Preference == "" {
		result.Preference = result.Recommended
	}
	sort.SliceStable(result.Samples, func(i, j int) bool { return result.Samples[i].BytesPerSecond > result.Samples[j].BytesPerSecond })
	jsonReply(w, result)
}

func (s *Server) chooseDebianMirror(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Preference string `json:"preference"`
	}
	if !readJSON(w, r, &input) {
		return
	}
	if !cage.ValidDebianMirror(input.Preference) {
		http.Error(w, "Debian 软件源只能选择官方优先、国内镜像优先或固定官方源。", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	next := s.preferences
	s.mu.Unlock()
	next.DebianMirror = input.Preference
	if err := writePrivate(filepath.Join(s.profile, "workbench.json"), next); err != nil {
		requestError(w, errors.New("保存软件源选择失败，请重试"))
		return
	}
	s.mu.Lock()
	s.preferences = next
	s.mu.Unlock()
	jsonReply(w, map[string]any{"preference": input.Preference, "decided": true})
}
