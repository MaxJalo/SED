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

type Decision struct {
	Decision           string `json:"decision"`
	Reason             string `json:"reason"`
	RecommendationText string `json:"recommendation_text"`
}

func (s *Store) RouteAction(ctx context.Context, actor Actor, documentID, action, reason string, colleagues []Colleague) (Document, []Effect, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Document{}, nil, err
	}
	defer tx.Rollback(ctx)
	doc, err := loadDocument(ctx, tx, documentID, actor.UserID, actor.DepartmentCode)
	if err != nil {
		return Document{}, nil, err
	}
	if doc.OrganizationCode != actor.OrganizationCode || doc.Status == "Закрыт (в деле)" {
		return Document{}, nil, ErrForbidden
	}
	var effects []Effect
	switch action {
	case "start":
		effects, err = startRoute(ctx, tx, actor, doc, colleagues)
	case "recall":
		err = recallRoute(ctx, tx, actor, doc, reason)
	case "pause":
		err = setPause(ctx, tx, actor, doc, true)
	case "resume":
		err = setPause(ctx, tx, actor, doc, false)
	case "force_sign":
		err = forceSign(ctx, tx, actor, doc)
	default:
		err = fmt.Errorf("%w: неизвестное действие маршрута", ErrInvalid)
	}
	if err != nil {
		return Document{}, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, nil, err
	}
	updated, err := loadDocument(ctx, s.pool, documentID, actor.UserID, actor.DepartmentCode)
	return updated, effects, err
}

func isSelfVisaStep(kind string) bool {
	switch kind {
	case "approval", "signing", "resolution", "review":
		return true
	default:
		return false
	}
}

func checkNoSelfVisa(kind, authorID, candidateID string) error {
	if candidateID != "" && authorID != "" && candidateID == authorID && isSelfVisaStep(kind) {
		return fmt.Errorf("%w: автор не может визировать, рассматривать или подписывать собственный документ", ErrConflict)
	}
	return nil
}

func cloneDraftRouteOnCreate(ctx context.Context, tx pgx.Tx, docID, typeCode, authorID, addresseeUserID string, colleagues []Colleague) error {
	var templateID string
	err := tx.QueryRow(ctx, `
		SELECT r.id::text
		FROM routes r
		JOIN document_types t ON t.id = r.type_id
		WHERE t.code = $1 AND r.is_template = true AND r.status = 'действует'
		LIMIT 1
	`, typeCode).Scan(&templateID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	var routeID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO routes (id, type_id, document_id, template_id, is_template, status)
		SELECT gen_random_uuid(), type_id, $2::uuid, id, false, 'черновик'
		FROM routes WHERE id = $1::uuid
		RETURNING id::text
	`, templateID, docID).Scan(&routeID); err != nil {
		return err
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
		return err
	}
	mgr := findAuthorManager(colleagues, authorID)
	if mgr != "" && mgr != authorID {
		if _, err := tx.Exec(ctx, `
			UPDATE route_steps
			SET assignee_user_id = $2
			WHERE route_id = $1::uuid AND order_no = 1 AND assignee_role = 'manager' AND COALESCE(assignee_user_id, '') = ''
		`, routeID, mgr); err != nil {
			return err
		}
	} else if len(colleagues) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE route_steps
			SET condition_rule = 'skip_no_manager'
			WHERE route_id = $1::uuid AND order_no = 1 AND assignee_role = 'manager' AND COALESCE(assignee_user_id, '') = '' AND COALESCE(condition_rule, '') = ''
		`, routeID); err != nil {
			return err
		}
	}
	if addresseeUserID != "" {
		if _, err := tx.Exec(ctx, `
			UPDATE route_steps
			SET assignee_user_id = $2
			WHERE route_id = $1::uuid
			  AND COALESCE(assignee_user_id, '') = ''
			  AND (kind = 'execution' OR (kind = 'resolution' AND $2 <> $3))
		`, routeID, addresseeUserID, authorID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE documents SET route_instance_id = $2::uuid WHERE id = $1::uuid`, docID, routeID); err != nil {
		return err
	}
	return nil
}

func validateDocumentStart(ctx context.Context, tx pgx.Tx, doc Document, colleagues []Colleague, routeID string) error {
	if err := validateRequiredFields(CreateInput{
		TypeCode:            doc.TypeCode,
		Title:               doc.Title,
		Body:                doc.Body,
		AddresseeUserID:     doc.AddresseeUserID,
		AddresseeDepartment: doc.AddresseeDepartment,
		CounterpartyName:    doc.CounterpartyName,
		ServiceName:         doc.ServiceName,
	}); err != nil {
		return err
	}
	if err := validateFormatFields(doc.CounterpartyINN, doc.Amount); err != nil {
		return err
	}
	if (doc.TypeCode == "договор" || doc.TypeCode == "счет") && (doc.Amount == nil || *doc.Amount <= 0) {
		return fmt.Errorf("%w: укажите сумму больше нуля для запуска маршрута", ErrInvalid)
	}
	if doc.TypeCode == "допсоглашение" {
		var hasContract bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM document_links l
				JOIN documents p ON p.id = l.from_document_id
				JOIN document_types t ON t.id = p.type_id
				WHERE l.to_document_id = $1::uuid AND l.link_type = 'допсоглашение' AND t.code = 'договор'
			)
		`, doc.ID).Scan(&hasContract); err != nil {
			return err
		}
		if !hasContract {
			return fmt.Errorf("%w: доп. соглашение должно быть связано с основным договором", ErrInvalid)
		}
	}
	rows, err := tx.Query(ctx, `
		SELECT id::text, kind, is_blocking, is_mandatory, sla_enabled, COALESCE(due_days, 3), condition_rule,
		       COALESCE(assignee_role, ''), COALESCE(assignee_user_id, '')
		FROM route_steps
		WHERE route_id = $1::uuid
		ORDER BY order_no, kind
	`, routeID)
	if err != nil {
		return err
	}
	var steps []stepRow
	for rows.Next() {
		var step stepRow
		if err := rows.Scan(&step.ID, &step.Kind, &step.Blocking, &step.Mandatory, &step.SLA, &step.DueDays, &step.Condition, &step.AssigneeRole, &step.AssigneeUser); err != nil {
			rows.Close()
			return err
		}
		if conditionMet(step.Condition, doc) {
			steps = append(steps, step)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, step := range steps {
		assignee, err := resolveStepAssignee(step, doc, colleagues)
		if err != nil {
			return err
		}
		if err := checkNoSelfVisa(step.Kind, doc.AuthorUserID, assignee); err != nil {
			return err
		}
	}
	return nil
}

func startRoute(ctx context.Context, tx pgx.Tx, actor Actor, doc Document, colleagues []Colleague) ([]Effect, error) {
	if doc.AuthorUserID != actor.UserID {
		return nil, ErrForbidden
	}
	switch doc.Status {
	case "Черновик", "Возвращён на доработку", "Отозван автором", "Зарегистрирован":
	default:
		return nil, fmt.Errorf("%w: маршрут нельзя запустить из статуса %s", ErrConflict, doc.Status)
	}
	var instanceID string
	if doc.RouteInstanceID != nil {
		var rStatus string
		if err := tx.QueryRow(ctx, `SELECT status FROM routes WHERE id = $1::uuid`, *doc.RouteInstanceID).Scan(&rStatus); err == nil && rStatus == "черновик" {
			instanceID = *doc.RouteInstanceID
			if _, err := tx.Exec(ctx, `UPDATE routes SET status = 'идёт' WHERE id = $1::uuid`, instanceID); err != nil {
				return nil, err
			}
		}
	}
	if instanceID == "" {
		var templateID string
		err := tx.QueryRow(ctx, `
			SELECT r.id::text
			FROM routes r
			JOIN document_types t ON t.id = r.type_id
			WHERE t.code = $1 AND r.is_template = true AND r.status = 'действует'
			LIMIT 1
		`, doc.TypeCode).Scan(&templateID)
		if err != nil {
			return nil, fmt.Errorf("%w: нет шаблона маршрута", ErrConflict)
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO routes (id, type_id, document_id, template_id, is_template, status)
			SELECT gen_random_uuid(), type_id, $2::uuid, id, false, 'идёт'
			FROM routes WHERE id = $1::uuid
			RETURNING id::text
		`, templateID, doc.ID).Scan(&instanceID); err != nil {
			return nil, err
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
		`, templateID, instanceID); err != nil {
			return nil, err
		}
	}
	if err := validateDocumentStart(ctx, tx, doc, colleagues, instanceID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE documents
		SET route_instance_id = $2::uuid, route_started = true, status = 'На рассмотрении / согласовании', updated_at = now()
		WHERE id = $1::uuid
	`, doc.ID, instanceID); err != nil {
		return nil, err
	}
	if err := writeAudit(ctx, tx, doc.ID, actor.UserID, "переход", doc.Status, "На рассмотрении / согласовании", ""); err != nil {
		return nil, err
	}
	doc.RouteInstanceID = &instanceID
	doc.Status = "На рассмотрении / согласовании"
	doc.RouteStarted = true
	if err := resumeChildRoutes(ctx, tx, doc.ID, actor.UserID); err != nil {
		return nil, err
	}
	var first int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MIN(order_no), 0) FROM route_steps WHERE route_id = $1::uuid`, instanceID).Scan(&first); err != nil {
		return nil, err
	}
	return openOrder(ctx, tx, doc, colleagues, first)
}

func recallRoute(ctx context.Context, tx pgx.Tx, actor Actor, doc Document, reason string) error {
	if doc.AuthorUserID != actor.UserID {
		return ErrForbidden
	}
	if reason == "" {
		return fmt.Errorf("%w: отзыв требует причину", ErrInvalid)
	}
	if doc.Status != "На рассмотрении / согласовании" && doc.Status != "На подписи" {
		return fmt.Errorf("%w: отозвать можно только запущенный маршрут", ErrConflict)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE step_tasks SET state = 'снята', decided_at = now()
		WHERE document_id = $1::uuid AND state = 'назначена'
	`, doc.ID); err != nil {
		return err
	}
	if doc.RouteInstanceID != nil {
		if _, err := tx.Exec(ctx, `UPDATE routes SET status = 'завершён' WHERE id = $1::uuid`, *doc.RouteInstanceID); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE documents SET status = 'Отозван автором', updated_at = now() WHERE id = $1::uuid`, doc.ID); err != nil {
		return err
	}
	if err := pauseChildRoutes(ctx, tx, doc.ID, actor.UserID, "отзыв родителя"); err != nil {
		return err
	}
	return writeAudit(ctx, tx, doc.ID, actor.UserID, "переход", doc.Status, "Отозван автором", reason)
}

func setPause(ctx context.Context, tx pgx.Tx, actor Actor, doc Document, pause bool) error {
	if !actor.canPause() {
		return ErrForbidden
	}
	if doc.RouteInstanceID == nil {
		return fmt.Errorf("%w: маршрут не запущен", ErrConflict)
	}
	status := "идёт"
	if pause {
		status = "Приостановлено"
	}
	if _, err := tx.Exec(ctx, `UPDATE routes SET status = $2 WHERE id = $1::uuid`, *doc.RouteInstanceID, status); err != nil {
		return err
	}
	return writeAudit(ctx, tx, doc.ID, actor.UserID, "переход", "", status, "")
}

func forceSign(ctx context.Context, tx pgx.Tx, actor Actor, doc Document) error {
	if doc.RouteInstanceID == nil {
		return ErrConflict
	}
	var allowed bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM route_steps WHERE route_id = $1::uuid AND allow_early_sign = true
		)
	`, *doc.RouteInstanceID).Scan(&allowed); err != nil {
		return err
	}
	if !allowed {
		return fmt.Errorf("%w: досрочная подпись выключена в шаблоне", ErrConflict)
	}
	if _, err := tx.Exec(ctx, `UPDATE documents SET status = 'На подписи', updated_at = now() WHERE id = $1::uuid`, doc.ID); err != nil {
		return err
	}
	return writeAudit(ctx, tx, doc.ID, actor.UserID, "переход", doc.Status, "На подписи", "досрочно")
}

func (s *Store) Decide(ctx context.Context, actor Actor, taskID string, in Decision, colleagues []Colleague) (Document, []Effect, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Document{}, nil, err
	}
	defer tx.Rollback(ctx)

	var docID, kind, state, routeID string
	var stopOnReject, blocking bool
	var orderNo int
	err = tx.QueryRow(ctx, `
		SELECT t.document_id::text, t.state, s.kind, s.route_id::text, s.order_no, s.stop_on_reject, s.is_blocking
		FROM step_tasks t
		JOIN route_steps s ON s.id = t.step_id
		WHERE t.id = $1::uuid AND t.assignee_user_id = $2
	`, taskID, actor.UserID).Scan(&docID, &state, &kind, &routeID, &orderNo, &stopOnReject, &blocking)
	if err != nil {
		return Document{}, nil, ErrNotFound
	}
	if state != "назначена" {
		return Document{}, nil, fmt.Errorf("%w: задача уже закрыта", ErrConflict)
	}
	var routeStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM routes WHERE id = $1::uuid`, routeID).Scan(&routeStatus); err != nil {
		return Document{}, nil, err
	}
	if routeStatus == "Приостановлено" {
		return Document{}, nil, fmt.Errorf("%w: маршрут на паузе", ErrConflict)
	}
	doc, err := loadDocument(ctx, tx, docID, actor.UserID, actor.DepartmentCode)
	if err != nil {
		return Document{}, nil, err
	}
	if doc.Status == "Закрыт (в деле)" {
		return Document{}, nil, ErrForbidden
	}

	nextState, err := decisionState(kind, in)
	if err != nil {
		return Document{}, nil, err
	}
	if kind == "execution" && in.Decision == "approve" {
		if strings.TrimSpace(in.Reason) == "" && strings.TrimSpace(in.RecommendationText) == "" {
			var hasFile bool
			if err := tx.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM files
					WHERE document_id = $1::uuid AND version_id = $2::uuid AND uploaded_by = $3
				)
			`, doc.ID, doc.VersionID, actor.UserID).Scan(&hasFile); err != nil {
				return Document{}, nil, err
			}
			if !hasFile {
				return Document{}, nil, fmt.Errorf("%w: укажите отчёт об исполнении или прикрепите файл результата", ErrInvalid)
			}
		}
	}
	if kind == "execution" && in.Decision == "approve" && doc.AuthorUserID != actor.UserID {
		nextState = "ожидает_подтверждения"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE step_tasks
		SET state = $2, decided_at = now(), comment = NULLIF($3, '')
		WHERE id = $1::uuid
	`, taskID, nextState, firstNonEmpty(in.RecommendationText, in.Reason)); err != nil {
		return Document{}, nil, err
	}

	var effects []Effect
	switch in.Decision {
	case "return":
		if kind == "review" || kind == "resolution" {
			effects, err = reopenResolution(ctx, tx, doc, colleagues, routeID)
		} else {
			err = stopDocument(ctx, tx, actor, doc, routeID, "Возвращён на доработку", in.Reason)
		}
	case "reject":
		if kind == "review" || kind == "resolution" {
			effects, err = reopenResolution(ctx, tx, doc, colleagues, routeID)
		} else if stopOnReject {
			err = stopDocument(ctx, tx, actor, doc, routeID, "Отклонён", in.Reason)
		} else if err = clearGroup(ctx, tx, routeID, orderNo, false); err == nil {
			effects, err = advance(ctx, tx, doc, colleagues, routeID, orderNo)
		}
	default:
		if kind == "signing" && in.Decision == "sign" {
			if _, err = tx.Exec(ctx, `UPDATE documents SET status = 'Подписан', updated_at = now() WHERE id = $1::uuid`, doc.ID); err != nil {
				return Document{}, nil, err
			}
			doc.Status = "Подписан"
			if doc.CustomerContract {
				effects = append(effects, Effect{Kind: "external_order", DocumentID: doc.ID, AuthorID: doc.AuthorUserID, Title: doc.Title, RegNumber: deref(doc.RegNumber)})
			}
		}
		if kind == "execution" && in.Decision == "approve" {
			if _, err = tx.Exec(ctx, `UPDATE documents SET status = 'На исполнении', updated_at = now() WHERE id = $1::uuid`, doc.ID); err != nil {
				return Document{}, nil, err
			}
			doc.Status = "На исполнении"
			if nextState == "ожидает_подтверждения" {
				effects = append(effects, Effect{
					Kind: "notification", DocumentID: doc.ID, TaskID: taskID, AssigneeID: doc.AuthorUserID, Title: "Ожидает подтверждения: " + doc.Title,
				})
				break
			}
		}
		done, doneErr := blockingGroupDone(ctx, tx, routeID, orderNo)
		if doneErr != nil {
			return Document{}, nil, doneErr
		}
		if done {
			more, advErr := advance(ctx, tx, doc, colleagues, routeID, orderNo)
			if advErr != nil {
				return Document{}, nil, advErr
			}
			effects = append(effects, more...)
		}
	}
	if err != nil {
		return Document{}, nil, err
	}
	if err := writeAudit(ctx, tx, doc.ID, actor.UserID, "решение", state, nextState, in.Reason); err != nil {
		return Document{}, nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, nil, err
	}
	updated, err := loadDocument(ctx, s.pool, doc.ID, actor.UserID, actor.DepartmentCode)
	return updated, effects, err
}

func decisionState(kind string, in Decision) (string, error) {
	switch kind {
	case "approval", "resolution":
		switch in.Decision {
		case "approve":
			return "согласовано", nil
		case "return":
			if in.Reason == "" {
				return "", fmt.Errorf("%w: возврат требует причину", ErrInvalid)
			}
			return "замечания", nil
		case "reject":
			if in.Reason == "" {
				return "", fmt.Errorf("%w: отказ требует причину", ErrInvalid)
			}
			return "отклонено", nil
		}
	case "review":
		switch in.Decision {
		case "recommend":
			if in.RecommendationText == "" {
				return "", fmt.Errorf("%w: рекомендация обязательна", ErrInvalid)
			}
			return "согласовано", nil
		case "return", "reject":
			if in.Reason == "" {
				return "", fmt.Errorf("%w: нужна причина", ErrInvalid)
			}
			return "замечания", nil
		}
	case "signing":
		switch in.Decision {
		case "sign":
			return "подписано", nil
		case "return":
			if in.Reason == "" {
				return "", fmt.Errorf("%w: возврат требует причину", ErrInvalid)
			}
			return "замечания", nil
		case "reject":
			if in.Reason == "" {
				return "", fmt.Errorf("%w: отказ требует причину", ErrInvalid)
			}
			return "отклонено", nil
		}
	case "execution":
		if in.Decision == "approve" {
			return "исполнено", nil
		}
	case "acknowledgment":
		if in.Decision == "ack" {
			return "ознакомлен", nil
		}
	}
	return "", fmt.Errorf("%w: решение %s не подходит шагу %s", ErrInvalid, in.Decision, kind)
}

func stopDocument(ctx context.Context, tx pgx.Tx, actor Actor, doc Document, routeID, status, reason string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE step_tasks SET state = 'снята', decided_at = now()
		WHERE document_id = $1::uuid AND state = 'назначена'
	`, doc.ID); err != nil {
		return err
	}
	routeStatus := "завершён"
	if status == "Отклонён" {
		routeStatus = "отклонён"
	}
	if _, err := tx.Exec(ctx, `UPDATE routes SET status = $2 WHERE id = $1::uuid`, routeID, routeStatus); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE documents SET status = $2, updated_at = now() WHERE id = $1::uuid`, doc.ID, status); err != nil {
		return err
	}
	if status == "Возвращён на доработку" {
		if err := pauseChildRoutes(ctx, tx, doc.ID, actor.UserID, "возврат родителя"); err != nil {
			return err
		}
	}
	return writeAudit(ctx, tx, doc.ID, actor.UserID, "переход", doc.Status, status, reason)
}

func reopenResolution(ctx context.Context, tx pgx.Tx, doc Document, colleagues []Colleague, routeID string) ([]Effect, error) {
	if _, err := tx.Exec(ctx, `
		UPDATE step_tasks SET state = 'снята', decided_at = now()
		WHERE document_id = $1::uuid AND state = 'назначена'
	`, doc.ID); err != nil {
		return nil, err
	}
	var orderNo int
	err := tx.QueryRow(ctx, `
		SELECT order_no FROM route_steps
		WHERE route_id = $1::uuid AND kind = 'resolution'
		ORDER BY order_no LIMIT 1
	`, routeID).Scan(&orderNo)
	if err != nil {
		return nil, fmt.Errorf("%w: нет шага резолюции для возврата", ErrConflict)
	}
	return openOrder(ctx, tx, doc, colleagues, orderNo)
}

func advance(ctx context.Context, tx pgx.Tx, doc Document, colleagues []Colleague, routeID string, orderNo int) ([]Effect, error) {
	var next *int
	err := tx.QueryRow(ctx, `
		SELECT MIN(order_no) FROM route_steps WHERE route_id = $1::uuid AND order_no > $2
	`, routeID, orderNo).Scan(&next)
	if err != nil {
		return nil, err
	}
	if next == nil {
		_, err := tx.Exec(ctx, `UPDATE routes SET status = 'завершён' WHERE id = $1::uuid AND status = 'идёт'`, routeID)
		return nil, err
	}
	return openOrder(ctx, tx, doc, colleagues, *next)
}

func blockingGroupDone(ctx context.Context, tx pgx.Tx, routeID string, orderNo int) (bool, error) {
	var waiting int
	err := tx.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM step_tasks t
		JOIN route_steps s ON s.id = t.step_id
		WHERE s.route_id = $1::uuid AND s.order_no = $2 AND s.is_blocking = true AND t.state = 'назначена'
	`, routeID, orderNo).Scan(&waiting)
	return waiting == 0, err
}

func clearGroup(ctx context.Context, tx pgx.Tx, routeID string, orderNo int, blockingOnly bool) error {
	_, err := tx.Exec(ctx, `
		UPDATE step_tasks t
		SET state = 'снята', decided_at = now()
		FROM route_steps s
		WHERE t.step_id = s.id AND s.route_id = $1::uuid AND s.order_no = $2
		  AND t.state = 'назначена' AND ($3::bool = false OR s.is_blocking = true)
	`, routeID, orderNo, blockingOnly)
	return err
}

type stepRow struct {
	ID           string
	Kind         string
	Blocking     bool
	Mandatory    bool
	SLA          bool
	DueDays      int
	Condition    string
	AssigneeRole string
	AssigneeUser string
}

func openOrder(ctx context.Context, tx pgx.Tx, doc Document, colleagues []Colleague, orderNo int) ([]Effect, error) {
	if orderNo == 0 {
		return nil, nil
	}
	rows, err := tx.Query(ctx, `
		SELECT id::text, kind, is_blocking, is_mandatory, sla_enabled, COALESCE(due_days, 3), condition_rule,
		       COALESCE(assignee_role, ''), COALESCE(assignee_user_id, '')
		FROM route_steps
		WHERE route_id = $1::uuid AND order_no = $2
	`, *doc.RouteInstanceID, orderNo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var steps []stepRow
	for rows.Next() {
		var step stepRow
		if err := rows.Scan(&step.ID, &step.Kind, &step.Blocking, &step.Mandatory, &step.SLA, &step.DueDays, &step.Condition, &step.AssigneeRole, &step.AssigneeUser); err != nil {
			return nil, err
		}
		if conditionMet(step.Condition, doc) {
			steps = append(steps, step)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(steps) == 0 {
		return advance(ctx, tx, doc, colleagues, *doc.RouteInstanceID, orderNo)
	}
	var effects []Effect
	blocking := false
	for _, step := range steps {
		assignee, err := resolveStepAssignee(step, doc, colleagues)
		if err != nil {
			return nil, err
		}

		// Вытеснение дублирующих участников (1С:ДО 3.0):
		// если сотрудник на шаге approval уже является подписантом в этом же маршруте и шаг не обязательный, пропускаем.
		if step.Kind == "approval" && !step.Mandatory {
			dup, err := willSignInRoute(ctx, tx, *doc.RouteInstanceID, orderNo, doc, colleagues, assignee)
			if err != nil {
				return nil, err
			}
			if dup {
				if _, err := tx.Exec(ctx, `
					INSERT INTO step_tasks (id, step_id, document_id, version_id, assignee_user_id, state, started_at, decided_at, comment)
					VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $3::uuid, $4, 'пропущен', now(), now(), 'вытеснено подписанием')
				`, step.ID, doc.ID, doc.VersionID, assignee); err != nil {
					return nil, err
				}
				continue
			}
		}

		// Серверные замещения: проверяем активное замещение на текущую дату.
		deputyFor := ""
		if dep, ok, err := resolveActiveDeputy(ctx, tx, doc.OrganizationCode, assignee); err != nil {
			return nil, err
		} else if ok && !(isSelfVisaStep(step.Kind) && dep == doc.AuthorUserID) {
			deputyFor = assignee
			assignee = dep
		}

		var due *time.Time
		if step.SLA && step.Blocking {
			stamp := sla.AddWorking(time.Now().UTC(), step.DueDays, doc.Importance)
			due = &stamp
		}
		var taskID string
		err = tx.QueryRow(ctx, `
			SELECT id::text FROM step_tasks
			WHERE step_id = $1::uuid AND state = 'назначена'
			LIMIT 1
		`, step.ID).Scan(&taskID)
		if err == nil {
			if _, err = tx.Exec(ctx, `
				UPDATE step_tasks
				SET started_at = now(), plan_due_at = $2, assignee_user_id = $3, deputy_for_user_id = $4
				WHERE id = $1::uuid
			`, taskID, due, assignee, deputyFor); err != nil {
				return nil, err
			}
		} else if errors.Is(err, pgx.ErrNoRows) {
			if err = tx.QueryRow(ctx, `
				INSERT INTO step_tasks (id, step_id, document_id, version_id, assignee_user_id, deputy_for_user_id, state, started_at, plan_due_at)
				VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $3::uuid, $4, $5, 'назначена', now(), $6)
				RETURNING id::text
			`, step.ID, doc.ID, doc.VersionID, assignee, deputyFor, due).Scan(&taskID); err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
		effects = append(effects, Effect{
			Kind: "notification", DocumentID: doc.ID, TaskID: taskID, AssigneeID: assignee, Title: doc.Title, Due: derefTime(due),
		})
		if step.Kind == "execution" {
			effects = append(effects, Effect{
				Kind: "platform_task", DocumentID: doc.ID, TaskID: taskID, AssigneeID: assignee, AuthorID: doc.AuthorUserID, Title: doc.Title, Due: derefTime(due),
			})
		}
		if step.Blocking {
			blocking = true
		}
	}
	status := doc.Status
	switch {
	case containsKind(steps, "signing"):
		status = "На подписи"
	case containsKind(steps, "execution"):
		status = "На исполнении"
	case containsKind(steps, "approval") || containsKind(steps, "resolution") || containsKind(steps, "review"):
		status = "На рассмотрении / согласовании"
	}
	if status != doc.Status {
		if _, err := tx.Exec(ctx, `UPDATE documents SET status = $2, updated_at = now() WHERE id = $1::uuid`, doc.ID, status); err != nil {
			return nil, err
		}
		doc.Status = status
	}
	if !blocking {
		more, err := advance(ctx, tx, doc, colleagues, *doc.RouteInstanceID, orderNo)
		if err != nil {
			return nil, err
		}
		effects = append(effects, more...)
	}
	return effects, nil
}

func findAuthorManager(list []Colleague, authorID string) string {
	for _, person := range list {
		if person.UserID == authorID {
			return person.ManagerID
		}
	}
	return ""
}

func conditionMet(rule string, doc Document) bool {
	switch rule {
	case "":
		return true
	case "customer_contract":
		return doc.CustomerContract
	case "addressee_above":
		return doc.AddresseeUserID != ""
	case "skip_no_manager":
		return false
	default:
		return false
	}
}

func containsKind(steps []stepRow, kind string) bool {
	for _, step := range steps {
		if step.Kind == kind && step.Blocking {
			return true
		}
	}
	return false
}

func deref(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func derefTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (s *Store) ListTasks(ctx context.Context, actor Actor, limit int, cursor string, urgent bool, tab string) (struct {
	Items      []TaskInfo `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
}, error) {
	var page struct {
		Items      []TaskInfo `json:"items"`
		NextCursor string     `json:"next_cursor,omitempty"`
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	if tab == "" {
		tab = "to_me"
	}
	cursorTime, cursorID, err := DecodeCursor(cursor)
	if err != nil {
		return page, err
	}
	deadline := sla.DeadlineWithin(time.Now().UTC(), 16)
	var tabSQL string
	switch tab {
	case "to_me":
		tabSQL = "t.assignee_user_id = $1 AND t.state = 'назначена'"
	case "execution":
		tabSQL = "t.assignee_user_id = $1 AND t.state = 'назначена' AND s.kind = 'execution'"
	case "pending_confirm":
		tabSQL = "d.author_user_id = $1 AND t.state = 'ожидает_подтверждения'"
	case "on_control":
		tabSQL = `t.assignee_user_id <> $1 AND t.state IN ('назначена', 'ожидает_подтверждения')
			AND (d.author_user_id = $1 OR d.addressee_user_id = $1 OR ($7::bool AND t.plan_due_at IS NOT NULL))`
	case "from_me":
		tabSQL = "d.author_user_id = $1 AND t.state IN ('назначена', 'ожидает_подтверждения')"
	default:
		return page, fmt.Errorf("%w: неизвестная вкладка задач", ErrInvalid)
	}
	rows, err := s.pool.Query(ctx, `
		SELECT t.id::text, t.document_id::text, d.title, d.status, d.author_user_id,
		       t.assignee_user_id, t.deputy_for_user_id, t.delegated_from_user_id,
		       s.kind, t.state, COALESCE(t.comment, ''), t.accepted_at, t.plan_due_at, t.started_at
		FROM step_tasks t
		JOIN documents d ON d.id = t.document_id
		JOIN route_steps s ON s.id = t.step_id
		WHERE d.organization_code = $2
		  AND ($7::bool OR NOT $7::bool)
		  AND `+tabSQL+`
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
		  AND ($3::bool = false OR (t.plan_due_at IS NOT NULL AND t.plan_due_at <= $4))
		  AND (
		    $5::timestamptz IS NULL
		    OR (t.started_at, t.id) < ($5::timestamptz, $6::uuid)
		  )
		ORDER BY t.started_at DESC, t.id DESC
		LIMIT $8
	`, actor.UserID, actor.OrganizationCode, urgent, deadline, nullTime(cursorTime), nullUUID(cursorID), actor.isController(), limit+1)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	type stamped struct {
		task TaskInfo
		at   time.Time
	}
	var all []stamped
	for rows.Next() {
		var item stamped
		if err := rows.Scan(
			&item.task.ID, &item.task.DocumentID, &item.task.Title, &item.task.DocumentStatus, &item.task.AuthorUserID,
			&item.task.AssigneeUserID, &item.task.DeputyForUserID, &item.task.DelegatedFromUserID,
			&item.task.Kind, &item.task.State, &item.task.Comment, &item.task.AcceptedAt, &item.task.PlanDueAt, &item.at,
		); err != nil {
			return page, err
		}
		item.task.StartedAt = &item.at
		all = append(all, item)
	}
	if len(all) > limit {
		all = all[:limit]
		last := all[len(all)-1]
		page.NextCursor = encodeCursor(last.at, last.task.ID)
	}
	for _, item := range all {
		page.Items = append(page.Items, item.task)
	}
	if page.Items == nil {
		page.Items = []TaskInfo{}
	}
	return page, rows.Err()
}

func (s *Store) RememberExternal(ctx context.Context, documentID, orderID, taskID, remoteTaskID string) error {
	if orderID != "" {
		_, err := s.pool.Exec(ctx, `
			UPDATE documents SET external_order_id = $2
			WHERE id = $1::uuid AND external_order_id IS NULL
		`, documentID, orderID)
		if err != nil {
			return err
		}
	}
	if taskID != "" && remoteTaskID != "" {
		_, err := s.pool.Exec(ctx, `UPDATE step_tasks SET orchestrator_task_id = $2 WHERE id = $1::uuid AND orchestrator_task_id IS NULL`, taskID, remoteTaskID)
		return err
	}
	return nil
}
