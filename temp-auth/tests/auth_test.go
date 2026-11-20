package tests

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Maxjalo/SED/temp-auth/internal/httpapi"
	"github.com/Maxjalo/SED/temp-auth/internal/store"
)

type tokenResponse struct {
	AccessToken string     `json:"access_token"`
	JTI         string     `json:"jti"`
	User        store.User `json:"user"`
}

func newTestServer() *httptest.Server {
	st := store.NewDefaultStore()
	srv := httpapi.NewServer(st, []byte("test-signing-key-dev"), 30*time.Minute)
	return httptest.NewServer(srv)
}

func issueToken(t *testing.T, baseURL string, body map[string]string) tokenResponse {
	t.Helper()
	raw, _ := json.Marshal(body)
	resp, err := http.Post(baseURL+"/dev/token", "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("POST /dev/token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	var out tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode token response: %v", err)
	}
	return out
}

func TestIssueTokenAndAuthMeAllNineRoles(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	roles := []string{"author", "clerk", "manager", "approver", "signer", "executor", "controller", "admin", "observer"}
	for _, role := range roles {
		tok := issueToken(t, ts.URL, map[string]string{"role": role})
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET /auth/me (%s): %v", role, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("role %s: expected 200, got %d", role, resp.StatusCode)
		}
	}
}

func TestInstantRevocationAndUserBlock_AC01(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	tok := issueToken(t, ts.URL, map[string]string{"user_id": "usr-org1-author"})

	// 1. Valid initially
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/me", nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 before revoke, got %d", resp.StatusCode)
	}

	// 2. Revoke session -> immediate 401
	revResp, err := http.Post(ts.URL+"/dev/sessions/"+tok.JTI+"/revoke", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	revResp.Body.Close()
	if revResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 on revoke, got %d", revResp.StatusCode)
	}

	req2, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/me", nil)
	req2.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 after revoke, got %d", resp2.StatusCode)
	}

	// 3. Issue new token, then block user -> immediate 401
	tok2 := issueToken(t, ts.URL, map[string]string{"user_id": "usr-org1-author"})
	blkResp, err := http.Post(ts.URL+"/dev/users/usr-org1-author/block", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	blkResp.Body.Close()

	req3, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/me", nil)
	req3.Header.Set("Authorization", "Bearer "+tok2.AccessToken)
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatal(err)
	}
	resp3.Body.Close()
	if resp3.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 after block, got %d", resp3.StatusCode)
	}
}

func TestDirectoryIsolationByOrganization(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	tok := issueToken(t, ts.URL, map[string]string{"user_id": "usr-org2-author"})
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/v1/auth/directory", nil)
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var body struct {
		OrganizationCode string       `json:"organization_code"`
		Items            []store.User `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.OrganizationCode != "ORG-02" || len(body.Items) != 1 {
		t.Fatalf("unexpected ORG-02 directory isolation result: %+v", body)
	}
}
