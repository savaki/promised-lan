package web

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/savaki/promised-lan/internal/config"
	"github.com/savaki/promised-lan/internal/nm"
)

func TestServerBindsToLoopback(t *testing.T) {
	f := nm.NewFake()
	f.SetStatus(nm.Status{Link: "down"})

	ifaceName := loopbackInterface(t)

	cfg := config.Config{
		InteriorInterface: ifaceName,
		HTTPPort:          0, // pick any free port
	}
	srv := NewServer(cfg, f, slog.Default())

	// Use a fixed free port for the test.
	port, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	srv.cfg.HTTPPort = port

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run(ctx) }()

	// Wait for the server to be listening.
	url := fmt.Sprintf("http://127.0.0.1:%d/status", port)
	deadline := time.Now().Add(3 * time.Second)
	var resp *http.Response
	for time.Now().Before(deadline) {
		resp, err = http.Get(url)
		if err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d, body %s", resp.StatusCode, body)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil && err != context.Canceled && err != http.ErrServerClosed {
			t.Errorf("Run returned: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Error("server did not shut down within 3s")
	}
}

func loopbackInterface(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"lo", "lo0"} {
		if _, err := net.InterfaceByName(name); err == nil {
			return name
		}
	}
	t.Skip("no loopback interface available")
	return ""
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
