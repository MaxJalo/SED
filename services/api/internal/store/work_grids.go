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

type WorkGridColumn struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Kind   string `json:"kind"` // person | department | role | date | text
	Custom bool   `json:"custom,omitempty"`
}

type WorkGrid struct {
	OwnerKind string           `json:"owner_kind"`
	OwnerID   string           `json:"owner_id"`
	Columns   []WorkGridColumn `json:"columns"`
	Rows      []map[string]any `json:"rows"`
	UpdatedBy string           `json:"updated_by,omitempty"`
	UpdatedAt time.Time        `json:"updated_at,omitempty"`
	Applied   []string         `json:"applied,omitempty"`
}

func DefaultWorkGridColumns() []WorkGridColumn {
	return []WorkGridColumn{
		{Key: "person", Label: "Сотрудник", Kind: "person"},
		{Key: "department", Label: "Отдел", Kind: "department"},
		{Key: "role", Label: "Роль в шаге", Kind: "role"},
		{Key: "due_at", Label: "Срок", Kind: "date"},
	}
}

func (s *Store) ensureGridAccess(ctx context.Context, actor Actor, ownerKind, ownerID string) error {
	switch ownerKind {
	case "document":
		_, err := s.GetDocument(ctx, actor, ownerID)
		return err
	case "meeting":
		var org, owner, organizer, scope string
		err := s.pool.QueryRow(ctx, `
			SELECT organization_code, owner_user_id, organizer_user_id, scope
			FROM calendar_events WHERE id = $1::uuid
		`, ownerID).Scan(&org, &owner, &organizer, &scope)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if org != actor.OrganizationCode {
			return ErrForbidden
		}
		if scope != "shared" && owner != actor.UserID && organizer != actor.UserID && !actor.has("admin") {
			return ErrForbidden
		}
		return nil
	default:
		return fmt.Errorf("%w: неизвестный тип таблицы", ErrInvalid)
	}
}

func (s *Store) GetWorkGrid(ctx context.Context, actor Actor, ownerKind, ownerID string) (WorkGrid, error) {
	if err := s.ensureGridAccess(ctx, actor, ownerKind, ownerID); err != nil {
		return WorkGrid{}, err
	}
	var colsRaw, rowsRaw []byte
	var updatedBy string
	var updatedAt time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT columns, rows, updated_by, updated_at
		FROM work_grids
		WHERE owner_kind = $1 AND owner_id = $2::uuid
	`, ownerKind, ownerID).Scan(&colsRaw, &rowsRaw, &updatedBy, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return WorkGrid{
			OwnerKind: ownerKind,
			OwnerID:   ownerID,
			Columns:   DefaultWorkGridColumns(),
			Rows:      []map[string]any{},
		}, nil
	}
	if err != nil {
		return WorkGrid{}, err
	}
	grid := WorkGrid{
		OwnerKind: ownerKind,
		OwnerID:   ownerID,
		UpdatedBy: updatedBy,
		UpdatedAt: updatedAt,
	}
	if err := json.Unmarshal(colsRaw, &grid.Columns); err != nil || len(grid.Columns) == 0 {
		grid.Columns = DefaultWorkGridColumns()
	}
	if err := json.Unmarshal(rowsRaw, &grid.Rows); err != nil || grid.Rows == nil {
		grid.Rows = []map[string]any{}
	}
	return grid, nil
}

func (s *Store) SaveWorkGrid(
	ctx context.Context,
	actor Actor,
	colleagues []Colleague,
	ownerKind, ownerID string,
	columns []WorkGridColumn,
	rows []map[string]any,
	applyToRoute bool,
) (WorkGrid, []Effect, error) {
	if err := s.ensureGridAccess(ctx, actor, ownerKind, ownerID); err != nil {
		return WorkGrid{}, nil, err
	}
	if len(columns) == 0 {
		columns = DefaultWorkGridColumns()
	}
	if rows == nil {
		rows = []map[string]any{}
	}

	// Заполнить руководителя отдела, если человек не выбран, но выбран отдел.
	for i := range rows {
		uid := strVal(rows[i]["user_id"])
		dept := strVal(rows[i]["department_code"])
		if uid == "" && dept != "" {
			head := ""
			if ownerKind == "document" {
				head = s.DepartmentHeadForDocument(ctx, actor.OrganizationCode, ownerID, colleagues, dept)
			} else {
				head = DepartmentHead(colleagues, dept)
			}
			if head != "" {
				rows[i]["user_id"] = head
				for _, c := range colleagues {
					if c.UserID == head {
						rows[i]["person"] = c.FullName
						if strVal(rows[i]["department"]) == "" {
							rows[i]["department"] = c.DepartmentName
						}
						break
					}
				}
			}
		}
	}

	colsJSON, err := json.Marshal(columns)
	if err != nil {
		return WorkGrid{}, nil, err
	}
	rowsJSON, err := json.Marshal(rows)
	if err != nil {
		return WorkGrid{}, nil, err
	}

	var updatedAt time.Time
	err = s.pool.QueryRow(ctx, `
		INSERT INTO work_grids (owner_kind, owner_id, columns, rows, updated_by, updated_at)
		VALUES ($1, $2::uuid, $3::jsonb, $4::jsonb, $5, now())
		ON CONFLICT (owner_kind, owner_id) DO UPDATE
		SET columns = EXCLUDED.columns,
		    rows = EXCLUDED.rows,
		    updated_by = EXCLUDED.updated_by,
		    updated_at = now()
		RETURNING updated_at
	`, ownerKind, ownerID, colsJSON, rowsJSON, actor.UserID).Scan(&updatedAt)
	if err != nil {
		return WorkGrid{}, nil, err
	}

	grid := WorkGrid{
		OwnerKind: ownerKind,
		OwnerID:   ownerID,
		Columns:   columns,
		Rows:      rows,
		UpdatedBy: actor.UserID,
		UpdatedAt: updatedAt,
	}

	var effects []Effect
	if ownerKind == "document" && applyToRoute {
		applied, eff, err := s.applyGridToDocumentRoute(ctx, actor, colleagues, ownerID, rows)
		if err != nil {
			return grid, nil, err
		}
		grid.Applied = applied
		effects = eff
	}
	return grid, effects, nil
}

func (s *Store) applyGridToDocumentRoute(
	ctx context.Context,
	actor Actor,
	colleagues []Colleague,
	docID string,
	rows []map[string]any,
) ([]string, []Effect, error) {
	doc, err := loadDocument(ctx, s.pool, docID, actor.UserID, actor.DepartmentCode)
	if err != nil {
		return nil, nil, err
	}
	if doc.AuthorUserID != actor.UserID && !actor.canEdit() && !actor.has("manager") && !actor.hasContour(doc.BusinessDomain, ContourLevelLead) {
		return nil, nil, fmt.Errorf("%w: сохранять путь может автор, руководитель или сотрудник с правом ведения", ErrForbidden)
	}
	if doc.Status == "Закрыт (в деле)" || doc.Status == "Отклонён" {
		return nil, nil, fmt.Errorf("%w: документ закрыт", ErrConflict)
	}

	var applied []string
	var allEffects []Effect

	// Если маршрут ещё не запущен, разрешаем обновлять/добавлять шаги прямо из таблицы.
	if !doc.RouteStarted {
		var draftInputs []RouteStepEditInput
		existingByID := map[string]RouteStepView{}
		for _, s := range doc.RoutePath {
			if s.StepID != "" {
				existingByID[s.StepID] = s
			}
		}
		usedMandatory := map[string]bool{}
		order := 1
		for _, row := range rows {
			role := strings.TrimSpace(strVal(row["role"]))
			if role == "" {
				continue
			}
			sid := strings.TrimSpace(strVal(row["step_id"]))
			uid := strings.TrimSpace(strVal(row["user_id"]))
			var ex RouteStepView
			var hasEx bool
			if sid != "" {
				ex, hasEx = existingByID[sid]
			}
			if hasEx && ex.IsMandatory {
				usedMandatory[sid] = true
			}
			dueDays := 2
			if hasEx && ex.DueDays > 0 {
				dueDays = ex.DueDays
			}
			assigneeRole := ""
			if uid == "" && hasEx {
				assigneeRole = ex.AssigneeRole
			}
			draftInputs = append(draftInputs, RouteStepEditInput{
				StepID:         sid,
				OrderNo:        order,
				Kind:           role,
				DueDays:        dueDays,
				AssigneeRole:   assigneeRole,
				AssigneeUserID: uid,
				IsBlocking:     role != "acknowledgment" && role != "review",
			})
			order++
		}
		// Сохранить обязательные шаги, если они не попали в строки
		for _, s := range doc.RoutePath {
			if s.IsMandatory && !usedMandatory[s.StepID] {
				draftInputs = append(draftInputs, RouteStepEditInput{
					StepID:         s.StepID,
					OrderNo:        order,
					Kind:           s.Kind,
					DueDays:        s.DueDays,
					AssigneeRole:   s.AssigneeRole,
					AssigneeUserID: s.AssigneeUserID,
					IsBlocking:     s.IsBlocking,
				})
				order++
			}
		}
		if len(draftInputs) == 0 {
			return nil, nil, fmt.Errorf("%w: укажите роль шага хотя бы в одной строке", ErrInvalid)
		}
		if _, err := s.EditDraftRouteSteps(ctx, actor, docID, draftInputs); err != nil {
			return nil, nil, err
		}
		applied = append(applied, "обновлены шаги черновика")
		return applied, allEffects, nil
	}
	if doc.RouteInstanceID == nil {
		return nil, nil, fmt.Errorf("%w: маршрут ещё не создан", ErrConflict)
	}

	for _, row := range rows {
		role := strings.TrimSpace(strVal(row["role"]))
		uid := strings.TrimSpace(strVal(row["user_id"]))
		sid := strings.TrimSpace(strVal(row["step_id"]))
		if role == "" || uid == "" {
			continue
		}
		var matchedTaskID, matchedStepID, currentAssignee string
		for _, t := range doc.RoutePath {
			if (sid != "" && t.StepID == sid) || (sid == "" && t.Kind == role && (t.TaskState == "назначена" || t.TaskState == "ожидает_подтверждения" || t.StageState == "предстоит")) {
				if t.StageState != "пройден" && t.StageState != "пропущен" {
					matchedTaskID = t.TaskID
					matchedStepID = t.StepID
					currentAssignee = t.AssigneeUserID
					break
				}
			}
		}
		if matchedTaskID != "" || matchedStepID != "" {
			if currentAssignee != uid {
				updated, err := s.ReassignRoute(ctx, actor, docID, matchedTaskID, matchedStepID, uid)
				if err != nil {
					return applied, allEffects, err
				}
				doc = updated
				applied = append(applied, "переназначен шаг "+role)
			}
			continue
		}
		if role == "approval" || role == "acknowledgment" {
			already := false
			for _, t := range doc.RoutePath {
				if t.Kind == role && t.AssigneeUserID == uid && (t.TaskState == "назначена" || t.TaskState == "ожидает_подтверждения") {
					already = true
					break
				}
			}
			if !already {
				updated, eff, err := s.AddRouteParticipant(ctx, actor, colleagues, docID, role, uid)
				if err != nil {
					return applied, allEffects, err
				}
				doc = updated
				allEffects = append(allEffects, eff...)
				applied = append(applied, "добавлен участник "+role)
			}
		}
	}
	return applied, allEffects, nil
}

func strVal(v any) string {
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}
