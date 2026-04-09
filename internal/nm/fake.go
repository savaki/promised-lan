package nm

import (
	"context"
	"sync"
)

// Fake is an in-memory Client suitable for tests. It never stores PSKs: the
// UpdateAndActivate method immediately drops its psk argument after using it
// to decide whether the staged result applies.
type Fake struct {
	mu     sync.Mutex
	status Status
	scan   []Network
	next   *fakeUpdateResult

	// LastPSK is intentionally not stored; kept as an empty field so tests
	// can assert it remains empty after calls.
	LastPSK string
}

type fakeUpdateResult struct {
	Status Status
	Err    error
}

func NewFake() *Fake {
	return &Fake{status: Status{Link: "down"}}
}

func (f *Fake) SetStatus(s Status) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = s
}

func (f *Fake) SetScan(n []Network) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scan = append([]Network(nil), n...)
}

// SetNextUpdateResult stages the result of the next UpdateAndActivate call.
// If err is nil, the staged Status becomes the new current status on success.
// If err is non-nil, the staged Status is still set (reflecting observed state
// after the failed attempt) and the error is returned.
func (f *Fake) SetNextUpdateResult(s Status, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.next = &fakeUpdateResult{Status: s, Err: err}
}

func (f *Fake) Status(ctx context.Context) (Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, nil
}

func (f *Fake) Scan(ctx context.Context) ([]Network, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Network(nil), f.scan...), nil
}

func (f *Fake) UpdateAndActivate(ctx context.Context, ssid, psk string) (Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_ = psk // never stored
	if f.next == nil {
		f.status = Status{Link: "up", SSID: ssid, Interface: f.status.Interface}
		return f.status, nil
	}
	res := f.next
	f.next = nil
	f.status = res.Status
	return res.Status, res.Err
}

func (f *Fake) EnsureProfile(ctx context.Context) error {
	return nil
}
