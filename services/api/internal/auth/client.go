package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Principal struct {
	UserID           string   `json:"user_id"`
	FullName         string   `json:"full_name"`
	Position         string   `json:"position"`
	DepartmentCode   string   `json:"department_code"`
	DepartmentName   string   `json:"department_name"`
	OrganizationCode string   `json:"organization_code"`
	ManagerID        string   `json:"manager_id"`
	TopManagerID     string   `json:"top_manager_id"`
	Roles            []string `json:"roles"`
	HasPOA           bool     `json:"has_power_of_attorney"`
}

func (p Principal) Has(role string) bool {
	for _, item := range p.Roles {
		if item == role {
			return true
		}
	}
	return false
}

func (p Principal) CanCreate() bool {
	return p.Has("write") || p.Has("edit") || p.Has("author") || p.Has("clerk") || p.Has("manager")
}

func (p Principal) CanRegister() bool {
	return p.Has("edit") || p.Has("clerk")
}

func (p Principal) CanReadOrg() bool {
	return p.Has("read") || p.Has("edit") || p.Has("clerk") || p.Has("admin") || p.Has("controller") || p.Has("manager")
}

func (p Principal) CanPause() bool {
	return p.Has("edit") || p.Has("clerk") || p.Has("admin")
}

type Client struct {
	base string
	http *http.Client
}

func New(base string) *Client {
	return &Client{
		base: strings.TrimRight(base, "/"),
		http: &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *Client) Me(ctx context.Context, token string) (Principal, error) {
	var principal Principal
	err := c.get(ctx, token, "/api/v1/auth/me", &principal)
	return principal, err
}

func (c *Client) Directory(ctx context.Context, token string) ([]Principal, error) {
	var body struct {
		Items []Principal `json:"items"`
	}
	if err := c.get(ctx, token, "/api/v1/auth/directory", &body); err != nil {
		return nil, err
	}
	return body.Items, nil
}

func (c *Client) get(ctx context.Context, token, path string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("сервис входа недоступен")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("сервис входа ответил %d", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("не разобран ответ сервиса входа")
	}
	return nil
}

var ErrUnauthorized = fmt.Errorf("сессия не подтверждена")
