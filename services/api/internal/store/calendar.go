package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type CalendarEvent struct {
	ID                 string           `json:"id"`
	Scope              string           `json:"scope"`
	Title              string           `json:"title"`
	StartsAt           time.Time        `json:"starts_at"`
	EndsAt             time.Time        `json:"ends_at"`
	Note               string           `json:"note,omitempty"`
	Place              string           `json:"place,omitempty"`
	Kind               string           `json:"kind,omitempty"`
	OwnerUserID        string           `json:"owner_user_id"`
	OrganizerUserID    string           `json:"organizer_user_id"`
	ProtocolDocumentID string           `json:"protocol_document_id,omitempty"`
	ProtocolTitle      string           `json:"protocol_title,omitempty"`
	ProtocolRegNumber  string           `json:"protocol_reg_number,omitempty"`
	Participants       []map[string]any `json:"participants,omitempty"`
}

type CalendarMark struct {
	ID         string    `json:"id"`
	Layer      string    `json:"layer"` // registration | approval | execution | event
	Scope      string    `json:"scope"` // personal | shared
	Title      string    `json:"title"`
	DocumentID string    `json:"document_id,omitempty"`
	EventID    string    `json:"event_id,omitempty"`
	StartsAt   time.Time `json:"starts_at"`
	EndsAt     time.Time `json:"ends_at"`
}

type CalendarView struct {
	Tasks  []TaskInfo      `json:"tasks"`
	Events []CalendarEvent `json:"events"`
	Marks  []CalendarMark  `json:"marks"`
}

func (s *Store) Calendar(ctx context.Context, actor Actor, from, to time.Time) (CalendarView, error) {
	view := CalendarView{Tasks: []TaskInfo{}, Events: []CalendarEvent{}, Marks: []CalendarMark{}}
	rows, err := s.pool.Query(ctx, `
		SELECT t.id::text, t.document_id::text, d.title, s.kind, t.state, t.plan_due_at
		FROM step_tasks t
		JOIN documents d ON d.id = t.document_id
		JOIN route_steps s ON s.id = t.step_id
		WHERE t.assignee_user_id = $1
		  AND d.organization_code = $2
		  AND t.state = 'назначена'
		  AND t.plan_due_at IS NOT NULL
		  AND t.plan_due_at >= $3
		  AND t.plan_due_at < $4
		ORDER BY t.plan_due_at
	`, actor.UserID, actor.OrganizationCode, from, to)
	if err != nil {
		return view, err
	}
	defer rows.Close()
	for rows.Next() {
		var task TaskInfo
		if err := rows.Scan(&task.ID, &task.DocumentID, &task.Title, &task.Kind, &task.State, &task.PlanDueAt); err != nil {
			return view, err
		}
		view.Tasks = append(view.Tasks, task)
	}
	if err := rows.Err(); err != nil {
		return view, err
	}

	eventRows, err := s.pool.Query(ctx, `
		SELECT c.id::text, c.scope, c.title, c.starts_at, c.ends_at, c.note,
		       c.owner_user_id, c.place, c.kind, c.organizer_user_id,
		       COALESCE(c.protocol_document_id::text, ''),
		       COALESCE(d.title, ''),
		       COALESCE(d.reg_number, ''),
		       COALESCE(g.rows, '[]'::jsonb)
		FROM calendar_events c
		LEFT JOIN documents d ON d.id = c.protocol_document_id
		LEFT JOIN work_grids g ON g.owner_kind = 'meeting' AND g.owner_id = c.id
		WHERE c.organization_code = $1
		  AND c.starts_at < $3
		  AND c.ends_at >= $2
		  AND (c.scope = 'shared' OR c.owner_user_id = $4 OR c.organizer_user_id = $4)
		ORDER BY c.starts_at
	`, actor.OrganizationCode, from, to, actor.UserID)
	if err != nil {
		return view, err
	}
	defer eventRows.Close()
	for eventRows.Next() {
		var event CalendarEvent
		var partRaw []byte
		if err := eventRows.Scan(
			&event.ID, &event.Scope, &event.Title, &event.StartsAt, &event.EndsAt, &event.Note,
			&event.OwnerUserID, &event.Place, &event.Kind, &event.OrganizerUserID,
			&event.ProtocolDocumentID, &event.ProtocolTitle, &event.ProtocolRegNumber,
			&partRaw,
		); err != nil {
			return view, err
		}
		if len(partRaw) > 0 {
			_ = json.Unmarshal(partRaw, &event.Participants)
		}
		if event.OrganizerUserID == "" {
			event.OrganizerUserID = event.OwnerUserID
		}
		view.Events = append(view.Events, event)
		end := event.EndsAt
		if !end.After(event.StartsAt) {
			end = event.StartsAt.Add(time.Hour)
		}
		view.Marks = append(view.Marks, CalendarMark{
			ID: event.ID, Layer: "event", Scope: event.Scope, Title: event.Title,
			DocumentID: event.ProtocolDocumentID, EventID: event.ID,
			StartsAt: event.StartsAt, EndsAt: end,
		})
	}
	if err := eventRows.Err(); err != nil {
		return view, err
	}
	if err := s.appendDocumentMarks(ctx, actor, from, to, &view); err != nil {
		return view, err
	}
	return view, nil
}

func (s *Store) appendDocumentMarks(ctx context.Context, actor Actor, from, to time.Time, view *CalendarView) error {
	visible := `
		d.organization_code = $1
		AND (
			$2::bool
			OR d.author_user_id = $3
			OR d.addressee_user_id = $3
			OR EXISTS (SELECT 1 FROM step_tasks vis WHERE vis.document_id = d.id AND vis.assignee_user_id = $3)
			OR EXISTS (SELECT 1 FROM document_grants g WHERE g.document_id = d.id AND g.user_id = $3 AND g.expires_at > now())
		)`
	regRows, err := s.pool.Query(ctx, `
		SELECT d.id::text, d.title, COALESCE(d.reg_number, ''), d.reg_date, d.author_user_id
		FROM documents d
		WHERE `+visible+`
		  AND d.reg_date IS NOT NULL
		  AND d.reg_date >= $4
		  AND d.reg_date < $5
	`, actor.OrganizationCode, actor.canReadOrg(), actor.UserID, from, to)
	if err != nil {
		return err
	}
	defer regRows.Close()
	for regRows.Next() {
		var id, title, number, author string
		var at time.Time
		if err := regRows.Scan(&id, &title, &number, &at, &author); err != nil {
			return err
		}
		y, m, d := at.In(time.Local).Date()
		start := time.Date(y, m, d, 9, 0, 0, 0, time.Local)
		label := title
		if number != "" {
			label = "№ " + number + " " + title
		}
		scope := "personal"
		if author != actor.UserID {
			scope = "shared"
		}
		view.Marks = append(view.Marks, CalendarMark{
			ID: "reg-" + id, Layer: "registration", Scope: scope, Title: label,
			DocumentID: id, StartsAt: start, EndsAt: start.Add(time.Hour),
		})
	}
	if err := regRows.Err(); err != nil {
		return err
	}

	winRows, err := s.pool.Query(ctx, `
		SELECT t.id::text, d.id::text, d.title, s.kind, t.started_at, t.plan_due_at, t.assignee_user_id
		FROM step_tasks t
		JOIN documents d ON d.id = t.document_id
		JOIN route_steps s ON s.id = t.step_id
		WHERE `+visible+`
		  AND s.kind IN ('approval', 'signing', 'resolution')
		  AND t.state = 'назначена'
		  AND t.plan_due_at IS NOT NULL
		  AND COALESCE(t.started_at, t.plan_due_at) < $5
		  AND t.plan_due_at >= $4
	`, actor.OrganizationCode, actor.canReadOrg(), actor.UserID, from, to)
	if err != nil {
		return err
	}
	defer winRows.Close()
	for winRows.Next() {
		var id, docID, title, kind, assignee string
		var started *time.Time
		var due time.Time
		if err := winRows.Scan(&id, &docID, &title, &kind, &started, &due, &assignee); err != nil {
			return err
		}
		start := due
		if started != nil {
			start = *started
		}
		scope := "personal"
		if assignee != actor.UserID {
			scope = "shared"
		}
		view.Marks = append(view.Marks, CalendarMark{
			ID: "win-" + id, Layer: "approval", Scope: scope, Title: title,
			DocumentID: docID, StartsAt: start, EndsAt: due,
		})
	}
	if err := winRows.Err(); err != nil {
		return err
	}

	for _, task := range view.Tasks {
		if task.Kind != "execution" || task.PlanDueAt == nil {
			continue
		}
		due := *task.PlanDueAt
		view.Marks = append(view.Marks, CalendarMark{
			ID: "due-" + task.ID, Layer: "execution", Scope: "personal", Title: task.Title,
			DocumentID: task.DocumentID, StartsAt: due, EndsAt: due,
		})
	}
	return nil
}

func (s *Store) GetMeeting(ctx context.Context, actor Actor, id string) (CalendarEvent, error) {
	var event CalendarEvent
	var org string
	var partRaw []byte
	err := s.pool.QueryRow(ctx, `
		SELECT c.id::text, c.organization_code, c.scope, c.title, c.starts_at, c.ends_at, c.note,
		       c.owner_user_id, c.place, c.kind, c.organizer_user_id,
		       COALESCE(c.protocol_document_id::text, ''),
		       COALESCE(d.title, ''),
		       COALESCE(d.reg_number, ''),
		       COALESCE(g.rows, '[]'::jsonb)
		FROM calendar_events c
		LEFT JOIN documents d ON d.id = c.protocol_document_id
		LEFT JOIN work_grids g ON g.owner_kind = 'meeting' AND g.owner_id = c.id
		WHERE c.id = $1::uuid
	`, id).Scan(
		&event.ID, &org, &event.Scope, &event.Title, &event.StartsAt, &event.EndsAt, &event.Note,
		&event.OwnerUserID, &event.Place, &event.Kind, &event.OrganizerUserID,
		&event.ProtocolDocumentID, &event.ProtocolTitle, &event.ProtocolRegNumber,
		&partRaw,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return CalendarEvent{}, ErrNotFound
	}
	if err != nil {
		return CalendarEvent{}, err
	}
	if org != actor.OrganizationCode {
		return CalendarEvent{}, ErrForbidden
	}
	if event.Scope != "shared" && event.OwnerUserID != actor.UserID && event.OrganizerUserID != actor.UserID && !actor.has("admin") {
		return CalendarEvent{}, ErrForbidden
	}
	if len(partRaw) > 0 {
		_ = json.Unmarshal(partRaw, &event.Participants)
	}
	if event.OrganizerUserID == "" {
		event.OrganizerUserID = event.OwnerUserID
	}
	return event, nil
}

func (s *Store) CreateCalendarEvent(ctx context.Context, actor Actor, scope, kind, title, note, place string, starts, ends time.Time) (CalendarEvent, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return CalendarEvent{}, fmt.Errorf("%w: укажите название события", ErrInvalid)
	}
	if kind != "meeting" {
		kind = "event"
	}
	if ends.Before(starts) {
		return CalendarEvent{}, fmt.Errorf("%w: окончание раньше начала", ErrInvalid)
	}
	switch scope {
	case "personal":
	case "shared":
		if !actor.has("manager") && !actor.has("clerk") && !actor.has("admin") {
			return CalendarEvent{}, ErrForbidden
		}
	default:
		return CalendarEvent{}, fmt.Errorf("%w: слой события personal или shared", ErrInvalid)
	}
	var event CalendarEvent
	err := s.pool.QueryRow(ctx, `
		INSERT INTO calendar_events (id, organization_code, owner_user_id, organizer_user_id, scope, kind, title, starts_at, ends_at, note, place)
		VALUES (gen_random_uuid(), $1, $2, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id::text, scope, kind, title, starts_at, ends_at, note, owner_user_id, organizer_user_id, place
	`, actor.OrganizationCode, actor.UserID, scope, kind, title, starts, ends, strings.TrimSpace(note), strings.TrimSpace(place)).Scan(
		&event.ID, &event.Scope, &event.Kind, &event.Title, &event.StartsAt, &event.EndsAt, &event.Note,
		&event.OwnerUserID, &event.OrganizerUserID, &event.Place,
	)
	return event, err
}

func (s *Store) UpdateCalendarEvent(ctx context.Context, actor Actor, id, scope, kind, title, note, place string, starts, ends time.Time) (CalendarEvent, error) {
	existing, err := s.GetMeeting(ctx, actor, id)
	if err != nil {
		return CalendarEvent{}, err
	}
	if existing.OrganizerUserID != actor.UserID && existing.OwnerUserID != actor.UserID && !actor.has("admin") {
		return CalendarEvent{}, ErrForbidden
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return CalendarEvent{}, fmt.Errorf("%w: укажите тему совещания", ErrInvalid)
	}
	if ends.Before(starts) {
		return CalendarEvent{}, fmt.Errorf("%w: окончание раньше начала", ErrInvalid)
	}
	if scope == "" {
		scope = existing.Scope
	}
	switch scope {
	case "personal":
	case "shared":
		if !actor.has("manager") && !actor.has("clerk") && !actor.has("admin") {
			return CalendarEvent{}, ErrForbidden
		}
	default:
		return CalendarEvent{}, fmt.Errorf("%w: слой события personal или shared", ErrInvalid)
	}
	if kind != "meeting" && kind != "event" {
		kind = existing.Kind
		if kind == "" {
			kind = "event"
		}
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE calendar_events
		SET scope = $2, kind = $3, title = $4, starts_at = $5, ends_at = $6, note = $7, place = $8
		WHERE id = $1::uuid
	`, id, scope, kind, title, starts, ends, strings.TrimSpace(note), strings.TrimSpace(place))
	if err != nil {
		return CalendarEvent{}, err
	}
	return s.GetMeeting(ctx, actor, id)
}

func (s *Store) DeleteCalendarEvent(ctx context.Context, actor Actor, id string) error {
	var owner, organizer, scope, org string
	err := s.pool.QueryRow(ctx, `
		SELECT owner_user_id, organizer_user_id, scope, organization_code FROM calendar_events WHERE id = $1::uuid
	`, id).Scan(&owner, &organizer, &scope, &org)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if org != actor.OrganizationCode {
		return ErrForbidden
	}
	if owner != actor.UserID && organizer != actor.UserID && !actor.has("admin") {
		return ErrForbidden
	}
	// Удаление встречи не удаляет связанный документ-протокол в СЭД.
	_, _ = s.pool.Exec(ctx, `DELETE FROM work_grids WHERE owner_kind = 'meeting' AND owner_id = $1::uuid`, id)
	_, err = s.pool.Exec(ctx, `DELETE FROM calendar_events WHERE id = $1::uuid`, id)
	return err
}

func (s *Store) LinkMeetingProtocol(ctx context.Context, actor Actor, meetingID, docID string) (CalendarEvent, error) {
	ev, err := s.GetMeeting(ctx, actor, meetingID)
	if err != nil {
		return CalendarEvent{}, err
	}
	if ev.OrganizerUserID != actor.UserID && ev.OwnerUserID != actor.UserID && !actor.has("admin") && !actor.has("clerk") {
		return CalendarEvent{}, ErrForbidden
	}
	if strings.TrimSpace(docID) == "" {
		_, err = s.pool.Exec(ctx, `UPDATE calendar_events SET protocol_document_id = NULL WHERE id = $1::uuid`, meetingID)
		if err != nil {
			return CalendarEvent{}, err
		}
		return s.GetMeeting(ctx, actor, meetingID)
	}
	doc, err := s.GetDocument(ctx, actor, docID)
	if err != nil {
		return CalendarEvent{}, err
	}
	if doc.TypeCode != "протокол" {
		return CalendarEvent{}, fmt.Errorf("%w: к совещанию привязывается документ вида протокол", ErrInvalid)
	}
	_, err = s.pool.Exec(ctx, `UPDATE calendar_events SET protocol_document_id = $2::uuid WHERE id = $1::uuid`, meetingID, doc.ID)
	if err != nil {
		return CalendarEvent{}, err
	}
	return s.GetMeeting(ctx, actor, meetingID)
}

func (s *Store) CreateMeetingProtocol(ctx context.Context, actor Actor, colleagues []Colleague, meetingID string) (CalendarEvent, Document, error) {
	ev, err := s.GetMeeting(ctx, actor, meetingID)
	if err != nil {
		return CalendarEvent{}, Document{}, err
	}
	if ev.OrganizerUserID != actor.UserID && ev.OwnerUserID != actor.UserID && !actor.has("admin") && !actor.has("clerk") {
		return CalendarEvent{}, Document{}, ErrForbidden
	}
	grid, err := s.GetWorkGrid(ctx, actor, "meeting", meetingID)
	if err != nil {
		return CalendarEvent{}, Document{}, err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Совещание: %s\n", ev.Title)
	fmt.Fprintf(&b, "Дата и время: %s — %s\n", ev.StartsAt.Local().Format("02.01.2006 15:04"), ev.EndsAt.Local().Format("15:04"))
	if ev.Place != "" {
		fmt.Fprintf(&b, "Место: %s\n", ev.Place)
	}
	orgName := ev.OrganizerUserID
	for _, c := range colleagues {
		if c.UserID == ev.OrganizerUserID {
			orgName = c.FullName
			break
		}
	}
	fmt.Fprintf(&b, "Организатор: %s\n", orgName)
	if ev.Note != "" {
		fmt.Fprintf(&b, "Повестка: %s\n", ev.Note)
	}
	if len(grid.Rows) > 0 {
		b.WriteString("\nУчастники и поручения:\n")
		for i, row := range grid.Rows {
			person := strVal(row["person"])
			dept := strVal(row["department"])
			role := strVal(row["role"])
			due := strVal(row["due_at"])
			if person == "" && dept == "" {
				continue
			}
			fmt.Fprintf(&b, "%d. %s", i+1, person)
			if dept != "" {
				fmt.Fprintf(&b, " (%s)", dept)
			}
			if role != "" {
				fmt.Fprintf(&b, " — %s", role)
			}
			if due != "" {
				fmt.Fprintf(&b, ", срок: %s", due)
			}
			b.WriteString("\n")
		}
	}

	docTitle := fmt.Sprintf("Протокол: %s (%s)", ev.Title, ev.StartsAt.Local().Format("02.01.2006"))
	raw, _, err := s.CreateDocument(ctx, actor, "", "", CreateInput{
		TypeCode:       "протокол",
		BusinessDomain: "орг",
		Title:          docTitle,
		Body:           b.String(),
		Importance:     "обычная",
	}, nil)
	if err != nil {
		return CalendarEvent{}, Document{}, err
	}
	var created Document
	if err := json.Unmarshal(raw, &created); err != nil {
		return CalendarEvent{}, Document{}, err
	}
	if _, err := s.pool.Exec(ctx, `UPDATE calendar_events SET protocol_document_id = $2::uuid WHERE id = $1::uuid`, meetingID, created.ID); err != nil {
		return CalendarEvent{}, Document{}, err
	}
	updatedMeeting, err := s.GetMeeting(ctx, actor, meetingID)
	if err != nil {
		return CalendarEvent{}, created, err
	}
	return updatedMeeting, created, nil
}

func DepartmentHead(list []Colleague, departmentCode string) string {
	if departmentCode == "" {
		return ""
	}
	members := make([]Colleague, 0)
	known := map[string]bool{}
	for _, person := range list {
		known[person.UserID] = true
		if person.DepartmentCode == departmentCode {
			members = append(members, person)
		}
	}
	if len(members) == 0 {
		return ""
	}
	score := map[string]int{}
	for _, person := range members {
		if person.ManagerID != "" && person.ManagerID != person.UserID && known[person.ManagerID] {
			score[person.ManagerID]++
		}
	}
	best := ""
	bestScore := 0
	for id, count := range score {
		if count > bestScore {
			best, bestScore = id, count
		}
	}
	if best != "" {
		return best
	}
	for _, person := range members {
		for _, role := range person.Roles {
			if role == "manager" || role == "signer" {
				return person.UserID
			}
		}
	}
	return ""
}
