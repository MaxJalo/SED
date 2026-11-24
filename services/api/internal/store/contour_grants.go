package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

const (
	ContourLevelView   = "view"
	ContourLevelCards  = "cards"
	ContourLevelLead   = "lead"
	ContourLevelManage = "manage"
)

var validDomains = map[string]bool{
	"общий":        true,
	"продажи":      true,
	"закупки":      true,
	"производство": true,
	"кб":           true,
	"качество":     true,
}

type ContourGrant struct {
	ID             string `json:"id"`
	UserID         string `json:"user_id"`
	BusinessDomain string `json:"business_domain"`
	AccessLevel    string `json:"access_level"`
	GrantedBy      string `json:"granted_by"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

func contourRank(level string) int {
	switch level {
	case ContourLevelView:
		return 1
	case ContourLevelCards:
		return 2
	case ContourLevelLead:
		return 3
	case ContourLevelManage:
		return 4
	default:
		return 0
	}
}

func validateDomain(domain string) error {
	if !validDomains[strings.TrimSpace(domain)] {
		return fmt.Errorf("%w: неизвестный контур %q", ErrInvalid, domain)
	}
	return nil
}

func validateContourLevel(level string, allowEmpty bool) error {
	if level == "" && allowEmpty {
		return nil
	}
	if contourRank(level) == 0 {
		return fmt.Errorf("%w: неизвестный уровень доступа %q", ErrInvalid, level)
	}
	return nil
}

func (a Actor) contourLevel(domain string) string {
	if a.ContourLevels == nil {
		return ""
	}
	return a.ContourLevels[domain]
}

func (a Actor) hasContour(domain, minLevel string) bool {
	return contourRank(a.contourLevel(domain)) >= contourRank(minLevel)
}

func (a Actor) hasAnyContour(minLevel string) bool {
	for _, level := range a.ContourLevels {
		if contourRank(level) >= contourRank(minLevel) {
			return true
		}
	}
	return false
}

func (a Actor) canManageAnyContour() bool {
	return a.has("admin") || a.hasAnyContour(ContourLevelManage)
}

func (a Actor) canManageContour(domain string) bool {
	return a.has("admin") || a.hasContour(domain, ContourLevelManage)
}

func (a Actor) domainsAtLeast(minLevel string) []string {
	minRank := contourRank(minLevel)
	var out []string
	for domain, level := range a.ContourLevels {
		if contourRank(level) >= minRank {
			out = append(out, domain)
		}
	}
	return out
}

func (s *Store) UserContourGrants(ctx context.Context, userID, orgCode string) (map[string]string, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT business_domain, access_level
		FROM contour_grants
		WHERE user_id = $1 AND organization_code = $2
	`, userID, orgCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var domain, level string
		if err := rows.Scan(&domain, &level); err != nil {
			return nil, err
		}
		out[domain] = level
	}
	return out, rows.Err()
}

func (s *Store) ListContourGrants(ctx context.Context, actor Actor) ([]ContourGrant, error) {
	if !actor.canManageAnyContour() {
		return nil, ErrForbidden
	}
	isAdmin := actor.has("admin")
	allowedDomains := actor.domainsAtLeast(ContourLevelManage)
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, user_id, business_domain, access_level, granted_by,
		       to_char(created_at, 'YYYY-MM-DD HH24:MI'),
		       to_char(updated_at, 'YYYY-MM-DD HH24:MI')
		FROM contour_grants
		WHERE organization_code = $1
		  AND ($2::bool OR business_domain = ANY($3::text[]))
		ORDER BY updated_at DESC, business_domain, user_id
	`, actor.OrganizationCode, isAdmin, allowedDomains)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []ContourGrant{}
	for rows.Next() {
		var item ContourGrant
		if err := rows.Scan(
			&item.ID, &item.UserID, &item.BusinessDomain, &item.AccessLevel,
			&item.GrantedBy, &item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}

func (s *Store) SetContourGrant(ctx context.Context, actor Actor, colleagues []Colleague, userID, domain, level string) error {
	domain = strings.TrimSpace(domain)
	level = strings.TrimSpace(level)
	if err := validateDomain(domain); err != nil {
		return err
	}
	if err := validateContourLevel(level, true); err != nil {
		return err
	}
	if !actor.canManageContour(domain) {
		return ErrForbidden
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
	var prevLevel string
	err = tx.QueryRow(ctx, `
		SELECT access_level
		FROM contour_grants
		WHERE user_id = $1 AND organization_code = $2 AND business_domain = $3
	`, userID, actor.OrganizationCode, domain).Scan(&prevLevel)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if level == "" {
		if _, err := tx.Exec(ctx, `
			DELETE FROM contour_grants
			WHERE user_id = $1 AND organization_code = $2 AND business_domain = $3
		`, userID, actor.OrganizationCode, domain); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec(ctx, `
			INSERT INTO contour_grants (id, user_id, organization_code, business_domain, access_level, granted_by)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, $5)
			ON CONFLICT (user_id, organization_code, business_domain)
			DO UPDATE SET access_level = EXCLUDED.access_level, granted_by = EXCLUDED.granted_by, updated_at = now()
		`, userID, actor.OrganizationCode, domain, level, actor.UserID); err != nil {
			return err
		}
	}
	if prevLevel != level {
		if _, err := tx.Exec(ctx, `
			INSERT INTO audit_log (id, document_id, user_id, action, from_state, to_state, reason)
			VALUES (gen_random_uuid(), NULL, $1, 'права', $2, $3, $4)
		`, actor.UserID, prevLevel, level, "контур "+domain+" для "+userID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *Store) RevokeContourGrant(ctx context.Context, actor Actor, grantID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var userID, domain, prevLevel string
	err = tx.QueryRow(ctx, `
		SELECT user_id, business_domain, access_level
		FROM contour_grants
		WHERE id = $1::uuid AND organization_code = $2
	`, grantID, actor.OrganizationCode).Scan(&userID, &domain, &prevLevel)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if !actor.canManageContour(domain) {
		return ErrForbidden
	}
	if _, err := tx.Exec(ctx, `DELETE FROM contour_grants WHERE id = $1::uuid`, grantID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO audit_log (id, document_id, user_id, action, from_state, to_state, reason)
		VALUES (gen_random_uuid(), NULL, $1, 'права', $2, '', $3)
	`, actor.UserID, prevLevel, "снят контур "+domain+" у "+userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Store) DepartmentHeadInDomain(ctx context.Context, orgCode, domain string, list []Colleague, departmentCode string) string {
	head := DepartmentHead(list, departmentCode)
	if head == "" {
		return ""
	}
	if domain == "" {
		domain = "общий"
	}
	var level string
	err := s.pool.QueryRow(ctx, `
		SELECT access_level
		FROM contour_grants
		WHERE user_id = $1 AND organization_code = $2 AND business_domain = $3
	`, head, orgCode, domain).Scan(&level)
	if err == nil && contourRank(level) >= contourRank(ContourLevelCards) {
		return head
	}
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return ""
	}
	for _, person := range list {
		if person.UserID == head {
			for _, role := range person.Roles {
				switch role {
				case "read", "edit", "clerk", "admin", "controller":
					return head
				}
			}
		}
	}
	var hasOrgRead bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM role_grants
			WHERE user_id = $1 AND organization_code = $2 AND role IN ('read', 'edit')
		)
	`, head, orgCode).Scan(&hasOrgRead); err == nil && hasOrgRead {
		return head
	}
	return ""
}

func (s *Store) DepartmentHeadForDocument(ctx context.Context, orgCode, docID string, list []Colleague, departmentCode string) string {
	var domain string
	if err := s.pool.QueryRow(ctx, `SELECT business_domain FROM documents WHERE id = $1::uuid AND organization_code = $2`, docID, orgCode).Scan(&domain); err != nil {
		return ""
	}
	return s.DepartmentHeadInDomain(ctx, orgCode, domain, list, departmentCode)
}
