package orch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	base string
	http *http.Client
}

func New(base string) *Client {
	return &Client{base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 5 * time.Second}}
}

func (c *Client) Notify(ctx context.Context, recipient, title, documentID string) error {
	return c.post(ctx, "/api/v1/notifications", map[string]string{
		"recipient_user_id": recipient,
		"title":             title,
		"body":              "Есть задача по документу",
		"source_document_id": documentID,
	}, nil)
}

func (c *Client) PlatformTask(ctx context.Context, author, assignee, title, documentID string, due time.Time) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	body := map[string]any{
		"author_user_id":    author,
		"assignee_user_id":  assignee,
		"title":             title,
		"description":       title,
		"source_document_id": documentID,
	}
	if !due.IsZero() {
		body["due_at"] = due
	}
	if err := c.post(ctx, "/api/v1/platform-tasks", body, &out); err != nil {
		return "", err
	}
	return out.ID, nil
}

func (c *Client) ExternalOrder(ctx context.Context, documentID, regNumber, authorID string) (string, error) {
	var out struct {
		ID string `json:"id"`
	}
	err := c.post(ctx, "/api/v1/external-orders", map[string]string{
		"document_id":    documentID,
		"reg_number":     regNumber,
		"author_user_id": authorID,
	}, &out)
	return out.ID, err
}

func (c *Client) post(ctx context.Context, path string, payload any, dest any) error {
	if c.base == "" {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("смежный контур ответил %d", resp.StatusCode)
	}
	if dest != nil {
		return json.NewDecoder(resp.Body).Decode(dest)
	}
	return nil
}
