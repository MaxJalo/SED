package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

var routeBasis = map[string]string{
	"входящий":          "Регистратор принимает документ, руководитель ставит резолюцию, дальше поручение или ознакомление. Так же регистрируется входящий межкорпоративный документ стороны Б.",
	"исходящий":         "Руководитель исполнителя, профильная проверка, подписант, затем канцелярия подтверждает отправку. Односторонний документ группы уходит этим маршрутом у стороны А.",
	"служебная_записка": "Профильная проверка, если она нужна, затем адресат решает: поручение, отказ или к сведению.",
	"приказ":            "Руководитель инициатора, заинтересованное подразделение, юрист, проверка реквизитов канцелярией, подписант, ознакомление и контроль исполнения.",
	"распоряжение":      "Тот же маршрут, что у приказа.",
	"поручение":         "Исполнитель сдаёт результат, автор принимает его подтверждением. Отдельный шаг приёмки не нужен.",
	"протокол":          "Председатель подписывает, участники знакомятся, поручения исполняются.",
	"заявка":            "Короткий внутренний маршрут: руководитель и исполнитель. В концепциях отдельного класса заявки нет.",
	"договор":           "Предметная, юридическая и финансовая проверки идут параллельно, затем проверка лимита и подписант своей стороны. Вторая сторона ведёт собственную карточку.",
	"допсоглашение":     "Тот же цикл, что у договора.",
	"лна":               "Владелец, параллельно профильное подразделение и юрист, затем кадровая проверка, утверждение и ознакомление.",
	"тз":                "Инициатор проекта, проектные роли, утверждение и приёмка результата. Конструкторский и технологический документ ведётся этим шаблоном, пока нет отдельного вида.",
	"ткп":               "Тот же маршрут, что у исходящего.",
	"счет":              "Проверка реквизитов и отражение в учёте.",
	"акт":               "Согласование, подписание своей стороны и исполнение. У контрагента своя карточка.",
}

type RouteTemplateStep struct {
	OrderNo       int    `json:"order_no"`
	ParallelGroup *int   `json:"parallel_group"`
	Kind          string `json:"kind"`
	Caption       string `json:"caption"`
	AssigneeRole  string `json:"assignee_role"`
	DueDays       int    `json:"due_days"`
	Blocking      bool   `json:"blocking"`
}

type RouteTemplate struct {
	ID       string              `json:"id"`
	TypeCode string              `json:"type_code"`
	TypeName string              `json:"type_name"`
	Basis    string              `json:"basis"`
	Steps    []RouteTemplateStep `json:"steps"`
}

type RoleGrant struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Role      string `json:"role"`
	GrantedBy string `json:"granted_by"`
	CreatedAt string `json:"created_at"`
}

func MergeRoles(base, extra []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(base)+len(extra))
	for _, role := range append(append([]string{}, base...), extra...) {
		if role == "" || seen[role] {
			continue
		}
		seen[role] = true
		out = append(out, role)
	}
	return out
}

func (s *Store) GrantedRoles(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT role FROM role_grants WHERE user_id = $1 ORDER BY role`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var roles []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

func (s *Store) GrantedRolesByUser(ctx context.Context) (map[string][]string, error) {
	rows, err := s.pool.Query(ctx, `SELECT user_id, role FROM role_grants ORDER BY user_id, role`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var userID, role string
		if err := rows.Scan(&userID, &role); err != nil {
			return nil, err
		}
		out[userID] = append(out[userID], role)
	}
	return out, rows.Err()
}

func validateRoleGrant(role string) error {
	switch role {
	case "read", "write", "edit":
		return nil
	default:
		return fmt.Errorf("%w: допустимы только права read, write, edit (получено %s)", ErrInvalid, role)
	}
}

func validatePermissionSet(perms []string) ([]string, error) {
	seen := map[string]bool{}
	var clean []string
	for _, p := range perms {
		if p == "" {
			continue
		}
		if err := validateRoleGrant(p); err != nil {
			return nil, err
		}
		if !seen[p] {
			seen[p] = true
			clean = append(clean, p)
		}
	}
	return clean, nil
}

func (s *Store) ListRoleGrants(ctx context.Context, actor Actor) ([]RoleGrant, error) {
	if !actor.has("admin") {
		return nil, ErrForbidden
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, user_id, role, granted_by, to_char(created_at, 'YYYY-MM-DD HH24:MI')
		FROM role_grants
		WHERE organization_code = $1
		ORDER BY created_at DESC
	`, actor.OrganizationCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []RoleGrant{}
	for rows.Next() {
		var item RoleGrant
		if err := rows.Scan(&item.ID, &item.UserID, &item.Role, &item.GrantedBy, &item.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

func (s *Store) GrantRole(ctx context.Context, actor Actor, colleagues []Colleague, userID, role string) error {
	if !actor.has("admin") {
		return ErrForbidden
	}
	if err := validateRoleGrant(role); err != nil {
		return err
	}
	known := false
	for _, person := range colleagues {
		if person.UserID == userID && person.OrganizationCode == actor.OrganizationCode {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("%w: сотрудник не найден в юрлице", ErrInvalid)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO role_grants (id, user_id, role, organization_code, granted_by)
		VALUES (gen_random_uuid(), $1, $2, $3, $4)
		ON CONFLICT (user_id, role, organization_code) DO NOTHING
	`, userID, role, actor.OrganizationCode, actor.UserID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_log (id, document_id, user_id, action, reason)
		VALUES (gen_random_uuid(), NULL, $1, 'права', $2)
	`, actor.UserID, "выдано право "+role+" сотруднику "+userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) SetUserPermissions(ctx context.Context, actor Actor, colleagues []Colleague, userID string, permissions []string) error {
	if !actor.has("admin") {
		return ErrForbidden
	}
	clean, err := validatePermissionSet(permissions)
	if err != nil {
		return err
	}
	known := false
	for _, person := range colleagues {
		if person.UserID == userID && person.OrganizationCode == actor.OrganizationCode {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("%w: сотрудник не найден в юрлице", ErrInvalid)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		DELETE FROM role_grants
		WHERE user_id = $1 AND organization_code = $2
	`, userID, actor.OrganizationCode); err != nil {
		return err
	}
	for _, perm := range clean {
		if _, err := tx.Exec(ctx, `
			INSERT INTO role_grants (id, user_id, role, organization_code, granted_by)
			VALUES (gen_random_uuid(), $1, $2, $3, $4)
		`, userID, perm, actor.OrganizationCode, actor.UserID); err != nil {
			return err
		}
	}
	summary := "сняты все дополнительные права у " + userID
	if len(clean) > 0 {
		summary = fmt.Sprintf("установлены права %v для %s", clean, userID)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_log (id, document_id, user_id, action, reason)
		VALUES (gen_random_uuid(), NULL, $1, 'права', $2)
	`, actor.UserID, summary); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) RevokeRole(ctx context.Context, actor Actor, grantID string) error {
	if !actor.has("admin") {
		return ErrForbidden
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID, role string
	err = tx.QueryRow(ctx, `
		DELETE FROM role_grants
		WHERE id = $1::uuid AND organization_code = $2
		RETURNING user_id, role
	`, grantID, actor.OrganizationCode).Scan(&userID, &role)
	if err == pgx.ErrNoRows {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_log (id, document_id, user_id, action, reason)
		VALUES (gen_random_uuid(), NULL, $1, 'роль', $2)
	`, actor.UserID, "снята роль "+role+" у сотрудника "+userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) ListRouteTemplates(ctx context.Context, actor Actor) ([]RouteTemplate, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT r.id::text, t.code, t.name,
		       s.order_no, s.parallel_group, s.kind, COALESCE(s.caption, ''), COALESCE(s.assignee_role, ''), COALESCE(s.due_days, 2), s.is_blocking
		FROM routes r
		JOIN document_types t ON t.id = r.type_id
		LEFT JOIN route_steps s ON s.route_id = r.id
		WHERE r.is_template = true AND r.status = 'действует'
		ORDER BY t.name, s.order_no, s.kind, s.id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[string]*RouteTemplate{}
	var order []string
	for rows.Next() {
		var id, code, name string
		var orderNo *int
		var parallel *int
		var kind, caption, role *string
		var due *int
		var blocking *bool
		if err := rows.Scan(&id, &code, &name, &orderNo, &parallel, &kind, &caption, &role, &due, &blocking); err != nil {
			return nil, err
		}
		item := byID[id]
		if item == nil {
			item = &RouteTemplate{ID: id, TypeCode: code, TypeName: name, Basis: routeBasis[code], Steps: []RouteTemplateStep{}}
			byID[id] = item
			order = append(order, id)
		}
		if orderNo == nil || kind == nil {
			continue
		}
		step := RouteTemplateStep{OrderNo: *orderNo, ParallelGroup: parallel, Kind: *kind, DueDays: 2, Blocking: true}
		if caption != nil {
			step.Caption = *caption
		}
		if role != nil {
			step.AssigneeRole = *role
		}
		if due != nil {
			step.DueDays = *due
		}
		if blocking != nil {
			step.Blocking = *blocking
		}
		item.Steps = append(item.Steps, step)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]RouteTemplate, 0, len(order))
	for _, id := range order {
		out = append(out, *byID[id])
	}
	return out, nil
}

func validateTemplateSteps(steps []RouteTemplateStep) error {
	if len(steps) == 0 {
		return fmt.Errorf("%w: маршрут не может быть пустым", ErrInvalid)
	}
	for _, step := range steps {
		switch step.Kind {
		case "approval", "signing", "execution", "acknowledgment", "resolution", "review":
		default:
			return fmt.Errorf("%w: недопустимый тип шага %s", ErrInvalid, step.Kind)
		}
		switch step.AssigneeRole {
		case "author", "clerk", "manager", "approver", "signer", "executor", "observer", "controller":
		default:
			return fmt.Errorf("%w: недопустимая роль шага %s", ErrInvalid, step.AssigneeRole)
		}
		if step.OrderNo <= 0 {
			return fmt.Errorf("%w: номер этапа должен быть больше нуля", ErrInvalid)
		}
		if step.DueDays < 0 || step.DueDays > 60 {
			return fmt.Errorf("%w: срок этапа от 0 до 60 рабочих дней", ErrInvalid)
		}
	}
	return nil
}

func (s *Store) SaveRouteTemplate(ctx context.Context, actor Actor, routeID string, steps []RouteTemplateStep) (RouteTemplate, error) {
	if !actor.has("manager") {
		return RouteTemplate{}, ErrForbidden
	}
	if err := validateTemplateSteps(steps); err != nil {
		return RouteTemplate{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return RouteTemplate{}, err
	}
	defer tx.Rollback(ctx)
	var code string
	err = tx.QueryRow(ctx, `
		SELECT t.code
		FROM routes r
		JOIN document_types t ON t.id = r.type_id
		WHERE r.id = $1::uuid AND r.is_template = true AND r.status = 'действует'
	`, routeID).Scan(&code)
	if err == pgx.ErrNoRows {
		return RouteTemplate{}, ErrNotFound
	}
	if err != nil {
		return RouteTemplate{}, err
	}
	var used int
	if err := tx.QueryRow(ctx, `
		SELECT COUNT(*) FROM step_tasks st
		JOIN route_steps s ON s.id = st.step_id
		WHERE s.route_id = $1::uuid
	`, routeID).Scan(&used); err != nil {
		return RouteTemplate{}, err
	}
	if used > 0 {
		return RouteTemplate{}, fmt.Errorf("%w: по шаблону уже есть задачи", ErrConflict)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM route_steps WHERE route_id = $1::uuid`, routeID); err != nil {
		return RouteTemplate{}, err
	}
	for _, step := range steps {
		var parallel any
		if step.ParallelGroup != nil {
			parallel = *step.ParallelGroup
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO route_steps (
				id, route_id, order_no, parallel_group, kind, caption, is_blocking, sla_enabled, due_days, condition_rule, assignee_role
			) VALUES (
				gen_random_uuid(), $1::uuid, $2, $3, $4, $5, $6, $6, $7, '', $8
			)
		`, routeID, step.OrderNo, parallel, step.Kind, step.Caption, step.Blocking, step.DueDays, step.AssigneeRole); err != nil {
			return RouteTemplate{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_log (id, document_id, user_id, action, reason)
		VALUES (gen_random_uuid(), NULL, $1, 'маршрут', $2)
	`, actor.UserID, "изменён шаблон маршрута "+code); err != nil {
		return RouteTemplate{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RouteTemplate{}, err
	}
	items, err := s.ListRouteTemplates(ctx, actor)
	if err != nil {
		return RouteTemplate{}, err
	}
	for _, item := range items {
		if item.ID == routeID {
			return item, nil
		}
	}
	return RouteTemplate{}, ErrNotFound
}
