package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func discardLogger() *slog.Logger { return slog.New(slog.NewJSONHandler(io.Discard, nil)) }

// startServe runs serve in the background and returns the base URL, a cancel func that
// simulates SIGTERM, and a channel with serve's result.
func startServe(t *testing.T, h http.Handler, timeout time.Duration) (string, context.CancelFunc, <-chan error) {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	done := make(chan error, 1)
	go func() { done <- serve(ctx, &http.Server{Handler: h}, ln, timeout, discardLogger()) }()
	return "http://" + ln.Addr().String(), cancel, done
}

// get sends a GET with a context, as production code must (noctx).
func get(t *testing.T, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}

func waitResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not return within 5s")
		return nil
	}
}

func TestServe(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	t.Run("clean shutdown on cancel", func(t *testing.T) {
		url, cancel, done := startServe(t, ok, time.Second)

		resp, err := get(t, url)
		if err != nil {
			t.Fatalf("request before shutdown: %v", err)
		}
		_ = resp.Body.Close()

		cancel()
		if err := waitResult(t, done); err != nil {
			t.Fatalf("serve() = %v, want nil", err)
		}
	})

	t.Run("in-flight request finishes during shutdown", func(t *testing.T) {
		started := make(chan struct{})
		slow := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			close(started)
			time.Sleep(300 * time.Millisecond)
			w.WriteHeader(http.StatusOK)
		})
		url, cancel, done := startServe(t, slow, 2*time.Second)

		respCh := make(chan int, 1)
		go func() {
			resp, err := get(t, url)
			if err != nil {
				respCh <- -1
				return
			}
			_ = resp.Body.Close()
			respCh <- resp.StatusCode
		}()

		<-started
		cancel() // SIGTERM while the request is being handled

		if code := <-respCh; code != http.StatusOK {
			t.Errorf("in-flight request got %d, want 200", code)
		}
		if err := waitResult(t, done); err != nil {
			t.Fatalf("serve() = %v, want nil", err)
		}
	})

	t.Run("shutdown timeout exceeded", func(t *testing.T) {
		started := make(chan struct{})
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		stuck := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			close(started)
			<-release
			w.WriteHeader(http.StatusOK)
		})
		url, cancel, done := startServe(t, stuck, 100*time.Millisecond)

		go func() {
			if resp, err := get(t, url); err == nil {
				_ = resp.Body.Close()
			}
		}()
		<-started
		cancel()

		err := waitResult(t, done)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("serve() = %v, want an error wrapping context.DeadlineExceeded", err)
		}
	})

	t.Run("listener failure is returned", func(t *testing.T) {
		var lc net.ListenConfig
		ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		_ = ln.Close() // Serve on a closed listener fails at once

		err = serve(context.Background(), &http.Server{Handler: ok}, ln, time.Second, discardLogger())
		if err == nil {
			t.Fatal("serve() = nil, want an error")
		}
	})
}

func TestHealthURL(t *testing.T) {
	tests := []struct {
		addr string
		want string
	}{
		{"", "http://127.0.0.1:8080/healthz"},
		{":8080", "http://127.0.0.1:8080/healthz"},
		{"0.0.0.0:9000", "http://127.0.0.1:9000/healthz"},
		{"[::]:9000", "http://127.0.0.1:9000/healthz"},
		{"127.0.0.1:9000", "http://127.0.0.1:9000/healthz"},
		{"[::1]:9000", "http://[::1]:9000/healthz"},
	}
	for _, tc := range tests {
		t.Run(tc.addr, func(t *testing.T) {
			if got := healthURL(tc.addr); got != tc.want {
				t.Errorf("healthURL(%q) = %q, want %q", tc.addr, got, tc.want)
			}
		})
	}
}

func TestProbe(t *testing.T) {
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer healthy.Close()

	unhealthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer unhealthy.Close()

	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()

	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"200 is healthy", healthy.URL, false},
		{"503 is unhealthy", unhealthy.URL, true},
		{"connection refused is unhealthy", closedURL, true},
		{"malformed URL", "http://[bad", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			err := probe(ctx, http.DefaultClient, tc.url)
			if (err != nil) != tc.wantErr {
				t.Errorf("probe() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
