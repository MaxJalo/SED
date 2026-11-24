package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/Maxjalo/SED/services/api/internal/store"
)

func (s *Server) getDocumentGrid(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	grid, err := s.store.GetWorkGrid(r.Context(), actor, "document", r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, grid)
}

func (s *Server) saveDocumentGrid(w http.ResponseWriter, r *http.Request, actor store.Actor, token string) {
	var body struct {
		Columns    []store.WorkGridColumn `json:"columns"`
		Rows       []map[string]any       `json:"rows"`
		ApplyRoute *bool                  `json:"apply_route"`
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
	apply := true
	if body.ApplyRoute != nil {
		apply = *body.ApplyRoute
	}
	grid, effects, err := s.store.SaveWorkGrid(r.Context(), actor, colleagues, "document", r.PathValue("id"), body.Columns, body.Rows, apply)
	if err != nil {
		fail(w, err)
		return
	}
	s.dispatch(r, effects)
	writeJSON(w, http.StatusOK, grid)
}

func (s *Server) getMeetingGrid(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	grid, err := s.store.GetWorkGrid(r.Context(), actor, "meeting", r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, grid)
}

func (s *Server) saveMeetingGrid(w http.ResponseWriter, r *http.Request, actor store.Actor, token string) {
	var body struct {
		Columns []store.WorkGridColumn `json:"columns"`
		Rows    []map[string]any       `json:"rows"`
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
	grid, _, err := s.store.SaveWorkGrid(r.Context(), actor, colleagues, "meeting", r.PathValue("id"), body.Columns, body.Rows, false)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, grid)
}
