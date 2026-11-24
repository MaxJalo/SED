package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/Maxjalo/SED/services/api/internal/auth"
	"github.com/Maxjalo/SED/services/api/internal/orch"
	"github.com/Maxjalo/SED/services/api/internal/store"
)

type Server struct {
	store *store.Store
	auth  *auth.Client
	orch  *orch.Client
	mux   *http.ServeMux
}

func New(st *store.Store, authClient *auth.Client, orchClient *orch.Client) http.Handler {
	s := &Server{store: st, auth: authClient, orch: orchClient, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "sed-api"})
	})
	s.mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "stage": "documents"})
	})
	s.mux.HandleFunc("GET /v1/profile", s.authed(s.profile))
	s.mux.HandleFunc("GET /v1/directory", s.authed(s.directory))
	s.mux.HandleFunc("GET /v1/document-types", s.authed(s.types))
	s.mux.HandleFunc("GET /v1/subtypes", s.authed(s.subtypes))
	s.mux.HandleFunc("GET /v1/templates", s.authed(s.templates))
	s.mux.HandleFunc("GET /v1/deputies", s.authed(s.listDeputies))
	s.mux.HandleFunc("POST /v1/deputies", s.authed(s.createDeputy))
	s.mux.HandleFunc("POST /v1/deputies/{id}/deactivate", s.authed(s.deactivateDeputy))
	s.mux.HandleFunc("GET /v1/tags", s.authed(s.listTags))
	s.mux.HandleFunc("POST /v1/tags", s.authed(s.createTag))
	s.mux.HandleFunc("GET /v1/cases", s.authed(s.listCases))
	s.mux.HandleFunc("GET /v1/bookmarks", s.authed(s.listBookmarks))
	s.mux.HandleFunc("GET /v1/role-grants", s.authed(s.listRoleGrants))
	s.mux.HandleFunc("POST /v1/role-grants", s.authed(s.grantRole))
	s.mux.HandleFunc("PUT /v1/role-grants", s.authed(s.grantRole))
	s.mux.HandleFunc("DELETE /v1/role-grants/{id}", s.authed(s.revokeRole))
	s.mux.HandleFunc("GET /v1/route-templates", s.authed(s.listRouteTemplates))
	s.mux.HandleFunc("PUT /v1/route-templates/{id}", s.authed(s.saveRouteTemplate))
	s.mux.HandleFunc("GET /v1/calendar", s.authed(s.calendar))
	s.mux.HandleFunc("POST /v1/calendar/events", s.authed(s.createCalendarEvent))
	s.mux.HandleFunc("PATCH /v1/calendar/events/{id}", s.authed(s.updateCalendarEvent))
	s.mux.HandleFunc("DELETE /v1/calendar/events/{id}", s.authed(s.deleteCalendarEvent))
	s.mux.HandleFunc("GET /v1/meetings/{id}", s.authed(s.getMeeting))
	s.mux.HandleFunc("PATCH /v1/meetings/{id}", s.authed(s.updateCalendarEvent))
	s.mux.HandleFunc("POST /v1/meetings/{id}/protocol", s.authed(s.createMeetingProtocol))
	s.mux.HandleFunc("PUT /v1/meetings/{id}/protocol", s.authed(s.linkMeetingProtocol))
	s.mux.HandleFunc("GET /v1/meetings/{id}/grid", s.authed(s.getMeetingGrid))
	s.mux.HandleFunc("PUT /v1/meetings/{id}/grid", s.authed(s.saveMeetingGrid))
	s.mux.HandleFunc("GET /v1/documents/{id}/grid", s.authed(s.getDocumentGrid))
	s.mux.HandleFunc("PUT /v1/documents/{id}/grid", s.authed(s.saveDocumentGrid))
	s.mux.HandleFunc("GET /v1/documents", s.authed(s.listDocuments))
	s.mux.HandleFunc("POST /v1/documents", s.authed(s.createDocument))
	s.mux.HandleFunc("GET /v1/documents/{id}", s.authed(s.getDocument))
	s.mux.HandleFunc("PATCH /v1/documents/{id}", s.authed(s.patchDocument))
	s.mux.HandleFunc("POST /v1/documents/{id}/route", s.authed(s.route))
	s.mux.HandleFunc("PUT /v1/documents/{id}/route/steps", s.authed(s.editRouteSteps))
	s.mux.HandleFunc("POST /v1/documents/{id}/route/template", s.authed(s.applyRouteTemplate))
	s.mux.HandleFunc("POST /v1/documents/{id}/route/assignee", s.authed(s.reassignRoute))
	s.mux.HandleFunc("POST /v1/documents/{id}/route/participants", s.authed(s.addRouteParticipant))
	s.mux.HandleFunc("POST /v1/documents/{id}/remarks/ack", s.authed(s.ackRemarks))
	s.mux.HandleFunc("POST /v1/documents/{id}/register", s.authed(s.register))
	s.mux.HandleFunc("POST /v1/documents/{id}/reviews", s.authed(s.addReview))
	s.mux.HandleFunc("POST /v1/documents/{id}/comments", s.authed(s.addComment))
	s.mux.HandleFunc("POST /v1/documents/{id}/links", s.authed(s.addDocumentLink))
	s.mux.HandleFunc("DELETE /v1/documents/{id}", s.authed(s.deleteDocument))
	s.mux.HandleFunc("DELETE /v1/documents/{id}/links/{link_id}", s.authed(s.deleteDocumentLink))
	s.mux.HandleFunc("POST /v1/documents/{id}/tags", s.authed(s.attachTag))
	s.mux.HandleFunc("POST /v1/documents/{id}/paper", s.authed(s.recordPaper))
	s.mux.HandleFunc("POST /v1/documents/{id}/close-case", s.authed(s.closeToCase))
	s.mux.HandleFunc("POST /v1/documents/{id}/complete", s.authed(s.completeByAuthor))
	s.mux.HandleFunc("POST /v1/documents/{id}/grants", s.authed(s.grantAccess))
	s.mux.HandleFunc("POST /v1/documents/{id}/bookmark", s.authed(s.saveBookmark))
	s.mux.HandleFunc("DELETE /v1/documents/{id}/bookmark", s.authed(s.deleteBookmark))
	s.mux.HandleFunc("GET /v1/documents/{id}/approval-sheet.docx", s.authed(s.approvalSheetDocx))
	s.mux.HandleFunc("POST /v1/files", s.authed(s.upload))
	s.mux.HandleFunc("GET /v1/files/{id}", s.authed(s.download))
	s.mux.HandleFunc("GET /v1/contour-grants", s.authed(s.listContourGrants))
	s.mux.HandleFunc("PUT /v1/contour-grants", s.authed(s.setContourGrant))
	s.mux.HandleFunc("DELETE /v1/contour-grants/{id}", s.authed(s.revokeContourGrant))
	s.mux.HandleFunc("GET /v1/tasks", s.authed(s.tasks))
	s.mux.HandleFunc("GET /v1/tasks/summary", s.authed(s.tasksSummary))
	s.mux.HandleFunc("POST /v1/tasks/batch-decide", s.authed(s.batchDecide))
	s.mux.HandleFunc("POST /v1/tasks/{task_id}/accept", s.authed(s.acceptTask))
	s.mux.HandleFunc("POST /v1/tasks/{task_id}/confirm", s.authed(s.confirmTask))
	s.mux.HandleFunc("POST /v1/tasks/{task_id}/decide", s.authed(s.decide))
	s.mux.HandleFunc("POST /v1/tasks/{task_id}/delegate", s.authed(s.delegateTask))
	return s.mux
}

func (s *Server) authed(next func(http.ResponseWriter, *http.Request, store.Actor, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearer(r.Header.Get("Authorization"))
		if token == "" {
			writeErr(w, http.StatusUnauthorized, "нужен Bearer-токен")
			return
		}
		principal, err := s.auth.Me(r.Context(), token)
		if errors.Is(err, auth.ErrUnauthorized) {
			writeErr(w, http.StatusUnauthorized, err.Error())
			return
		}
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		extra, err := s.store.GrantedRoles(r.Context(), principal.UserID)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		principal.Roles = store.MergeRoles(principal.Roles, extra)
		contourLevels, err := s.store.UserContourGrants(r.Context(), principal.UserID, principal.OrganizationCode)
		if err != nil {
			writeErr(w, http.StatusBadGateway, err.Error())
			return
		}
		next(w, r, store.Actor{
			UserID:           principal.UserID,
			OrganizationCode: principal.OrganizationCode,
			DepartmentCode:   principal.DepartmentCode,
			Roles:            principal.Roles,
			ContourLevels:    contourLevels,
		}, token)
	}
}

func (s *Server) profile(w http.ResponseWriter, r *http.Request, actor store.Actor, token string) {
	principal, err := s.auth.Me(r.Context(), token)
	if err != nil {
		fail(w, err)
		return
	}
	principal.Roles = actor.Roles
	people, err := s.auth.Directory(r.Context(), token)
	if err != nil {
		fail(w, err)
		return
	}
	names := make(map[string]string, len(people))
	for _, p := range people {
		names[p.UserID] = p.FullName + " (" + p.Position + ")"
	}
	types, err := s.store.Types(r.Context(), actor)
	if err != nil {
		fail(w, err)
		return
	}
	caps := []map[string]any{
		{"action": "Создание служебных записок, заявок и исходящих", "allowed": actor.CanCreateType("служебная_записка"), "reason": "Доступно всем рабочим должностям кроме наблюдателя"},
		{"action": "Создание приказов, распоряжений, протоколов и входящих", "allowed": actor.CanCreateType("приказ"), "reason": "Роль руководителя или делопроизводителя"},
		{"action": "Согласование и вынесение решений по задачам", "allowed": true, "reason": "По назначенным шагам маршрута и поручениям"},
		{"action": "Подписание документов", "allowed": principal.Has("signer") || principal.Has("manager") || principal.HasPOA, "reason": "Полномочие подписанта или доверенность"},
		{"action": "Регистрация номеров и списание в дело", "allowed": principal.Has("clerk"), "reason": "Роль делопроизводителя / канцелярии"},
		{"action": "Пауза и возобновление маршрута", "allowed": principal.CanPause(), "reason": "Роль канцелярии или администратора"},
		{"action": "Просмотр всех реестров юрлица", "allowed": principal.CanReadOrg(), "reason": "Руководитель, канцелярия, контролёр или администратор"},
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":           principal,
		"manager_label":  names[principal.ManagerID],
		"top_manager":    names[principal.TopManagerID],
		"capabilities":   caps,
		"document_types": types,
		"contour_grants": actor.ContourLevels,
	})
}

func (s *Server) directory(w http.ResponseWriter, r *http.Request, _ store.Actor, token string) {
	people, err := s.auth.Directory(r.Context(), token)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": people})
}

func (s *Server) types(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	items, err := s.store.Types(r.Context(), actor)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listDocuments(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	q := r.URL.Query()
	page, err := s.store.ListDocuments(r.Context(), actor, limitOf(r), q.Get("cursor"), store.ListFilter{
		StreamKind:     q.Get("stream_kind"),
		TypeCode:       q.Get("type_code"),
		SubtypeCode:    q.Get("subtype_code"),
		BusinessDomain: q.Get("business_domain"),
		Status:         q.Get("status"),
		Bucket:         q.Get("bucket"),
		TagID:          q.Get("tag_id"),
		People:         q.Get("people"),
		Query:          q.Get("q"),
	})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) createDocument(w http.ResponseWriter, r *http.Request, actor store.Actor, token string) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		writeErr(w, http.StatusBadRequest, "заголовок Idempotency-Key обязателен")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		fail(w, err)
		return
	}
	var in store.CreateInput
	if err := json.Unmarshal(raw, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	colleagues, err := s.colleagues(r, token)
	if err != nil {
		fail(w, err)
		return
	}
	if in.AddresseeDepartment != "" && in.AddresseeUserID == "" {
		domain := strings.TrimSpace(in.BusinessDomain)
		if domain == "" {
			domain = "общий"
		}
		head := s.store.DepartmentHeadInDomain(r.Context(), actor.OrganizationCode, domain, colleagues, in.AddresseeDepartment)
		if head == "" {
			writeErr(w, http.StatusBadRequest, "у отдела нет руководителя с правом на контур, выберите сотрудника")
			return
		}
		in.AddresseeUserID = head
	}
	body, code, err := s.store.CreateDocument(r.Context(), actor, key, hashBody(raw), in, colleagues)
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

func (s *Server) getDocument(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	doc, err := s.store.GetDocument(r.Context(), actor, r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) deleteDocument(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	if err := s.store.DeleteDocument(r.Context(), actor, r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) patchDocument(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	var in store.PatchInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	doc, err := s.store.PatchDocument(r.Context(), actor, r.PathValue("id"), in)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) route(w http.ResponseWriter, r *http.Request, actor store.Actor, token string) {
	var body struct {
		Action string `json:"action"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	colleagues, err := s.colleagues(r, token)
	if err != nil {
		fail(w, err)
		return
	}
	doc, effects, err := s.store.RouteAction(r.Context(), actor, r.PathValue("id"), body.Action, body.Reason, colleagues)
	if err != nil {
		fail(w, err)
		return
	}
	s.dispatch(r, effects)
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) register(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	key := r.Header.Get("Idempotency-Key")
	if key == "" {
		writeErr(w, http.StatusBadRequest, "заголовок Idempotency-Key обязателен")
		return
	}
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	body, code, err := s.store.Register(r.Context(), actor, r.PathValue("id"), key, hashBody(raw))
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write(body)
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	name, err := url.PathUnescape(r.Header.Get("X-File-Name"))
	if err != nil || name == "" {
		writeErr(w, http.StatusBadRequest, "заголовок X-File-Name обязателен")
		return
	}
	doc, err := s.store.SaveFile(r.Context(), actor, r.Header.Get("X-Document-Id"), name, r.Body)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, doc)
}

func (s *Server) download(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	path, name, err := s.store.OpenFile(r.Context(), actor, r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Disposition", "attachment; filename=\""+strings.ReplaceAll(name, "\"", "")+"\"")
	http.ServeFile(w, r, path)
}

func (s *Server) tasks(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	q := r.URL.Query()
	page, err := s.store.ListTasks(r.Context(), actor, limitOf(r), q.Get("cursor"), q.Get("urgent") == "true", q.Get("tab"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) decide(w http.ResponseWriter, r *http.Request, actor store.Actor, token string) {
	var in store.Decision
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	colleagues, err := s.colleagues(r, token)
	if err != nil {
		fail(w, err)
		return
	}
	doc, effects, err := s.store.Decide(r.Context(), actor, r.PathValue("task_id"), in, colleagues)
	if err != nil {
		fail(w, err)
		return
	}
	s.dispatch(r, effects)
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) colleagues(r *http.Request, token string) ([]store.Colleague, error) {
	people, err := s.auth.Directory(r.Context(), token)
	if err != nil {
		return nil, err
	}
	grants, err := s.store.GrantedRolesByUser(r.Context())
	if err != nil {
		return nil, err
	}
	out := make([]store.Colleague, 0, len(people))
	for _, person := range people {
		out = append(out, store.Colleague{
			UserID:           person.UserID,
			FullName:         person.FullName,
			Position:         person.Position,
			DepartmentCode:   person.DepartmentCode,
			DepartmentName:   person.DepartmentName,
			OrganizationCode: person.OrganizationCode,
			ManagerID:        person.ManagerID,
			TopManagerID:     person.TopManagerID,
			Roles:            store.MergeRoles(person.Roles, grants[person.UserID]),
		})
	}
	return out, nil
}

func (s *Server) dispatch(r *http.Request, effects []store.Effect) {
	for _, effect := range effects {
		switch effect.Kind {
		case "notification":
			if err := s.orch.Notify(r.Context(), effect.AssigneeID, effect.Title, effect.DocumentID); err != nil {
				log.Printf("notification %s: %v", effect.DocumentID, err)
			}
		case "platform_task":
			id, err := s.orch.PlatformTask(r.Context(), effect.AuthorID, effect.AssigneeID, effect.Title, effect.DocumentID, effect.Due)
			if err != nil {
				log.Printf("platform task %s: %v", effect.DocumentID, err)
				continue
			}
			if err := s.store.RememberExternal(r.Context(), effect.DocumentID, "", effect.TaskID, id); err != nil {
				log.Printf("remember task %s: %v", effect.TaskID, err)
			}
		case "external_order":
			id, err := s.orch.ExternalOrder(r.Context(), effect.DocumentID, effect.RegNumber, effect.AuthorID)
			if err != nil {
				log.Printf("external order %s: %v", effect.DocumentID, err)
				continue
			}
			if err := s.store.RememberExternal(r.Context(), effect.DocumentID, id, "", ""); err != nil {
				log.Printf("remember order %s: %v", effect.DocumentID, err)
			}
		}
	}
}

func limitOf(r *http.Request) int {
	value, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	return value
}

func bearer(header string) string {
	if !strings.HasPrefix(header, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
}

func hashBody(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrForbidden):
		writeErr(w, http.StatusForbidden, err.Error())
	case errors.Is(err, store.ErrConflict), errors.Is(err, store.ErrInvalid):
		status := http.StatusConflict
		if errors.Is(err, store.ErrInvalid) {
			status = http.StatusBadRequest
		}
		writeErr(w, status, err.Error())
	case errors.Is(err, store.ErrTooLarge):
		writeErr(w, http.StatusRequestEntityTooLarge, err.Error())
	case errors.Is(err, auth.ErrUnauthorized):
		writeErr(w, http.StatusUnauthorized, err.Error())
	default:
		log.Printf("api error: %v", err)
		writeErr(w, http.StatusInternalServerError, "внутренняя ошибка")
	}
}

func writeErr(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
