package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testAPI(t *testing.T, twoFactor bool, run commandRunner) *api {
	t.Helper()
	native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "nano-kvm-token=valid" {
			w.WriteHeader(401)
			return
		}
		switch r.URL.Path {
		case "/api/auth/account":
			respond(w, 200, map[string]any{"code": 0, "data": map[string]string{"username": "admin"}})
		case "/api/auth/2fa/status":
			respond(w, 200, map[string]any{"code": 0, "data": map[string]bool{"enabled": twoFactor}})
		case "/api/auth/2fa/protection":
			respond(w, 200, map[string]any{"code": 0, "data": map[string]any{"policy": map[string]bool{"tailscale": true}}})
		case "/api/auth/2fa/verify-sensitive-action":
			var input map[string]string
			_ = json.NewDecoder(r.Body).Decode(&input)
			if input["code"] != "123456" || (input["action"] != "tailscale_up" && input["action"] != "tailscale_down") {
				respond(w, 200, map[string]any{"code": -3})
				return
			}
			respond(w, 200, map[string]any{"code": 0, "data": map[string]string{"challenge": "verified-by-native"}})
		default:
			t.Errorf("Unexpected native call: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(native.Close)
	dir := t.TempDir()
	binary := filepath.Join(dir, "netbird")
	if err := os.WriteFile(binary, []byte("test binary"), 0700); err != nil {
		t.Fatal(err)
	}
	return &api{native: &nativeAPI{url: native.URL, client: native.Client()}, manager: &manager{binary: binary, stateDir: dir, runDir: dir, run: run}}
}

func callAPI(a *api, method, path, body, cookie, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://kvm.local"+path, strings.NewReader(body))
	r.Header.Set("Cookie", cookie)
	r.Header.Set("Origin", origin)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-NanoKVM-NetBird", "1")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	return w
}

func TestRejectsUnauthenticatedAndCrossOriginRequests(t *testing.T) {
	a := testAPI(t, false, func(context.Context, string, []string, ...string) ([]byte, error) {
		t.Fatal("unauthorized command executed")
		return nil, nil
	})
	for _, test := range []struct {
		method, path, cookie, origin string
		code                         int
	}{
		{"GET", "/api/status", "", "", 401},
		{"GET", "/api/status", "nano-kvm-token=invalid", "", 401},
		{"POST", "/api/disconnect", "nano-kvm-token=valid", "https://attacker.example", 403},
		{"POST", "/api/disconnect", "nano-kvm-token=valid", "", 403},
		{"POST", "/api/disconnect", "nano-kvm-token=valid", "https://kvm.local:9443", 403},
	} {
		w := callAPI(a, test.method, test.path, "{}", test.cookie, test.origin)
		if w.Code != test.code {
			t.Fatalf("%+v: %d %s", test, w.Code, w.Body.String())
		}
	}
}

func TestTwoFactorGatePrecedesCommands(t *testing.T) {
	var commands int
	a := testAPI(t, true, func(context.Context, string, []string, ...string) ([]byte, error) { commands++; return nil, nil })
	for _, otp := range []string{"", "wrong"} {
		w := callAPI(a, "POST", "/api/disconnect", `{"otp":"`+otp+`"}`, "nano-kvm-token=valid", "https://kvm.local")
		if w.Code != 403 || commands != 0 {
			t.Fatalf("2FA bypass: %d commands %d", w.Code, commands)
		}
	}
	w := callAPI(a, "POST", "/api/disconnect", `{"otp":"123456"}`, "nano-kvm-token=valid", "https://kvm.local")
	if w.Code != 200 || commands != 1 {
		t.Fatalf("valid code failed: %d %s", w.Code, w.Body.String())
	}
}

func TestConnectKeepsKeyOutOfArgsAndPersistentFiles(t *testing.T) {
	for _, succeed := range []bool{true, false} {
		t.Run(boolString(succeed), func(t *testing.T) {
			var keyFile string
			var started, stopped bool
			a := testAPI(t, false, func(_ context.Context, program string, _ []string, args ...string) ([]byte, error) {
				if strings.Contains(strings.Join(args, " "), "test-enrollment-secret") {
					t.Fatal("key leaked into process arguments")
				}
				if program == "systemctl" {
					switch args[0] {
					case "is-active":
						return nil, errors.New("inactive")
					case "start":
						started = true
					case "stop":
						stopped = true
					}
					return nil, nil
				}
				for i, arg := range args {
					if arg == "--setup-key-file" {
						keyFile = args[i+1]
					}
				}
				if keyFile == "" {
					t.Fatal("missing setup-key file")
				}
				info, err := os.Stat(keyFile)
				if err != nil || info.Mode().Perm() != 0600 {
					t.Fatal("key file permissions")
				}
				data, _ := os.ReadFile(keyFile)
				if string(data) != "test-enrollment-secret" {
					t.Fatalf("key input incorrect")
				}
				joined := strings.Join(args, " ")
				for _, flag := range []string{"--disable-dns=true", "--disable-client-routes=true", "--disable-server-routes=true", "--allow-server-ssh=false", "--disable-auto-connect=false"} {
					if !strings.Contains(joined, flag) {
						t.Fatalf("missing default %s", flag)
					}
				}
				if !succeed {
					return nil, errors.New("failed with test-enrollment-secret")
				}
				return nil, nil
			})
			w := callAPI(a, "POST", "/api/connect", `{"managementUrl":"https://netbird.example:443","hostname":"nanokvm","setupKey":"test-enrollment-secret","autoConnect":true}`, "nano-kvm-token=valid", "https://kvm.local")
			want := 200
			if !succeed {
				want = 502
			}
			if w.Code != want || !started {
				t.Fatalf("connect: %d %s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "test-enrollment-secret") {
				t.Fatal("key leaked in response")
			}
			if _, err := os.Stat(keyFile); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("temporary key not removed")
			}
			if !succeed && !stopped {
				t.Fatal("failed enrollment left a new daemon running")
			}
			data, _ := os.ReadFile(filepath.Join(a.manager.stateDir, "preferences.json"))
			if strings.Contains(string(data), "test-enrollment-secret") {
				t.Fatal("key persisted")
			}
			if succeed && !strings.Contains(string(data), "https://netbird.example:443") {
				t.Fatal("preferences not saved")
			}
		})
	}
}

func TestInvalidSettingsCannotRunCommands(t *testing.T) {
	a := testAPI(t, false, func(context.Context, string, []string, ...string) ([]byte, error) {
		t.Fatal("invalid command ran")
		return nil, nil
	})
	for _, body := range []string{
		`{"managementUrl":"http://example.com"}`, `{"managementUrl":"https://name:secret@example.com"}`,
		`{"managementUrl":"https://example.com/path"}`, `{"managementUrl":"https://example.com","hostname":";reboot"}`,
		`{"managementUrl":"https://example.com","command":"reboot"}`, `{} {}`, strings.Repeat("x", 9000),
	} {
		w := callAPI(a, "POST", "/api/connect", body, "nano-kvm-token=valid", "https://kvm.local")
		if w.Code != 400 {
			t.Fatalf("accepted %s: %d", body, w.Code)
		}
	}
}

func TestStatusParsesNetBirdJSON(t *testing.T) {
	a := testAPI(t, false, func(_ context.Context, program string, _ []string, args ...string) ([]byte, error) {
		t.Fatal("status must not spawn a process")
		return nil, nil
	})
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/daemon.DaemonService/Status" {
			t.Errorf("unexpected status RPC: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"status":"Connected","daemonVersion":"0.81.1","fullStatus":{"localPeerState":{"IP":"100.80.0.4/16","fqdn":"kvm.netbird.cloud"},"managementState":{"connected":true},"signalState":{"connected":true},"peers":[{"connStatus":"Connected"},{"connStatus":"Connected"},{"connStatus":"Disconnected"}]}}`))
	}))
	defer daemon.Close()
	a.manager.daemonClient = daemon.Client()
	a.manager.daemonURL = daemon.URL + "/daemon.DaemonService/Status"
	w := callAPI(a, "GET", "/api/status", "", "nano-kvm-token=valid", "")
	var got status
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatal(w.Body.String())
	}
	if got.State != "connected" || got.IP != "100.80.0.4" || got.Peers != 2 || got.TotalPeers != 3 || !got.SignalConnected {
		t.Fatalf("status: %+v", got)
	}
}

func TestDisconnectDisablesBootAndStopsDaemon(t *testing.T) {
	var got []string
	a := testAPI(t, false, func(_ context.Context, program string, _ []string, args ...string) ([]byte, error) {
		got = append([]string{program}, args...)
		return nil, nil
	})
	w := callAPI(a, "POST", "/api/disconnect", "{}", "nano-kvm-token=valid", "https://kvm.local")
	want := []string{"systemctl", "disable", "--now", "nanokvm-netbird-daemon.service"}
	if w.Code != 200 || !reflect.DeepEqual(got, want) {
		t.Fatalf("disconnect: %d %v", w.Code, got)
	}
}

func TestConflictingOperationReturnsConflict(t *testing.T) {
	a := testAPI(t, false, func(context.Context, string, []string, ...string) ([]byte, error) {
		t.Fatal("overlapping command")
		return nil, nil
	})
	a.manager.mu.Lock()
	defer a.manager.mu.Unlock()
	w := callAPI(a, "POST", "/api/disconnect", "{}", "nano-kvm-token=valid", "https://kvm.local")
	if w.Code != 409 {
		t.Fatalf("expected conflict, got %d", w.Code)
	}
}

func TestMalformedNativeAuthenticationFailsClosed(t *testing.T) {
	for _, body := range []string{`{}`, `{"data":{"username":"admin"}}`, `{"code":0,"data":{}}`} {
		n := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
		api := nativeAPI{url: n.URL, client: n.Client()}
		if api.authenticate(context.Background(), "token=x") == nil {
			t.Fatalf("accepted malformed native auth: %s", body)
		}
		n.Close()
	}
}
