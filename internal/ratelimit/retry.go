package ratelimit

import (
	"fmt"
	"net/http"
	"time"
)

// RetryConfig holds configuration for retry behavior.
type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

// DefaultRetryConfig returns sensible defaults.
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries: 3,
		BaseDelay:  100 * time.Millisecond,
		MaxDelay:   5 * time.Second,
	}
}

// ExponentialBackoff calculates the delay for a given attempt.
func (c *RetryConfig) ExponentialBackoff(attempt int) time.Duration {
	delay := c.BaseDelay * time.Duration(1<<uint(attempt))
	if delay > c.MaxDelay {
		delay = c.MaxDelay
	}
	return delay
}

// Retry executes a function with exponential backoff.
// Retries ALL errors (generic). For HTTP-aware retry with 4xx non-retry,
// use RetryHTTP.
func Retry(config *RetryConfig, fn func() error) error {
	if config == nil {
		config = DefaultRetryConfig()
	}

	var lastErr error
	for i := 0; i <= config.MaxRetries; i++ {
		if err := fn(); err != nil {
			lastErr = err
			if i < config.MaxRetries {
				time.Sleep(config.ExponentialBackoff(i))
			}
			continue
		}
		return nil
	}

	return fmt.Errorf("after %d retries, last error: %w", config.MaxRetries, lastErr)
}

// RetryHTTP executes an HTTP request with exponential backoff.
// Unlike generic Retry, this does NOT retry 4xx client errors
// (they indicate a problem with the request, not a transient server issue).
// Retries 429 (rate limited) and 5xx (server error).
func RetryHTTP(config *RetryConfig, reqFn func() (*http.Response, error)) error {
	if config == nil {
		config = DefaultRetryConfig()
	}

	var lastErr error
	for i := 0; i <= config.MaxRetries; i++ {
		resp, err := reqFn()
		if err != nil {
			lastErr = err
			if i < config.MaxRetries {
				time.Sleep(config.ExponentialBackoff(i))
			}
			continue
		}

		// Check response status
		if resp.StatusCode >= 500 || resp.StatusCode == 429 {
			// Server error or rate limited — retry
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			if i < config.MaxRetries {
				time.Sleep(config.ExponentialBackoff(i))
			}
			if resp.Body != nil {
				resp.Body.Close()
			}
			continue
		}

		// 4xx or 2xx — don't retry
		if resp.Body != nil {
			resp.Body.Close()
		}
		return nil
	}

	return fmt.Errorf("after %d retries, last error: %w", config.MaxRetries, lastErr)
}
