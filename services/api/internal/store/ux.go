package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Maxjalo/SED/services/api/internal/sla"
	"github.com/jackc/pgx/v5"
)

func loadDocumentExtras(ctx context.Context, q querier, doc *Document, viewer, viewerDept string) error {
	linkRows, err := q.Query(ctx, `
		SELECT l.id::text, 'child', d.id::text, d.title, d.status, t.name, COALESCE(d.reg_number, ''), l.link_type, l.created_at
		FROM document_links l
		JOIN documents d ON d.id = l.to_document_id
		JOIN document_types t ON t.id = d.type_id
		WHERE l.from_document_id = $1::uuid
		UNION ALL
		SELECT l.id::text, 'parent', d.id::text, d.title, d.status, t.name, COALESCE(d.reg_number, ''), l.link_type, l.created_at
		FROM document_links l
		JOIN documents d ON d.id = l.from_document_id
		JOIN document_types t ON t.id = d.type_id
		WHERE l.to_document_id = $1::uuid
		ORDER BY created_at
	`, doc.ID)
	if err != nil {
		return err
	}
	defer linkRows.Close()
	for linkRows.Next() {
		var l DocumentLink
		if err := linkRows.Scan(&l.ID, &l.Direction, &l.OtherDocID, &l.OtherTitle, &l.OtherStatus, &l.OtherTypeName, &l.OtherRegNumber, &l.LinkType, &l.CreatedAt); err != nil {
			return err
		}
		doc.Links = append(doc.Links, l)
	}
	if err := linkRows.Err(); err != nil {
		return err
	}
	if doc.Links == nil {
		doc.Links = []DocumentLink{}
	}

	crRows, err := q.Query(ctx, `
		SELECT id::text, entry_kind, author_user_id, COALESCE(target_user_id, ''), body, created_at
		FROM comments_reviews
		WHERE document_id = $1::uuid
		ORDER BY created_at ASC
	`, doc.ID)
	if err != nil {
		return err
	}
	defer crRows.Close()
	for crRows.Next() {
		var cr CommentReview
		if err := crRows.Scan(&cr.ID, &cr.EntryKind, &cr.AuthorUserID, &cr.TargetUserID, &cr.Body, &cr.CreatedAt); err != nil {
			return err
		}
		doc.CommentsReviews = append(doc.CommentsReviews, cr)
	}
	if err := crRows.Err(); err != nil {
		return err
	}
	if doc.CommentsReviews == nil {
		doc.CommentsReviews = []CommentReview{}
	}

	pmRows, err := q.Query(ctx, `
		SELECT id::text, holder_user_id, moved_at, COALESCE(note, ''), recorded_by
		FROM paper_movements
		WHERE document_id = $1::uuid
		ORDER BY moved_at DESC
	`, doc.ID)
	if err != nil {
		return err
	}
	defer pmRows.Close()
	for pmRows.Next() {
		var pm PaperMovement
		if err := pmRows.Scan(&pm.ID, &pm.HolderUserID, &pm.MovedAt, &pm.Note, &pm.RecordedBy); err != nil {
			return err
		}
		doc.PaperMovements = append(doc.PaperMovements, pm)
	}
	if err := pmRows.Err(); err != nil {
		return err
	}
	if doc.PaperMovements == nil {
		doc.PaperMovements = []PaperMovement{}
	}

	tagRows, err := q.Query(ctx, `
		SELECT tg.id::text, tg.scope, COALESCE(tg.department_code, ''), tg.name
		FROM document_tags dt
		JOIN tags tg ON tg.id = dt.tag_id
		WHERE dt.document_id = $1::uuid
		  AND (tg.scope = 'org' OR tg.department_code = $2)
		ORDER BY tg.scope, tg.name
	`, doc.ID, viewerDept)
	if err != nil {
		return err
	}
	defer tagRows.Close()
	for tagRows.Next() {
		var tg TagInfo
		if err := tagRows.Scan(&tg.ID, &tg.Scope, &tg.DepartmentCode, &tg.Name); err != nil {
			return err
		}
		doc.Tags = append(doc.Tags, tg)
	}
	if err := tagRows.Err(); err != nil {
		return err
	}
	if doc.Tags == nil {
		doc.Tags = []TagInfo{}
	}

	grantRows, err := q.Query(ctx, `
		SELECT id::text, user_id, granted_by, expires_at
		FROM document_grants
		WHERE document_id = $1::uuid AND expires_at > now()
		ORDER BY created_at DESC
	`, doc.ID)
	if err != nil {
		return err
	}
	defer grantRows.Close()
	for grantRows.Next() {
		var g GrantInfo
		if err := grantRows.Scan(&g.ID, &g.UserID, &g.GrantedBy, &g.ExpiresAt); err != nil {
			return err
		}
		doc.Grants = append(doc.Grants, g)
	}
	if err := grantRows.Err(); err != nil {
		return err
	}
	if doc.Grants == nil {
		doc.Grants = []GrantInfo{}
	}

	if viewer != "" {
		var note string
		err := q.QueryRow(ctx, `SELECT note_text FROM user_bookmarks WHERE user_id = $1 AND document_id = $2::uuid`, viewer, doc.ID).Scan(&note)
		if err == nil {
			doc.Bookmarked = true
			doc.BookmarkNote = note
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	return nil
}

func resolveStepAssignee(step stepRow, doc Document, colleagues []Colleague) (string, error) {
	assignee := step.AssigneeUser
	if assignee != "" {
		return assignee, nil
	}
	if (step.Kind == "execution" || step.Kind == "resolution") && doc.AddresseeUserID != "" {
		return doc.AddresseeUserID, nil
	}
	if step.AssigneeRole == "author" {
		return doc.AuthorUserID, nil
	}
	if step.AssigneeRole == "manager" {
		if mgr := findAuthorManager(colleagues, doc.AuthorUserID); mgr != "" {
			return mgr, nil
		}
	}
	return "", fmt.Errorf("%w: укажите участника для шага %s", ErrConflict, step.Kind)
}

func willSignInRoute(ctx context.Context, tx pgx.Tx, routeID string, afterOrder int, doc Document, colleagues []Colleague, candidate string) (bool, error) {
	rows, err := tx.Query(ctx, `
		SELECT id::text, kind, is_blocking, is_mandatory, sla_enabled, COALESCE(due_days, 3), condition_rule,
		       COALESCE(assignee_role, ''), COALESCE(assignee_user_id, '')
		FROM route_steps
		WHERE route_id = $1::uuid AND kind = 'signing' AND order_no >= $2
	`, routeID, afterOrder)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	var signSteps []stepRow
	for rows.Next() {
		var step stepRow
		if err := rows.Scan(&step.ID, &step.Kind, &step.Blocking, &step.Mandatory, &step.SLA, &step.DueDays, &step.Condition, &step.AssigneeRole, &step.AssigneeUser); err != nil {
			return false, err
		}
		if conditionMet(step.Condition, doc) {
			signSteps = append(signSteps, step)
		}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	for _, s := range signSteps {
		if signer, err := resolveStepAssignee(s, doc, colleagues); err == nil && signer == candidate {
			return true, nil
		}
	}
	return false, nil
}

func resolveActiveDeputy(ctx context.Context, tx pgx.Tx, orgCode, userID string) (string, bool, error) {
	var deputyID string
	err := tx.QueryRow(ctx, `
		SELECT deputy_user_id
		FROM deputies
		WHERE organization_code = $1
		  AND user_id = $2
		  AND active = true
		  AND valid_from <= CURRENT_DATE
		  AND valid_to >= CURRENT_DATE
		ORDER BY created_at DESC
		LIMIT 1
	`, orgCode, userID).Scan(&deputyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return deputyID, true, nil
}

func pauseChildRoutes(ctx context.Context, tx pgx.Tx, parentDocID, actorUserID, reason string) error {
	rows, err := tx.Query(ctx, `
		SELECT d.id::text, d.route_instance_id::text
		FROM document_links l
		JOIN documents d ON d.id = l.to_document_id
		JOIN routes r ON r.id = d.route_instance_id
		WHERE l.from_document_id = $1::uuid
		  AND r.status = 'идёт'
	`, parentDocID)
	if err != nil {
		return err
	}
	type pair struct{ docID, routeID string }
	var targets []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.docID, &p.routeID); err != nil {
			rows.Close()
			return err
		}
		targets = append(targets, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, t := range targets {
		if _, err := tx.Exec(ctx, `UPDATE routes SET status = 'Приостановлено' WHERE id = $1::uuid`, t.routeID); err != nil {
			return err
		}
		if err := writeAudit(ctx, tx, t.docID, actorUserID, "переход", "идёт", "Приостановлено", "автопауза: "+reason); err != nil {
			return err
		}
	}
	return nil
}

func resumeChildRoutes(ctx context.Context, tx pgx.Tx, parentDocID, actorUserID string) error {
	rows, err := tx.Query(ctx, `
		SELECT d.id::text, d.route_instance_id::text
		FROM document_links l
		JOIN documents d ON d.id = l.to_document_id
		JOIN routes r ON r.id = d.route_instance_id
		WHERE l.from_document_id = $1::uuid
		  AND r.status = 'Приостановлено'
	`, parentDocID)
	if err != nil {
		return err
	}
	type pair struct{ docID, routeID string }
	var targets []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.docID, &p.routeID); err != nil {
			rows.Close()
			return err
		}
		targets = append(targets, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, t := range targets {
		if _, err := tx.Exec(ctx, `UPDATE routes SET status = 'идёт' WHERE id = $1::uuid`, t.routeID); err != nil {
			return err
		}
		if err := writeAudit(ctx, tx, t.docID, actorUserID, "переход", "Приостановлено", "идёт", "снятие автопаузы родителем"); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListTemplates(ctx context.Context, typeCode string) ([]TemplateInfo, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, type_code, name, default_title, body_template, COALESCE(default_service_name, '')
		FROM document_templates
		WHERE ($1::text = '' OR type_code = $1)
		ORDER BY type_code, name
	`, typeCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []TemplateInfo
	for rows.Next() {
		var item TemplateInfo
		if err := rows.Scan(&item.ID, &item.TypeCode, &item.Name, &item.DefaultTitle, &item.BodyTemplate, &item.DefaultServiceName); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	if list == nil {
		list = []TemplateInfo{}
	}
	return list, rows.Err()
}

func (s *Store) ListDeputies(ctx context.Context, actor Actor) ([]DeputyInfo, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, user_id, deputy_user_id, to_char(valid_from, 'YYYY-MM-DD'), to_char(valid_to, 'YYYY-MM-DD'), active, created_by
		FROM deputies
		WHERE organization_code = $1
		ORDER BY created_at DESC
	`, actor.OrganizationCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []DeputyInfo
	for rows.Next() {
		var d DeputyInfo
		if err := rows.Scan(&d.ID, &d.UserID, &d.DeputyUserID, &d.ValidFrom, &d.ValidTo, &d.Active, &d.CreatedBy); err != nil {
			return nil, err
		}
		list = append(list, d)
	}
	if list == nil {
		list = []DeputyInfo{}
	}
	return list, rows.Err()
}

type CreateDeputyInput struct {
	UserID       string `json:"user_id"`
	DeputyUserID string `json:"deputy_user_id"`
	ValidFrom    string `json:"valid_from"`
	ValidTo      string `json:"valid_to"`
}

func (s *Store) CreateDeputy(ctx context.Context, actor Actor, colleagues []Colleague, in CreateDeputyInput) (DeputyInfo, error) {
	if in.UserID == "" || in.DeputyUserID == "" || in.UserID == in.DeputyUserID {
		return DeputyInfo{}, fmt.Errorf("%w: укажите разных сотрудников для замещения", ErrInvalid)
	}
	if in.ValidFrom == "" || in.ValidTo == "" || in.ValidTo < in.ValidFrom {
		return DeputyInfo{}, fmt.Errorf("%w: некорректный период замещения", ErrInvalid)
	}
	if !canManageDeputy(actor, colleagues, in.UserID) {
		return DeputyInfo{}, ErrForbidden
	}
	if !sameOrgColleague(colleagues, actor.OrganizationCode, in.UserID) || !sameOrgColleague(colleagues, actor.OrganizationCode, in.DeputyUserID) {
		return DeputyInfo{}, ErrForbidden
	}
	var d DeputyInfo
	err := s.pool.QueryRow(ctx, `
		INSERT INTO deputies (id, organization_code, user_id, deputy_user_id, valid_from, valid_to, active, created_by)
		VALUES (gen_random_uuid(), $1, $2, $3, $4::date, $5::date, true, $6)
		RETURNING id::text, user_id, deputy_user_id, to_char(valid_from, 'YYYY-MM-DD'), to_char(valid_to, 'YYYY-MM-DD'), active, created_by
	`, actor.OrganizationCode, in.UserID, in.DeputyUserID, in.ValidFrom, in.ValidTo, actor.UserID).Scan(
		&d.ID, &d.UserID, &d.DeputyUserID, &d.ValidFrom, &d.ValidTo, &d.Active, &d.CreatedBy,
	)
	return d, err
}

func (s *Store) DeactivateDeputy(ctx context.Context, actor Actor, colleagues []Colleague, deputyID string) error {
	var orgCode, targetUser string
	err := s.pool.QueryRow(ctx, `SELECT organization_code, user_id FROM deputies WHERE id = $1::uuid`, deputyID).Scan(&orgCode, &targetUser)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if orgCode != actor.OrganizationCode || !canManageDeputy(actor, colleagues, targetUser) {
		return ErrForbidden
	}
	_, err = s.pool.Exec(ctx, `UPDATE deputies SET active = false WHERE id = $1::uuid`, deputyID)
	return err
}

func canManageDeputy(actor Actor, colleagues []Colleague, targetUserID string) bool {
	if actor.has("clerk") || actor.has("admin") {
		return true
	}
	if actor.has("manager") {
		if targetUserID == actor.UserID {
			return true
		}
		for _, c := range colleagues {
			if c.UserID == targetUserID && c.OrganizationCode == actor.OrganizationCode &&
				(c.ManagerID == actor.UserID || (actor.DepartmentCode != "" && c.DepartmentCode == actor.DepartmentCode)) {
				return true
			}
		}
	}
	return false
}

func sameOrgColleague(colleagues []Colleague, orgCode, userID string) bool {
	for _, c := range colleagues {
		if c.UserID == userID && c.OrganizationCode == orgCode {
			return true
		}
	}
	return false
}

func (s *Store) DelegateTask(ctx context.Context, actor Actor, colleagues []Colleague, taskID, toUserID string) (Document, error) {
	if toUserID == "" || toUserID == actor.UserID {
		return Document{}, fmt.Errorf("%w: укажите другого коллегу для делегирования", ErrInvalid)
	}
	if !sameOrgColleague(colleagues, actor.OrganizationCode, toUserID) {
		return Document{}, ErrForbidden
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Document{}, err
	}
	defer tx.Rollback(ctx)

	var docID, state, currentAssignee, stepKind, authorID string
	err = tx.QueryRow(ctx, `
		SELECT t.document_id::text, t.state, t.assignee_user_id, s.kind, d.author_user_id
		FROM step_tasks t
		JOIN route_steps s ON s.id = t.step_id
		JOIN documents d ON d.id = t.document_id
		WHERE t.id = $1::uuid
	`, taskID).Scan(&docID, &state, &currentAssignee, &stepKind, &authorID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return Document{}, err
	}
	if currentAssignee != actor.UserID {
		return Document{}, ErrForbidden
	}
	if state != "назначена" {
		return Document{}, fmt.Errorf("%w: задача уже закрыта", ErrConflict)
	}
	if err := checkNoSelfVisa(stepKind, authorID, toUserID); err != nil {
		return Document{}, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE step_tasks
		SET assignee_user_id = $2,
		    delegated_from_user_id = CASE WHEN delegated_from_user_id = '' THEN $3 ELSE delegated_from_user_id END
		WHERE id = $1::uuid
	`, taskID, toUserID, actor.UserID); err != nil {
		return Document{}, err
	}
	if err := writeAudit(ctx, tx, docID, actor.UserID, "делегирование", actor.UserID, toUserID, "передача задачи без сброса срока"); err != nil {
		return Document{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, err
	}
	return loadDocument(ctx, s.pool, docID, actor.UserID, actor.DepartmentCode)
}

func (s *Store) AddCommentOrReview(ctx context.Context, actor Actor, colleagues []Colleague, docID, kind, targetUserID, body string) (Document, error) {
	if kind != "comment" && kind != "review" {
		return Document{}, fmt.Errorf("%w: неверный тип записи", ErrInvalid)
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return Document{}, fmt.Errorf("%w: текст обязателен", ErrInvalid)
	}
	if kind == "review" && targetUserID != "" && !sameOrgColleague(colleagues, actor.OrganizationCode, targetUserID) {
		return Document{}, ErrForbidden
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Document{}, err
	}
	defer tx.Rollback(ctx)
	doc, err := loadDocument(ctx, tx, docID, actor.UserID, actor.DepartmentCode)
	if err != nil {
		return Document{}, err
	}
	if err := s.ensureRead(ctx, tx, actor, doc); err != nil {
		return Document{}, err
	}
	if doc.Status == "Закрыт (в деле)" {
		return Document{}, ErrForbidden
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO comments_reviews (id, document_id, entry_kind, author_user_id, target_user_id, body)
		VALUES (gen_random_uuid(), $1::uuid, $2, $3, $4, $5)
	`, docID, kind, actor.UserID, targetUserID, body); err != nil {
		return Document{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, err
	}
	return loadDocument(ctx, s.pool, docID, actor.UserID, actor.DepartmentCode)
}

func (s *Store) ListTags(ctx context.Context, actor Actor) ([]TagInfo, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, scope, COALESCE(department_code, ''), name
		FROM tags
		WHERE organization_code = $1
		  AND (scope = 'org' OR department_code = $2)
		ORDER BY scope, name
	`, actor.OrganizationCode, actor.DepartmentCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []TagInfo
	for rows.Next() {
		var tg TagInfo
		if err := rows.Scan(&tg.ID, &tg.Scope, &tg.DepartmentCode, &tg.Name); err != nil {
			return nil, err
		}
		list = append(list, tg)
	}
	if list == nil {
		list = []TagInfo{}
	}
	return list, rows.Err()
}

func (s *Store) CreateTag(ctx context.Context, actor Actor, scope, name string) (TagInfo, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return TagInfo{}, fmt.Errorf("%w: имя тега обязательно", ErrInvalid)
	}
	if scope != "org" && scope != "dept" {
		return TagInfo{}, fmt.Errorf("%w: scope должен быть org или dept", ErrInvalid)
	}
	if scope == "org" && !actor.has("clerk") && !actor.has("admin") && !actor.has("manager") {
		return TagInfo{}, ErrForbidden
	}
	dept := ""
	if scope == "dept" {
		dept = actor.DepartmentCode
		if dept == "" {
			return TagInfo{}, fmt.Errorf("%w: у пользователя не задано подразделение", ErrInvalid)
		}
	}
	var tg TagInfo
	err := s.pool.QueryRow(ctx, `
		INSERT INTO tags (id, organization_code, scope, department_code, name)
		VALUES (gen_random_uuid(), $1, $2, $3, $4)
		RETURNING id::text, scope, COALESCE(department_code, ''), name
	`, actor.OrganizationCode, scope, dept, name).Scan(&tg.ID, &tg.Scope, &tg.DepartmentCode, &tg.Name)
	return tg, err
}

func (s *Store) AttachTag(ctx context.Context, actor Actor, docID, tagID string) (Document, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Document{}, err
	}
	defer tx.Rollback(ctx)
	doc, err := loadDocument(ctx, tx, docID, actor.UserID, actor.DepartmentCode)
	if err != nil {
		return Document{}, err
	}
	if err := s.ensureRead(ctx, tx, actor, doc); err != nil {
		return Document{}, err
	}
	if doc.Status == "Закрыт (в деле)" {
		return Document{}, ErrForbidden
	}
	var tagOrg, tagScope, tagDept string
	err = tx.QueryRow(ctx, `SELECT organization_code, scope, COALESCE(department_code, '') FROM tags WHERE id = $1::uuid`, tagID).
		Scan(&tagOrg, &tagScope, &tagDept)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return Document{}, err
	}
	if tagOrg != actor.OrganizationCode || (tagScope == "dept" && tagDept != actor.DepartmentCode) {
		return Document{}, ErrForbidden
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO document_tags (document_id, tag_id, added_by)
		VALUES ($1::uuid, $2::uuid, $3)
		ON CONFLICT DO NOTHING
	`, docID, tagID, actor.UserID); err != nil {
		return Document{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, err
	}
	return loadDocument(ctx, s.pool, docID, actor.UserID, actor.DepartmentCode)
}

func (s *Store) RecordPaperMovement(ctx context.Context, actor Actor, colleagues []Colleague, docID, holderUserID, note string) (Document, error) {
	if holderUserID == "" || !sameOrgColleague(colleagues, actor.OrganizationCode, holderUserID) {
		return Document{}, fmt.Errorf("%w: укажите сотрудника вашей организации", ErrInvalid)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Document{}, err
	}
	defer tx.Rollback(ctx)
	doc, err := loadDocument(ctx, tx, docID, actor.UserID, actor.DepartmentCode)
	if err != nil {
		return Document{}, err
	}
	if err := s.ensureRead(ctx, tx, actor, doc); err != nil {
		return Document{}, err
	}
	if doc.Status == "Закрыт (в деле)" {
		return Document{}, ErrForbidden
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO paper_movements (id, document_id, holder_user_id, note, recorded_by)
		VALUES (gen_random_uuid(), $1::uuid, $2, $3, $4)
	`, docID, holderUserID, strings.TrimSpace(note), actor.UserID); err != nil {
		return Document{}, err
	}
	if err := writeAudit(ctx, tx, docID, actor.UserID, "бумага", "", holderUserID, note); err != nil {
		return Document{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, err
	}
	return loadDocument(ctx, s.pool, docID, actor.UserID, actor.DepartmentCode)
}

func (s *Store) ListCases(ctx context.Context, actor Actor) ([]CaseInfo, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, code, title, year, retention_years, storage
		FROM case_nomenclature
		WHERE organization_code = $1
		ORDER BY year DESC, code
	`, actor.OrganizationCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []CaseInfo
	for rows.Next() {
		var c CaseInfo
		if err := rows.Scan(&c.ID, &c.Code, &c.Title, &c.Year, &c.RetentionYears, &c.Storage); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	if list == nil {
		list = []CaseInfo{}
	}
	return list, rows.Err()
}

func canCloseToCase(status string, completedAt *time.Time, activeTasks int) error {
	if status == "Закрыт (в деле)" {
		return fmt.Errorf("%w: документ уже списан в дело", ErrConflict)
	}
	if status == "Черновик" {
		return fmt.Errorf("%w: черновик нельзя списать в дело", ErrConflict)
	}
	if status != "Отклонён" && completedAt == nil {
		return fmt.Errorf("%w: сначала отметьте «Завершить ведение» у документа", ErrConflict)
	}
	if activeTasks > 0 {
		return fmt.Errorf("%w: нельзя списать в дело при наличии активных задач (%d)", ErrConflict, activeTasks)
	}
	return nil
}

func (s *Store) CloseToCase(ctx context.Context, actor Actor, docID, caseID string) (Document, error) {
	if !actor.has("clerk") {
		return Document{}, ErrForbidden
	}
	if caseID == "" {
		return Document{}, fmt.Errorf("%w: укажите дело номенклатуры", ErrInvalid)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Document{}, err
	}
	defer tx.Rollback(ctx)
	doc, err := loadDocument(ctx, tx, docID, actor.UserID, actor.DepartmentCode)
	if err != nil {
		return Document{}, err
	}
	if doc.OrganizationCode != actor.OrganizationCode {
		return Document{}, ErrForbidden
	}
	var activeTasks int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM step_tasks
		WHERE document_id = $1::uuid AND state IN ('назначена', 'ожидает_подтверждения')
	`, docID).Scan(&activeTasks); err != nil {
		return Document{}, err
	}
	if err := canCloseToCase(doc.Status, doc.CompletedByAuthorAt, activeTasks); err != nil {
		return Document{}, err
	}
	var caseOrg, caseCode string
	err = tx.QueryRow(ctx, `SELECT organization_code, code FROM case_nomenclature WHERE id = $1::uuid`, caseID).Scan(&caseOrg, &caseCode)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return Document{}, err
	}
	if caseOrg != actor.OrganizationCode {
		return Document{}, ErrForbidden
	}
	if doc.RouteInstanceID != nil {
		if _, err := tx.Exec(ctx, `UPDATE routes SET status = 'завершён' WHERE id = $1::uuid`, *doc.RouteInstanceID); err != nil {
			return Document{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE documents SET status = 'Закрыт (в деле)', case_id = $2::uuid, updated_at = now()
		WHERE id = $1::uuid
	`, docID, caseID); err != nil {
		return Document{}, err
	}
	if err := writeAudit(ctx, tx, docID, actor.UserID, "в_дело", doc.Status, "Закрыт (в деле)", "дело "+caseCode); err != nil {
		return Document{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, err
	}
	return loadDocument(ctx, s.pool, docID, actor.UserID, actor.DepartmentCode)
}

func (s *Store) AckRemarks(ctx context.Context, actor Actor, docID string) (Document, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Document{}, err
	}
	defer tx.Rollback(ctx)
	doc, err := loadDocument(ctx, tx, docID, actor.UserID, actor.DepartmentCode)
	if err != nil {
		return Document{}, err
	}
	if doc.OrganizationCode != actor.OrganizationCode || doc.AuthorUserID != actor.UserID {
		return Document{}, ErrForbidden
	}
	if _, err := tx.Exec(ctx, `
		UPDATE step_tasks SET remark_addressed = true
		WHERE document_id = $1::uuid AND state = 'замечания'
	`, docID); err != nil {
		return Document{}, err
	}
	if err := writeAudit(ctx, tx, docID, actor.UserID, "замечания_учтены", "", "", "автор отметил отработку замечаний"); err != nil {
		return Document{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, err
	}
	return loadDocument(ctx, s.pool, docID, actor.UserID, actor.DepartmentCode)
}

func (s *Store) AddRouteParticipant(ctx context.Context, actor Actor, colleagues []Colleague, docID, kind, userID string) (Document, []Effect, error) {
	if kind != "approval" && kind != "acknowledgment" {
		return Document{}, nil, fmt.Errorf("%w: можно добавить только согласование (approval) или ознакомление (acknowledgment)", ErrInvalid)
	}
	if !sameOrgColleague(colleagues, actor.OrganizationCode, userID) {
		return Document{}, nil, ErrForbidden
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Document{}, nil, err
	}
	defer tx.Rollback(ctx)
	doc, err := loadDocument(ctx, tx, docID, actor.UserID, actor.DepartmentCode)
	if err != nil {
		return Document{}, nil, err
	}
	if doc.OrganizationCode != actor.OrganizationCode {
		return Document{}, nil, ErrForbidden
	}
	if doc.AuthorUserID != actor.UserID && !actor.canEdit() && !actor.has("manager") && !actor.hasContour(doc.BusinessDomain, ContourLevelManage) {
		return Document{}, nil, ErrForbidden
	}
	if doc.RouteInstanceID == nil || doc.Status == "Закрыт (в деле)" || doc.Status == "Отклонён" {
		return Document{}, nil, fmt.Errorf("%w: маршрут не запущен или закрыт", ErrConflict)
	}
	if err := checkNoSelfVisa(kind, doc.AuthorUserID, userID); err != nil {
		return Document{}, nil, err
	}

	var maxOrder int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(order_no), 0) FROM route_steps WHERE route_id = $1::uuid`, *doc.RouteInstanceID).Scan(&maxOrder); err != nil {
		return Document{}, nil, err
	}
	nextOrder := maxOrder + 1
	blocking := kind == "approval"
	var stepID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO route_steps (
			id, route_id, order_no, parallel_group, join_mode, stop_on_reject, allow_early_sign,
			per_file_approval, is_mandatory, is_order_locked, kind, is_blocking, sla_enabled,
			due_days, condition_rule, assignee_user_id
		)
		VALUES (gen_random_uuid(), $1::uuid, $2, 1, 'all', true, false, false, true, false, $3, $4, $4, 2, '', $5)
		RETURNING id::text
	`, *doc.RouteInstanceID, nextOrder, kind, blocking, userID).Scan(&stepID); err != nil {
		return Document{}, nil, err
	}

	var waiting bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM route_steps prev
			WHERE prev.route_id = $1::uuid
			  AND prev.order_no < $2
			  AND prev.is_blocking
			  AND NOT EXISTS (
				SELECT 1 FROM step_tasks pt
				WHERE pt.step_id = prev.id
				  AND pt.state IN ('согласовано', 'подписано', 'исполнено', 'ознакомлен', 'пропущен')
			  )
		)
	`, *doc.RouteInstanceID, nextOrder).Scan(&waiting); err != nil {
		return Document{}, nil, err
	}
	if err := writeAudit(ctx, tx, doc.ID, actor.UserID, "досыл", "", kind, "добавлен участник "+userID); err != nil {
		return Document{}, nil, err
	}
	if waiting {
		if err := tx.Commit(ctx); err != nil {
			return Document{}, nil, err
		}
		updated, err := loadDocument(ctx, s.pool, doc.ID, actor.UserID, actor.DepartmentCode)
		return updated, nil, err
	}

	assignee := userID
	deputyFor := ""
	if dep, ok, err := resolveActiveDeputy(ctx, tx, doc.OrganizationCode, userID); err != nil {
		return Document{}, nil, err
	} else if ok && !(isSelfVisaStep(kind) && dep == doc.AuthorUserID) {
		deputyFor = userID
		assignee = dep
	}

	var due *time.Time
	if blocking {
		stamp := sla.AddWorking(time.Now().UTC(), 2, doc.Importance)
		due = &stamp
	}
	var taskID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO step_tasks (id, step_id, document_id, version_id, assignee_user_id, deputy_for_user_id, state, started_at, plan_due_at)
		VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $3::uuid, $4, $5, 'назначена', now(), $6)
		RETURNING id::text
	`, stepID, doc.ID, doc.VersionID, assignee, deputyFor, due).Scan(&taskID); err != nil {
		return Document{}, nil, err
	}

	if err := writeAudit(ctx, tx, doc.ID, actor.UserID, "досыл", "", kind, "задача открыта, предыдущие этапы закрыты, участник "+userID); err != nil {
		return Document{}, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, nil, err
	}
	updated, err := loadDocument(ctx, s.pool, doc.ID, actor.UserID, actor.DepartmentCode)
	effects := []Effect{{Kind: "notification", DocumentID: doc.ID, TaskID: taskID, AssigneeID: assignee, Title: doc.Title, Due: derefTime(due)}}
	return updated, effects, err
}

func (s *Store) ReassignRoute(ctx context.Context, actor Actor, docID, taskID, stepID, userID string) (Document, error) {
	if userID == "" || (taskID == "" && stepID == "") {
		return Document{}, fmt.Errorf("%w: укажите шаг и сотрудника", ErrInvalid)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Document{}, err
	}
	defer tx.Rollback(ctx)
	doc, err := loadDocument(ctx, tx, docID, actor.UserID, actor.DepartmentCode)
	if err != nil {
		return Document{}, err
	}
	if doc.OrganizationCode != actor.OrganizationCode || doc.Status == "Закрыт (в деле)" {
		return Document{}, ErrForbidden
	}
	if doc.AuthorUserID != actor.UserID && !actor.canEdit() && !actor.has("manager") && !actor.hasContour(doc.BusinessDomain, ContourLevelManage) {
		return Document{}, ErrForbidden
	}
	if taskID != "" {
		var state, stepKind string
		err = tx.QueryRow(ctx, `
			SELECT t.state, s.kind FROM step_tasks t
			JOIN route_steps s ON s.id = t.step_id
			WHERE t.id = $1::uuid AND t.document_id = $2::uuid
		`, taskID, docID).Scan(&state, &stepKind)
		if errors.Is(err, pgx.ErrNoRows) {
			return Document{}, ErrNotFound
		}
		if err != nil {
			return Document{}, err
		}
		if state != "назначена" && state != "ожидает_подтверждения" {
			return Document{}, fmt.Errorf("%w: этот шаг уже завершён", ErrConflict)
		}
		if err := checkNoSelfVisa(stepKind, doc.AuthorUserID, userID); err != nil {
			return Document{}, err
		}
		if _, err = tx.Exec(ctx, `
			UPDATE step_tasks SET assignee_user_id = $2, deputy_for_user_id = '' WHERE id = $1::uuid
		`, taskID, userID); err != nil {
			return Document{}, err
		}
	}
	if stepID != "" {
		var routeID, stepKind string
		err = tx.QueryRow(ctx, `SELECT route_id::text, kind FROM route_steps WHERE id = $1::uuid`, stepID).Scan(&routeID, &stepKind)
		if errors.Is(err, pgx.ErrNoRows) {
			return Document{}, ErrNotFound
		}
		if err != nil {
			return Document{}, err
		}
		if doc.RouteInstanceID == nil || *doc.RouteInstanceID != routeID {
			return Document{}, ErrForbidden
		}
		if err := checkNoSelfVisa(stepKind, doc.AuthorUserID, userID); err != nil {
			return Document{}, err
		}
		if _, err = tx.Exec(ctx, `UPDATE route_steps SET assignee_user_id = $2, assignee_role = '' WHERE id = $1::uuid`, stepID, userID); err != nil {
			return Document{}, err
		}
		if _, err = tx.Exec(ctx, `
			UPDATE step_tasks SET assignee_user_id = $2
			WHERE step_id = $1::uuid AND document_id = $3::uuid AND state IN ('назначена', 'ожидает_подтверждения')
		`, stepID, userID, docID); err != nil {
			return Document{}, err
		}
	}
	if err := writeAudit(ctx, tx, docID, actor.UserID, "маршрут", "", userID, "смена исполнителя шага"); err != nil {
		return Document{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, err
	}
	return loadDocument(ctx, s.pool, docID, actor.UserID, actor.DepartmentCode)
}

func checkLinkPair(fromID, fromType, fromStatus, toID, toType, toStatus, linkType string) error {
	if fromID == "" || toID == "" || fromID == toID {
		return fmt.Errorf("%w: выберите другой документ", ErrInvalid)
	}
	if fromStatus == "Отклонён" || toStatus == "Отклонён" {
		return fmt.Errorf("%w: нельзя связывать отклонённый документ", ErrConflict)
	}
	switch linkType {
	case "допсоглашение":
		if fromType != "договор" || toType != "допсоглашение" {
			return fmt.Errorf("%w: доп. соглашение связывается только от договора к доп. соглашению", ErrInvalid)
		}
	case "пункт_протокола":
		if fromType != "протокол" {
			return fmt.Errorf("%w: пункт протокола можно связать только с протоколом", ErrInvalid)
		}
	case "ответ":
		isRespType := func(c string) bool {
			switch c {
			case "входящий", "исходящий", "ткп", "служебная_записка", "поручение":
				return true
			default:
				return false
			}
		}
		if !isRespType(fromType) || !isRespType(toType) {
			return fmt.Errorf("%w: ответ допустим только между входящим, исходящим, ТКП, служебной запиской или поручением", ErrInvalid)
		}
	case "основание", "приложение", "замена":
	default:
		return fmt.Errorf("%w: неизвестный тип связи %s", ErrInvalid, linkType)
	}
	return nil
}

func validateDocumentLink(ctx context.Context, q querier, fromDoc, toDoc Document, linkType string) error {
	if fromDoc.OrganizationCode != toDoc.OrganizationCode {
		return ErrForbidden
	}
	if err := checkLinkPair(fromDoc.ID, fromDoc.TypeCode, fromDoc.Status, toDoc.ID, toDoc.TypeCode, toDoc.Status, linkType); err != nil {
		return err
	}
	var createsCycle bool
	if err := q.QueryRow(ctx, `
		WITH RECURSIVE reach AS (
			SELECT $1::uuid AS doc_id
			UNION
			SELECT l.to_document_id
			FROM document_links l
			JOIN reach r ON l.from_document_id = r.doc_id
		)
		SELECT EXISTS (SELECT 1 FROM reach WHERE doc_id = $2::uuid)
	`, toDoc.ID, fromDoc.ID).Scan(&createsCycle); err != nil {
		return err
	}
	if createsCycle {
		return fmt.Errorf("%w: связь замыкает цикл в цепочке документов", ErrConflict)
	}
	return nil
}

func (s *Store) AddDocumentLink(ctx context.Context, actor Actor, docID, otherID, linkType, place string) (Document, error) {
	if place != "before" && place != "after" {
		return Document{}, fmt.Errorf("%w: укажите место связи before или after", ErrInvalid)
	}
	if otherID == "" || otherID == docID {
		return Document{}, fmt.Errorf("%w: выберите другой документ", ErrInvalid)
	}
	doc, err := s.GetDocument(ctx, actor, docID)
	if err != nil {
		return Document{}, err
	}
	if doc.AuthorUserID != actor.UserID && !actor.canEdit() && !actor.has("manager") && !actor.hasContour(doc.BusinessDomain, ContourLevelLead) {
		return Document{}, ErrForbidden
	}
	other, err := s.GetDocument(ctx, actor, otherID)
	if err != nil {
		return Document{}, err
	}
	fromDoc, toDoc := doc, other
	if place == "before" {
		fromDoc, toDoc = other, doc
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Document{}, err
	}
	defer tx.Rollback(ctx)
	if err := validateDocumentLink(ctx, tx, fromDoc, toDoc, linkType); err != nil {
		return Document{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO document_links (id, from_document_id, to_document_id, link_type, created_by)
		VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $3, $4)
	`, fromDoc.ID, toDoc.ID, linkType, actor.UserID); err != nil {
		if isUnique(err) {
			return Document{}, fmt.Errorf("%w: такая связь уже есть", ErrConflict)
		}
		return Document{}, err
	}
	if err := writeAudit(ctx, tx, doc.ID, actor.UserID, "связь", "", linkType, other.Title); err != nil {
		return Document{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, err
	}
	return s.GetDocument(ctx, actor, doc.ID)
}

func (s *Store) DeleteDocumentLink(ctx context.Context, actor Actor, docID, linkID string) (Document, error) {
	doc, err := s.GetDocument(ctx, actor, docID)
	if err != nil {
		return Document{}, err
	}
	if doc.AuthorUserID != actor.UserID && !actor.canEdit() && !actor.has("manager") && !actor.hasContour(doc.BusinessDomain, ContourLevelLead) {
		return Document{}, ErrForbidden
	}
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM document_links
		WHERE id = $1::uuid AND (from_document_id = $2::uuid OR to_document_id = $2::uuid)
	`, linkID, doc.ID)
	if err != nil {
		return Document{}, err
	}
	if tag.RowsAffected() == 0 {
		return Document{}, ErrNotFound
	}
	return s.GetDocument(ctx, actor, doc.ID)
}
