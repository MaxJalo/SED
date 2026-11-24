package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Maxjalo/SED/services/api/internal/store"
)

func (s *Server) calendar(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	from, to, err := monthRange(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, "даты календаря в формате ГГГГ-ММ-ДД")
		return
	}
	view, err := s.store.Calendar(r.Context(), actor, from, to)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) getMeeting(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	ev, err := s.store.GetMeeting(r.Context(), actor, r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ev)
}

func (s *Server) createCalendarEvent(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	var body struct {
		Scope    string `json:"scope"`
		Kind     string `json:"kind"`
		Title    string `json:"title"`
		Note     string `json:"note"`
		Place    string `json:"place"`
		StartsAt string `json:"starts_at"`
		EndsAt   string `json:"ends_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	starts, err := time.Parse(time.RFC3339, body.StartsAt)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "начало события в формате RFC3339")
		return
	}
	ends, err := time.Parse(time.RFC3339, body.EndsAt)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "окончание события в формате RFC3339")
		return
	}
	event, err := s.store.CreateCalendarEvent(r.Context(), actor, body.Scope, body.Kind, body.Title, body.Note, body.Place, starts, ends)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, event)
}

func (s *Server) updateCalendarEvent(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	var body struct {
		Scope    string `json:"scope"`
		Kind     string `json:"kind"`
		Title    string `json:"title"`
		Note     string `json:"note"`
		Place    string `json:"place"`
		StartsAt string `json:"starts_at"`
		EndsAt   string `json:"ends_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	starts, err := time.Parse(time.RFC3339, body.StartsAt)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "начало события в формате RFC3339")
		return
	}
	ends, err := time.Parse(time.RFC3339, body.EndsAt)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "окончание события в формате RFC3339")
		return
	}
	event, err := s.store.UpdateCalendarEvent(r.Context(), actor, r.PathValue("id"), body.Scope, body.Kind, body.Title, body.Note, body.Place, starts, ends)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, event)
}

func (s *Server) deleteCalendarEvent(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	if err := s.store.DeleteCalendarEvent(r.Context(), actor, r.PathValue("id")); err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) createMeetingProtocol(w http.ResponseWriter, r *http.Request, actor store.Actor, token string) {
	colleagues, err := s.colleagues(r, token)
	if err != nil {
		fail(w, err)
		return
	}
	meeting, doc, err := s.store.CreateMeetingProtocol(r.Context(), actor, colleagues, r.PathValue("id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"meeting":  meeting,
		"document": doc,
	})
}

func (s *Server) linkMeetingProtocol(w http.ResponseWriter, r *http.Request, actor store.Actor, _ string) {
	var body struct {
		DocumentID string `json:"document_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, "некорректный JSON")
		return
	}
	meeting, err := s.store.LinkMeetingProtocol(r.Context(), actor, r.PathValue("id"), body.DocumentID)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, meeting)
}

func monthRange(fromRaw, toRaw string) (time.Time, time.Time, error) {
	if fromRaw == "" || toRaw == "" {
		now := time.Now()
		from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local)
		return from, from.AddDate(0, 1, 0), nil
	}
	from, err := time.Parse("2006-01-02", fromRaw)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	to, err := time.Parse("2006-01-02", toRaw)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return from, to, nil
}
