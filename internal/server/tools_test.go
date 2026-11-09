package server

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeUpdater struct {
	calls chan struct{}
	errs  []error
}

func (f *fakeUpdater) UpdateYtDlpIfOutdated(context.Context) (string, bool, error) {
	var err error
	if len(f.errs) > 0 {
		err, f.errs = f.errs[0], f.errs[1:]
	}
	f.calls <- struct{}{}
	return "2026.02.02", err == nil, err
}

// The first check runs right away, later ones every interval, a failure
// is retried after at most updateRetry, and cancelling ctx stops it.
func TestAutoUpdateYtDlp(t *testing.T) {
	updater := &fakeUpdater{calls: make(chan struct{}, 10), errs: []error{errors.New("offline")}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		AutoUpdateYtDlp(ctx, updater, 20*time.Millisecond)
		close(done)
	}()

	for i := range 3 {
		select {
		case <-updater.calls:
		case <-time.After(5 * time.Second):
			t.Fatalf("check %d didn't happen", i+1)
		}
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("didn't stop with ctx")
	}
}
