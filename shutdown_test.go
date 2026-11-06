package main

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"
)

func echoBackend(t *testing.T) (string, *tls.Config) {
	t.Helper()
	cert, roots := testIdentity(t, "backend.test", false)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		var workers sync.WaitGroup
		defer workers.Wait()
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer conn.Close()
				stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
				defer stop()
				_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
				if err := writeBytes(conn, greeting(true)); err != nil {
					return
				}
				if _, err := readPacket(conn, 32, 1); err != nil {
					return
				}
				secured := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
				_, _ = io.Copy(secured, secured)
			}()
		}
	}()
	t.Cleanup(func() { cancel(); _ = l.Close(); waitStopped(t, done) })
	return l.Addr().String(), &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: "backend.test"}
}

func startProxy(t *testing.T, p *proxy) (string, context.CancelFunc, <-chan struct{}) {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := p.serve(ctx, l); err != nil {
			t.Error(err)
		}
	}()
	t.Cleanup(func() { cancel(); waitStopped(t, done) })
	return l.Addr().String(), cancel, done
}

func waitStopped(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop")
	}
}

func echo(t *testing.T, c net.Conn) {
	t.Helper()
	want := "opaque application bytes"
	if _, err := io.WriteString(c, want); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(c, got); err != nil || string(got) != want {
		t.Fatalf("stream interrupted: %q, %v", got, err)
	}
}

func TestGracefulShutdown(t *testing.T) {
	for _, expire := range []bool{false, true} {
		t.Run(map[bool]string{false: "finish-before-deadline", true: "force-at-deadline"}[expire], func(t *testing.T) {
			backend, backendTLS := echoBackend(t)
			cert, roots := testIdentity(t, "proxy.test", false)
			p := &proxy{
				backend: backend, backendTLS: backendTLS,
				frontendTLS:      &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
				handshakeTimeout: time.Second, shutdownTimeout: 500 * time.Millisecond,
				maxConnections: 2, logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			}
			address, cancel, done := startProxy(t, p)
			client := connectTLS(t, address, &tls.Config{RootCAs: roots, ServerName: "proxy.test", MinVersion: tls.VersionTLS12})
			echo(t, client)
			started := time.Now()
			cancel()
			// Wait for refusal by the listener, rather than relying on a sleep.
			deadline := time.Now().Add(250 * time.Millisecond)
			for {
				c, err := net.DialTimeout("tcp", address, 20*time.Millisecond)
				if err != nil {
					break
				}
				_ = c.Close()
				if time.Now().After(deadline) {
					t.Fatal("listener still accepts connections while draining")
				}
				time.Sleep(time.Millisecond)
			}
			echo(t, client)
			select {
			case <-done:
				t.Fatal("active session was closed before drain deadline")
			default:
			}
			if !expire {
				_ = client.Close()
				select {
				case <-done:
				case <-time.After(250 * time.Millisecond):
					t.Fatal("shutdown did not finish when the last session closed")
				}
			} else {
				_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
				_, err := client.Read(make([]byte, 1))
				if err == nil {
					t.Fatal("session remained open after deadline")
				}
				if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
					t.Fatal("client timed out instead of being closed by proxy")
				}
				if time.Since(started) < 450*time.Millisecond {
					t.Fatal("session closed before the configured deadline")
				}
				waitStopped(t, done)
			}
		})
	}
}
