package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type recordedReq struct {
	Method  string
	Path    string
	Query   string
	Headers http.Header
	Body    string
}

func newMockAPI(t *testing.T) (*httptest.Server, *[]recordedReq) {
	t.Helper()
	var mu sync.Mutex
	var reqs []recordedReq

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		reqs = append(reqs, recordedReq{
			Method:  r.Method,
			Path:    r.URL.Path,
			Query:   r.URL.RawQuery,
			Headers: r.Header.Clone(),
			Body:    string(raw),
		})
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/profile":
			_, _ = w.Write([]byte(`{"user":{"user_id":"u-1"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/directory":
			_, _ = w.Write([]byte(`{"items":[{"user_id":"u-1","full_name":"Иванов Иван","position":"Менеджер","department_code":"SALES","department_name":"Продажи"},{"user_id":"u-2","full_name":"Петров Пётр","position":"Инженер","department_code":"KB","department_name":"КБ"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/documents":
			_, _ = w.Write([]byte(`{"items":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/tasks":
			_, _ = w.Write([]byte(`{"items":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/documents/doc-1":
			_, _ = w.Write([]byte(`{"id":"doc-1","title":"Тест"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/documents/missing":
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"нет доступа к контуру"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/documents":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"doc-new"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/documents/doc-1/route":
			_, _ = w.Write([]byte(`{"id":"doc-1","route_started":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/files":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"doc-1","files":[{"id":"file-1"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/files/file-1":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", `attachment; filename="report.txt"`)
			_, _ = w.Write([]byte("hello-sed"))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/tasks/task-1/decide":
			_, _ = w.Write([]byte(`{"id":"doc-1"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/documents/doc-1/comments":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"doc-1"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/documents/doc-1/links":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"doc-1"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"not found"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &reqs
}

func TestToolsAndStdioFlow(t *testing.T) {
	srv, reqs := newMockAPI(t)
	client, err := NewAPIClient(srv.URL, "test-token")
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(client)
	mcp := NewMCPServer(reg)

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"whoami","arguments":{}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"find","arguments":{"kind":"people","q":"Иванов"}}}`,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"start_route","arguments":{"document_id":"doc-1","action":"recall"}}}`,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"open_document","arguments":{"document_id":"missing"}}}`,
	}, "\n") + "\n"

	var out bytes.Buffer
	if err := mcp.Serve(context.Background(), strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 6 {
		t.Fatalf("expected 6 responses (notification ignored), got %d:\n%s", len(lines), out.String())
	}

	// Verify tools/list has 10 tools
	var listResp struct {
		Result struct {
			Tools []ToolDef `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Result.Tools) != 10 {
		t.Fatalf("expected 10 tools, got %d", len(listResp.Result.Tools))
	}

	// Verify people filter
	var findResp struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	_ = json.Unmarshal([]byte(lines[3]), &findResp)
	if findResp.Result.IsError || !strings.Contains(findResp.Result.Content[0].Text, "u-1") || strings.Contains(findResp.Result.Content[0].Text, "u-2") {
		t.Fatalf("unexpected people filter result: %+v", findResp)
	}

	// Verify start_route only sent action=start
	if (*reqs)[2].Method != http.MethodPost || (*reqs)[2].Path != "/v1/documents/doc-1/route" {
		t.Fatalf("unexpected start_route request: %+v", (*reqs)[2])
	}
	if strings.TrimSpace((*reqs)[2].Body) != `{"action":"start"}` {
		t.Fatalf("start_route must only send action=start, got %s", (*reqs)[2].Body)
	}
	if got := (*reqs)[2].Headers.Get("Authorization"); got != "Bearer test-token" {
		t.Fatalf("expected Bearer test-token, got %q", got)
	}

	// Verify error propagation
	var errResp struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	_ = json.Unmarshal([]byte(lines[5]), &errResp)
	if !errResp.Result.IsError || !strings.Contains(errResp.Result.Content[0].Text, "HTTP 403: нет доступа к контуру") {
		t.Fatalf("expected formatted API error, got %+v", errResp)
	}
}

func TestAttachSaveCreateDecideCommentLink(t *testing.T) {
	srv, reqs := newMockAPI(t)
	client, err := NewAPIClient(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	reg := NewRegistry(client)
	ctx := context.Background()

	// create_draft idempotency key stability
	draftArgs := json.RawMessage(`{"type_code":"служебная_записка","title":"Тест","business_domain":"продажи"}`)
	if _, err := reg.Call(ctx, "create_draft", draftArgs); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Call(ctx, "create_draft", draftArgs); err != nil {
		t.Fatal(err)
	}
	k1 := (*reqs)[0].Headers.Get("Idempotency-Key")
	k2 := (*reqs)[1].Headers.Get("Idempotency-Key")
	if k1 == "" || k1 != k2 {
		t.Fatalf("expected stable non-empty Idempotency-Key, got %q and %q", k1, k2)
	}

	// attach_file from disk
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "договор.txt")
	if err := os.WriteFile(tmpFile, []byte("file-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	attachArgs, _ := json.Marshal(map[string]string{"document_id": "doc-1", "path": tmpFile})
	if _, err := reg.Call(ctx, "attach_file", attachArgs); err != nil {
		t.Fatal(err)
	}
	upReq := (*reqs)[2]
	if upReq.Path != "/v1/files" || upReq.Headers.Get("X-Document-Id") != "doc-1" || upReq.Body != "file-bytes" {
		t.Fatalf("unexpected upload request: %+v", upReq)
	}
	if got, _ := url.PathUnescape(upReq.Headers.Get("X-File-Name")); got != "договор.txt" {
		t.Fatalf("unexpected X-File-Name: %q", got)
	}

	// attach_file rejects directory
	dirArgs, _ := json.Marshal(map[string]string{"document_id": "doc-1", "path": tmpDir})
	if _, err := reg.Call(ctx, "attach_file", dirArgs); err == nil {
		t.Fatal("expected error when path is a directory")
	}

	// save_file writes bytes to dir
	outDir := filepath.Join(tmpDir, "out")
	saveArgs, _ := json.Marshal(map[string]string{"file_id": "file-1", "dir": outDir})
	rawSave, err := reg.Call(ctx, "save_file", saveArgs)
	if err != nil {
		t.Fatal(err)
	}
	var saveRes struct {
		Path     string `json:"path"`
		FileName string `json:"file_name"`
		Bytes    int64  `json:"bytes"`
	}
	if err := json.Unmarshal(rawSave, &saveRes); err != nil {
		t.Fatal(err)
	}
	savedBytes, err := os.ReadFile(saveRes.Path)
	if err != nil || string(savedBytes) != "hello-sed" || saveRes.FileName != "report.txt" || saveRes.Bytes != 9 {
		t.Fatalf("unexpected saved file: %+v, content=%q, err=%v", saveRes, string(savedBytes), err)
	}

	// decide_task outcome mapping
	outcomes := map[string]string{
		"согласовать":  "approve",
		"вернуть":      "return",
		"отказать":     "reject",
		"подписать":    "sign",
		"рекомендовать": "recommend",
		"исполнить":    "approve",
		"ознакомиться": "ack",
	}
	for ru, want := range outcomes {
		before := len(*reqs)
		args, _ := json.Marshal(map[string]string{"task_id": "task-1", "outcome": ru, "reason": "причина"})
		if _, err := reg.Call(ctx, "decide_task", args); err != nil {
			t.Fatalf("outcome %s failed: %v", ru, err)
		}
		var body struct {
			Decision string `json:"decision"`
		}
		_ = json.Unmarshal([]byte((*reqs)[before].Body), &body)
		if body.Decision != want {
			t.Fatalf("outcome %s mapped to %q, want %q", ru, body.Decision, want)
		}
	}
	if _, err := reg.Call(ctx, "decide_task", json.RawMessage(`{"task_id":"task-1","outcome":"удалить"}`)); err == nil {
		t.Fatal("expected error for unknown outcome")
	}

	// comment & link_documents
	if _, err := reg.Call(ctx, "comment", json.RawMessage(`{"document_id":"doc-1","body":"ок"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Call(ctx, "link_documents", json.RawMessage(`{"document_id":"doc-1","other_document_id":"doc-2","link_type":"ответ"}`)); err != nil {
		t.Fatal(err)
	}
}
