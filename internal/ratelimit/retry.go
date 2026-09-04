package ratelimit

import (
	"fmt"
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
