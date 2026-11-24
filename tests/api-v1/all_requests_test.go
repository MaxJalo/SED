package apiv1_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAllV1Requests(t *testing.T) {
	base := strings.TrimRight(os.Getenv("SED_BASE_URL"), "/")
	if base == "" {
		base = "http://localhost:8000"
	}
	probe, err := http.Get(base + "/healthz")
	if err != nil {
		t.Skipf("стенд недоступен (%s): %v", base, err)
	}
	probe.Body.Close()
	if probe.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz: %d", probe.StatusCode)
	}

	httpClient := &http.Client{Timeout: 20 * time.Second}
	token := func(userID string) string {
		t.Helper()
		status, raw := call(t, httpClient, base, http.MethodPost, "/dev/token", "", map[string]string{"user_id": userID}, nil)
		if status != http.StatusOK {
			t.Fatalf("token %s: %d %s", userID, status, raw)
		}
		var issued struct {
			AccessToken string `json:"access_token"`
		}
		mustJSON(t, raw, &issued)
		if issued.AccessToken == "" {
			t.Fatal("пустой токен")
		}
		return issued.AccessToken
	}

	author := token("usr-org1-author")
	manager := token("usr-org1-head-sales")
	clerk := token("usr-org1-clerk")
	executor := token("usr-org1-executor")
	observer := token("usr-org1-observer")
	otherOrg := token("usr-org2-author")

	check := func(name, method, path, bearer string, body any, want int, headers map[string]string) []byte {
		t.Helper()
		status, raw := call(t, httpClient, base, method, path, bearer, body, headers)
		t.Run(name, func(t *testing.T) {
			if status != want {
				t.Fatalf("%s %s: ожидали %d, получили %d: %s", method, path, want, status, truncate(raw))
			}
		})
		if status != want {
			t.Fatalf("%s: %s %s ожидали %d, получили %d: %s", name, method, path, want, status, truncate(raw))
		}
		return raw
	}

	check("health", http.MethodGet, "/v1/health", "", nil, http.StatusOK, nil)
	check("profile без токена", http.MethodGet, "/v1/profile", "", nil, http.StatusUnauthorized, nil)
	check("profile автора", http.MethodGet, "/v1/profile", author, nil, http.StatusOK, nil)
	check("directory", http.MethodGet, "/v1/directory", author, nil, http.StatusOK, nil)
	typesRaw := check("виды документов", http.MethodGet, "/v1/document-types", author, nil, http.StatusOK, nil)
	var typesPage struct {
		Items []struct {
			Code string `json:"code"`
		} `json:"items"`
	}
	mustJSON(t, typesRaw, &typesPage)
	if len(typesPage.Items) != 15 {
		t.Fatalf("видов документов: %d, ожидали 15", len(typesPage.Items))
	}
	subsRaw := check("подвиды", http.MethodGet, "/v1/subtypes", author, nil, http.StatusOK, nil)
	var subsPage struct {
		Items []struct {
			Code string `json:"code"`
		} `json:"items"`
	}
	mustJSON(t, subsRaw, &subsPage)
	if len(subsPage.Items) < 12 {
		t.Fatalf("подвидов: %d, ожидали минимум 12", len(subsPage.Items))
	}
	check("шаблоны", http.MethodGet, "/v1/templates", author, nil, http.StatusOK, nil)
	check("замещения", http.MethodGet, "/v1/deputies", manager, nil, http.StatusOK, nil)
	check("теги", http.MethodGet, "/v1/tags", author, nil, http.StatusOK, nil)
	casesRaw := check("дела", http.MethodGet, "/v1/cases", clerk, nil, http.StatusOK, nil)
	var casesPage struct {
		Items []struct {
			ID string `json:"id"`
		} `json:"items"`
	}
	mustJSON(t, casesRaw, &casesPage)
	if len(casesPage.Items) == 0 {
		t.Fatal("номенклатура дел пуста")
	}
	check("закладки", http.MethodGet, "/v1/bookmarks", author, nil, http.StatusOK, nil)
	check("реестр", http.MethodGet, "/v1/documents?limit=2&stream_kind=внутренний", author, nil, http.StatusOK, nil)
	check("сводка задач", http.MethodGet, "/v1/tasks/summary", author, nil, http.StatusOK, nil)
	for _, tab := range []string{"to_me", "execution", "pending_confirm", "on_control", "from_me"} {
		check("задачи "+tab, http.MethodGet, "/v1/tasks?limit=5&tab="+tab, author, nil, http.StatusOK, nil)
	}

	check("создание без ключа", http.MethodPost, "/v1/documents", author, map[string]any{
		"type_code": "служебная_записка", "title": "без ключа", "body": "текст", "addressee_user_id": "usr-org1-executor",
	}, http.StatusBadRequest, nil)

	idem := fmt.Sprintf("all-requests-%d", time.Now().UnixNano())
	createBody := map[string]any{
		"type_code":         "служебная_записка",
		"title":             "Контрактный прогон всех запросов",
		"body":              "Проверка полного каталога /v1",
		"importance":        "обычная",
		"addressee_user_id": "usr-org1-executor",
		"business_domain":   "продажи",
	}
	createdRaw := check("создание документа", http.MethodPost, "/v1/documents", author, createBody, http.StatusCreated, map[string]string{"Idempotency-Key": idem})
	doc := decodeDoc(t, createdRaw)
	replayRaw := check("повтор Idempotency-Key", http.MethodPost, "/v1/documents", author, createBody, http.StatusCreated, map[string]string{"Idempotency-Key": idem})
	replay := decodeDoc(t, replayRaw)
	if replay.ID != doc.ID {
		t.Fatalf("идемпотентность вернула другой id: %s и %s", doc.ID, replay.ID)
	}

	check("карточка", http.MethodGet, "/v1/documents/"+doc.ID, author, nil, http.StatusOK, nil)
	check("чужое юрлицо", http.MethodGet, "/v1/documents/"+doc.ID, otherOrg, nil, http.StatusForbidden, nil)
	check("наблюдатель без доступа", http.MethodGet, "/v1/documents/"+doc.ID, observer, nil, http.StatusForbidden, nil)
	check("правка заголовка", http.MethodPatch, "/v1/documents/"+doc.ID, author, map[string]any{"title": "Контрактный прогон (правка)"}, http.StatusOK, nil)
	check("дозаполнение пустого поля", http.MethodPatch, "/v1/documents/"+doc.ID, author, map[string]any{"counterparty_inn": "7700000000"}, http.StatusOK, nil)

	card := decodeDoc(t, check("карточка до конструктора", http.MethodGet, "/v1/documents/"+doc.ID, author, nil, http.StatusOK, nil))
	steps := make([]map[string]any, 0)
	for _, step := range card.RoutePath {
		if step.IsMandatory || step.IsOrderLocked {
			steps = append(steps, map[string]any{
				"step_id": step.StepID, "order_no": step.OrderNo, "kind": step.Kind,
				"due_days": 1, "assignee_user_id": "usr-org1-head-sales", "is_blocking": step.IsBlocking,
			})
		}
	}
	steps = append(steps,
		map[string]any{"order_no": 1, "kind": "approval", "due_days": 1, "assignee_user_id": "usr-org1-head-sales", "is_blocking": true},
		map[string]any{"order_no": 2, "kind": "execution", "due_days": 1, "assignee_user_id": "usr-org1-executor", "is_blocking": true},
	)
	built := decodeDoc(t, check("конструктор шагов", http.MethodPut, "/v1/documents/"+doc.ID+"/route/steps", author, map[string]any{"steps": steps}, http.StatusOK, nil))
	if built.RouteStarted {
		t.Fatal("конструктор не должен запускать маршрут")
	}

	asUser := map[string]string{
		"usr-org1-author":      author,
		"usr-org1-head-sales":  manager,
		"usr-org1-clerk":       clerk,
		"usr-org1-executor":    executor,
		"usr-org1-observer":    observer,
		"usr-org1-lawyer":      "",
		"usr-org2-author":      otherOrg,
	}
	bearerOf := func(userID string) string {
		t.Helper()
		if tokenValue := asUser[userID]; tokenValue != "" {
			return tokenValue
		}
		issued := token(userID)
		asUser[userID] = issued
		return issued
	}

	started := decodeDoc(t, check("старт маршрута", http.MethodPost, "/v1/documents/"+doc.ID+"/route", author, map[string]any{"action": "start"}, http.StatusOK, nil))
	approval := currentTask(t, started, "approval")
	approver := bearerOf(approval.AssigneeID)
	check("принять к исполнению", http.MethodPost, "/v1/tasks/"+approval.TaskID+"/accept", approver, map[string]any{}, http.StatusOK, nil)
	check("возврат на доработку", http.MethodPost, "/v1/tasks/"+approval.TaskID+"/decide", approver, map[string]any{"decision": "return", "reason": "Уточнить формулировку"}, http.StatusOK, nil)
	check("учёт замечаний", http.MethodPost, "/v1/documents/"+doc.ID+"/remarks/ack", author, map[string]any{}, http.StatusOK, nil)
	restarted := decodeDoc(t, check("повторный старт", http.MethodPost, "/v1/documents/"+doc.ID+"/route", author, map[string]any{"action": "start"}, http.StatusOK, nil))
	approval = currentTask(t, restarted, "approval")
	approver = bearerOf(approval.AssigneeID)
	check("делегирование", http.MethodPost, "/v1/tasks/"+approval.TaskID+"/delegate", approver, map[string]any{"to_user_id": "usr-org1-lawyer"}, http.StatusOK, nil)
	check("решение согласующего", http.MethodPost, "/v1/tasks/"+approval.TaskID+"/decide", bearerOf("usr-org1-lawyer"), map[string]any{"decision": "approve"}, http.StatusOK, nil)

	afterApproval := decodeDoc(t, check("карточка на исполнении", http.MethodGet, "/v1/documents/"+doc.ID, executor, nil, http.StatusOK, nil))
	execTask := currentTask(t, afterApproval, "execution")
	check("принять исполнение", http.MethodPost, "/v1/tasks/"+execTask.TaskID+"/accept", executor, map[string]any{}, http.StatusOK, nil)
	check("сдать отчёт", http.MethodPost, "/v1/tasks/"+execTask.TaskID+"/decide", executor, map[string]any{"decision": "approve"}, http.StatusOK, nil)
	check("вернуть отчёт", http.MethodPost, "/v1/tasks/"+execTask.TaskID+"/confirm", author, map[string]any{"decision": "rework", "comment": "Нужна смета"}, http.StatusOK, nil)
	check("сдать отчёт повторно", http.MethodPost, "/v1/tasks/"+execTask.TaskID+"/decide", executor, map[string]any{"decision": "approve"}, http.StatusOK, nil)
	check("подтвердить исполнение", http.MethodPost, "/v1/tasks/"+execTask.TaskID+"/confirm", author, map[string]any{"decision": "accept", "comment": "Принято"}, http.StatusOK, nil)

	check("досыл ознакомления", http.MethodPost, "/v1/documents/"+doc.ID+"/route/participants", author, map[string]any{"kind": "acknowledgment", "user_id": "usr-org1-clerk"}, http.StatusCreated, nil)
	withAck := decodeDoc(t, check("карточка после досыла", http.MethodGet, "/v1/documents/"+doc.ID, clerk, nil, http.StatusOK, nil))
	ack := currentTask(t, withAck, "acknowledgment")
	check("ознакомление", http.MethodPost, "/v1/tasks/"+ack.TaskID+"/decide", clerk, map[string]any{"decision": "ack"}, http.StatusOK, nil)

	check("комментарий", http.MethodPost, "/v1/documents/"+doc.ID+"/comments", author, map[string]any{"body": "Обсуждение контрактного прогона"}, http.StatusCreated, nil)
	check("рецензия", http.MethodPost, "/v1/documents/"+doc.ID+"/reviews", author, map[string]any{"target_user_id": "usr-org1-lawyer", "body": "Боковая рецензия без SLA"}, http.StatusCreated, nil)

	tagRaw := check("создать тег", http.MethodPost, "/v1/tags", author, map[string]any{"scope": "dept", "name": fmt.Sprintf("прогон-%d", time.Now().UnixNano()%100000)}, http.StatusCreated, nil)
	var tag struct {
		ID string `json:"id"`
	}
	mustJSON(t, tagRaw, &tag)
	check("назначить тег", http.MethodPost, "/v1/documents/"+doc.ID+"/tags", author, map[string]any{"tag_id": tag.ID}, http.StatusOK, nil)
	check("фильтр по тегу", http.MethodGet, "/v1/documents?tag_id="+tag.ID, author, nil, http.StatusOK, nil)
	check("бумага", http.MethodPost, "/v1/documents/"+doc.ID+"/paper", clerk, map[string]any{"holder_user_id": "usr-org1-clerk", "note": "Папка контрактного прогона"}, http.StatusCreated, nil)
	check("временный доступ", http.MethodPost, "/v1/documents/"+doc.ID+"/grants", author, map[string]any{"user_id": "usr-org1-observer", "days": 3}, http.StatusCreated, nil)
	check("наблюдатель с доступом", http.MethodGet, "/v1/documents/"+doc.ID, observer, nil, http.StatusOK, nil)
	check("закладка", http.MethodPost, "/v1/documents/"+doc.ID+"/bookmark", author, map[string]any{"note_text": "Личная заметка прогона"}, http.StatusOK, nil)
	check("список закладок", http.MethodGet, "/v1/bookmarks", author, nil, http.StatusOK, nil)
	check("снять закладку", http.MethodDelete, "/v1/documents/"+doc.ID+"/bookmark", author, nil, http.StatusOK, nil)

	check("пауза", http.MethodPost, "/v1/documents/"+doc.ID+"/route", clerk, map[string]any{"action": "pause"}, http.StatusOK, nil)
	check("возобновление", http.MethodPost, "/v1/documents/"+doc.ID+"/route", clerk, map[string]any{"action": "resume"}, http.StatusOK, nil)

	fileRaw := check("загрузка файла", http.MethodPost, "/v1/files", author, []byte("contract-bytes"), http.StatusCreated, map[string]string{
		"X-Document-Id": doc.ID,
		"X-File-Name":   "contract.txt",
		"Content-Type":  "application/octet-stream",
	})
	withFile := decodeDoc(t, fileRaw)
	if len(withFile.Files) == 0 {
		t.Fatal("файл не появился в карточке")
	}
	check("скачивание файла", http.MethodGet, "/v1/files/"+withFile.Files[0].ID, author, nil, http.StatusOK, nil)

	sheet := check("лист согласования", http.MethodGet, "/v1/documents/"+doc.ID+"/approval-sheet.docx", author, nil, http.StatusOK, nil)
	if len(sheet) < 4 || string(sheet[:2]) != "PK" {
		t.Fatalf("ожидали ZIP/DOCX, получили %q", truncate(sheet))
	}

	check("регистрация", http.MethodPost, "/v1/documents/"+doc.ID+"/register", clerk, map[string]any{}, http.StatusOK, map[string]string{"Idempotency-Key": idem + "-reg"})
	check("готов в дело", http.MethodPost, "/v1/documents/"+doc.ID+"/complete", author, map[string]any{}, http.StatusOK, nil)
	closed := decodeDoc(t, check("списание в дело", http.MethodPost, "/v1/documents/"+doc.ID+"/close-case", clerk, map[string]any{"case_id": casesPage.Items[0].ID}, http.StatusOK, nil))
	if closed.Status != "Закрыт (в деле)" {
		t.Fatalf("статус после списания: %s", closed.Status)
	}
	check("правка закрытого", http.MethodPatch, "/v1/documents/"+doc.ID, author, map[string]any{"title": "нельзя"}, http.StatusForbidden, nil)

	today := time.Now().Format("2006-01-02")
	depRaw := check("создать замещение", http.MethodPost, "/v1/deputies", clerk, map[string]any{
		"user_id": "usr-org1-clerk", "deputy_user_id": "usr-org1-executor", "valid_from": today, "valid_to": today,
	}, http.StatusCreated, nil)
	var dep struct {
		ID string `json:"id"`
	}
	mustJSON(t, depRaw, &dep)
	check("отключить замещение", http.MethodPost, "/v1/deputies/"+dep.ID+"/deactivate", clerk, map[string]any{}, http.StatusOK, nil)

	batchKey := idem + "-batch"
	batchDoc := decodeDoc(t, check("документ для пакета", http.MethodPost, "/v1/documents", author, map[string]any{
		"type_code": "поручение", "title": "Пакетное решение", "body": "Один шаг согласования",
	}, http.StatusCreated, map[string]string{"Idempotency-Key": batchKey}))
	check("шаги пакетного документа", http.MethodPut, "/v1/documents/"+batchDoc.ID+"/route/steps", author, map[string]any{
		"steps": []map[string]any{
			{"order_no": 1, "kind": "approval", "due_days": 1, "assignee_user_id": "usr-org1-author", "is_blocking": true},
		},
	}, http.StatusOK, nil)
	batchStarted := decodeDoc(t, check("старт пакетного", http.MethodPost, "/v1/documents/"+batchDoc.ID+"/route", author, map[string]any{"action": "start"}, http.StatusOK, nil))
	batchTask := currentTask(t, batchStarted, "approval")
	check("пакетное согласование", http.MethodPost, "/v1/tasks/batch-decide", author, map[string]any{
		"task_ids": []string{batchTask.TaskID}, "decision": "approve",
	}, http.StatusOK, nil)

	check("срез продаж", http.MethodGet, "/v1/documents?business_domain=продажи&limit=5", author, nil, http.StatusOK, nil)
}

type liveDoc struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	RouteStarted bool   `json:"route_started"`
	RoutePath    []struct {
		StepID         string `json:"step_id"`
		TaskID         string `json:"task_id"`
		Kind           string `json:"kind"`
		StageState     string `json:"stage_state"`
		IsMandatory    bool   `json:"is_mandatory"`
		IsOrderLocked  bool   `json:"is_order_locked"`
		IsBlocking     bool   `json:"is_blocking"`
		OrderNo        int    `json:"order_no"`
		AssigneeUserID string `json:"assignee_user_id"`
	} `json:"route_path"`
	Files []struct {
		ID string `json:"id"`
	} `json:"files"`
}

func decodeDoc(t *testing.T, raw []byte) liveDoc {
	t.Helper()
	var doc liveDoc
	mustJSON(t, raw, &doc)
	if doc.ID == "" {
		t.Fatalf("пустой документ: %s", truncate(raw))
	}
	return doc
}

type openTask struct {
	TaskID     string
	Kind       string
	AssigneeID string
}

func currentTask(t *testing.T, doc liveDoc, kind string) openTask {
	t.Helper()
	for _, step := range doc.RoutePath {
		if step.StageState == "текущий" && (kind == "" || step.Kind == kind) && step.TaskID != "" {
			return openTask{TaskID: step.TaskID, Kind: step.Kind, AssigneeID: step.AssigneeUserID}
		}
	}
	t.Fatalf("нет текущей задачи %s в %s", kind, doc.ID)
	return openTask{}
}

func call(t *testing.T, client *http.Client, base, method, path, bearer string, body any, headers map[string]string) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		switch typed := body.(type) {
		case []byte:
			reader = bytes.NewReader(typed)
		default:
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			reader = bytes.NewReader(raw)
		}
	}
	req, err := http.NewRequest(method, base+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if body != nil {
		if _, ok := body.([]byte); ok {
			if headers == nil || headers["Content-Type"] == "" {
				req.Header.Set("Content-Type", "application/octet-stream")
			}
		} else {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, raw
}

func mustJSON(t *testing.T, raw []byte, dest any) {
	t.Helper()
	if err := json.Unmarshal(raw, dest); err != nil {
		t.Fatalf("json: %v: %s", err, truncate(raw))
	}
}

func truncate(raw []byte) string {
	text := string(raw)
	if len(text) > 400 {
		return text[:400]
	}
	return text
}
