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

func (s *Store) ListSubtypes(ctx context.Context, typeCode string) ([]SubtypeInfo, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, type_code, code, name, business_domain, default_title, body_template, default_importance, COALESCE(default_service_name, '')
		FROM document_subtypes
		WHERE ($1::text = '' OR type_code = $1)
		ORDER BY type_code, name
	`, typeCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []SubtypeInfo
	for rows.Next() {
		var item SubtypeInfo
		if err := rows.Scan(
			&item.ID, &item.TypeCode, &item.Code, &item.Name, &item.BusinessDomain,
			&item.DefaultTitle, &item.BodyTemplate, &item.DefaultImportance, &item.DefaultServiceName,
		); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	if list == nil {
		list = []SubtypeInfo{}
	}
	return list, rows.Err()
}

func (s *Store) TasksSummary(ctx context.Context, actor Actor) (TaskSummary, error) {
	var sum TaskSummary
	deadline := sla.DeadlineWithin(time.Now().UTC(), 16)
	err := s.pool.QueryRow(ctx, `
		SELECT
		  COUNT(*) FILTER (WHERE t.assignee_user_id = $1 AND t.state = 'назначена' AND t.accepted_at IS NULL),
		  COUNT(*) FILTER (WHERE t.assignee_user_id = $1 AND t.state = 'назначена' AND t.plan_due_at IS NOT NULL AND t.plan_due_at <= $3),
		  COUNT(*) FILTER (WHERE t.assignee_user_id = $1 AND t.state = 'назначена'),
		  COUNT(*) FILTER (WHERE t.assignee_user_id = $1 AND t.state = 'назначена' AND s.kind = 'execution'),
		  COUNT(*) FILTER (WHERE d.author_user_id = $1 AND t.state = 'ожидает_подтверждения'),
		  COUNT(*) FILTER (WHERE t.assignee_user_id <> $1 AND t.state IN ('назначена', 'ожидает_подтверждения')
		    AND (d.author_user_id = $1 OR d.addressee_user_id = $1 OR ($4::bool AND t.plan_due_at IS NOT NULL))),
		  COUNT(*) FILTER (WHERE d.author_user_id = $1 AND t.state IN ('назначена', 'ожидает_подтверждения'))
		FROM step_tasks t
		JOIN documents d ON d.id = t.document_id
		JOIN route_steps s ON s.id = t.step_id
		WHERE d.organization_code = $2
		  AND NOT EXISTS (
		    SELECT 1 FROM route_steps prev
		    WHERE prev.route_id = s.route_id
		      AND prev.order_no < s.order_no
		      AND prev.is_blocking
		      AND NOT EXISTS (
		        SELECT 1 FROM step_tasks pt
		        WHERE pt.step_id = prev.id
		          AND pt.state IN ('согласовано', 'подписано', 'исполнено', 'ознакомлен', 'пропущен')
		      )
		  )
	`, actor.UserID, actor.OrganizationCode, deadline, actor.isController()).Scan(
		&sum.NewCount, &sum.OverdueCount, &sum.TotalToMe, &sum.ForExecution,
		&sum.PendingConfirm, &sum.OnControl, &sum.FromMe,
	)
	if err != nil {
		return sum, err
	}
	err = s.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM user_bookmarks b
		JOIN documents d ON d.id = b.document_id
		WHERE b.user_id = $1 AND d.organization_code = $2
	`, actor.UserID, actor.OrganizationCode).Scan(&sum.Bookmarks)
	return sum, err
}

func (s *Store) AcceptTask(ctx context.Context, actor Actor, taskID string) (Document, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Document{}, err
	}
	defer tx.Rollback(ctx)
	var docID, assignee, state string
	err = tx.QueryRow(ctx, `SELECT document_id::text, assignee_user_id, state FROM step_tasks WHERE id = $1::uuid`, taskID).
		Scan(&docID, &assignee, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return Document{}, err
	}
	if assignee != actor.UserID {
		return Document{}, ErrForbidden
	}
	if state != "назначена" {
		return Document{}, fmt.Errorf("%w: задача уже не активна", ErrConflict)
	}
	if _, err := tx.Exec(ctx, `UPDATE step_tasks SET accepted_at = COALESCE(accepted_at, now()) WHERE id = $1::uuid`, taskID); err != nil {
		return Document{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, err
	}
	return loadDocument(ctx, s.pool, docID, actor.UserID, actor.DepartmentCode)
}

func (s *Store) ConfirmExecutionTask(ctx context.Context, actor Actor, colleagues []Colleague, taskID, decision, comment string) (Document, []Effect, error) {
	if decision != "accept" && decision != "rework" {
		return Document{}, nil, fmt.Errorf("%w: решение должно быть accept или rework", ErrInvalid)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Document{}, nil, err
	}
	defer tx.Rollback(ctx)

	var docID, state, routeID string
	var orderNo int
	err = tx.QueryRow(ctx, `
		SELECT t.document_id::text, t.state, s.route_id::text, s.order_no
		FROM step_tasks t
		JOIN route_steps s ON s.id = t.step_id
		WHERE t.id = $1::uuid
	`, taskID).Scan(&docID, &state, &routeID, &orderNo)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, nil, ErrNotFound
	}
	if err != nil {
		return Document{}, nil, err
	}
	doc, err := loadDocument(ctx, tx, docID, actor.UserID, actor.DepartmentCode)
	if err != nil {
		return Document{}, nil, err
	}
	if doc.AuthorUserID != actor.UserID && !actor.has("clerk") {
		return Document{}, nil, ErrForbidden
	}
	if state != "ожидает_подтверждения" {
		return Document{}, nil, fmt.Errorf("%w: задача не ожидает подтверждения", ErrConflict)
	}

	var effects []Effect
	if decision == "rework" {
		if strings.TrimSpace(comment) == "" {
			return Document{}, nil, fmt.Errorf("%w: возврат результата требует комментарий", ErrInvalid)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE step_tasks SET state = 'назначена', decided_at = NULL, comment = $2 WHERE id = $1::uuid
		`, taskID, strings.TrimSpace(comment)); err != nil {
			return Document{}, nil, err
		}
		if err := writeAudit(ctx, tx, docID, actor.UserID, "поручение", "ожидает_подтверждения", "возврат_в_работу", comment); err != nil {
			return Document{}, nil, err
		}
	} else {
		if _, err := tx.Exec(ctx, `
			UPDATE step_tasks SET state = 'исполнено', decided_at = now(), comment = COALESCE(NULLIF($2, ''), comment)
			WHERE id = $1::uuid
		`, taskID, strings.TrimSpace(comment)); err != nil {
			return Document{}, nil, err
		}
		if err := writeAudit(ctx, tx, docID, actor.UserID, "решение", "ожидает_подтверждения", "исполнено", comment); err != nil {
			return Document{}, nil, err
		}
		done, err := blockingGroupDone(ctx, tx, routeID, orderNo)
		if err != nil {
			return Document{}, nil, err
		}
		if done {
			more, err := advance(ctx, tx, doc, colleagues, routeID, orderNo)
			if err != nil {
				return Document{}, nil, err
			}
			effects = append(effects, more...)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, nil, err
	}
	updated, err := loadDocument(ctx, s.pool, docID, actor.UserID, actor.DepartmentCode)
	return updated, effects, err
}

func (s *Store) BatchDecide(ctx context.Context, actor Actor, colleagues []Colleague, taskIDs []string, decision, reason string) (int, []Effect, error) {
	if len(taskIDs) == 0 {
		return 0, nil, fmt.Errorf("%w: не выбраны задачи", ErrInvalid)
	}
	for _, id := range taskIDs {
		var kind string
		err := s.pool.QueryRow(ctx, `
			SELECT s.kind FROM step_tasks t
			JOIN route_steps s ON s.id = t.step_id
			WHERE t.id = $1::uuid
		`, id).Scan(&kind)
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil, ErrNotFound
		}
		if err != nil {
			return 0, nil, err
		}
		if kind != "approval" {
			return 0, nil, fmt.Errorf("%w: пакетное решение доступно только для задач согласования", ErrInvalid)
		}
	}
	var allEffects []Effect
	done := 0
	for _, id := range taskIDs {
		_, eff, err := s.Decide(ctx, actor, id, Decision{Decision: decision, Reason: reason}, colleagues)
		if err != nil {
			return done, allEffects, err
		}
		done++
		allEffects = append(allEffects, eff...)
	}
	return done, allEffects, nil
}

type existingStepMeta struct {
	ID          string
	OrderNo     int
	Kind        string
	Mandatory   bool
	OrderLocked bool
}

func validateRouteStepsEdit(existing []existingStepMeta, next []RouteStepEditInput) error {
	if len(next) == 0 {
		return fmt.Errorf("%w: маршрут не может быть пустым", ErrInvalid)
	}
	nextByID := make(map[string]RouteStepEditInput, len(next))
	for _, s := range next {
		switch s.Kind {
		case "approval", "signing", "execution", "acknowledgment", "resolution", "review":
		default:
			return fmt.Errorf("%w: недопустимый тип шага %s", ErrInvalid, s.Kind)
		}
		if s.OrderNo <= 0 {
			return fmt.Errorf("%w: order_no должен быть >= 1", ErrInvalid)
		}
		if s.StepID != "" {
			nextByID[s.StepID] = s
		}
	}
	for _, ex := range existing {
		in, ok := nextByID[ex.ID]
		if ex.Mandatory && !ok {
			return fmt.Errorf("%w: нельзя удалить обязательный шаг (%s)", ErrConflict, ex.Kind)
		}
		if ex.OrderLocked && ok && in.OrderNo != ex.OrderNo {
			return fmt.Errorf("%w: нельзя менять порядок заблокированного шага (%s)", ErrConflict, ex.Kind)
		}
	}
	return nil
}

func (s *Store) EditDraftRouteSteps(ctx context.Context, actor Actor, docID string, steps []RouteStepEditInput) (Document, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Document{}, err
	}
	defer tx.Rollback(ctx)
	doc, err := loadDocument(ctx, tx, docID, actor.UserID, actor.DepartmentCode)
	if err != nil {
		return Document{}, err
	}
	if doc.OrganizationCode != actor.OrganizationCode || (doc.AuthorUserID != actor.UserID && !actor.canEdit() && !actor.hasContour(doc.BusinessDomain, ContourLevelManage)) {
		return Document{}, ErrForbidden
	}
	if doc.RouteStarted {
		return Document{}, fmt.Errorf("%w: редактировать шаги конструктора можно только до старта маршрута", ErrConflict)
	}

	var routeID string
	clonedFromTpl := map[string]string{}
	if doc.RouteInstanceID != nil {
		routeID = *doc.RouteInstanceID
	} else {
		var tplID string
		err := tx.QueryRow(ctx, `
			SELECT r.id::text FROM routes r
			JOIN document_types t ON t.id = r.type_id
			WHERE t.code = $1 AND r.is_template = true AND r.status = 'действует'
			LIMIT 1
		`, doc.TypeCode).Scan(&tplID)
		if err != nil {
			return Document{}, fmt.Errorf("%w: нет шаблона маршрута", ErrConflict)
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO routes (id, type_id, document_id, template_id, is_template, status)
			SELECT gen_random_uuid(), type_id, $2::uuid, id, false, 'черновик'
			FROM routes WHERE id = $1::uuid
			RETURNING id::text
		`, tplID, doc.ID).Scan(&routeID); err != nil {
			return Document{}, err
		}
		tplRows, err := tx.Query(ctx, `SELECT id::text FROM route_steps WHERE route_id = $1::uuid ORDER BY order_no, kind`, tplID)
		if err != nil {
			return Document{}, err
		}
		var tplStepIDs []string
		for tplRows.Next() {
			var sid string
			if err := tplRows.Scan(&sid); err != nil {
				tplRows.Close()
				return Document{}, err
			}
			tplStepIDs = append(tplStepIDs, sid)
		}
		tplRows.Close()
		for _, oldID := range tplStepIDs {
			var newID string
			if err := tx.QueryRow(ctx, `
				INSERT INTO route_steps (
					id, route_id, order_no, parallel_group, join_mode, stop_on_reject, allow_early_sign,
					per_file_approval, is_mandatory, is_order_locked, kind, caption, is_blocking, sla_enabled,
					due_days, condition_rule, assignee_role, assignee_user_id
				)
				SELECT gen_random_uuid(), $2::uuid, order_no, parallel_group, join_mode, stop_on_reject, allow_early_sign,
				       per_file_approval, is_mandatory, is_order_locked, kind, caption, is_blocking, sla_enabled,
				       due_days, condition_rule, assignee_role, assignee_user_id
				FROM route_steps WHERE id = $1::uuid
				RETURNING id::text
			`, oldID, routeID).Scan(&newID); err != nil {
				return Document{}, err
			}
			clonedFromTpl[oldID] = newID
		}
		if _, err := tx.Exec(ctx, `UPDATE documents SET route_instance_id = $2::uuid WHERE id = $1::uuid`, doc.ID, routeID); err != nil {
			return Document{}, err
		}
	}
	for i := range steps {
		if mapped, ok := clonedFromTpl[steps[i].StepID]; ok {
			steps[i].StepID = mapped
		}
	}

	exRows, err := tx.Query(ctx, `
		SELECT id::text, order_no, kind, is_mandatory, is_order_locked
		FROM route_steps WHERE route_id = $1::uuid
	`, routeID)
	if err != nil {
		return Document{}, err
	}
	var existing []existingStepMeta
	existingMap := map[string]existingStepMeta{}
	for exRows.Next() {
		var m existingStepMeta
		if err := exRows.Scan(&m.ID, &m.OrderNo, &m.Kind, &m.Mandatory, &m.OrderLocked); err != nil {
			exRows.Close()
			return Document{}, err
		}
		existing = append(existing, m)
		existingMap[m.ID] = m
	}
	exRows.Close()
	if err := exRows.Err(); err != nil {
		return Document{}, err
	}

	if err := validateRouteStepsEdit(existing, steps); err != nil {
		return Document{}, err
	}
	for _, sIn := range steps {
		if err := checkNoSelfVisa(sIn.Kind, doc.AuthorUserID, sIn.AssigneeUserID); err != nil {
			return Document{}, err
		}
	}

	keepIDs := map[string]bool{}
	for _, sIn := range steps {
		due := sIn.DueDays
		if due <= 0 && sIn.Kind != "acknowledgment" {
			due = 2
		}
		blocking := sIn.IsBlocking
		if sIn.Kind == "acknowledgment" {
			blocking = false
			due = 0
		}
		if _, ok := existingMap[sIn.StepID]; ok {
			keepIDs[sIn.StepID] = true
			if _, err := tx.Exec(ctx, `
				UPDATE route_steps
				SET order_no = $2, kind = $3, due_days = $4, assignee_role = NULLIF($5, ''), assignee_user_id = NULLIF($6, ''), is_blocking = $7, sla_enabled = $7
				WHERE id = $1::uuid AND route_id = $8::uuid
			`, sIn.StepID, sIn.OrderNo, sIn.Kind, due, sIn.AssigneeRole, sIn.AssigneeUserID, blocking, routeID); err != nil {
				return Document{}, err
			}
		} else {
			var newID string
			if err := tx.QueryRow(ctx, `
				INSERT INTO route_steps (
					id, route_id, order_no, parallel_group, join_mode, stop_on_reject, allow_early_sign,
					per_file_approval, is_mandatory, is_order_locked, kind, is_blocking, sla_enabled,
					due_days, condition_rule, assignee_role, assignee_user_id
				)
				VALUES (gen_random_uuid(), $1::uuid, $2, NULL, 'all', true, false, false, false, false, $3, $4, $4, $5, '', NULLIF($6, ''), NULLIF($7, ''))
				RETURNING id::text
			`, routeID, sIn.OrderNo, sIn.Kind, blocking, due, sIn.AssigneeRole, sIn.AssigneeUserID).Scan(&newID); err != nil {
				return Document{}, err
			}
			keepIDs[newID] = true
		}
	}
	for _, ex := range existing {
		if !keepIDs[ex.ID] {
			if _, err := tx.Exec(ctx, `DELETE FROM route_steps WHERE id = $1::uuid`, ex.ID); err != nil {
				return Document{}, err
			}
		}
	}

	if err := writeAudit(ctx, tx, doc.ID, actor.UserID, "маршрут", "", "настроен_до_старта", fmt.Sprintf("шагов: %d", len(steps))); err != nil {
		return Document{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, err
	}
	return loadDocument(ctx, s.pool, docID, actor.UserID, actor.DepartmentCode)
}

func (s *Store) ApplyDraftRouteTemplate(ctx context.Context, actor Actor, docID, templateID string) (Document, error) {
	if templateID == "" {
		return Document{}, fmt.Errorf("%w: выберите маршрут", ErrInvalid)
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
	if doc.OrganizationCode != actor.OrganizationCode || (doc.AuthorUserID != actor.UserID && !actor.canEdit() && !actor.hasContour(doc.BusinessDomain, ContourLevelManage)) {
		return Document{}, ErrForbidden
	}
	if doc.RouteStarted {
		return Document{}, fmt.Errorf("%w: маршрут уже запущен", ErrConflict)
	}
	var templateType string
	err = tx.QueryRow(ctx, `
		SELECT t.code
		FROM routes r
		JOIN document_types t ON t.id = r.type_id
		WHERE r.id = $1::uuid AND r.is_template = true AND r.status = 'действует'
	`, templateID).Scan(&templateType)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return Document{}, err
	}
	if templateType != doc.TypeCode {
		return Document{}, fmt.Errorf("%w: этот маршрут относится к другому виду документа", ErrInvalid)
	}
	var routeID string
	if doc.RouteInstanceID != nil {
		routeID = *doc.RouteInstanceID
	} else {
		if err := tx.QueryRow(ctx, `
			INSERT INTO routes (id, type_id, document_id, template_id, is_template, status)
			SELECT gen_random_uuid(), type_id, $2::uuid, id, false, 'черновик'
			FROM routes WHERE id = $1::uuid
			RETURNING id::text
		`, templateID, doc.ID).Scan(&routeID); err != nil {
			return Document{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE documents SET route_instance_id = $2::uuid WHERE id = $1::uuid`, doc.ID, routeID); err != nil {
			return Document{}, err
		}
	}
	var busy int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM step_tasks st
		JOIN route_steps s ON s.id = st.step_id
		WHERE s.route_id = $1::uuid
	`, routeID).Scan(&busy); err != nil {
		return Document{}, err
	}
	if busy > 0 {
		return Document{}, fmt.Errorf("%w: по маршруту уже есть задачи", ErrConflict)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM route_steps WHERE route_id = $1::uuid`, routeID); err != nil {
		return Document{}, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO route_steps (
			id, route_id, order_no, parallel_group, join_mode, stop_on_reject, allow_early_sign,
			per_file_approval, is_mandatory, is_order_locked, kind, caption, is_blocking, sla_enabled,
			due_days, condition_rule, assignee_role, assignee_user_id
		)
		SELECT gen_random_uuid(), $2::uuid, order_no, parallel_group, join_mode, stop_on_reject, allow_early_sign,
		       per_file_approval, is_mandatory, is_order_locked, kind, caption, is_blocking, sla_enabled,
		       due_days, condition_rule, assignee_role, assignee_user_id
		FROM route_steps WHERE route_id = $1::uuid
	`, templateID, routeID); err != nil {
		return Document{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE routes SET template_id = $2::uuid, status = 'черновик' WHERE id = $1::uuid`, routeID, templateID); err != nil {
		return Document{}, err
	}
	if err := writeAudit(ctx, tx, doc.ID, actor.UserID, "маршрут", "", "выбран_шаблон", templateType); err != nil {
		return Document{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, err
	}
	return loadDocument(ctx, s.pool, docID, actor.UserID, actor.DepartmentCode)
}

func (s *Store) ListBookmarks(ctx context.Context, actor Actor) ([]BookmarkItem, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT d.id::text, d.title, d.status, t.name, b.note_text, b.created_at
		FROM user_bookmarks b
		JOIN documents d ON d.id = b.document_id
		JOIN document_types t ON t.id = d.type_id
		WHERE b.user_id = $1 AND d.organization_code = $2
		ORDER BY b.created_at DESC
	`, actor.UserID, actor.OrganizationCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []BookmarkItem
	for rows.Next() {
		var item BookmarkItem
		if err := rows.Scan(&item.DocumentID, &item.Title, &item.Status, &item.TypeName, &item.NoteText, &item.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	if list == nil {
		list = []BookmarkItem{}
	}
	return list, rows.Err()
}

func (s *Store) SaveBookmark(ctx context.Context, actor Actor, docID, note string) (Document, error) {
	doc, err := loadDocument(ctx, s.pool, docID, actor.UserID, actor.DepartmentCode)
	if err != nil {
		return Document{}, err
	}
	if err := s.ensureRead(ctx, s.pool, actor, doc); err != nil {
		return Document{}, err
	}
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO user_bookmarks (user_id, document_id, note_text)
		VALUES ($1, $2::uuid, $3)
		ON CONFLICT (user_id, document_id) DO UPDATE SET note_text = EXCLUDED.note_text
	`, actor.UserID, docID, strings.TrimSpace(note)); err != nil {
		return Document{}, err
	}
	return loadDocument(ctx, s.pool, docID, actor.UserID, actor.DepartmentCode)
}

func (s *Store) DeleteBookmark(ctx context.Context, actor Actor, docID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM user_bookmarks WHERE user_id = $1 AND document_id = $2::uuid`, actor.UserID, docID)
	return err
}

func (s *Store) GrantAccess(ctx context.Context, actor Actor, colleagues []Colleague, docID, targetUserID string, days int) (Document, error) {
	if targetUserID == "" || !sameOrgColleague(colleagues, actor.OrganizationCode, targetUserID) {
		return Document{}, fmt.Errorf("%w: укажите сотрудника вашей организации", ErrInvalid)
	}
	if days <= 0 || days > 365 {
		days = 14
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
	if doc.OrganizationCode != actor.OrganizationCode || (doc.AuthorUserID != actor.UserID && !actor.canEdit() && !actor.has("manager") && !actor.hasContour(doc.BusinessDomain, ContourLevelManage)) {
		return Document{}, ErrForbidden
	}
	expiresAt := time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour)
	if _, err := tx.Exec(ctx, `
		INSERT INTO document_grants (id, document_id, user_id, granted_by, expires_at)
		VALUES (gen_random_uuid(), $1::uuid, $2, $3, $4)
	`, docID, targetUserID, actor.UserID, expiresAt); err != nil {
		return Document{}, err
	}
	if err := writeAudit(ctx, tx, docID, actor.UserID, "право", "", targetUserID, fmt.Sprintf("временный доступ на %d дн.", days)); err != nil {
		return Document{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, err
	}
	return loadDocument(ctx, s.pool, docID, actor.UserID, actor.DepartmentCode)
}

func (s *Store) CompleteByAuthor(ctx context.Context, actor Actor, docID string) (Document, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Document{}, err
	}
	defer tx.Rollback(ctx)
	doc, err := loadDocument(ctx, tx, docID, actor.UserID, actor.DepartmentCode)
	if err != nil {
		return Document{}, err
	}
	if doc.OrganizationCode != actor.OrganizationCode || (doc.AuthorUserID != actor.UserID && !actor.canEdit()) {
		return Document{}, ErrForbidden
	}
	if doc.Status == "Закрыт (в деле)" || doc.Status == "Отклонён" {
		return Document{}, fmt.Errorf("%w: документ уже закрыт", ErrConflict)
	}
	var activeBlocking int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM step_tasks t
		JOIN route_steps s ON s.id = t.step_id
		WHERE t.document_id = $1::uuid AND s.is_blocking = true AND t.state IN ('назначена', 'ожидает_подтверждения')
	`, docID).Scan(&activeBlocking); err != nil {
		return Document{}, err
	}
	if activeBlocking > 0 {
		return Document{}, fmt.Errorf("%w: есть незавершённые обязательные шаги/поручения", ErrConflict)
	}
	if _, err := tx.Exec(ctx, `UPDATE documents SET completed_by_author_at = now(), updated_at = now() WHERE id = $1::uuid`, docID); err != nil {
		return Document{}, err
	}
	if err := writeAudit(ctx, tx, docID, actor.UserID, "переход", doc.Status, doc.Status, "автор завершил ведение (готов в дело)"); err != nil {
		return Document{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, err
	}
	return loadDocument(ctx, s.pool, docID, actor.UserID, actor.DepartmentCode)
}
