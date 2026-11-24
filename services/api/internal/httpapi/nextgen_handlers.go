package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/Maxjalo/SED/services/api/internal/store"
)

func (s *Server) subtypes(w http.ResponseWriter, r *http.Request, _ store.Actor, _ string) {
	items, err := s.store.ListSubtypes(r.Context(), r.URL.Query().Get("type_code"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) tasksSummary(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	sum, err := s.store.TasksSummary(r.Context(), actor)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

func (s *Server) acceptTask(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	doc, err := s.store.AcceptTask(r.Context(), actor, r.PathValue("task_id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) confirmTask(w http.ResponseWriter, r *http.Request, actor store.Actor, token string) {
	var body struct {
		Decision string `json:"decision"`
		Comment  string `json:"comment"`
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
	doc, effects, err := s.store.ConfirmExecutionTask(r.Context(), actor, colleagues, r.PathValue("task_id"), body.Decision, body.Comment)
	if err != nil {
		fail(w, err)
		return
	}
	s.dispatch(r, effects)
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) batchDecide(w http.ResponseWriter, r *http.Request, actor store.Actor, token string) {
	var body struct {
		TaskIDs  []string `json:"task_ids"`
		Decision string   `json:"decision"`
		Reason   string   `json:"reason"`
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
	done, effects, err := s.store.BatchDecide(r.Context(), actor, colleagues, body.TaskIDs, body.Decision, body.Reason)
	s.dispatch(r, effects)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"processed": done})
}

func (s *Server) editRouteSteps(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	var body struct {
		Steps []store.RouteStepEditInput `json:"steps"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	doc, err := s.store.EditDraftRouteSteps(r.Context(), actor, r.PathValue("id"), body.Steps)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) applyRouteTemplate(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	var body struct {
		TemplateID string `json:"template_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	doc, err := s.store.ApplyDraftRouteTemplate(r.Context(), actor, r.PathValue("id"), body.TemplateID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) listBookmarks(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	items, err := s.store.ListBookmarks(r.Context(), actor)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) saveBookmark(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	var body struct {
		NoteText string `json:"note_text"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	doc, err := s.store.SaveBookmark(r.Context(), actor, r.PathValue("id"), body.NoteText)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) deleteBookmark(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	if err := s.store.DeleteBookmark(r.Context(), actor, r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) grantAccess(w http.ResponseWriter, r *http.Request, actor store.Actor, token string) {
	var body struct {
		UserID string `json:"user_id"`
		Days   int    `json:"days"`
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
	doc, err := s.store.GrantAccess(r.Context(), actor, colleagues, r.PathValue("id"), body.UserID, body.Days)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, doc)
}

func (s *Server) completeByAuthor(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	doc, err := s.store.CompleteByAuthor(r.Context(), actor, r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) approvalSheetDocx(w http.ResponseWriter, r *http.Request, actor store.Actor, token string) {
	people, err := s.auth.Directory(r.Context(), token)
	if err != nil {
		fail(w, err)
		return
	}
	names := make(map[string]string, len(people))
	for _, p := range people {
		names[p.UserID] = p.FullName + " (" + p.Position + ")"
	}
	data, filename, err := s.store.BuildApprovalSheetDocx(r.Context(), actor, r.PathValue("id"), names)
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (s *Server) reassignRoute(w http.ResponseWriter, r *http.Request, actor store.Actor, token string) {
	var body struct {
		TaskID     string `json:"task_id"`
		StepID     string `json:"step_id"`
		UserID     string `json:"user_id"`
		Department string `json:"department_code"`
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
	if body.UserID == "" && body.Department != "" {
		body.UserID = s.store.DepartmentHeadForDocument(r.Context(), actor.OrganizationCode, r.PathValue("id"), colleagues, body.Department)
	}
	if body.UserID == "" {
		writeErr(w, http.StatusBadRequest, "у отдела нет руководителя с правом на контур, выберите сотрудника")
		return
	}
	known := false
	for _, person := range colleagues {
		if person.UserID == body.UserID && person.OrganizationCode == actor.OrganizationCode {
			known = true
			break
		}
	}
	if !known {
		writeErr(w, http.StatusForbidden, "сотрудник не из вашей организации")
		return
	}
	doc, err := s.store.ReassignRoute(r.Context(), actor, r.PathValue("id"), body.TaskID, body.StepID, body.UserID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}
