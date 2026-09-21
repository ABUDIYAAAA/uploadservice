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

// ServiceTokenData contains the token payload returned by Identity Service.
type ServiceTokenData struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// ServiceTokenResponse is the standard response from /api/v1/services/token.
type ServiceTokenResponse struct {
	Status      string           `json:"status"`
	Message     string           `json:"message"`
	Data        ServiceTokenData `json:"data"`
	AccessToken string           `json:"access_token"` // for flat fallback
	Token       string           `json:"token"`        // for flat fallback
}

// ServiceTokenRequest is the request body for obtaining an M2M token.
type ServiceTokenRequest struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
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
	reqBody := ServiceTokenRequest{
		ClientID:     strings.TrimSpace(clientID),
		ClientSecret: strings.TrimSpace(clientSecret),
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal token request: %w", err)
	}

	endpoints := []string{
		c.baseURL + "/api/v1/services/token",
		c.baseURL + "/services/token",
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

		var tokResp ServiceTokenResponse
		if err := json.NewDecoder(resp.Body).Decode(&tokResp); err != nil {
			lastErr = fmt.Errorf("decode token response from %s: %w", endpoint, err)
			continue
		}

		token := tokResp.Data.AccessToken
		if token == "" {
			token = tokResp.AccessToken
		}
		if token == "" {
			token = tokResp.Token
		}
		if token == "" {
			lastErr = fmt.Errorf("no token found in response from %s", endpoint)
			continue
		}

		return token, nil
	}

	if lastErr != nil {
		return "", fmt.Errorf("failed to fetch token: %w", lastErr)
	}
	return "", errors.New("failed to fetch token: unknown error")
}
