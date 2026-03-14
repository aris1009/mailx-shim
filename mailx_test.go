package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// newTestMailxServer creates a mock Mailx API server. authStatus and aliasStatus
// control the HTTP status codes returned. aliasName is the alias returned on
// success. authCalls counts how many times /api/authenticate was called.
func newTestMailxServer(authStatus, aliasStatus int, aliasName string, authCalls *atomic.Int32) *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /api/authenticate", func(w http.ResponseWriter, r *http.Request) {
		if authCalls != nil {
			authCalls.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		if authStatus != http.StatusOK {
			w.WriteHeader(authStatus)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "auth failed"})
			return
		}
		_ = json.NewEncoder(w).Encode(authResponse{Token: "test-jwt-token"})
	})

	mux.HandleFunc("POST /api/alias", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if aliasStatus == http.StatusUnauthorized {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		if aliasStatus != http.StatusOK && aliasStatus != http.StatusCreated {
			w.WriteHeader(aliasStatus)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "server error"})
			return
		}
		w.WriteHeader(aliasStatus)
		_ = json.NewEncoder(w).Encode(createAliasResponse{
			Alias: struct {
				Name string `json:"name"`
			}{Name: aliasName},
		})
	})

	return httptest.NewServer(mux)
}

func testConfig(baseURL string) Config {
	return Config{
		AccessKey:  "test-access-key",
		Recipient:  "test@example.com",
		Domain:     "test.example.com",
		BridgeKey:  "test-bridge-key",
		BaseURL:    baseURL,
		ListenAddr: ":0",
	}
}

func TestAuthenticate_Success(t *testing.T) {
	srv := newTestMailxServer(http.StatusOK, 0, "", nil)
	defer srv.Close()

	client := NewMailxClient(testConfig(srv.URL), srv.Client())
	if err := client.Authenticate(context.Background()); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	client.mu.Lock()
	token := client.sessionToken
	client.mu.Unlock()

	if token != "test-jwt-token" {
		t.Errorf("expected token 'test-jwt-token', got %q", token)
	}
}

func TestAuthenticate_NonOK(t *testing.T) {
	srv := newTestMailxServer(http.StatusForbidden, 0, "", nil)
	defer srv.Close()

	client := NewMailxClient(testConfig(srv.URL), srv.Client())
	err := client.Authenticate(context.Background())
	if err == nil {
		t.Fatal("expected error for non-200 status")
	}
}

func TestAuthenticate_EmptyToken(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/authenticate", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(authResponse{Token: ""})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewMailxClient(testConfig(srv.URL), srv.Client())
	err := client.Authenticate(context.Background())
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestCreateAlias_Success(t *testing.T) {
	srv := newTestMailxServer(http.StatusOK, http.StatusCreated, "test.alias@example.com", nil)
	defer srv.Close()

	client := NewMailxClient(testConfig(srv.URL), srv.Client())
	if err := client.Authenticate(context.Background()); err != nil {
		t.Fatalf("auth failed: %v", err)
	}

	alias, err := client.CreateAlias(context.Background(), "github.com")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if alias != "test.alias@example.com" {
		t.Errorf("expected alias 'test.alias@example.com', got %q", alias)
	}
}

func TestCreateAlias_RetryOn401(t *testing.T) {
	var authCalls atomic.Int32
	callCount := 0

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/authenticate", func(w http.ResponseWriter, r *http.Request) {
		authCalls.Add(1)
		_ = json.NewEncoder(w).Encode(authResponse{Token: "refreshed-token"})
	})
	mux.HandleFunc("POST /api/alias", func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "expired"})
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(createAliasResponse{
			Alias: struct {
				Name string `json:"name"`
			}{Name: "retried@example.com"},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewMailxClient(testConfig(srv.URL), srv.Client())
	client.sessionToken = "expired-token"

	alias, err := client.CreateAlias(context.Background(), "test")
	if err != nil {
		t.Fatalf("expected no error after retry, got: %v", err)
	}
	if alias != "retried@example.com" {
		t.Errorf("expected alias 'retried@example.com', got %q", alias)
	}
	if authCalls.Load() != 1 {
		t.Errorf("expected 1 re-auth call, got %d", authCalls.Load())
	}
}

func TestCreateAlias_RetryOn401_ReauthFails(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/authenticate", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "denied"})
	})
	mux.HandleFunc("POST /api/alias", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "expired"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	client := NewMailxClient(testConfig(srv.URL), srv.Client())
	client.sessionToken = "expired-token"

	_, err := client.CreateAlias(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error when re-auth fails")
	}
}

func TestCreateAlias_NonRetryableError(t *testing.T) {
	srv := newTestMailxServer(http.StatusOK, http.StatusInternalServerError, "", nil)
	defer srv.Close()

	client := NewMailxClient(testConfig(srv.URL), srv.Client())
	if err := client.Authenticate(context.Background()); err != nil {
		t.Fatalf("auth failed: %v", err)
	}

	_, err := client.CreateAlias(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error for 500 status")
	}
}
