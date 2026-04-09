package nm

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestRunnerRedactsPSK(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := newRunner(logger)
	// Use /bin/echo as a fake nmcli so the test runs on any host.
	r.binary = "/bin/echo"
	args := []string{"con", "mod", "foo", "wifi-sec.psk", "topsecret"}
	out, err := r.run(context.Background(), args, 4) // psk at index 4
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// Stdout from echo just mirrors args — that's fine, we're testing logs.
	if !bytes.Contains(out, []byte("topsecret")) {
		t.Errorf("echo output missing: %s", out)
	}
	log := logBuf.String()
	if strings.Contains(log, "topsecret") {
		t.Errorf("psk leaked into log: %s", log)
	}
	if !strings.Contains(log, "***") {
		t.Errorf("redaction marker missing from log: %s", log)
	}
}

func TestRunnerFailure(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	r := newRunner(logger)
	r.binary = "/bin/sh"
	// Exit 42 via sh -c "exit 42"
	_, err := r.run(context.Background(), []string{"-c", "exit 42"})
	if err == nil {
		t.Fatal("expected error from exit 42")
	}
	var nerr *NMError
	if !asNMError(err, &nerr) {
		t.Fatalf("expected *NMError, got %T: %v", err, err)
	}
	if nerr.Kind != ErrUnknown {
		t.Errorf("kind: got %v, want ErrUnknown for exit 42", nerr.Kind)
	}
}

// asNMError is a local helper replacing errors.As for tests.
func asNMError(err error, target **NMError) bool {
	for err != nil {
		if e, ok := err.(*NMError); ok {
			*target = e
			return true
		}
		type unwrapper interface{ Unwrap() error }
		if u, ok := err.(unwrapper); ok {
			err = u.Unwrap()
		} else {
			break
		}
	}
	return false
}
