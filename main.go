// mysql-tls-proxy is a minimal, TLS-only bridge for the classic MySQL protocol.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "mysql-tls-proxy:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("mysql-tls-proxy", flag.ContinueOnError)
	flags.SetOutput(stderr)
	listen := flags.String("listen", "127.0.0.1:3306", "client listen address")
	backend := flags.String("backend", "", "backend host:port (required)")
	certPath := flags.String("cert", "", "frontend PEM certificate chain (required)")
	keyPath := flags.String("key", "", "frontend PEM private key (required)")
	caPath := flags.String("backend-ca", "", "trusted backend CA PEM file (required)")
	serverName := flags.String("backend-server-name", "", "backend certificate name; defaults to backend host")
	timeout := flags.Duration("handshake-timeout", 10*time.Second, "total connection and TLS setup timeout")
	limit := flags.Int("max-connections", 100, "maximum simultaneous client connections")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mysql-tls-proxy [options]")
		fmt.Fprintln(stderr, "\nTLS-only MySQL/MariaDB bridge. Authentication stays with the backend.")
		fmt.Fprintln(stderr, "\nOptions:")
		flags.PrintDefaults()
		fmt.Fprintln(stderr, "\nExample:")
		fmt.Fprintln(stderr, "  mysql-tls-proxy --backend db.example.org:3306 \\")
		fmt.Fprintln(stderr, "    --cert server.crt --key server.key --backend-ca backend-ca.crt")
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("unexpected positional arguments")
	}
	if *backend == "" || *certPath == "" || *keyPath == "" || *caPath == "" {
		return errors.New("--backend, --cert, --key and --backend-ca are required")
	}
	if *timeout <= 0 || *limit <= 0 {
		return errors.New("--handshake-timeout and --max-connections must be positive")
	}
	host, port, err := net.SplitHostPort(*backend)
	if err != nil || host == "" {
		return errors.New("--backend must be host:port")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return errors.New("--backend port must be between 1 and 65535")
	}
	if *serverName == "" {
		*serverName = host
	}
	cert, err := tls.LoadX509KeyPair(*certPath, *keyPath)
	if err != nil {
		return fmt.Errorf("load frontend certificate: %w", err)
	}
	ca, err := os.ReadFile(*caPath)
	if err != nil {
		return fmt.Errorf("read backend CA: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return errors.New("backend CA file contains no valid certificates")
	}
	logger := slog.New(slog.NewTextHandler(stderr, nil))
	p := proxy{
		backend: *backend,
		frontendTLS: &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cert},
		},
		backendTLS: &tls.Config{
			MinVersion: tls.VersionTLS12,
			RootCAs:    roots,
			ServerName: *serverName,
		},
		handshakeTimeout: *timeout,
		maxConnections:   *limit,
		logger:           logger,
	}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	logger.Info("listening", "address", listener.Addr().String(), "backend", *backend, "tls", "required on both connections")
	err = p.serve(ctx, listener)
	logger.Info("stopped")
	return err
}
