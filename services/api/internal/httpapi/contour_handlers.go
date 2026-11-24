package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/Maxjalo/SED/services/api/internal/store"
)

type contourGrantView struct {
	ID             string `json:"id"`
	UserID         string `json:"user_id"`
	DisplayName    string `json:"display_name"`
	DepartmentName string `json:"department_name"`
	Position       string `json:"position"`
	BusinessDomain string `json:"business_domain"`
	AccessLevel    string `json:"access_level"`
	GrantedBy      string `json:"granted_by"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

func (s *Server) listContourGrants(w http.ResponseWriter, r *http.Request, actor store.Actor, token string) {
	items, err := s.store.ListContourGrants(r.Context(), actor)
	if err != nil {
		fail(w, err)
		return
	}
	colleagues, err := s.colleagues(r, token)
	if err != nil {
		fail(w, err)
		return
	}
	byID := map[string]store.Colleague{}
	for _, person := range colleagues {
		byID[person.UserID] = person
	}
	out := make([]contourGrantView, 0, len(items))
	for _, item := range items {
		person := byID[item.UserID]
		out = append(out, contourGrantView{
			ID:             item.ID,
			UserID:         item.UserID,
			DisplayName:    person.FullName,
			DepartmentName: person.DepartmentName,
			Position:       person.Position,
			BusinessDomain: item.BusinessDomain,
			AccessLevel:    item.AccessLevel,
			GrantedBy:      item.GrantedBy,
			CreatedAt:      item.CreatedAt,
			UpdatedAt:      item.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) setContourGrant(w http.ResponseWriter, r *http.Request, actor store.Actor, token string) {
	var body struct {
		UserID         string `json:"user_id"`
		BusinessDomain string `json:"business_domain"`
		AccessLevel    string `json:"access_level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	if body.UserID == "" || body.BusinessDomain == "" {
		writeErr(w, http.StatusBadRequest, "нужны user_id и business_domain")
		return
	}
	colleagues, err := s.colleagues(r, token)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.store.SetContourGrant(r.Context(), actor, colleagues, body.UserID, body.BusinessDomain, body.AccessLevel); err != nil {
		fail(w, err)
		return
	}
	s.listContourGrants(w, r, actor, token)
}

func (s *Server) revokeContourGrant(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	if err := s.store.RevokeContourGrant(r.Context(), actor, r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
