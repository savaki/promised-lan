package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/savaki/promised-lan/internal/config"
	"github.com/savaki/promised-lan/internal/netiface"
	"github.com/savaki/promised-lan/internal/nm"
)

// Server is the interior-bound HTTP server.
type Server struct {
	cfg    config.Config
	client nm.Client
	logger *slog.Logger
}

func NewServer(cfg config.Config, client nm.Client, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{cfg: cfg, client: client, logger: logger}
}

// Run blocks until ctx is canceled or a fatal error occurs.
// It polls the interior interface for an IPv4 (up to 60 seconds) and then
// binds the HTTP server to exactly that address — never 0.0.0.0.
func (s *Server) Run(ctx context.Context) error {
	addr, err := s.waitForInteriorIP(ctx, 60*time.Second)
	if err != nil {
		return err
	}

	listenAddr := net.JoinHostPort(addr, strconv.Itoa(s.cfg.HTTPPort))
	ln, err := net.Listen("tcp4", listenAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", listenAddr, err)
	}

	srv := &http.Server{
		Handler:           NewHandler(s.client, s.logger),
		ReadHeaderTimeout: 5 * time.Second,
	}

	s.logger.Info("web server listening",
		"addr", listenAddr,
		"interface", s.cfg.InteriorInterface)

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

func (s *Server) waitForInteriorIP(ctx context.Context, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		addr, err := netiface.InterfaceIPv4(s.cfg.InteriorInterface)
		if err == nil {
			return addr.String(), nil
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("interior interface %s had no IPv4 after %s: %w",
				s.cfg.InteriorInterface, timeout, err)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}
