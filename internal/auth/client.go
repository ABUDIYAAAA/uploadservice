package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var (
	// ErrInvalidToken is returned when the identity service rejects the token.
	ErrInvalidToken = errors.New("invalid or expired token")
)

// Option is a functional option for configuring the Client.
type Option func(*Client)

// Client handles communication with the Identity Service.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// WithHTTPClient sets a custom http.Client on the Client.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) {
		if httpClient != nil {
			c.httpClient = httpClient
		}
	}
}

// NewClient creates a new Identity Service verification client.
func NewClient(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Verify verifies the access token with the Identity Service.
func (c *Client) Verify(ctx context.Context, token string) (*Identity, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/services/verify",
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("create verification request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute verification request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrInvalidToken, resp.StatusCode)
	}

	var identity Identity
	if err := json.NewDecoder(resp.Body).Decode(&identity); err != nil {
		return nil, fmt.Errorf("decode identity response: %w", err)
	}

	return &identity, nil
}
