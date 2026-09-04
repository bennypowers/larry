package httpclient

import (
	"net/http"
	"time"
)

// Client wraps an http.Client with rate-limit helpers.
type Client struct {
	http *http.Client
}

// NewClient creates a new HTTP client with sensible defaults.
func NewClient() *Client {
	return &Client{
		http: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// Get performs a GET request.
func (c *Client) Get(url string) (*http.Response, error) {
	return c.http.Get(url)
}

// Do performs an HTTP request.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	return c.http.Do(req)
}
