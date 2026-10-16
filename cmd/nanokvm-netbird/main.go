package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func pinnedClient(certFile string) (*http.Client, error) {
	data, err := os.ReadFile(certFile)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("invalid NanoKVM certificate")
	}
	pin := sha256.Sum256(block.Bytes)
	return &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			// NanoKVM uses a self-signed certificate without an IP SAN. Trust only
			// the exact local certificate, even when its hostname does not match.
			InsecureSkipVerify: true,
			VerifyConnection: func(cs tls.ConnectionState) error {
				if len(cs.PeerCertificates) == 0 || sha256.Sum256(cs.PeerCertificates[0].Raw) != pin {
					return errors.New("NanoKVM certificate changed")
				}
				return nil
			},
		}},
	}, nil
}

func main() {
	listen := flag.String("listen", ":443", "HTTPS listen address")
	httpListen := flag.String("http-listen", ":80", "HTTP redirect listen address")
	cert := flag.String("cert", "/etc/kvm/server.crt", "NanoKVM TLS certificate")
	key := flag.String("key", "/etc/kvm/server.key", "NanoKVM TLS key")
	native := flag.String("native-url", "https://127.0.0.1:8443", "local NanoKVM API")
	binary := flag.String("netbird", "/opt/nanokvm-netbird/netbird", "pinned NetBird executable")
	state := flag.String("state-dir", "/var/lib/nanokvm-netbird", "plugin preferences directory")
	run := flag.String("run-dir", "/run/nanokvm-netbird", "temporary setup-key directory")
	check := flag.Bool("check", false, "check local prerequisites without starting services")
	flag.Parse()
	client, err := pinnedClient(*cert)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := tls.LoadX509KeyPair(*cert, *key); err != nil {
		log.Fatal(err)
	}
	if *check {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		out, err := runCommand(ctx, *binary, nil, "version")
		if err != nil {
			log.Fatal("NetBird version check failed")
		}
		fmt.Printf("NanoKVM certificate pair valid; NetBird %s", out)
		return
	}
	for _, dir := range []string{*state, *run} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			log.Fatal(err)
		}
	}
	manager := &manager{binary: *binary, stateDir: *state, runDir: *run, run: runCommand, daemonClient: daemonClient(), daemonURL: "http://netbird/daemon.DaemonService/Status"}
	api := &api{native: &nativeAPI{url: *native, client: client}, manager: manager}
	target, err := url.Parse(*native)
	if err != nil {
		log.Fatal(err)
	}
	proxy := &httputil.ReverseProxy{
		Rewrite:       func(p *httputil.ProxyRequest) { p.SetURL(target); p.Out.Host = p.In.Host; p.SetXForwarded() },
		Transport:     client.Transport,
		FlushInterval: -1,
	}
	mux := http.NewServeMux()
	mux.Handle("/api/extensions/netbird/", http.StripPrefix("/api/extensions/netbird", api))
	mux.Handle("/", proxy)
	// Video, terminal WebSockets, and image uploads must not inherit an API timeout.
	server := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10}
	redirect := &http.Server{Addr: *httpListen, ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, e := net.SplitHostPort(host); e == nil {
			host = h
			if strings.Contains(host, ":") {
				host = "[" + host + "]"
			}
		}
		if strings.ContainsAny(host, "/\\\r\n") {
			http.Error(w, "Invalid host", 400)
			return
		}
		http.Redirect(w, r, "https://"+host+r.URL.RequestURI(), http.StatusTemporaryRedirect)
	})}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		deadline, cancel := context.WithTimeout(context.Background(), 100*time.Second)
		defer cancel()
		_ = server.Shutdown(deadline)
		_ = redirect.Shutdown(deadline)
	}()
	go func() {
		if err := redirect.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()
	log.Printf("NanoKVM NetBird provider listening on %s", *listen)
	if err := server.ListenAndServeTLS(*cert, *key); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
