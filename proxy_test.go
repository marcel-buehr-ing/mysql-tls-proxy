package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"io"
	"log/slog"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"
)

func testIdentity(t *testing.T, name string, expired bool) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(time.Hour)
	if expired {
		until = time.Now().Add(-time.Hour)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name},
		DNSNames: []string{name}, NotBefore: time.Now().Add(-2 * time.Hour), NotAfter: until,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, IsCA: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, roots
}

func packet(payload []byte, sequence byte) []byte {
	n := len(payload)
	return append([]byte{byte(n), byte(n >> 8), byte(n >> 16), sequence}, payload...)
}

func greeting(withTLS bool) []byte {
	p := append([]byte{10}, []byte("11.4.13-test\x00")...)
	p = append(p, 1, 0, 0, 0)
	p = append(p, []byte("12345678\x00")...)
	caps := uint16(clientProtocol41)
	if withTLS {
		caps |= clientSSL
	}
	p = binary.LittleEndian.AppendUint16(p, caps)
	p = append(p, 45, 2, 0, 8, 0, 21)
	p = append(p, make([]byte, 10)...)
	p = append(p, []byte("abcdefghijkl\x00mysql_native_password\x00")...)
	return packet(p, 0)
}

func sslRequest() []byte {
	p := make([]byte, 32)
	binary.LittleEndian.PutUint32(p, clientSSL|clientProtocol41)
	p[8] = 45
	return packet(p, 1)
}

// Each test backend accepts one connection. An accidental reconnect cannot
// silently pass a test by creating another session.
func testBackend(t *testing.T, fn func(net.Conn)) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := l.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
		defer stop()
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		fn(conn)
	}()
	t.Cleanup(func() {
		cancel()
		_ = l.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("backend did not stop")
		}
	})
	return l.Addr().String()
}

func testProxy(t *testing.T, backend string, backendTLS *tls.Config, timeout time.Duration) (string, *tls.Config, context.CancelFunc) {
	t.Helper()
	cert, roots := testIdentity(t, "proxy.test", false)
	p := &proxy{
		backend: backend, backendTLS: backendTLS,
		frontendTLS:      &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}},
		handshakeTimeout: timeout, maxConnections: 2,
		logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.serve(ctx, l) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("proxy did not stop")
		}
	})
	return l.Addr().String(), &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: "proxy.test"}, cancel
}

func connectTLS(t *testing.T, address string, config *tls.Config) *tls.Conn {
	t.Helper()
	c, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	_ = c.SetDeadline(time.Now().Add(10 * time.Second))
	g, err := readPacket(c, maxGreetingSize, 0)
	if err != nil || !bytes.Equal(g, greeting(true)) {
		t.Fatalf("greeting changed: %v", err)
	}
	if err := writeBytes(c, sslRequest()); err != nil {
		t.Fatal(err)
	}
	conn := tls.Client(c, config)
	if err := conn.Handshake(); err != nil {
		t.Fatal(err)
	}
	return conn
}

func TestTLSBridgePreservesStream(t *testing.T) {
	cert, roots := testIdentity(t, "backend.test", false)
	// Larger than a classic protocol packet: the relay must not reinterpret or
	// truncate the authenticated stream at packet boundaries or TLS records.
	want := bytes.Repeat([]byte{0, 255, 1, 10, 13, 32, 127}, 2_500_000)
	backendDone := make(chan error, 1)
	address := testBackend(t, func(c net.Conn) {
		if err := writeBytes(c, greeting(true)); err != nil {
			backendDone <- err
			return
		}
		r, err := readPacket(c, 32, 1)
		if err != nil || !bytes.Equal(r, sslRequest()) {
			t.Error("SSLRequest was not preserved", err)
			backendDone <- io.ErrUnexpectedEOF
			return
		}
		conn := tls.Server(c, &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}})
		got := make([]byte, len(want))
		if _, err := io.ReadFull(conn, got); err != nil {
			backendDone <- err
			return
		}
		if !bytes.Equal(got, want) {
			t.Error("client stream changed")
		}
		backendDone <- writeBytes(conn, got)
	})
	front, clientTLS, _ := testProxy(t, address, &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: "backend.test"}, time.Second)
	client := connectTLS(t, front, clientTLS)
	if err := writeBytes(client, want); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(client, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("backend stream changed")
	}
	if err := <-backendDone; err != nil {
		t.Fatal(err)
	}
}

func TestBackendVerificationFailsClosed(t *testing.T) {
	for _, problem := range []string{"untrusted", "wrong-name", "expired"} {
		t.Run(problem, func(t *testing.T) {
			cert, roots := testIdentity(t, "backend.test", problem == "expired")
			name := "backend.test"
			if problem == "untrusted" {
				_, roots = testIdentity(t, "other.test", false)
			}
			if problem == "wrong-name" {
				name = "wrong.test"
			}
			backendDone := make(chan int, 1)
			address := testBackend(t, func(c net.Conn) {
				_ = writeBytes(c, greeting(true))
				if _, err := readPacket(c, 32, 1); err != nil {
					backendDone <- 0
					return
				}
				conn := tls.Server(c, &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
				buf := make([]byte, 128)
				n, _ := conn.Read(buf)
				backendDone <- n
			})
			front, clientTLS, _ := testProxy(t, address, &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: name}, time.Second)
			client := connectTLS(t, front, clientTLS)
			_, _ = client.Write([]byte("authentication-must-not-reach-unverified-backend"))
			if _, err := client.Read(make([]byte, 1)); err == nil {
				t.Fatal("invalid backend certificate accepted")
			}
			if n := <-backendDone; n != 0 {
				t.Fatal("authentication bytes reached unverified backend")
			}
		})
	}
}

func TestPlaintextClientRejected(t *testing.T) {
	backendDone := make(chan int, 1)
	address := testBackend(t, func(c net.Conn) {
		_ = writeBytes(c, greeting(true))
		n, _ := c.Read(make([]byte, 128))
		backendDone <- n
	})
	front, _, _ := testProxy(t, address, &tls.Config{ServerName: "backend.test"}, time.Second)
	c, err := net.Dial("tcp", front)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(time.Second))
	if _, err := readPacket(c, maxGreetingSize, 0); err != nil {
		t.Fatal(err)
	}
	// A normal, unencrypted HandshakeResponse includes username/auth data.
	p := make([]byte, 48)
	binary.LittleEndian.PutUint32(p, clientProtocol41)
	_ = writeBytes(c, packet(p, 1))
	if _, err := c.Read(make([]byte, 1)); err == nil {
		t.Fatal("plaintext client accepted")
	}
	if n := <-backendDone; n != 0 {
		t.Fatal("plaintext authentication reached backend")
	}
}

func TestHandshakeTimeoutAndShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "timeout", true: "shutdown"}[shutdown], func(t *testing.T) {
			address := testBackend(t, func(c net.Conn) {
				_ = writeBytes(c, greeting(true))
				_, _ = c.Read(make([]byte, 1))
			})
			front, _, cancel := testProxy(t, address, &tls.Config{}, 200*time.Millisecond)
			c, err := net.Dial("tcp", front)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			_ = c.SetDeadline(time.Now().Add(2 * time.Second))
			if _, err := readPacket(c, maxGreetingSize, 0); err != nil {
				t.Fatal(err)
			}
			if shutdown {
				cancel()
			}
			_, err = c.Read(make([]byte, 1))
			if err == nil {
				t.Fatal("idle handshake remained open")
			}
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				t.Fatal("client timeout expired instead of proxy closing")
			}
		})
	}
}

func TestHandshakeValidation(t *testing.T) {
	if err := checkGreeting(greeting(false)[4:]); err == nil {
		t.Fatal("non-TLS backend accepted")
	}
	for _, p := range [][]byte{nil, {10}, {10, 'a'}, {10, 0}, {9, 0}} {
		if err := checkGreeting(p); err == nil {
			t.Fatalf("malformed greeting accepted: %v", p)
		}
	}
	for _, p := range [][]byte{nil, make([]byte, 32), make([]byte, 31), make([]byte, 33)} {
		if err := checkSSLRequest(p); err == nil {
			t.Fatal("invalid SSLRequest accepted")
		}
	}
	for _, p := range [][]byte{{0, 0, 0, 0}, {255, 255, 255, 0}, {1, 0, 0, 4, 10}, {10, 0, 0, 0, 10}} {
		if _, err := readPacket(bytes.NewReader(p), maxGreetingSize, 0); err == nil {
			t.Fatal("malformed packet accepted")
		}
	}
	// A reader returning one byte at a time exercises TCP fragmentation.
	got, err := readPacket(oneByteReader{bytes.NewReader(greeting(true))}, maxGreetingSize, 0)
	if err != nil || !bytes.Equal(got, greeting(true)) {
		t.Fatal("fragmented greeting failed", err)
	}
	input := bytes.NewBuffer(append(sslRequest(), []byte("next TLS bytes")...))
	if _, err := readPacket(input, 32, 1); err != nil || input.String() != "next TLS bytes" {
		t.Fatal("SSLRequest consumed pipelined TLS bytes", err)
	}
}

type oneByteReader struct{ io.Reader }

func (r oneByteReader) Read(p []byte) (int, error) { return r.Reader.Read(p[:1]) }

func TestCLIRejectsIncompleteConfiguration(t *testing.T) {
	for _, args := range [][]string{nil, {"extra"}, {"--backend", "localhost:3306"}} {
		if err := run(context.Background(), args, io.Discard); err == nil {
			t.Fatal("incomplete configuration accepted")
		}
	}
	var help bytes.Buffer
	if err := run(context.Background(), []string{"--help"}, &help); err != nil || !strings.Contains(help.String(), "TLS-only") {
		t.Fatal("help failed", err)
	}
}

func FuzzHandshake(f *testing.F) {
	f.Add(greeting(true))
	f.Add(sslRequest())
	f.Add([]byte{255, 255, 255, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		_ = checkGreeting(data)
		_ = checkSSLRequest(data)
		_, _ = readPacket(bytes.NewReader(data), maxGreetingSize, 0)
	})
}
