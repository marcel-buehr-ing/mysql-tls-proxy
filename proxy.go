package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"time"
)

const (
	clientProtocol41 = 1 << 9
	clientSSL        = 1 << 11
	maxGreetingSize  = 16 * 1024
)

type proxy struct {
	backend          string
	frontendTLS      *tls.Config
	backendTLS       *tls.Config
	handshakeTimeout time.Duration
	shutdownTimeout  time.Duration
	maxConnections   int
	logger           *slog.Logger
}

func (p *proxy) serve(ctx context.Context, listener net.Listener) error {
	// Stopping the listener must not cancel already accepted sessions. Their
	// context is cancelled only after the drain deadline or an accept failure.
	sessions, cancel := context.WithCancel(context.WithoutCancel(ctx))
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stop()
	defer listener.Close()
	var workers sync.WaitGroup
	slots := make(chan struct{}, p.maxConnections)
	var acceptErr error
	for {
		client, err := listener.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				acceptErr = fmt.Errorf("accept: %w", err)
			}
			break
		}
		if ctx.Err() != nil {
			_ = client.Close()
			break
		}
		select {
		case slots <- struct{}{}:
		default:
			_ = client.Close()
			continue
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			defer func() { <-slots }()
			if err := p.handle(sessions, client); err != nil && sessions.Err() == nil {
				// Never log packet contents, usernames, passwords or SQL text.
				p.logger.Warn("connection closed", "reason", err)
			}
		}()
	}
	done := make(chan struct{})
	go func() { workers.Wait(); close(done) }()
	if ctx.Err() != nil && p.shutdownTimeout > 0 {
		p.logger.Info("draining connections", "timeout", p.shutdownTimeout)
		timer := time.NewTimer(p.shutdownTimeout)
		defer timer.Stop()
		select {
		case <-done:
			return acceptErr
		case <-timer.C:
			p.logger.Info("drain deadline reached; closing remaining connections")
		}
	}
	cancel()
	<-done
	return acceptErr
}

func (p *proxy) handle(ctx context.Context, client net.Conn) error {
	defer client.Close()
	stopClient := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stopClient()
	setup, cancel := context.WithTimeout(ctx, p.handshakeTimeout)
	defer cancel()
	deadline, _ := setup.Deadline()
	if err := client.SetDeadline(deadline); err != nil {
		return err
	}
	backend, err := (&net.Dialer{}).DialContext(setup, "tcp", p.backend)
	if err != nil {
		return fmt.Errorf("connect to backend: %w", err)
	}
	defer backend.Close()
	stopBackend := context.AfterFunc(ctx, func() { _ = backend.Close() })
	defer stopBackend()
	if err := backend.SetDeadline(deadline); err != nil {
		return err
	}

	greeting, err := readPacket(backend, maxGreetingSize, 0)
	if err != nil {
		return fmt.Errorf("read backend greeting: %w", err)
	}
	if greeting[4] == 0xff {
		// Preserve the backend's initial error, without logging its contents.
		_ = writeBytes(client, greeting)
		return errors.New("backend refused connection before authentication")
	}
	if err := checkGreeting(greeting[4:]); err != nil {
		return fmt.Errorf("backend greeting: %w", err)
	}
	if err := writeBytes(client, greeting); err != nil {
		return fmt.Errorf("send greeting: %w", err)
	}
	request, err := readPacket(client, 32, 1)
	if err != nil {
		return fmt.Errorf("read client SSLRequest: %w", err)
	}
	if err := checkSSLRequest(request[4:]); err != nil {
		return err
	}
	frontendTLS := tls.Server(client, p.frontendTLS)
	if err := frontendTLS.HandshakeContext(setup); err != nil {
		return fmt.Errorf("client TLS handshake: %w", err)
	}
	// Forward the exact SSLRequest, preserving capabilities and packet sequence.
	if err := writeBytes(backend, request); err != nil {
		return fmt.Errorf("send backend SSLRequest: %w", err)
	}
	backendTLS := tls.Client(backend, p.backendTLS)
	if err := backendTLS.HandshakeContext(setup); err != nil {
		return fmt.Errorf("backend TLS handshake: %w", err)
	}
	if err := client.SetDeadline(time.Time{}); err != nil {
		return err
	}
	if err := backend.SetDeadline(time.Time{}); err != nil {
		return err
	}
	cancel()
	// Authentication and all subsequent bytes belong to MariaDB and the client.
	// There is no credential handling, query inspection, retry or reconnection.
	done := make(chan error, 2)
	go func() { _, err := io.Copy(backendTLS, frontendTLS); done <- err }()
	go func() { _, err := io.Copy(frontendTLS, backendTLS); done <- err }()
	first := <-done
	// A disconnected peer ends the complete database session. Closing the raw
	// sockets also releases the other copy goroutine, including on shutdown.
	_ = client.Close()
	_ = backend.Close()
	<-done
	if first != nil && !errors.Is(first, net.ErrClosed) {
		return fmt.Errorf("relay: %w", first)
	}
	return nil
}

// readPacket bounds untrusted allocations during the unencrypted handshake.
// It reads exactly one packet so pipelined TLS bytes remain on the connection.
func readPacket(r io.Reader, limit int, sequence byte) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	n := int(header[0]) | int(header[1])<<8 | int(header[2])<<16
	if n == 0 || n > limit {
		return nil, errors.New("invalid handshake packet length; TLS SSLRequest required")
	}
	if header[3] != sequence {
		return nil, errors.New("unexpected handshake packet sequence")
	}
	packet := make([]byte, n+4)
	copy(packet, header[:])
	if _, err := io.ReadFull(r, packet[4:]); err != nil {
		return nil, err
	}
	return packet, nil
}

func checkGreeting(payload []byte) error {
	if len(payload) < 2 || payload[0] != 10 {
		return errors.New("classic protocol version 10 required")
	}
	end := bytes.IndexByte(payload[1:], 0)
	if end < 0 {
		return errors.New("unterminated server version")
	}
	// Version byte + NUL-terminated version + connection ID + nonce + filler.
	offset := 1 + end + 1 + 4 + 8 + 1
	if len(payload) < offset+2 {
		return errors.New("truncated capabilities")
	}
	caps := binary.LittleEndian.Uint16(payload[offset : offset+2])
	if caps&(clientSSL|clientProtocol41) != clientSSL|clientProtocol41 {
		return errors.New("backend must support TLS and protocol 4.1")
	}
	return nil
}

func checkSSLRequest(payload []byte) error {
	if len(payload) != 32 {
		return errors.New("TLS required: expected a 32-byte SSLRequest")
	}
	caps := binary.LittleEndian.Uint32(payload[:4])
	if caps&(clientSSL|clientProtocol41) != clientSSL|clientProtocol41 {
		return errors.New("TLS required: SSLRequest capability flags missing")
	}
	return nil
}

func writeBytes(w io.Writer, data []byte) error {
	_, err := io.Copy(w, bytes.NewReader(data))
	return err
}
