package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/Maxjalo/SED/services/api/internal/store"
)

func (s *Server) listRoleGrants(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	items, err := s.store.ListRoleGrants(r.Context(), actor)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) grantRole(w http.ResponseWriter, r *http.Request, actor store.Actor, token string) {
	var body struct {
		UserID      string   `json:"user_id"`
		Role        string   `json:"role"`
		Permissions []string `json:"permissions"`
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
	if body.Permissions != nil {
		if err := s.store.SetUserPermissions(r.Context(), actor, colleagues, body.UserID, body.Permissions); err != nil {
			fail(w, err)
			return
		}
	} else {
		if err := s.store.GrantRole(r.Context(), actor, colleagues, body.UserID, body.Role); err != nil {
			fail(w, err)
			return
		}
	}
	items, err := s.store.ListRoleGrants(r.Context(), actor)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) revokeRole(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	if err := s.store.RevokeRole(r.Context(), actor, r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	items, err := s.store.ListRoleGrants(r.Context(), actor)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) listRouteTemplates(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	items, err := s.store.ListRouteTemplates(r.Context(), actor)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) saveRouteTemplate(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	var body struct {
		Steps []store.RouteTemplateStep `json:"steps"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	item, err := s.store.SaveRouteTemplate(r.Context(), actor, r.PathValue("id"), body.Steps)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
