package nm

import (
	"bytes"
	"context"
	"log/slog"
	"os/exec"
)

// runner is a small wrapper around os/exec that redacts sensitive args in logs
// and converts nmcli exit codes to typed NMError values.
type runner struct {
	binary string
	logger *slog.Logger
}

func newRunner(logger *slog.Logger) *runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &runner{binary: "nmcli", logger: logger}
}

// run executes r.binary with args. redactIdx lists argv positions whose values
// should be replaced with "***" in any logged form. Returns stdout on success
// or an *NMError on failure.
func (r *runner) run(ctx context.Context, args []string, redactIdx ...int) ([]byte, error) {
	logArgs := make([]string, len(args))
	copy(logArgs, args)
	for _, i := range redactIdx {
		if i >= 0 && i < len(logArgs) {
			logArgs[i] = "***"
		}
	}
	r.logger.Debug("exec", "bin", r.binary, "args", logArgs)

	cmd := exec.CommandContext(ctx, r.binary, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		exitCode := -1
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		}
		kind := Classify(exitCode, stderr.String())
		if kind == ErrNone {
			kind = ErrUnknown
		}
		r.logger.Debug("exec failed",
			"bin", r.binary,
			"args", logArgs,
			"exit", exitCode,
			"stderr", stderr.String(),
			"kind", kind)
		return out, &NMError{Kind: kind, Stderr: stderr.String()}
	}
	return out, nil
}
