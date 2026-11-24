package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type ToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

type ToolHandler func(ctx context.Context, args json.RawMessage) (json.RawMessage, error)

type Registry struct {
	client   *APIClient
	defs     []ToolDef
	handlers map[string]ToolHandler
}

var outcomeMap = map[string]string{
	"согласовать":  "approve",
	"вернуть":      "return",
	"отказать":     "reject",
	"подписать":    "sign",
	"рекомендовать": "recommend",
	"исполнить":    "approve",
	"ознакомиться": "ack",
	"approve":      "approve",
	"return":       "return",
	"reject":       "reject",
	"sign":         "sign",
	"recommend":    "recommend",
	"ack":          "ack",
}

func NewRegistry(client *APIClient) *Registry {
	r := &Registry{
		client:   client,
		handlers: map[string]ToolHandler{},
	}
	r.registerAll()
	return r
}

func (r *Registry) List() []ToolDef {
	return r.defs
}

func (r *Registry) Call(ctx context.Context, name string, args json.RawMessage) (json.RawMessage, error) {
	h, ok := r.handlers[name]
	if !ok {
		return nil, fmt.Errorf("неизвестный инструмент: %s", name)
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	return h(ctx, args)
}

func (r *Registry) add(def ToolDef, h ToolHandler) {
	r.defs = append(r.defs, def)
	r.handlers[def.Name] = h
}

func (r *Registry) registerAll() {
	r.add(ToolDef{
		Name:        "whoami",
		Description: "Текущий сотрудник, его роли, права на контуры и доступные виды документов (GET /v1/profile).",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}, func(ctx context.Context, _ json.RawMessage) (json.RawMessage, error) {
		return r.client.doJSON(ctx, http.MethodGet, "/v1/profile", nil, nil)
	})

	r.add(ToolDef{
		Name:        "find",
		Description: "Поиск документов, своих задач или сотрудников организации.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"kind": map[string]any{
					"type":        "string",
					"enum":        []string{"documents", "tasks", "people"},
					"default":     "documents",
					"description": "Что искать: documents (по умолчанию), tasks или people",
				},
				"q": map[string]any{
					"type":        "string",
					"description": "Строка поиска для документов или ФИО/отдела/должности сотрудников",
				},
				"business_domain": map[string]any{
					"type":        "string",
					"enum":        []string{"общий", "продажи", "закупки", "производство", "кб", "качество"},
					"description": "Фильтр по контуру (для documents)",
				},
				"status": map[string]any{
					"type":        "string",
					"description": "Статус документа (для documents)",
				},
				"bucket": map[string]any{
					"type":        "string",
					"description": "Срез реестра, например my или on_control (для documents)",
				},
				"tab": map[string]any{
					"type":        "string",
					"enum":        []string{"to_me", "execution", "pending_confirm", "on_control", "from_me"},
					"default":     "to_me",
					"description": "Вкладка задач (для tasks)",
				},
				"urgent": map[string]any{
					"type":        "boolean",
					"description": "Только срочные задачи (для tasks)",
				},
				"limit": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"maximum":     100,
					"description": "Максимум записей (по умолчанию 50)",
				},
			},
		},
	}, r.handleFind)

	r.add(ToolDef{
		Name:        "open_document",
		Description: "Открыть карточку документа целиком (GET /v1/documents/{id}).",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"document_id"},
			"properties": map[string]any{
				"document_id": map[string]any{"type": "string", "description": "UUID документа"},
			},
		},
	}, func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
		var in struct {
			DocumentID string `json:"document_id"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.DocumentID) == "" {
			return nil, errors.New("нужен document_id")
		}
		return r.client.doJSON(ctx, http.MethodGet, "/v1/documents/"+url.PathEscape(in.DocumentID), nil, nil)
	})

	r.add(ToolDef{
		Name:        "create_draft",
		Description: "Создать черновик документа (POST /v1/documents) с автоматическим ключом идемпотентности.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"type_code", "title"},
			"properties": map[string]any{
				"type_code":                 map[string]any{"type": "string", "description": "Код вида документа"},
				"subtype_code":              map[string]any{"type": "string", "description": "Код подвида"},
				"business_domain":           map[string]any{"type": "string", "enum": []string{"общий", "продажи", "закупки", "производство", "кб", "качество"}},
				"title":                     map[string]any{"type": "string", "description": "Заголовок документа"},
				"body":                      map[string]any{"type": "string", "description": "Текст документа"},
				"importance":                map[string]any{"type": "string", "enum": []string{"обычная", "повышенная", "немедленная"}},
				"customer_contract":         map[string]any{"type": "boolean"},
				"addressee_user_id":         map[string]any{"type": "string"},
				"addressee_department_code": map[string]any{"type": "string"},
				"counterparty_name":         map[string]any{"type": "string"},
				"counterparty_inn":          map[string]any{"type": "string"},
				"service_name":              map[string]any{"type": "string"},
				"due_date":                  map[string]any{"type": "string", "description": "Срок ГГГГ-ММ-ДД"},
				"amount":                    map[string]any{"type": "number"},
				"source_link": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"document_id": map[string]any{"type": "string"},
						"link_type":   map[string]any{"type": "string"},
						"place":       map[string]any{"type": "string"},
					},
				},
			},
		},
	}, r.handleCreateDraft)

	r.add(ToolDef{
		Name:        "start_route",
		Description: "Запустить маршрут уже созданного черновика (POST /v1/documents/{id}/route с action=start).",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"document_id"},
			"properties": map[string]any{
				"document_id": map[string]any{"type": "string", "description": "UUID документа"},
			},
		},
	}, func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
		var in struct {
			DocumentID string `json:"document_id"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.DocumentID) == "" {
			return nil, errors.New("нужен document_id")
		}
		return r.client.doJSON(ctx, http.MethodPost, "/v1/documents/"+url.PathEscape(in.DocumentID)+"/route", map[string]string{"action": "start"}, nil)
	})

	r.add(ToolDef{
		Name:        "attach_file",
		Description: "Приложить локальный файл по абсолютному пути к документу (POST /v1/files).",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"document_id", "path"},
			"properties": map[string]any{
				"document_id": map[string]any{"type": "string", "description": "UUID документа"},
				"path":        map[string]any{"type": "string", "description": "Путь к обычному файлу на диске"},
				"file_name":   map[string]any{"type": "string", "description": "Необязательное имя файла в карточке"},
			},
		},
	}, func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
		var in struct {
			DocumentID string `json:"document_id"`
			Path       string `json:"path"`
			FileName   string `json:"file_name"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.DocumentID) == "" || strings.TrimSpace(in.Path) == "" {
			return nil, errors.New("нужны document_id и path")
		}
		return r.client.uploadFile(ctx, in.DocumentID, in.Path, in.FileName)
	})

	r.add(ToolDef{
		Name:        "save_file",
		Description: "Скачать вложение по file_id в указанную папку на диске (GET /v1/files/{id}).",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"file_id", "dir"},
			"properties": map[string]any{
				"file_id":   map[string]any{"type": "string", "description": "UUID файла из карточки документа"},
				"dir":       map[string]any{"type": "string", "description": "Папка для сохранения"},
				"file_name": map[string]any{"type": "string", "description": "Необязательное имя файла"},
			},
		},
	}, func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
		var in struct {
			FileID   string `json:"file_id"`
			Dir      string `json:"dir"`
			FileName string `json:"file_name"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.FileID) == "" || strings.TrimSpace(in.Dir) == "" {
			return nil, errors.New("нужны file_id и dir")
		}
		dest, n, name, err := r.client.downloadFile(ctx, in.FileID, in.Dir, in.FileName)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]any{
			"path":      dest,
			"file_name": name,
			"bytes":     n,
		})
	})

	r.add(ToolDef{
		Name:        "decide_task",
		Description: "Вынести решение по своей задаче (POST /v1/tasks/{id}/decide).",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"task_id", "outcome"},
			"properties": map[string]any{
				"task_id":             map[string]any{"type": "string", "description": "UUID задачи"},
				"outcome":             map[string]any{"type": "string", "enum": []string{"согласовать", "вернуть", "отказать", "подписать", "рекомендовать", "исполнить", "ознакомиться"}},
				"reason":              map[string]any{"type": "string", "description": "Причина (обязательна для возврата и отказа)"},
				"recommendation_text": map[string]any{"type": "string", "description": "Текст рекомендации (для шага рассмотрения)"},
			},
		},
	}, r.handleDecideTask)

	r.add(ToolDef{
		Name:        "comment",
		Description: "Добавить комментарий к документу (POST /v1/documents/{id}/comments).",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"document_id", "body"},
			"properties": map[string]any{
				"document_id": map[string]any{"type": "string", "description": "UUID документа"},
				"body":        map[string]any{"type": "string", "description": "Текст комментария"},
			},
		},
	}, func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
		var in struct {
			DocumentID string `json:"document_id"`
			Body       string `json:"body"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.DocumentID) == "" || strings.TrimSpace(in.Body) == "" {
			return nil, errors.New("нужны document_id и body")
		}
		return r.client.doJSON(ctx, http.MethodPost, "/v1/documents/"+url.PathEscape(in.DocumentID)+"/comments", map[string]string{"body": in.Body}, nil)
	})

	r.add(ToolDef{
		Name:        "link_documents",
		Description: "Связать два документа (POST /v1/documents/{id}/links).",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"document_id", "other_document_id", "link_type"},
			"properties": map[string]any{
				"document_id":       map[string]any{"type": "string", "description": "UUID исходного документа"},
				"other_document_id": map[string]any{"type": "string", "description": "UUID второго документа"},
				"link_type":         map[string]any{"type": "string", "enum": []string{"ответ", "основание", "допсоглашение", "приложение", "замена", "пункт_протокола"}},
				"place":             map[string]any{"type": "string", "enum": []string{"after", "before"}, "default": "after"},
			},
		},
	}, func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
		var in struct {
			DocumentID      string `json:"document_id"`
			OtherDocumentID string `json:"other_document_id"`
			LinkType        string `json:"link_type"`
			Place           string `json:"place"`
		}
		if err := json.Unmarshal(args, &in); err != nil {
			return nil, err
		}
		if strings.TrimSpace(in.DocumentID) == "" || strings.TrimSpace(in.OtherDocumentID) == "" || strings.TrimSpace(in.LinkType) == "" {
			return nil, errors.New("нужны document_id, other_document_id и link_type")
		}
		if in.Place == "" {
			in.Place = "after"
		}
		return r.client.doJSON(ctx, http.MethodPost, "/v1/documents/"+url.PathEscape(in.DocumentID)+"/links", map[string]string{
			"other_document_id": in.OtherDocumentID,
			"link_type":         in.LinkType,
			"place":             in.Place,
		}, nil)
	})
}

func (r *Registry) handleFind(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var in struct {
		Kind           string `json:"kind"`
		Q              string `json:"q"`
		BusinessDomain string `json:"business_domain"`
		Status         string `json:"status"`
		Bucket         string `json:"bucket"`
		Tab            string `json:"tab"`
		Urgent         bool   `json:"urgent"`
		Limit          int    `json:"limit"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	if in.Kind == "" {
		in.Kind = "documents"
	}
	if in.Limit <= 0 {
		in.Limit = 50
	}
	switch in.Kind {
	case "documents":
		q := url.Values{}
		q.Set("limit", strconv.Itoa(in.Limit))
		if in.Q != "" {
			q.Set("q", in.Q)
		}
		if in.BusinessDomain != "" {
			q.Set("business_domain", in.BusinessDomain)
		}
		if in.Status != "" {
			q.Set("status", in.Status)
		}
		if in.Bucket != "" {
			q.Set("bucket", in.Bucket)
		}
		return r.client.doJSON(ctx, http.MethodGet, "/v1/documents?"+q.Encode(), nil, nil)
	case "tasks":
		q := url.Values{}
		q.Set("limit", strconv.Itoa(in.Limit))
		tab := in.Tab
		if tab == "" {
			tab = "to_me"
		}
		q.Set("tab", tab)
		if in.Urgent {
			q.Set("urgent", "true")
		}
		return r.client.doJSON(ctx, http.MethodGet, "/v1/tasks?"+q.Encode(), nil, nil)
	case "people":
		raw, err := r.client.doJSON(ctx, http.MethodGet, "/v1/directory", nil, nil)
		if err != nil {
			return nil, err
		}
		var dir struct {
			Items []struct {
				UserID         string `json:"user_id"`
				FullName       string `json:"full_name"`
				Position       string `json:"position"`
				DepartmentCode string `json:"department_code"`
				DepartmentName string `json:"department_name"`
			} `json:"items"`
		}
		if err := json.Unmarshal(raw, &dir); err != nil {
			return raw, nil
		}
		needle := strings.ToLower(strings.TrimSpace(in.Q))
		filtered := dir.Items[:0]
		for _, item := range dir.Items {
			if needle == "" ||
				strings.Contains(strings.ToLower(item.FullName), needle) ||
				strings.Contains(strings.ToLower(item.Position), needle) ||
				strings.Contains(strings.ToLower(item.DepartmentName), needle) ||
				strings.Contains(strings.ToLower(item.UserID), needle) {
				filtered = append(filtered, item)
				if len(filtered) >= in.Limit {
					break
				}
			}
		}
		return json.Marshal(map[string]any{"items": filtered})
	default:
		return nil, fmt.Errorf("неизвестный kind: %s", in.Kind)
	}
}

func (r *Registry) handleCreateDraft(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var in struct {
		TypeCode            string         `json:"type_code"`
		SubtypeCode         string         `json:"subtype_code,omitempty"`
		BusinessDomain      string         `json:"business_domain,omitempty"`
		Title               string         `json:"title"`
		Body                string         `json:"body,omitempty"`
		Importance          string         `json:"importance,omitempty"`
		CustomerContract    bool           `json:"customer_contract,omitempty"`
		AddresseeUserID     string         `json:"addressee_user_id,omitempty"`
		AddresseeDepartment string         `json:"addressee_department_code,omitempty"`
		CounterpartyName    string         `json:"counterparty_name,omitempty"`
		CounterpartyINN     string         `json:"counterparty_inn,omitempty"`
		ServiceName         string         `json:"service_name,omitempty"`
		DueDate             string         `json:"due_date,omitempty"`
		Amount              *float64       `json:"amount,omitempty"`
		SourceLink          map[string]any `json:"source_link,omitempty"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.TypeCode) == "" || strings.TrimSpace(in.Title) == "" {
		return nil, errors.New("нужны type_code и title")
	}
	key, err := idempotencyKey(in)
	if err != nil {
		return nil, err
	}
	return r.client.doJSON(ctx, http.MethodPost, "/v1/documents", in, map[string]string{
		"Idempotency-Key": key,
	})
}

func (r *Registry) handleDecideTask(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
	var in struct {
		TaskID             string `json:"task_id"`
		Outcome            string `json:"outcome"`
		Reason             string `json:"reason"`
		RecommendationText string `json:"recommendation_text"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.TaskID) == "" {
		return nil, errors.New("нужен task_id")
	}
	code, ok := outcomeMap[strings.ToLower(strings.TrimSpace(in.Outcome))]
	if !ok {
		return nil, fmt.Errorf("неизвестный исход %q", in.Outcome)
	}
	return r.client.doJSON(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(in.TaskID)+"/decide", map[string]string{
		"decision":            code,
		"reason":              in.Reason,
		"recommendation_text": in.RecommendationText,
	}, nil)
}
