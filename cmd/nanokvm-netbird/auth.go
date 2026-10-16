package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

var errUnauthorized = errors.New("Sign in to NanoKVM again.")
var errNative = errors.New("Cannot verify NanoKVM authentication. Try again.")

type nativeAPI struct {
	url    string
	client *http.Client
}
type nativeReply struct {
	Code *int            `json:"code"`
	Data json.RawMessage `json:"data"`
}

func (n *nativeAPI) call(ctx context.Context, cookie, method, path string, body any) (nativeReply, error) {
	var input io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nativeReply{}, errNative
		}
		input = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, n.url+path, input)
	if err != nil {
		return nativeReply{}, errNative
	}
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", n.url)
	}
	resp, err := n.client.Do(req)
	if err != nil {
		return nativeReply{}, errNative
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return nativeReply{}, errUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return nativeReply{}, errNative
	}
	var reply nativeReply
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&reply); err != nil || reply.Code == nil {
		return nativeReply{}, errNative
	}
	return reply, nil
}

func (n *nativeAPI) authenticate(ctx context.Context, cookie string) error {
	if cookie == "" {
		return errUnauthorized
	}
	reply, err := n.call(ctx, cookie, "GET", "/api/auth/account", nil)
	if err != nil {
		return err
	}
	var account struct {
		Username string `json:"username"`
	}
	if *reply.Code != 0 || json.Unmarshal(reply.Data, &account) != nil || account.Username == "" {
		return errUnauthorized
	}
	return nil
}

func (n *nativeAPI) needsOTP(ctx context.Context, cookie string) (bool, error) {
	reply, err := n.call(ctx, cookie, "GET", "/api/auth/2fa/status", nil)
	if err != nil {
		return false, err
	}
	var status struct {
		Enabled *bool `json:"enabled"`
	}
	if *reply.Code != 0 || json.Unmarshal(reply.Data, &status) != nil || status.Enabled == nil {
		return false, errNative
	}
	if !*status.Enabled {
		return false, nil
	}
	reply, err = n.call(ctx, cookie, "GET", "/api/auth/2fa/protection", nil)
	if err != nil {
		return false, err
	}
	var protection struct {
		Policy struct {
			Tailscale *bool `json:"tailscale"`
		} `json:"policy"`
	}
	if *reply.Code != 0 || json.Unmarshal(reply.Data, &protection) != nil || protection.Policy.Tailscale == nil {
		return false, errNative
	}
	return *protection.Policy.Tailscale, nil
}

func (n *nativeAPI) verifyOTP(ctx context.Context, cookie, action, code string) error {
	if code == "" {
		return errors.New("Enter your NanoKVM two-factor code.")
	}
	reply, err := n.call(ctx, cookie, "POST", "/api/auth/2fa/verify-sensitive-action", map[string]string{"action": action, "code": code})
	if err != nil {
		return err
	}
	var data struct {
		Challenge string `json:"challenge"`
	}
	if *reply.Code != 0 || json.Unmarshal(reply.Data, &data) != nil || data.Challenge == "" {
		return errors.New("NanoKVM rejected the two-factor code. Wait before retrying.")
	}
	return nil
}
