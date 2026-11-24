package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type APIClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func NewAPIClient(baseURL, token string) (*APIClient, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "http://localhost:8000"
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, errors.New("не задан SED_TOKEN")
	}
	return &APIClient{
		baseURL: baseURL,
		token:   token,
		http:    &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (c *APIClient) doJSON(ctx context.Context, method, path string, body any, headers map[string]string) (json.RawMessage, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, formatAPIError(resp.StatusCode, raw)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage(`{"status":"ok"}`), nil
	}
	return json.RawMessage(raw), nil
}

func (c *APIClient) uploadFile(ctx context.Context, docID, filePath, customName string) (json.RawMessage, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("файл недоступен: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("указан не обычный файл")
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	name := strings.TrimSpace(customName)
	if name == "" {
		name = filepath.Base(filePath)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/files", f)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-Document-Id", docID)
	req.Header.Set("X-File-Name", url.PathEscape(name))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = info.Size()

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, formatAPIError(resp.StatusCode, raw)
	}
	return json.RawMessage(raw), nil
}

func (c *APIClient) downloadFile(ctx context.Context, fileID, dir, customName string) (string, int64, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/files/"+url.PathEscape(fileID), nil)
	if err != nil {
		return "", 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return "", 0, "", formatAPIError(resp.StatusCode, raw)
	}

	name := strings.TrimSpace(customName)
	if name == "" {
		if cd := resp.Header.Get("Content-Disposition"); cd != "" {
			if _, params, err := mime.ParseMediaType(cd); err == nil && params["filename"] != "" {
				name = params["filename"]
			}
		}
	}
	if name == "" {
		name = fileID
	}
	name = filepath.Base(name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", 0, "", err
	}
	dest := filepath.Join(dir, name)
	out, err := os.Create(dest)
	if err != nil {
		return "", 0, "", err
	}
	n, copyErr := io.Copy(out, resp.Body)
	closeErr := out.Close()
	if copyErr != nil {
		return "", 0, "", copyErr
	}
	if closeErr != nil {
		return "", 0, "", closeErr
	}
	return dest, n, name, nil
}

func formatAPIError(status int, raw []byte) error {
	var parsed struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &parsed) == nil && strings.TrimSpace(parsed.Error) != "" {
		return fmt.Errorf("HTTP %d: %s", status, parsed.Error)
	}
	msg := strings.TrimSpace(string(raw))
	if msg == "" {
		msg = http.StatusText(status)
	}
	return fmt.Errorf("HTTP %d: %s", status, msg)
}

func idempotencyKey(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "mcp-" + hex.EncodeToString(sum[:16]), nil
}
