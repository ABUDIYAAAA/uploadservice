package test

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

// TokenResponse represents possible token response schemas from the identity service.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	Token       string `json:"token"`
}

// IdentityClient handles fetching access tokens for service-to-service authentication.
type IdentityClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewIdentityClient creates a new client for obtaining service tokens.
func NewIdentityClient(baseURL string) *IdentityClient {
	return &IdentityClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// FetchToken obtains an access token for the specified service credentials.
func (c *IdentityClient) FetchToken(ctx context.Context, clientID, clientSecret string) (string, error) {
	endpoints := []string{
		c.baseURL + "/api/v1/services/token",
		c.baseURL + "/services/token",
	}

	payload, err := json.Marshal(map[string]string{
		"client_id":      clientID,
		"client_secret":  clientSecret,
		"service_id":     clientID,
		"service_secret": clientSecret,
	})
	if err != nil {
		return "", fmt.Errorf("marshal token request: %w", err)
	}

	var lastErr error
	for _, endpoint := range endpoints {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return "", fmt.Errorf("create token request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("endpoint %s returned status %s", endpoint, resp.Status)
			continue
		}

		var tokResp TokenResponse
		if err := json.NewDecoder(resp.Body).Decode(&tokResp); err != nil {
			lastErr = fmt.Errorf("decode token response from %s: %w", endpoint, err)
			continue
		}

		token := tokResp.AccessToken
		if token == "" {
			token = tokResp.Token
		}
		if token == "" {
			lastErr = fmt.Errorf("no token in response from %s", endpoint)
			continue
		}

		return token, nil
	}

	if lastErr != nil {
		return "", fmt.Errorf("failed to fetch token: %w", lastErr)
	}
	return "", errors.New("failed to fetch token: unknown error")
}
