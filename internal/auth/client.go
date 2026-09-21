package auth

import (
	"bytes"
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
	// ErrDeactivatedService is returned when the caller or target service is inactive.
	ErrDeactivatedService = errors.New("service is deactivated")
)

// Option is a functional option for configuring the Client.
type Option func(*Client)

// Client handles communication with the Identity Service.
type Client struct {
	baseURL            string
	targetClientID     string
	targetClientSecret string
	httpClient         *http.Client
}

// VerifyServiceTokenRequest matches the Identity Service verification payload.
type VerifyServiceTokenRequest struct {
	TargetClientID     string `json:"target_client_id"`
	TargetClientSecret string `json:"target_client_secret"`
	CallerToken        string `json:"caller_token"`
}

// ServiceInfo represents metadata of a service returned by Identity Service.
type ServiceInfo struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ClientID     string `json:"client_id"`
	Description  string `json:"description"`
	IsActive     bool   `json:"is_active"`
	TokenVersion int    `json:"token_version"`
}

// ServiceVerificationData contains caller & target service verification details.
type ServiceVerificationData struct {
	Valid         bool        `json:"valid"`
	CallerService ServiceInfo `json:"caller_service"`
	TargetService ServiceInfo `json:"target_service"`
}

// ServiceVerificationResult is the full response structure from /api/v1/services/verify.
type ServiceVerificationResult struct {
	Status  string                  `json:"status"`
	Message string                  `json:"message"`
	Data    ServiceVerificationData `json:"data"`
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
func NewClient(baseURL, targetClientID, targetClientSecret string, opts ...Option) *Client {
	c := &Client{
		baseURL:            strings.TrimRight(baseURL, "/"),
		targetClientID:     strings.TrimSpace(targetClientID),
		targetClientSecret: strings.TrimSpace(targetClientSecret),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Verify verifies the caller's access token against this service's credentials via the Identity Service.
func (c *Client) Verify(ctx context.Context, token string) (*Identity, error) {
	reqBody := VerifyServiceTokenRequest{
		TargetClientID:     c.targetClientID,
		TargetClientSecret: c.targetClientSecret,
		CallerToken:        token,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal verification request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.baseURL+"/api/v1/services/verify",
		bytes.NewReader(payload),
	)
	if err != nil {
		return nil, fmt.Errorf("create verification request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute verification request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("%w: status %d unauthorized", ErrInvalidToken, resp.StatusCode)
	}
	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("%w: status %d forbidden", ErrDeactivatedService, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("verification failed with status %d", resp.StatusCode)
	}

	var result ServiceVerificationResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode verification response: %w", err)
	}

	if !result.Data.Valid {
		return nil, fmt.Errorf("%w: verification marked invalid by identity service", ErrInvalidToken)
	}

	if !result.Data.CallerService.IsActive {
		return nil, fmt.Errorf("%w: caller service is deactivated", ErrDeactivatedService)
	}

	return &Identity{
		ID:           result.Data.CallerService.ID,
		Name:         result.Data.CallerService.Name,
		ClientID:     result.Data.CallerService.ClientID,
		ServiceID:    result.Data.CallerService.ClientID,
		IsActive:     result.Data.CallerService.IsActive,
		TokenVersion: result.Data.CallerService.TokenVersion,
	}, nil
}
