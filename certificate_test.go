package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeIdentity(t *testing.T, directory string, cert tls.Certificate) {
	t.Helper()
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	key, err := x509.MarshalPKCS8PrivateKey(cert.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"tls.crt": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Certificate[0]}),
		"tls.key": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}),
	} {
		if err := os.WriteFile(filepath.Join(directory, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCertificateReloadRejectsInvalidUpdate(t *testing.T) {
	valid, _ := testIdentity(t, "proxy.test", false)
	expired, _ := testIdentity(t, "proxy.test", true)
	future, _ := testIdentityValidity(t, "proxy.test", time.Now().Add(time.Hour), time.Now().Add(2*time.Hour))
	other, _ := testIdentity(t, "proxy.test", false)
	for _, problem := range []string{"malformed", "missing-key", "mismatched-key", "expired", "future"} {
		t.Run(problem, func(t *testing.T) {
			dir := t.TempDir()
			writeIdentity(t, dir, valid)
			r, err := newCertificateReloader(filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key"))
			if err != nil {
				t.Fatal(err)
			}
			snapshot, _ := r.getCertificate(nil)
			switch problem {
			case "malformed":
				err = os.WriteFile(r.certPath, []byte("not a certificate"), 0600)
			case "missing-key":
				err = os.Remove(r.keyPath)
			case "mismatched-key":
				mismatched := valid
				mismatched.PrivateKey = other.PrivateKey
				writeIdentity(t, dir, mismatched)
			case "expired":
				writeIdentity(t, dir, expired)
			case "future":
				writeIdentity(t, dir, future)
			}
			if err != nil {
				t.Fatal(err)
			}
			if changed, err := r.reload(); err == nil || changed {
				t.Fatal("invalid update accepted")
			}
			if current, err := r.getCertificate(nil); err != nil || current != snapshot {
				t.Fatal("last valid certificate was lost", err)
			}
			if _, err := newCertificateReloader(r.certPath, r.keyPath); err == nil {
				t.Fatal("invalid initial certificate accepted")
			}
			writeIdentity(t, dir, other)
			if changed, err := r.reload(); err != nil || !changed {
				t.Fatal("recovery after invalid update failed", err)
			}
		})
	}
	// Even clients that skip verification must not get a retained expired leaf.
	r := &certificateReloader{}
	r.current.Store(&expired)
	if _, err := r.getCertificate(nil); err == nil {
		t.Fatal("retained expired certificate was served")
	}
}

func TestCertificateRotationWithLiveSession(t *testing.T) {
	dir := t.TempDir()
	first, roots := testIdentity(t, "proxy.test", false)
	second, _ := testIdentity(t, "proxy.test", false)
	roots.AddCert(second.Leaf)
	writeIdentity(t, filepath.Join(dir, "v1"), first)
	writeIdentity(t, filepath.Join(dir, "v2"), second)
	for name, target := range map[string]string{"..data": "v1", "tls.crt": "..data/tls.crt", "tls.key": "..data/tls.key"} {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	r, err := newCertificateReloader(filepath.Join(dir, "tls.crt"), filepath.Join(dir, "tls.key"))
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); r.watch(ctx, 5*time.Millisecond, logger) }()
	t.Cleanup(func() { cancel(); waitStopped(t, done) })
	backend, backendTLS := echoBackend(t)
	p := &proxy{
		backend: backend, backendTLS: backendTLS,
		frontendTLS:      &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: r.getCertificate, SessionTicketsDisabled: true},
		handshakeTimeout: time.Second, maxConnections: 8, logger: logger,
	}
	address, _, _ := startProxy(t, p)
	clientTLS := &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: "proxy.test", ClientSessionCache: tls.NewLRUClientSessionCache(4)}
	existing := connectTLS(t, address, clientTLS)
	echo(t, existing)
	if !bytes.Equal(existing.ConnectionState().PeerCertificates[0].Raw, first.Certificate[0]) {
		t.Fatal("initial certificate mismatch")
	}
	if err := os.Symlink("v2", filepath.Join(dir, "..next")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "..next"), filepath.Join(dir, "..data")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !bytes.Equal(r.current.Load().Certificate[0], second.Certificate[0]) {
		if time.Now().After(deadline) {
			t.Fatal("atomic Secret update was not detected")
		}
		time.Sleep(time.Millisecond)
	}
	fresh := connectTLS(t, address, clientTLS)
	echo(t, fresh)
	if fresh.ConnectionState().DidResume || !bytes.Equal(fresh.ConnectionState().PeerCertificates[0].Raw, second.Certificate[0]) {
		t.Fatal("new connection did not verify the rotated certificate")
	}
	echo(t, existing)
	if changed, err := r.reload(); err != nil || changed {
		t.Fatal("unchanged certificate reported as a rotation", err)
	}
}
