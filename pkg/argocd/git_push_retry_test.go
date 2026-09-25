package argocd

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubRemote struct {
	pushes []bool
	pulls  int
	pushFn func(force bool) error
	pullFn func() error
}

func (s *stubRemote) Push(_ context.Context, _, _ string, force bool) error {
	s.pushes = append(s.pushes, force)
	if s.pushFn != nil {
		return s.pushFn(force)
	}
	return nil
}

func (s *stubRemote) Pull(_ context.Context, _, _ string) error {
	s.pulls++
	if s.pullFn != nil {
		return s.pullFn()
	}
	return nil
}

func TestPushWithRetry_DisabledDoesNotPull(t *testing.T) {
	stub := &stubRemote{pushFn: func(bool) error {
		return errors.New("hint: 'git pull ...' before pushing again")
	}}
	err := pushWithRetry(context.Background(), stub, "origin", "main", false, GitPushRetry{}, func(context.Context, time.Duration) error {
		t.Fatal("sleep should not be called")
		return nil
	}, nil)
	require.Error(t, err)
	assert.Equal(t, 1, len(stub.pushes))
	assert.Equal(t, 0, stub.pulls)
}

func TestPushWithRetry_RebasesThenPushesWithoutForce(t *testing.T) {
	calls := 0
	stub := &stubRemote{pushFn: func(force bool) error {
		calls++
		if calls == 1 {
			return errors.New("! [rejected] main -> main (non-fast-forward)")
		}
		assert.False(t, force)
		return nil
	}}
	var slept []time.Duration
	err := pushWithRetry(context.Background(), stub, "origin", "main", false, GitPushRetry{
		Enabled:  true,
		Attempts: 3,
		Interval: 2 * time.Second,
	}, func(_ context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}, func(n int64) int64 { return 0 })
	require.NoError(t, err)
	assert.Equal(t, []bool{false, false}, stub.pushes)
	assert.Equal(t, 1, stub.pulls)
	require.Len(t, slept, 1)
	assert.Equal(t, time.Second, slept[0])
}

func TestPushWithRetry_DoesNotRetryOtherErrors(t *testing.T) {
	stub := &stubRemote{pushFn: func(bool) error {
		return errors.New("authentication failed")
	}}
	err := pushWithRetry(context.Background(), stub, "origin", "main", false, GitPushRetry{
		Enabled:  true,
		Attempts: 5,
		Interval: time.Second,
	}, func(context.Context, time.Duration) error {
		t.Fatal("sleep should not be called")
		return nil
	}, func(int64) int64 { return 0 })
	require.Error(t, err)
	assert.Equal(t, 1, len(stub.pushes))
	assert.Equal(t, 0, stub.pulls)
}

func TestPushWithRetry_StopsWhenPullFails(t *testing.T) {
	stub := &stubRemote{
		pushFn: func(bool) error {
			return errors.New("Updates were rejected because the tip of your current branch is behind")
		},
		pullFn: func() error { return errors.New("conflict") },
	}
	err := pushWithRetry(context.Background(), stub, "origin", "main", true, GitPushRetry{
		Enabled:  true,
		Attempts: 4,
		Interval: 0,
	}, func(context.Context, time.Duration) error { return nil }, func(int64) int64 { return 0 })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git pull --rebase")
	assert.Equal(t, 1, len(stub.pushes))
	assert.Equal(t, 1, stub.pulls)
}

func TestRetryDelay_JitterBounds(t *testing.T) {
	interval := 4 * time.Second
	assert.Equal(t, 2*time.Second, retryDelay(interval, func(int64) int64 { return 0 }))
	assert.Equal(t, 4*time.Second-time.Nanosecond, retryDelay(interval, func(n int64) int64 {
		assert.Equal(t, int64(2*time.Second), n)
		return n - 1
	}))
	assert.Equal(t, time.Duration(0), retryDelay(0, func(int64) int64 {
		t.Fatal("rng should not be called")
		return 0
	}))
}

func TestValidateGitPushRetry(t *testing.T) {
	assert.NoError(t, ValidateGitPushRetry(GitPushRetry{}))
	assert.NoError(t, ValidateGitPushRetry(GitPushRetry{Enabled: true, Attempts: 3, Interval: time.Second}))
	assert.Error(t, ValidateGitPushRetry(GitPushRetry{Enabled: true, Attempts: 0, Interval: time.Second}))
	assert.Error(t, ValidateGitPushRetry(GitPushRetry{Enabled: true, Attempts: 1, Interval: -time.Second}))
}
