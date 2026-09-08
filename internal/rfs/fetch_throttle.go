package rfs

import (
	"errors"
	"time"
)

// Secondary requests must preserve Retry-After just like the initial fetch.
type fetchThrottle struct{ retryAfter time.Duration }

func (*fetchThrottle) Error() string { return "upstream throttled" }
func pollFailure(err error) (PollResult, error) {
	var throttle *fetchThrottle
	if errors.As(err, &throttle) {
		return PollResult{Status: PollThrottled, RetryAfter: throttle.retryAfter}, nil
	}
	return PollResult{}, err
}
