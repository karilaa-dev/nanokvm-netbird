package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type api struct {
	native  *nativeAPI
	manager *manager
}

func (a *api) allowedOrigin(r *http.Request) bool {
	if r.Header.Get("Origin") == "" && r.Method == "GET" {
		return true
	}
	u, err := url.Parse(r.Header.Get("Origin"))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

func respond(w http.ResponseWriter, code int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(data)
}

func fail(w http.ResponseWriter, code int, err error) {
	respond(w, code, map[string]string{"error": err.Error()})
}

func (a *api) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Vary", "Origin")
	if r.URL.Path == "/healthz" && r.Method == "GET" {
		respond(w, 200, map[string]string{"status": "ok"})
		return
	}
	if !a.allowedOrigin(r) {
		fail(w, 403, errors.New("Open NetBird from the NanoKVM Remote Network settings."))
		return
	}
	cookie := r.Header.Get("Cookie")
	if err := a.native.authenticate(r.Context(), cookie); err != nil {
		code := 503
		if errors.Is(err, errUnauthorized) {
			code = 401
		}
		fail(w, code, err)
		return
	}
	if r.URL.Path == "/api/status" && r.Method == "GET" {
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		s, err := a.manager.status(ctx)
		if err != nil {
			fail(w, 503, err)
			return
		}
		s.NeedsOTP, err = a.native.needsOTP(r.Context(), cookie)
		if err != nil {
			fail(w, 503, err)
			return
		}
		respond(w, 200, s)
		return
	}
	if (r.URL.Path != "/api/connect" && r.URL.Path != "/api/disconnect") || r.Method != "POST" {
		fail(w, 404, errors.New("Unknown provider action."))
		return
	}
	media, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if media != "application/json" || r.Header.Get("X-NanoKVM-NetBird") != "1" {
		fail(w, 400, errors.New("Expected a NetBird JSON request."))
		return
	}
	var req connectRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		fail(w, 400, errors.New("Invalid provider settings."))
		return
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		fail(w, 400, errors.New("Expected one settings object."))
		return
	}
	if r.URL.Path == "/api/connect" {
		if err := validateConnect(&req); err != nil {
			fail(w, 400, err)
			return
		}
	}
	if !a.manager.mu.TryLock() {
		fail(w, 409, errors.New("A NetBird operation is already running."))
		return
	}
	defer a.manager.mu.Unlock()
	needed, err := a.native.needsOTP(r.Context(), cookie)
	if err != nil {
		fail(w, 503, err)
		return
	}
	if needed {
		action := "tailscale_up"
		if r.URL.Path == "/api/disconnect" {
			action = "tailscale_down"
		}
		if err := a.native.verifyOTP(r.Context(), cookie, action, req.OTP); err != nil {
			fail(w, 403, err)
			return
		}
	}
	// Complete an accepted operation even if the browser closes or the tunnel drops.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 90*time.Second)
	defer cancel()
	if r.URL.Path == "/api/connect" {
		err = a.manager.connect(ctx, req)
	} else {
		err = a.manager.disconnect(ctx)
	}
	if err != nil {
		fail(w, 502, err)
		return
	}
	respond(w, 200, map[string]bool{"ok": true})
}
