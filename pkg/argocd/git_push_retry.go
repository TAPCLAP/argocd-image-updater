package argocd

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/argoproj-labs/argocd-image-updater/ext/git"
	"github.com/argoproj-labs/argocd-image-updater/registry-scanner/pkg/log"
)

// GitPushRetry controls retries of a non-fast-forward git push.
// When Enabled, a rejected push is retried after `git pull --rebase`.
// Attempts is the total number of push attempts (the first try plus retries).
// Interval is the base delay between attempts; each wait is jittered to
// [interval/2, interval).
type GitPushRetry struct {
	Enabled  bool
	Attempts int
	Interval time.Duration
}

// ValidateGitPushRetry checks retry settings when the mode is enabled.
func ValidateGitPushRetry(cfg GitPushRetry) error {
	if !cfg.Enabled {
		return nil
	}
	if cfg.Attempts < 1 {
		return fmt.Errorf("invalid value %d for --git-push-retry-attempts: must be >= 1", cfg.Attempts)
	}
	if cfg.Interval < 0 {
		return fmt.Errorf("invalid value %s for --git-push-retry-interval: must be >= 0", cfg.Interval)
	}
	return nil
}

// gitRemoteWriter is the subset of git.Client used to push and rebase.
type gitRemoteWriter interface {
	Push(ctx context.Context, remote string, branch string, force bool) error
	Pull(ctx context.Context, remote string, branch string) error
}

func pushChanges(ctx context.Context, gitC git.Client, remote, branch string, force bool, retry GitPushRetry) error {
	return pushWithRetry(ctx, gitC, remote, branch, force, retry, sleepContext, rand.Int64N)
}

func pushWithRetry(ctx context.Context, gitC gitRemoteWriter, remote, branch string, force bool, retry GitPushRetry, sleep func(context.Context, time.Duration) error, intn func(int64) int64) error {
	logCtx := log.LoggerFromContext(ctx)

	err := gitC.Push(ctx, remote, branch, force)
	if err == nil || !retry.Enabled {
		return err
	}

	attempts := retry.Attempts
	if attempts < 1 {
		attempts = 1
	}

	for attempt := 2; attempt <= attempts; attempt++ {
		if !isRetryablePushError(err) {
			return err
		}
		delay := retryDelay(retry.Interval, intn)
		logCtx.Infof("git push of %s rejected (%v); retry %d/%d after %s with pull --rebase", branch, err, attempt, attempts, delay)
		if sleepErr := sleep(ctx, delay); sleepErr != nil {
			return sleepErr
		}
		if pullErr := gitC.Pull(ctx, remote, branch); pullErr != nil {
			return fmt.Errorf("git pull --rebase before retrying push: %w", pullErr)
		}
		// After a successful rebase the local commit is based on the remote tip,
		// so the follow-up push must not force-overwrite other writers' commits.
		err = gitC.Push(ctx, remote, branch, false)
		if err == nil {
			return nil
		}
	}
	return err
}

func isRetryablePushError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "before pushing again") ||
		strings.Contains(msg, "non-fast-forward") ||
		strings.Contains(msg, "tip of your current branch is behind")
}

// retryDelay returns a wait in [interval/2, interval). A non-positive interval waits nothing.
func retryDelay(interval time.Duration, intn func(int64) int64) time.Duration {
	if interval <= 0 {
		return 0
	}
	half := interval / 2
	if half <= 0 {
		return interval
	}
	return half + time.Duration(intn(int64(half)))
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
