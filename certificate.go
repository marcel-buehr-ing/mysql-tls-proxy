package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"
)

// Published certificates are immutable: existing handshakes keep their snapshot.
type certificateReloader struct {
	certPath string
	keyPath  string
	current  atomic.Pointer[tls.Certificate]
}

func newCertificateReloader(certPath, keyPath string) (*certificateReloader, error) {
	r := &certificateReloader{certPath: certPath, keyPath: keyPath}
	if _, err := r.reload(); err != nil {
		return nil, err
	}
	return r, nil
}

func validCertificate(cert *tls.Certificate, now time.Time) error {
	if now.Before(cert.Leaf.NotBefore) || !now.Before(cert.Leaf.NotAfter) {
		return errors.New("frontend certificate is outside its validity period")
	}
	return nil
}

func (r *certificateReloader) reload() (bool, error) {
	// Reopen paths on every poll to follow atomic directory/symlink replacements
	// such as Kubernetes Secret updates. A partially updated pair is rejected.
	cert, err := tls.LoadX509KeyPair(r.certPath, r.keyPath)
	if err != nil {
		return false, fmt.Errorf("load frontend certificate: %w", err)
	}
	cert.Leaf, err = x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		return false, fmt.Errorf("parse frontend certificate: %w", err)
	}
	if err := validCertificate(&cert, time.Now()); err != nil {
		return false, err
	}
	previous := r.current.Load()
	if previous != nil && len(previous.Certificate) == len(cert.Certificate) {
		same := true
		for i := range cert.Certificate {
			same = same && bytes.Equal(cert.Certificate[i], previous.Certificate[i])
		}
		if same {
			return false, nil
		}
	}
	r.current.Store(&cert)
	return true, nil
}

func (r *certificateReloader) getCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	cert := r.current.Load()
	if err := validCertificate(cert, time.Now()); err != nil {
		return nil, err
	}
	return cert, nil
}

func (r *certificateReloader) watch(ctx context.Context, interval time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			changed, err := r.reload()
			if err != nil {
				logger.Warn("frontend certificate reload rejected; retaining previous certificate", "reason", err)
			} else if changed {
				logger.Info("frontend certificate reloaded", "expires", r.current.Load().Leaf.NotAfter)
			}
		}
	}
}
