package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

const daemonUnit = "nanokvm-netbird-daemon.service"

type preferences struct {
	ManagementURL string `json:"managementUrl"`
	Hostname      string `json:"hostname"`
	AllowDNS      bool   `json:"allowDNS"`
	AllowRoutes   bool   `json:"allowRoutes"`
	AutoConnect   bool   `json:"autoConnect"`
}

type connectRequest struct {
	preferences
	SetupKey string `json:"setupKey"`
	OTP      string `json:"otp"`
}

type status struct {
	State           string      `json:"state"`
	IP              string      `json:"ip"`
	FQDN            string      `json:"fqdn"`
	Version         string      `json:"version"`
	Peers           int         `json:"peers"`
	TotalPeers      int         `json:"totalPeers"`
	SignalConnected bool        `json:"signalConnected"`
	NeedsOTP        bool        `json:"needsOTP"`
	Preferences     preferences `json:"preferences"`
}

type commandRunner func(context.Context, string, []string, ...string) ([]byte, error)
type manager struct {
	binary, stateDir, runDir string
	run                      commandRunner
	mu                       sync.Mutex
	daemonClient             *http.Client
	daemonURL                string
}

func daemonClient() *http.Client {
	return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", "/run/nanokvm-netbird-daemon/http.sock")
		},
	}}
}

func runCommand(ctx context.Context, program string, env []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.WaitDelay = 2 * time.Second
	// stderr is intentionally discarded: CLI failures can include enrollment data.
	return cmd.Output()
}

func (m *manager) preferences() (preferences, error) {
	p := preferences{ManagementURL: "https://api.netbird.io:443", AutoConnect: true}
	data, err := os.ReadFile(filepath.Join(m.stateDir, "preferences.json"))
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	return p, json.Unmarshal(data, &p)
}

func (m *manager) save(p preferences) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(m.stateDir, ".preferences-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(m.stateDir, "preferences.json"))
}

func (m *manager) command(ctx context.Context, args ...string) ([]byte, error) {
	return m.run(ctx, m.binary, []string{"GOMEMLIMIT=24MiB", "GOGC=50"}, append([]string{"--daemon-addr", "unix:///run/nanokvm-netbird-daemon/netbird.sock"}, args...)...)
}

func (m *manager) status(ctx context.Context) (status, error) {
	p, err := m.preferences()
	if err != nil {
		return status{}, errors.New("Cannot read NetBird preferences.")
	}
	s := status{State: "disconnected", Preferences: p}
	if !m.mu.TryLock() {
		s.State = "busy"
		return s, nil
	}
	defer m.mu.Unlock()
	if _, err := os.Stat(m.binary); err != nil {
		s.State = "notInstalled"
		return s, nil
	}
	req, err := http.NewRequestWithContext(ctx, "POST", m.daemonURL, strings.NewReader(`{"getFullPeerStatus":true}`))
	if err != nil {
		return s, err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := m.daemonClient.Do(req)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ECONNREFUSED) {
			return s, nil
		}
		return s, errors.New("NetBird is starting or unavailable. Refresh in a few seconds.")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return s, errors.New("NetBird is not ready. Refresh in a few seconds.")
	}
	var raw struct {
		State   string `json:"status"`
		Version string `json:"daemonVersion"`
		Full    struct {
			Local struct {
				IP   string `json:"IP"`
				FQDN string `json:"fqdn"`
			} `json:"localPeerState"`
			Management struct {
				Connected bool `json:"connected"`
			} `json:"managementState"`
			Signal struct {
				Connected bool `json:"connected"`
			} `json:"signalState"`
			Peers []struct {
				State string `json:"connStatus"`
			} `json:"peers"`
		} `json:"fullStatus"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&raw) != nil || raw.State == "" {
		return s, errors.New("NetBird returned an unreadable status.")
	}
	s.IP, s.FQDN, s.Version = strings.Split(raw.Full.Local.IP, "/")[0], raw.Full.Local.FQDN, raw.Version
	if net.ParseIP(s.IP) == nil {
		s.IP = ""
	}
	s.TotalPeers, s.SignalConnected = len(raw.Full.Peers), raw.Full.Signal.Connected
	for _, peer := range raw.Full.Peers {
		if peer.State == "Connected" {
			s.Peers++
		}
	}
	if raw.Full.Management.Connected && s.IP != "" {
		s.State = "connected"
	}
	return s, nil
}

var hostnamePattern = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

func validateConnect(r *connectRequest) error {
	r.ManagementURL = strings.TrimSpace(r.ManagementURL)
	r.Hostname = strings.TrimSpace(r.Hostname)
	r.SetupKey = strings.TrimSpace(r.SetupKey)
	u, err := url.Parse(r.ManagementURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("Management URL must be an HTTPS server address without credentials, a query, or a path.")
	}
	if r.Hostname != "" && !hostnamePattern.MatchString(r.Hostname) {
		return errors.New("Device name must be a hostname of 1–63 letters, digits, or hyphens.")
	}
	if len(r.SetupKey) > 512 || strings.ContainsAny(r.SetupKey, "\r\n\x00") {
		return errors.New("Invalid setup key.")
	}
	if len(r.OTP) > 128 {
		return errors.New("Invalid two-factor code.")
	}
	return nil
}

func (m *manager) connect(ctx context.Context, r connectRequest) error {
	args := []string{"up", "--management-url", r.ManagementURL,
		"--disable-dns=" + boolString(!r.AllowDNS), "--disable-client-routes=" + boolString(!r.AllowRoutes),
		"--disable-server-routes=true", "--allow-server-ssh=false", "--disable-auto-connect=" + boolString(!r.AutoConnect)}
	if r.Hostname != "" {
		args = append(args, "--hostname", r.Hostname)
	}
	if r.SetupKey != "" {
		f, err := os.CreateTemp(m.runDir, "setup-key-*")
		if err != nil {
			return errors.New("Cannot create temporary enrollment file.")
		}
		defer os.Remove(f.Name())
		_, writeErr := f.WriteString(r.SetupKey)
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			return errors.New("Cannot write temporary enrollment file.")
		}
		args = append(args, "--setup-key-file", f.Name())
	}
	_, activeErr := m.run(ctx, "systemctl", nil, "is-active", "--quiet", daemonUnit)
	if _, err := m.run(ctx, "systemctl", nil, "start", daemonUnit); err != nil {
		return errors.New("Cannot start the NetBird service.")
	}
	if _, err := m.command(ctx, args...); err != nil {
		if activeErr != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			_, _ = m.run(cleanup, "systemctl", nil, "stop", daemonUnit)
		}
		return errors.New("NetBird could not connect. Check the server URL and setup key. First-time enrollment requires a setup key.")
	}
	verb := "disable"
	if r.AutoConnect {
		verb = "enable"
	}
	if _, err := m.run(ctx, "systemctl", nil, verb, daemonUnit); err != nil {
		return errors.New("Connected, but could not save automatic startup. Check the service and retry.")
	}
	if err := m.save(r.preferences); err != nil {
		return errors.New("Connected, but could not save preferences.")
	}
	return nil
}

func (m *manager) disconnect(ctx context.Context) error {
	if _, err := m.run(ctx, "systemctl", nil, "disable", "--now", daemonUnit); err != nil {
		return errors.New("Could not stop NetBird. The connection may still be active.")
	}
	return nil
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
