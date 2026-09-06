package httpclient

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/ratelimit"
)

// Client wraps an http.Client with rate-limit helpers.
type Client struct {
	http      *http.Client
	rateLimiter *ratelimit.RateLimiter
	config    *ratelimit.RetryConfig
}

// NewClient creates a new HTTP client with sensible defaults.
func NewClient() *Client {
	return NewClientWithLimiter(ratelimit.NewRateLimiter())
}

// NewClientWithLimiter creates a new HTTP client with a specific rate limiter.
func NewClientWithLimiter(rl *ratelimit.RateLimiter) *Client {
	return &Client{
		http: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		rateLimiter: rl,
		config:      ratelimit.DefaultRetryConfig(),
	}
}

// Get performs a GET request with rate limiting.
func (c *Client) Get(url string) (*http.Response, error) {
	return c.doWithRateLimit(http.MethodGet, url, nil)
}

// Do performs an HTTP request with rate limiting.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	host := req.URL.Host
	if host == "" {
		host = req.Host
	}
	if host == "" {
		return nil, fmt.Errorf("no host in request")
	}
	var body io.Reader
	if req.Body != nil {
		body = req.Body
	}
	return c.doWithRateLimit(req.Method, req.URL.String(), body)
}

// GetWithRetry performs a GET request with rate limiting and retry on 5xx/429.
func (c *Client) GetWithRetry(url string) (*http.Response, error) {
	host := extractHost(url)
	return c.doWithRetry(host, http.MethodGet, url, nil)
}

// DoWithRetry performs an HTTP request with rate limiting and retry on 5xx/429.
func (c *Client) DoWithRetry(req *http.Request) (*http.Response, error) {
	host := req.URL.Host
	if host == "" {
		host = req.Host
	}
	if host == "" {
		return nil, fmt.Errorf("no host in request")
	}
	var body io.Reader
	if req.Body != nil {
		body = req.Body
	}
	return c.doWithRetry(host, req.Method, req.URL.String(), body)
}

// NewClientForProvider creates an HTTP client optimized for a specific provider type.
// It reuses the provided rate limiter (which should already be configured for the provider).
func NewClientForProvider(rl *ratelimit.RateLimiter, rtype remoteid.RemoteIdType) *Client {
	_ = rtype // provider type is used for rate limiter config (applied via rl)
	return &Client{
		http: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
		rateLimiter: rl,
		config:      ratelimit.DefaultRetryConfig(),
	}
}

// ParseRateHeaders extracts rate-limit information from response headers.
func ParseRateHeaders(resp *http.Response) map[string]string {
	if resp == nil {
		return nil
	}
	headers := make(map[string]string)
	for _, key := range []string{"X-RateLimit-Remaining", "X-RateLimit-Reset", "X-RateLimit-Limit", "Retry-After"} {
		if val := resp.Header.Get(key); val != "" {
			headers[key] = val
		}
	}
	return headers
}

func (c *Client) doWithRateLimit(method, url string, body io.Reader) (*http.Response, error) {
	host := extractHost(url)
	var resp *http.Response
	err := c.rateLimiter.Do(nil, host, func() error {
		req, err := http.NewRequest(method, url, body)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "larry/0.1.0")
		resp, err = c.http.Do(req)
		return err
	})
	return resp, err
}

func (c *Client) doWithRetry(host, method, url string, body io.Reader) (*http.Response, error) {
	var resp *http.Response
	var lastErr error
	for i := 0; i <= c.config.MaxRetries; i++ {
		var reqErr error
		reqErr = c.rateLimiter.Do(nil, host, func() error {
			req, err := http.NewRequest(method, url, body)
			if err != nil {
				return err
			}
			req.Header.Set("User-Agent", "larry/0.1.0")
			resp, reqErr = c.http.Do(req)
			return reqErr
		})
		if reqErr != nil {
			lastErr = reqErr
			if i < c.config.MaxRetries {
				time.Sleep(c.config.ExponentialBackoff(i))
			}
			continue
		}

		// Check status code
		if resp.StatusCode >= 500 || resp.StatusCode == 429 {
			lastErr = fmt.Errorf("HTTP %d", resp.StatusCode)
			if resp.Body != nil {
				resp.Body.Close()
			}
			if i < c.config.MaxRetries {
				time.Sleep(c.config.ExponentialBackoff(i))
			}
			continue
		}

		return resp, nil
	}
	return nil, fmt.Errorf("after %d retries, last error: %w", c.config.MaxRetries, lastErr)
}

func extractHost(url string) string {
	// Strip scheme
	host := url
	if idx := len("https://"); len(url) > idx && url[:idx] == "https://" {
		host = url[idx:]
	} else if idx := len("http://"); len(url) > idx && url[:idx] == "http://" {
		host = url[idx:]
	}
	// Strip path
	if idx := len(host) - len(url); idx > 0 {
		host = url[:idx]
	}
	return host
}
