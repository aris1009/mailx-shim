package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	mailxAuthPath  = "/api/authenticate"
	mailxAliasPath = "/api/alias"
)

// MailxClient communicates with the IVPN Mailx API. It manages a session
// token obtained via access key authentication and automatically retries
// alias creation once if the token has expired.
type MailxClient struct {
	cfg          Config
	httpClient   *http.Client
	mu           sync.Mutex
	sessionToken string
}

// NewMailxClient creates a client for the Mailx API. If httpClient is nil,
// a default client with a 15-second timeout is used.
func NewMailxClient(cfg Config, httpClient *http.Client) *MailxClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &MailxClient{
		cfg:        cfg,
		httpClient: httpClient,
	}
}

// Mailx API request/response types

type authRequest struct {
	AccessKey string `json:"access_key"`
}

type authResponse struct {
	Token string `json:"token"`
}

type createAliasRequest struct {
	Domain      string `json:"domain"`
	Recipients  string `json:"recipients"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

type createAliasResponse struct {
	Alias struct {
		Name string `json:"name"`
	} `json:"alias"`
}

// Authenticate obtains a session token from the Mailx API using the
// configured access key.
func (c *MailxClient) Authenticate(ctx context.Context) error {
	body, err := json.Marshal(authRequest{AccessKey: c.cfg.AccessKey})
	if err != nil {
		return fmt.Errorf("marshalling auth request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.cfg.BaseURL+mailxAuthPath, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("sending auth request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("authenticate returned status %d", resp.StatusCode)
	}

	var ar authResponse
	if err := json.NewDecoder(resp.Body).Decode(&ar); err != nil {
		return fmt.Errorf("decoding auth response: %w", err)
	}
	if ar.Token == "" {
		return fmt.Errorf("authenticate response contained empty token")
	}

	c.mu.Lock()
	c.sessionToken = ar.Token
	c.mu.Unlock()

	return nil
}

// CreateAlias creates a new email alias via the Mailx API. If the session
// token has expired (401), it re-authenticates once and retries.
func (c *MailxClient) CreateAlias(ctx context.Context, description string) (string, error) {
	alias, err := c.doCreateAlias(ctx, description)
	if err == nil {
		return alias, nil
	}

	if isUnauthorized(err) {
		if authErr := c.Authenticate(ctx); authErr != nil {
			return "", fmt.Errorf("re-authentication failed: %w", authErr)
		}
		return c.doCreateAlias(ctx, description)
	}

	return "", err
}

type errUnauthorized struct{}

func (e errUnauthorized) Error() string { return "unauthorized (401)" }

func isUnauthorized(err error) bool {
	_, ok := err.(errUnauthorized)
	return ok
}

func (c *MailxClient) doCreateAlias(ctx context.Context, description string) (string, error) {
	body, err := json.Marshal(createAliasRequest{
		Domain:      c.cfg.Domain,
		Recipients:  c.cfg.Recipient,
		Description: description,
		Enabled:     true,
	})
	if err != nil {
		return "", fmt.Errorf("marshalling alias request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.cfg.BaseURL+mailxAliasPath, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("creating alias request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	c.mu.Lock()
	token := c.sessionToken
	c.mu.Unlock()

	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("sending alias request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		io.Copy(io.Discard, resp.Body)
		return "", errUnauthorized{}
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf("create alias returned status %d", resp.StatusCode)
	}

	var car createAliasResponse
	if err := json.NewDecoder(resp.Body).Decode(&car); err != nil {
		return "", fmt.Errorf("decoding alias response: %w", err)
	}
	if car.Alias.Name == "" {
		return "", fmt.Errorf("create alias response contained empty name")
	}

	return car.Alias.Name, nil
}
