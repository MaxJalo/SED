package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/Maxjalo/SED/services/api/internal/store"
)

func (s *Server) templates(w http.ResponseWriter, r *http.Request, _ store.Actor, _ string) {
	items, err := s.store.ListTemplates(r.Context(), r.URL.Query().Get("type_code"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listDeputies(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	items, err := s.store.ListDeputies(r.Context(), actor)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createDeputy(w http.ResponseWriter, r *http.Request, actor store.Actor, token string) {
	var in store.CreateDeputyInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	colleagues, err := s.colleagues(r, token)
	if err != nil {
		fail(w, err)
		return
	}
	item, err := s.store.CreateDeputy(r.Context(), actor, colleagues, in)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) deactivateDeputy(w http.ResponseWriter, r *http.Request, actor store.Actor, token string) {
	colleagues, err := s.colleagues(r, token)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.store.DeactivateDeputy(r.Context(), actor, colleagues, r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deactivated"})
}

func (s *Server) delegateTask(w http.ResponseWriter, r *http.Request, actor store.Actor, token string) {
	var body struct {
		ToUserID string `json:"to_user_id"`
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
	doc, err := s.store.DelegateTask(r.Context(), actor, colleagues, r.PathValue("task_id"), body.ToUserID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) addReview(w http.ResponseWriter, r *http.Request, actor store.Actor, token string) {
	var body struct {
		TargetUserID string `json:"target_user_id"`
		Body         string `json:"body"`
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
	doc, err := s.store.AddCommentOrReview(r.Context(), actor, colleagues, r.PathValue("id"), "review", body.TargetUserID, body.Body)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, doc)
}

func (s *Server) addComment(w http.ResponseWriter, r *http.Request, actor store.Actor, token string) {
	var body struct {
		Body string `json:"body"`
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
	doc, err := s.store.AddCommentOrReview(r.Context(), actor, colleagues, r.PathValue("id"), "comment", "", body.Body)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, doc)
}

func (s *Server) listTags(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	items, err := s.store.ListTags(r.Context(), actor)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) createTag(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	var body struct {
		Scope string `json:"scope"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	item, err := s.store.CreateTag(r.Context(), actor, body.Scope, body.Name)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) attachTag(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	var body struct {
		TagID string `json:"tag_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	doc, err := s.store.AttachTag(r.Context(), actor, r.PathValue("id"), body.TagID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) recordPaper(w http.ResponseWriter, r *http.Request, actor store.Actor, token string) {
	var body struct {
		HolderUserID string `json:"holder_user_id"`
		Note         string `json:"note"`
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
	doc, err := s.store.RecordPaperMovement(r.Context(), actor, colleagues, r.PathValue("id"), body.HolderUserID, body.Note)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, doc)
}

func (s *Server) listCases(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	items, err := s.store.ListCases(r.Context(), actor)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) closeToCase(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	var body struct {
		CaseID string `json:"case_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	doc, err := s.store.CloseToCase(r.Context(), actor, r.PathValue("id"), body.CaseID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) ackRemarks(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	doc, err := s.store.AckRemarks(r.Context(), actor, r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}

func (s *Server) addRouteParticipant(w http.ResponseWriter, r *http.Request, actor store.Actor, token string) {
	var body struct {
		Kind   string `json:"kind"`
		UserID string `json:"user_id"`
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
	doc, effects, err := s.store.AddRouteParticipant(r.Context(), actor, colleagues, r.PathValue("id"), body.Kind, body.UserID)
	if err != nil {
		fail(w, err)
		return
	}
	s.dispatch(r, effects)
	writeJSON(w, http.StatusCreated, doc)
}

func (s *Server) addDocumentLink(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	var body struct {
		OtherDocumentID string `json:"other_document_id"`
		LinkType        string `json:"link_type"`
		Place           string `json:"place"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	doc, err := s.store.AddDocumentLink(r.Context(), actor, r.PathValue("id"), body.OtherDocumentID, body.LinkType, body.Place)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, doc)
}

func (s *Server) deleteDocumentLink(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	doc, err := s.store.DeleteDocumentLink(r.Context(), actor, r.PathValue("id"), r.PathValue("link_id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doc)
}
