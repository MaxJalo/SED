package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Actor struct {
	UserID           string
	OrganizationCode string
	DepartmentCode   string
	Roles            []string
	ContourLevels    map[string]string
}

func (a Actor) has(role string) bool {
	for _, item := range a.Roles {
		if item == role {
			return true
		}
	}
	return false
}

func (a Actor) canWrite() bool {
	return a.has("write") || a.has("edit")
}

func (a Actor) canEdit() bool {
	return a.has("edit") || a.has("clerk")
}

func (a Actor) isWorker() bool {
	return a.canWrite() || a.has("author") || a.has("clerk") || a.has("manager") ||
		a.has("executor") || a.has("approver") || a.has("signer") ||
		a.has("controller") || a.has("admin")
}

func (a Actor) CanCreateType(typeCode string) bool {
	hasLead := a.hasAnyContour(ContourLevelLead)
	if !a.isWorker() && !hasLead {
		return false
	}
	switch typeCode {
	case "служебная_записка", "заявка", "исходящий", "договор", "поручение",
		"допсоглашение", "лна", "тз", "ткп", "счет", "акт":
		return true
	case "входящий", "приказ", "распоряжение", "протокол":
		return a.canWrite() || a.has("clerk") || a.has("manager") || hasLead
	default:
		return false
	}
}

func (a Actor) CanCreateInDomain(typeCode, domain string) bool {
	if !a.CanCreateType(typeCode) {
		return false
	}
	if a.canWrite() || a.has("clerk") || a.has("manager") || a.has("admin") {
		return true
	}
	return a.hasContour(domain, ContourLevelLead)
}

func (a Actor) canRegister() bool { return a.canEdit() }

func (a Actor) canReadOrg() bool {
	return a.has("read") || a.has("edit") || a.has("clerk") || a.has("admin") || a.has("controller") || a.has("manager")
}

func (a Actor) isController() bool { return a.has("controller") }

func (a Actor) canPause() bool { return a.canEdit() || a.has("admin") }

type TypeInfo struct {
	Code           string   `json:"code"`
	Name           string   `json:"name"`
	StreamKind     string   `json:"stream_kind"`
	CanCreate      bool     `json:"can_create"`
	RequiredFields []string `json:"required_fields"`
}

func RequiredFieldsFor(typeCode string) []string {
	switch typeCode {
	case "служебная_записка":
		return []string{"title", "body", "addressee_user_id"}
	case "заявка":
		return []string{"title", "body", "service_name"}
	case "входящий":
		return []string{"title", "counterparty_name", "addressee_user_id"}
	case "исходящий", "договор", "допсоглашение", "ткп", "счет", "акт":
		return []string{"title", "body", "counterparty_name"}
	default:
		return []string{"title", "body"}
	}
}

func (s *Store) Types(ctx context.Context, actor Actor) ([]TypeInfo, error) {
	rows, err := s.pool.Query(ctx, `SELECT code, name, stream_kind FROM document_types ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TypeInfo
	for rows.Next() {
		var item TypeInfo
		if err := rows.Scan(&item.Code, &item.Name, &item.StreamKind); err != nil {
			return nil, err
		}
		item.CanCreate = actor.CanCreateType(item.Code)
		item.RequiredFields = RequiredFieldsFor(item.Code)
		out = append(out, item)
	}
	return out, rows.Err()
}

type SourceLinkInput struct {
	FromDocumentID string `json:"from_document_id"`
	LinkType       string `json:"link_type"`
}

type CreateInput struct {
	TypeCode            string           `json:"type_code"`
	SubtypeCode         string           `json:"subtype_code"`
	BusinessDomain      string           `json:"business_domain"`
	Title               string           `json:"title"`
	Body                string           `json:"body"`
	Importance          string           `json:"importance"`
	CustomerContract    bool             `json:"customer_contract"`
	AddresseeUserID     string           `json:"addressee_user_id"`
	AddresseeDepartment string           `json:"addressee_department_code"`
	CounterpartyName    string           `json:"counterparty_name"`
	CounterpartyINN     string           `json:"counterparty_inn"`
	ServiceName         string           `json:"service_name"`
	DueDate             string           `json:"due_date"`
	Amount              *float64         `json:"amount"`
	SourceLink          *SourceLinkInput `json:"source_link,omitempty"`
}

func validateRequiredFields(in CreateInput) error {
	if strings.TrimSpace(in.Title) == "" || in.TypeCode == "" {
		return fmt.Errorf("%w: укажите вид и заголовок документа", ErrInvalid)
	}
	for _, f := range RequiredFieldsFor(in.TypeCode) {
		switch f {
		case "body":
			if strings.TrimSpace(in.Body) == "" {
				return fmt.Errorf("%w: заполните содержание документа", ErrInvalid)
			}
		case "addressee_user_id":
			if strings.TrimSpace(in.AddresseeUserID) == "" {
				return fmt.Errorf("%w: выберите адресата документа", ErrInvalid)
			}
		case "counterparty_name":
			if strings.TrimSpace(in.CounterpartyName) == "" {
				return fmt.Errorf("%w: укажите контрагента", ErrInvalid)
			}
		case "service_name":
			if strings.TrimSpace(in.ServiceName) == "" {
				return fmt.Errorf("%w: укажите службу-адресат", ErrInvalid)
			}
		}
	}
	if in.TypeCode == "поручение" && strings.TrimSpace(in.AddresseeDepartment) == "" && strings.TrimSpace(in.AddresseeUserID) == "" {
		return fmt.Errorf("%w: выберите отдел для поручения", ErrInvalid)
	}
	return nil
}

func validateFormatFields(inn string, amount *float64) error {
	inn = strings.TrimSpace(inn)
	if inn != "" {
		if len(inn) != 10 && len(inn) != 12 {
			return fmt.Errorf("%w: ИНН должен содержать 10 или 12 цифр", ErrInvalid)
		}
		for i := 0; i < len(inn); i++ {
			if inn[i] < '0' || inn[i] > '9' {
				return fmt.Errorf("%w: ИНН должен содержать только цифры", ErrInvalid)
			}
		}
	}
	if amount != nil && *amount <= 0 {
		return fmt.Errorf("%w: сумма должна быть больше нуля", ErrInvalid)
	}
	return nil
}

func (s *Store) CreateDocument(ctx context.Context, actor Actor, idemKey, bodyHash string, in CreateInput, colleagues []Colleague) ([]byte, int, error) {
	if !actor.CanCreateType(in.TypeCode) {
		return nil, 0, fmt.Errorf("%w: ваша должность не может создавать документы вида %s", ErrForbidden, in.TypeCode)
	}
	if err := validateRequiredFields(in); err != nil {
		return nil, 0, err
	}
	if err := validateFormatFields(in.CounterpartyINN, in.Amount); err != nil {
		return nil, 0, err
	}
	if in.Importance == "" {
		in.Importance = "обычная"
	}
	switch in.Importance {
	case "обычная", "повышенная", "немедленная":
	default:
		return nil, 0, fmt.Errorf("%w: неизвестная важность", ErrInvalid)
	}
	var parsedDue *time.Time
	if strings.TrimSpace(in.DueDate) != "" {
		t, err := time.Parse("2006-01-02", strings.TrimSpace(in.DueDate))
		if err != nil {
			return nil, 0, fmt.Errorf("%w: срок должен быть в формате ГГГГ-ММ-ДД", ErrInvalid)
		}
		parsedDue = &t
	}
	if body, code, ok, err := s.loadIdempotency(ctx, actor.UserID, idemKey, bodyHash); err != nil || ok {
		return body, code, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)

	var typeID string
	err = tx.QueryRow(ctx, `SELECT id::text FROM document_types WHERE code = $1`, in.TypeCode).Scan(&typeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, fmt.Errorf("%w: неизвестный вид документа", ErrInvalid)
	}
	if err != nil {
		return nil, 0, err
	}

	domain := strings.TrimSpace(in.BusinessDomain)
	if in.SubtypeCode != "" {
		var subTypeCode, subDomain string
		err = tx.QueryRow(ctx, `SELECT type_code, business_domain FROM document_subtypes WHERE code = $1`, in.SubtypeCode).Scan(&subTypeCode, &subDomain)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, 0, fmt.Errorf("%w: неизвестный подвид документа", ErrInvalid)
		}
		if err != nil {
			return nil, 0, err
		}
		if subTypeCode != in.TypeCode {
			return nil, 0, fmt.Errorf("%w: подвид %s не относится к виду %s", ErrInvalid, in.SubtypeCode, in.TypeCode)
		}
		if domain == "" {
			domain = subDomain
		}
	}
	if domain == "" {
		domain = "общий"
	}
	if !actor.CanCreateInDomain(in.TypeCode, domain) {
		return nil, 0, fmt.Errorf("%w: нет права ведения в контуре %s", ErrForbidden, domain)
	}

	var docID string
	err = tx.QueryRow(ctx, `
		INSERT INTO documents (
			id, type_id, subtype_code, business_domain, organization_code, status, importance, title, body,
			addressee_user_id, addressee_department_code, counterparty_name, counterparty_inn, service_name,
			due_date, amount, author_user_id, customer_contract
		) VALUES (
			gen_random_uuid(), $1::uuid, $2, $3, $4, 'Черновик', $5, $6, $7,
			$8, $9, $10, $11, $12, $13, $14, $15, $16
		) RETURNING id::text
	`, typeID, strings.TrimSpace(in.SubtypeCode), domain, actor.OrganizationCode, in.Importance, strings.TrimSpace(in.Title), strings.TrimSpace(in.Body),
		strings.TrimSpace(in.AddresseeUserID), strings.TrimSpace(in.AddresseeDepartment), strings.TrimSpace(in.CounterpartyName), strings.TrimSpace(in.CounterpartyINN),
		strings.TrimSpace(in.ServiceName), parsedDue, in.Amount, actor.UserID, in.CustomerContract).Scan(&docID)
	if err != nil {
		return nil, 0, err
	}
	var versionID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO document_versions (id, document_id, number, created_by)
		VALUES (gen_random_uuid(), $1::uuid, 1, $2) RETURNING id::text
	`, docID, actor.UserID).Scan(&versionID); err != nil {
		return nil, 0, err
	}
	if _, err := tx.Exec(ctx, `UPDATE documents SET current_version_id = $2::uuid WHERE id = $1::uuid`, docID, versionID); err != nil {
		return nil, 0, err
	}
	if _, _, err := assignTypeNumber(ctx, tx, docID, actor.OrganizationCode, in.TypeCode, strings.TrimSpace(in.SubtypeCode)); err != nil {
		return nil, 0, err
	}
	if err := cloneDraftRouteOnCreate(ctx, tx, docID, in.TypeCode, actor.UserID, strings.TrimSpace(in.AddresseeUserID), colleagues); err != nil {
		return nil, 0, err
	}
	if in.SourceLink != nil && in.SourceLink.FromDocumentID != "" {
		fromDoc, err := loadDocument(ctx, tx, in.SourceLink.FromDocumentID, actor.UserID, actor.DepartmentCode)
		if err != nil {
			return nil, 0, fmt.Errorf("%w: исходный документ не найден", ErrInvalid)
		}
		if err := s.ensureRead(ctx, tx, actor, fromDoc); err != nil {
			return nil, 0, err
		}
		toDoc := Document{ID: docID, TypeCode: in.TypeCode, Status: "Черновик", OrganizationCode: actor.OrganizationCode}
		if err := validateDocumentLink(ctx, tx, fromDoc, toDoc, in.SourceLink.LinkType); err != nil {
			return nil, 0, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO document_links (id, from_document_id, to_document_id, link_type, created_by)
			VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $3, $4)
		`, fromDoc.ID, docID, in.SourceLink.LinkType, actor.UserID); err != nil {
			return nil, 0, fmt.Errorf("%w: не удалось связать с исходным документом", ErrInvalid)
		}
	}
	if err := writeAudit(ctx, tx, docID, actor.UserID, "переход", "", "Черновик", ""); err != nil {
		return nil, 0, err
	}
	doc, err := loadDocument(ctx, tx, docID, actor.UserID, actor.DepartmentCode)
	if err != nil {
		return nil, 0, err
	}
	body := mustJSON(doc)
	if err := insertIdempotency(ctx, tx, actor.UserID, idemKey, bodyHash, 201, body); err != nil {
		if isUnique(err) {
			_ = tx.Rollback(ctx)
			body, code, _, idemErr := s.loadIdempotency(ctx, actor.UserID, idemKey, bodyHash)
			return body, code, idemErr
		}
		return nil, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, err
	}
	return body, 201, nil
}

type ListFilter struct {
	StreamKind     string
	TypeCode       string
	SubtypeCode    string
	BusinessDomain string
	Status         string
	Bucket         string // my | on_control | all
	TagID          string
	People         string
	Query          string
}

func (s *Store) ListDocuments(ctx context.Context, actor Actor, limit int, cursor string, f ListFilter) (ListPage, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	cursorTime, cursorID, err := DecodeCursor(cursor)
	if err != nil {
		return ListPage{}, err
	}
	qPattern := ""
	if strings.TrimSpace(f.Query) != "" {
		qPattern = "%" + strings.TrimSpace(f.Query) + "%"
	}
	viewDomains := actor.domainsAtLeast(ContourLevelView)
	rows, err := s.pool.Query(ctx, `
		SELECT d.id::text, d.created_at
		FROM documents d
		JOIN document_types t ON t.id = d.type_id
		WHERE d.organization_code = $1
		  AND (
		    $2::bool
		    OR d.business_domain = ANY($17::text[])
		    OR d.author_user_id = $3
		    OR d.addressee_user_id = $3
		    OR EXISTS (
		      SELECT 1 FROM step_tasks st
		      WHERE st.document_id = d.id
		        AND (st.assignee_user_id = $3 OR st.deputy_for_user_id = $3 OR st.delegated_from_user_id = $3)
		    )
		    OR (d.route_started = true AND d.route_instance_id IS NOT NULL AND EXISTS (
		      SELECT 1 FROM route_steps rs
		      WHERE rs.route_id = d.route_instance_id AND rs.assignee_user_id = $3
		    ))
		    OR EXISTS (
		      SELECT 1 FROM document_grants dg
		      WHERE dg.document_id = d.id AND dg.user_id = $3 AND dg.expires_at > now()
		    )
		  )
		  AND ($4::text = '' OR t.stream_kind = $4)
		  AND ($5::text = '' OR t.code = ANY(string_to_array($5, ',')))
		  AND ($14::text = '' OR d.subtype_code = $14)
		  AND ($15::text = '' OR d.business_domain = $15)
		  AND ($6::text = '' OR d.status = $6)
		  AND ($7::text <> 'my' OR d.author_user_id = $3)
		  AND ($7::text <> 'on_control' OR (d.status NOT IN ('Черновик', 'Отклонён', 'Закрыт (в деле)')))
		  AND ($8::text = '' OR EXISTS (
		    SELECT 1 FROM document_tags dt
		    JOIN tags tg ON tg.id = dt.tag_id
		    WHERE dt.document_id = d.id AND tg.id::text = $8
		      AND (tg.scope = 'org' OR tg.department_code = $12)
		  ))
		  AND ($9::text = '' OR d.title ILIKE $9 OR d.body ILIKE $9 OR COALESCE(d.reg_number, '') ILIKE $9
		       OR d.counterparty_name ILIKE $9 OR d.service_name ILIKE $9 OR d.addressee_department_code ILIKE $9
		       OR ($16::text <> '' AND (
		         d.author_user_id = ANY(string_to_array($16, ','))
		         OR d.addressee_user_id = ANY(string_to_array($16, ','))
		       )))
		  AND (
		    $10::timestamptz IS NULL
		    OR (d.created_at, d.id) < ($10::timestamptz, $11::uuid)
		  )
		ORDER BY d.created_at DESC, d.id DESC
		LIMIT $13
	`, actor.OrganizationCode, actor.canReadOrg(), actor.UserID,
		f.StreamKind, f.TypeCode, f.Status, f.Bucket, f.TagID, qPattern,
		nullTime(cursorTime), nullUUID(cursorID), actor.DepartmentCode, limit+1,
		f.SubtypeCode, f.BusinessDomain, f.People, viewDomains)
	if err != nil {
		return ListPage{}, err
	}
	defer rows.Close()
	var ids []string
	var stamps []time.Time
	for rows.Next() {
		var id string
		var createdAt time.Time
		if err := rows.Scan(&id, &createdAt); err != nil {
			return ListPage{}, err
		}
		ids = append(ids, id)
		stamps = append(stamps, createdAt)
	}
	if err := rows.Err(); err != nil {
		return ListPage{}, err
	}
	page := ListPage{}
	if len(ids) > limit {
		page.NextCursor = encodeCursor(stamps[limit-1], ids[limit-1])
		ids = ids[:limit]
	}
	items, err := loadDocumentHeads(ctx, s.pool, ids)
	if err != nil {
		return ListPage{}, err
	}
	page.Items = items
	if page.Items == nil {
		page.Items = []Document{}
	}
	return page, nil
}

func loadDocumentHeads(ctx context.Context, q querier, ids []string) ([]Document, error) {
	if len(ids) == 0 {
		return []Document{}, nil
	}
	rows, err := q.Query(ctx, `
		SELECT d.id::text, t.code, t.name, d.subtype_code, COALESCE(st.name, ''), d.business_domain,
		       t.stream_kind, d.title, d.body, d.status, d.importance,
		       d.organization_code, d.author_user_id, d.addressee_user_id, d.addressee_department_code,
		       d.counterparty_name, d.counterparty_inn, d.service_name, d.due_date, d.amount::float8,
		       d.reg_number, d.reg_date, d.case_id::text, COALESCE(cn.code || ' — ' || cn.title, ''),
		       d.completed_by_author_at, d.customer_contract, d.onec_sync_status,
		       d.route_started, d.route_instance_id::text, r.status, v.id::text, v.number, d.created_at
		FROM documents d
		JOIN document_types t ON t.id = d.type_id
		JOIN document_versions v ON v.id = d.current_version_id
		LEFT JOIN document_subtypes st ON st.code = d.subtype_code
		LEFT JOIN routes r ON r.id = d.route_instance_id
		LEFT JOIN case_nomenclature cn ON cn.id = d.case_id
		WHERE d.id = ANY($1::uuid[])
	`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[string]Document{}
	for rows.Next() {
		var doc Document
		var dueDate *time.Time
		var caseLabel string
		if err := rows.Scan(
			&doc.ID, &doc.TypeCode, &doc.TypeName, &doc.SubtypeCode, &doc.SubtypeName, &doc.BusinessDomain,
			&doc.StreamKind, &doc.Title, &doc.Body, &doc.Status, &doc.Importance,
			&doc.OrganizationCode, &doc.AuthorUserID, &doc.AddresseeUserID, &doc.AddresseeDepartment, &doc.CounterpartyName,
			&doc.CounterpartyINN, &doc.ServiceName, &dueDate, &doc.Amount,
			&doc.RegNumber, &doc.RegDate, &doc.CaseID, &caseLabel,
			&doc.CompletedByAuthorAt, &doc.CustomerContract, &doc.OnecSyncStatus,
			&doc.RouteStarted, &doc.RouteInstanceID, &doc.RouteStatus, &doc.VersionID, &doc.VersionNumber, &doc.CreatedAt,
		); err != nil {
			return nil, err
		}
		doc.CaseLabel = caseLabel
		if dueDate != nil {
			formatted := dueDate.Format("2006-01-02")
			doc.DueDate = &formatted
		}
		doc.Files = []FileInfo{}
		doc.MyTasks = []TaskInfo{}
		doc.RoutePath = []RouteStepView{}
		doc.Links = []DocumentLink{}
		doc.CommentsReviews = []CommentReview{}
		doc.PaperMovements = []PaperMovement{}
		doc.Tags = []TagInfo{}
		doc.Grants = []GrantInfo{}
		doc.AuditTrail = []AuditEntry{}
		byID[doc.ID] = doc
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]Document, 0, len(ids))
	for _, id := range ids {
		if doc, ok := byID[id]; ok {
			out = append(out, doc)
		}
	}
	return out, nil
}

func (s *Store) GetDocument(ctx context.Context, actor Actor, id string) (Document, error) {
	doc, err := loadDocument(ctx, s.pool, id, actor.UserID, actor.DepartmentCode)
	if err != nil {
		return Document{}, err
	}
	if err := s.ensureRead(ctx, s.pool, actor, doc); err != nil {
		return Document{}, err
	}
	return doc, nil
}

func clipAudit(s string) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) > 240 {
		return string(runes[:240]) + "…"
	}
	return s
}

func auditPair(from, to string) (string, string, bool) {
	from, to = clipAudit(from), clipAudit(to)
	return from, to, from != to
}

func auditField(ctx context.Context, tx pgx.Tx, id, userID, field, from, to string) error {
	from, to, changed := auditPair(from, to)
	if !changed {
		return nil
	}
	return writeAudit(ctx, tx, id, userID, "изменение", from, to, field)
}

func amountAudit(v *float64) string {
	if v == nil || *v == 0 {
		return ""
	}
	return strconv.FormatFloat(*v, 'f', -1, 64)
}

func dueAudit(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

type PatchInput struct {
	Title               *string  `json:"title"`
	Body                *string  `json:"body"`
	Importance          *string  `json:"importance"`
	BusinessDomain      *string  `json:"business_domain"`
	CounterpartyINN     *string  `json:"counterparty_inn"`
	CounterpartyName    *string  `json:"counterparty_name"`
	ServiceName         *string  `json:"service_name"`
	AddresseeUserID     *string  `json:"addressee_user_id"`
	DueDate             *string  `json:"due_date"`
	AddresseeDepartment *string  `json:"addressee_department_code"`
	Amount              *float64 `json:"amount"`
}

func (s *Store) PatchDocument(ctx context.Context, actor Actor, id string, in PatchInput) (Document, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Document{}, err
	}
	defer tx.Rollback(ctx)
	doc, err := loadDocument(ctx, tx, id, actor.UserID, actor.DepartmentCode)
	if err != nil {
		return Document{}, err
	}
	if doc.OrganizationCode != actor.OrganizationCode || doc.Status == "Закрыт (в деле)" {
		return Document{}, ErrForbidden
	}
	editable := doc.Status != "Закрыт (в деле)" && doc.Status != "Отклонён" && (doc.AuthorUserID == actor.UserID || actor.canEdit() || actor.hasContour(doc.BusinessDomain, ContourLevelLead))
	if in.Title != nil {
		if !editable {
			return Document{}, ErrForbidden
		}
		title := strings.TrimSpace(*in.Title)
		if title == "" {
			return Document{}, fmt.Errorf("%w: пустой заголовок", ErrInvalid)
		}
		if _, err := tx.Exec(ctx, `UPDATE documents SET title = $2, updated_at = now() WHERE id = $1::uuid`, id, title); err != nil {
			return Document{}, err
		}
		if err := auditField(ctx, tx, id, actor.UserID, "заголовок", doc.Title, title); err != nil {
			return Document{}, err
		}
	}
	if in.Body != nil {
		if !editable {
			return Document{}, ErrForbidden
		}
		body := strings.TrimSpace(*in.Body)
		if _, err := tx.Exec(ctx, `UPDATE documents SET body = $2, updated_at = now() WHERE id = $1::uuid`, id, body); err != nil {
			return Document{}, err
		}
		if err := auditField(ctx, tx, id, actor.UserID, "содержание", doc.Body, body); err != nil {
			return Document{}, err
		}
	}
	if in.Importance != nil {
		switch *in.Importance {
		case "обычная", "повышенная", "немедленная":
		default:
			return Document{}, fmt.Errorf("%w: неизвестная важность", ErrInvalid)
		}
		if doc.RouteStarted && !actor.canEdit() {
			return Document{}, fmt.Errorf("%w: после старта важность меняет только делопроизводитель или право изменения", ErrForbidden)
		}
		if !doc.RouteStarted && doc.AuthorUserID != actor.UserID && !actor.canEdit() {
			return Document{}, ErrForbidden
		}
		if _, err := tx.Exec(ctx, `UPDATE documents SET importance = $2, updated_at = now() WHERE id = $1::uuid`, id, *in.Importance); err != nil {
			return Document{}, err
		}
		if err := auditField(ctx, tx, id, actor.UserID, "важность", doc.Importance, *in.Importance); err != nil {
			return Document{}, err
		}
	}
	if in.BusinessDomain != nil {
		if doc.AuthorUserID != actor.UserID && !actor.canEdit() {
			return Document{}, ErrForbidden
		}
		domain := strings.TrimSpace(*in.BusinessDomain)
		if _, err := tx.Exec(ctx, `UPDATE documents SET business_domain = $2, updated_at = now() WHERE id = $1::uuid`, id, domain); err != nil {
			return Document{}, err
		}
		if err := auditField(ctx, tx, id, actor.UserID, "контур", doc.BusinessDomain, domain); err != nil {
			return Document{}, err
		}
	}
	if in.CounterpartyINN != nil || in.Amount != nil {
		innVal := ""
		if in.CounterpartyINN != nil {
			innVal = *in.CounterpartyINN
		}
		var amountCheck *float64
		if in.Amount != nil && *in.Amount != 0 {
			amountCheck = in.Amount
		}
		if err := validateFormatFields(innVal, amountCheck); err != nil {
			return Document{}, err
		}
	}
	if editable {
		if in.CounterpartyINN != nil {
			inn := strings.TrimSpace(*in.CounterpartyINN)
			if _, err := tx.Exec(ctx, `UPDATE documents SET counterparty_inn = $2, updated_at = now() WHERE id = $1::uuid`, id, inn); err != nil {
				return Document{}, err
			}
			if err := auditField(ctx, tx, id, actor.UserID, "ИНН", doc.CounterpartyINN, inn); err != nil {
				return Document{}, err
			}
		}
		if in.CounterpartyName != nil {
			name := strings.TrimSpace(*in.CounterpartyName)
			if _, err := tx.Exec(ctx, `UPDATE documents SET counterparty_name = $2, updated_at = now() WHERE id = $1::uuid`, id, name); err != nil {
				return Document{}, err
			}
			if err := auditField(ctx, tx, id, actor.UserID, "контрагент", doc.CounterpartyName, name); err != nil {
				return Document{}, err
			}
		}
		if in.ServiceName != nil {
			service := strings.TrimSpace(*in.ServiceName)
			if _, err := tx.Exec(ctx, `UPDATE documents SET service_name = $2, updated_at = now() WHERE id = $1::uuid`, id, service); err != nil {
				return Document{}, err
			}
			if err := auditField(ctx, tx, id, actor.UserID, "служба", doc.ServiceName, service); err != nil {
				return Document{}, err
			}
		}
		if in.AddresseeUserID != nil {
			addressee := strings.TrimSpace(*in.AddresseeUserID)
			if _, err := tx.Exec(ctx, `UPDATE documents SET addressee_user_id = $2, updated_at = now() WHERE id = $1::uuid`, id, addressee); err != nil {
				return Document{}, err
			}
			if err := auditField(ctx, tx, id, actor.UserID, "адресат", doc.AddresseeUserID, addressee); err != nil {
				return Document{}, err
			}
		}
		if in.AddresseeDepartment != nil {
			department := strings.TrimSpace(*in.AddresseeDepartment)
			if _, err := tx.Exec(ctx, `UPDATE documents SET addressee_department_code = $2, updated_at = now() WHERE id = $1::uuid`, id, department); err != nil {
				return Document{}, err
			}
			if err := auditField(ctx, tx, id, actor.UserID, "отдел", doc.AddresseeDepartment, department); err != nil {
				return Document{}, err
			}
		}
		if in.DueDate != nil {
			rawDue := strings.TrimSpace(*in.DueDate)
			var parsedDue *time.Time
			if rawDue != "" {
				t, err := time.Parse("2006-01-02", rawDue)
				if err != nil {
					return Document{}, fmt.Errorf("%w: срок должен быть в формате ГГГГ-ММ-ДД", ErrInvalid)
				}
				parsedDue = &t
			}
			if _, err := tx.Exec(ctx, `UPDATE documents SET due_date = $2, updated_at = now() WHERE id = $1::uuid`, id, parsedDue); err != nil {
				return Document{}, err
			}
			var nextDue *string
			if parsedDue != nil {
				formatted := parsedDue.Format("2006-01-02")
				nextDue = &formatted
			}
			if err := auditField(ctx, tx, id, actor.UserID, "срок", dueAudit(doc.DueDate), dueAudit(nextDue)); err != nil {
				return Document{}, err
			}
		}
		if in.Amount != nil {
			var nextAmount *float64
			if *in.Amount == 0 {
				if _, err := tx.Exec(ctx, `UPDATE documents SET amount = NULL, updated_at = now() WHERE id = $1::uuid`, id); err != nil {
					return Document{}, err
				}
			} else {
				nextAmount = in.Amount
				if _, err := tx.Exec(ctx, `UPDATE documents SET amount = $2, updated_at = now() WHERE id = $1::uuid`, id, *in.Amount); err != nil {
					return Document{}, err
				}
			}
			if err := auditField(ctx, tx, id, actor.UserID, "сумма", amountAudit(doc.Amount), amountAudit(nextAmount)); err != nil {
				return Document{}, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, err
	}
	return loadDocument(ctx, s.pool, id, actor.UserID, actor.DepartmentCode)
}

func (s *Store) Register(ctx context.Context, actor Actor, id, idemKey, bodyHash string) ([]byte, int, error) {
	if !actor.canRegister() {
		return nil, 0, ErrForbidden
	}
	if body, code, ok, err := s.loadIdempotency(ctx, actor.UserID, idemKey, bodyHash); err != nil || ok {
		return body, code, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer tx.Rollback(ctx)
	doc, err := loadDocument(ctx, tx, id, actor.UserID, actor.DepartmentCode)
	if err != nil {
		return nil, 0, err
	}
	if doc.OrganizationCode != actor.OrganizationCode {
		return nil, 0, ErrForbidden
	}
	if doc.Status == "Отклонён" || doc.Status == "Закрыт (в деле)" {
		return nil, 0, fmt.Errorf("%w: документ нельзя регистрировать", ErrConflict)
	}
	var hasBlockingSign bool
	if doc.RouteInstanceID != nil {
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM route_steps WHERE route_id = $1::uuid AND kind = 'signing' AND is_blocking = true
			)
		`, *doc.RouteInstanceID).Scan(&hasBlockingSign); err != nil {
			return nil, 0, err
		}
	} else {
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM route_steps s
				JOIN routes r ON r.id = s.route_id
				JOIN document_types t ON t.id = r.type_id
				WHERE t.code = $1 AND r.is_template = true AND r.status = 'действует'
				  AND s.kind = 'signing' AND s.is_blocking = true
			)
		`, doc.TypeCode).Scan(&hasBlockingSign); err != nil {
			return nil, 0, err
		}
	}
	if hasBlockingSign && doc.Status != "Подписан" {
		var signed bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM step_tasks t
				JOIN route_steps s ON s.id = t.step_id
				WHERE t.document_id = $1::uuid AND s.kind = 'signing' AND t.state = 'подписано'
			)
		`, id).Scan(&signed); err != nil {
			return nil, 0, err
		}
		if !signed {
			return nil, 0, fmt.Errorf("%w: регистрация канцелярией возможна только после подписания документа", ErrConflict)
		}
	}
	number := ""
	if doc.RegNumber != nil {
		number = *doc.RegNumber
	}
	year := time.Now().UTC().Year()
	if number == "" {
		var err error
		number, year, err = assignTypeNumber(ctx, tx, id, actor.OrganizationCode, doc.TypeCode, doc.SubtypeCode)
		if err != nil {
			return nil, 0, err
		}
	}
	if doc.Status == "Зарегистрирован" {
		return nil, 0, fmt.Errorf("%w: номер уже присвоен", ErrConflict)
	}
	kind := streamKind(doc.TypeCode)
	journalID, err := ensureJournal(ctx, tx, actor.OrganizationCode, kind)
	if err != nil {
		return nil, 0, err
	}
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `
		UPDATE documents
		SET journal_id = $2::uuid, reg_number = $3, reg_date = COALESCE(reg_date, $4), reg_year = $5,
		    status = 'Зарегистрирован', onec_sync_status = 'ожидает', updated_at = now()
		WHERE id = $1::uuid
	`, id, journalID, number, now, year); err != nil {
		return nil, 0, err
	}
	payload := mustJSON(map[string]any{
		"document_id": id,
		"type_code":   doc.TypeCode,
		"reg_number":  number,
		"reg_date":    now,
		"title":       doc.Title,
	})
	if _, err := tx.Exec(ctx, `
		INSERT INTO outbox_events (id, document_id, event_type, idempotency_key, status, next_retry_at, payload)
		VALUES (gen_random_uuid(), $1::uuid, 'registration', $2, 'ожидает', now(), $3)
	`, id, idemKey, string(payload)); err != nil {
		return nil, 0, err
	}
	if err := writeAudit(ctx, tx, id, actor.UserID, "переход", doc.Status, "Зарегистрирован", ""); err != nil {
		return nil, 0, err
	}
	updated, err := loadDocument(ctx, tx, id, actor.UserID, actor.DepartmentCode)
	if err != nil {
		return nil, 0, err
	}
	body := mustJSON(updated)
	if err := insertIdempotency(ctx, tx, actor.UserID, idemKey, bodyHash, 200, body); err != nil {
		if isUnique(err) {
			_ = tx.Rollback(ctx)
			body, code, _, idemErr := s.loadIdempotency(ctx, actor.UserID, idemKey, bodyHash)
			return body, code, idemErr
		}
		return nil, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, err
	}
	return body, 200, nil
}

type querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func (s *Store) ensureRead(ctx context.Context, q querier, actor Actor, doc Document) error {
	if doc.OrganizationCode != actor.OrganizationCode {
		return ErrForbidden
	}
	if actor.canReadOrg() || actor.hasContour(doc.BusinessDomain, ContourLevelCards) || doc.AuthorUserID == actor.UserID || doc.AddresseeUserID == actor.UserID {
		return nil
	}
	var ok bool
	if err := q.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM step_tasks
			WHERE document_id = $1::uuid
			  AND (assignee_user_id = $2 OR deputy_for_user_id = $2 OR delegated_from_user_id = $2)
			UNION ALL
			SELECT 1 FROM route_steps rs
			JOIN documents d ON d.route_instance_id = rs.route_id
			WHERE d.id = $1::uuid AND d.route_started = true AND rs.assignee_user_id = $2
			UNION ALL
			SELECT 1 FROM document_grants WHERE document_id = $1::uuid AND user_id = $2 AND expires_at > now()
		)
	`, doc.ID, actor.UserID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return ErrForbidden
	}
	return nil
}

func loadDocument(ctx context.Context, q querier, id, viewer, viewerDept string) (Document, error) {
	var doc Document
	var dueDate *time.Time
	var caseLabel string
	err := q.QueryRow(ctx, `
		SELECT d.id::text, t.code, t.name, d.subtype_code, COALESCE(st.name, ''), d.business_domain,
		       t.stream_kind, d.title, d.body, d.status, d.importance,
		       d.organization_code, d.author_user_id, d.addressee_user_id, d.addressee_department_code,
		       d.counterparty_name,
		       d.counterparty_inn, d.service_name, d.due_date, d.amount::float8,
		       d.reg_number, d.reg_date, d.case_id::text, COALESCE(cn.code || ' — ' || cn.title, ''),
		       d.completed_by_author_at, d.customer_contract, d.onec_sync_status,
		       d.route_started, d.route_instance_id::text, r.status, v.id::text, v.number, d.created_at
		FROM documents d
		JOIN document_types t ON t.id = d.type_id
		JOIN document_versions v ON v.id = d.current_version_id
		LEFT JOIN document_subtypes st ON st.code = d.subtype_code
		LEFT JOIN routes r ON r.id = d.route_instance_id
		LEFT JOIN case_nomenclature cn ON cn.id = d.case_id
		WHERE d.id = $1::uuid
	`, id).Scan(
		&doc.ID, &doc.TypeCode, &doc.TypeName, &doc.SubtypeCode, &doc.SubtypeName, &doc.BusinessDomain,
		&doc.StreamKind, &doc.Title, &doc.Body, &doc.Status, &doc.Importance,
		&doc.OrganizationCode, &doc.AuthorUserID, &doc.AddresseeUserID, &doc.AddresseeDepartment, &doc.CounterpartyName,
		&doc.CounterpartyINN, &doc.ServiceName, &dueDate, &doc.Amount,
		&doc.RegNumber, &doc.RegDate, &doc.CaseID, &caseLabel,
		&doc.CompletedByAuthorAt, &doc.CustomerContract, &doc.OnecSyncStatus,
		&doc.RouteStarted, &doc.RouteInstanceID, &doc.RouteStatus, &doc.VersionID, &doc.VersionNumber, &doc.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Document{}, ErrNotFound
	}
	if err != nil {
		return Document{}, err
	}
	doc.CaseLabel = caseLabel
	if dueDate != nil {
		formatted := dueDate.Format("2006-01-02")
		doc.DueDate = &formatted
	}

	files, err := q.Query(ctx, `
		SELECT f.id::text, f.name, f.size_bytes, f.sha256, ver.number
		FROM files f
		JOIN document_versions ver ON ver.id = f.version_id
		WHERE f.document_id = $1::uuid
		ORDER BY ver.number, f.uploaded_at
	`, id)
	if err != nil {
		return Document{}, err
	}
	defer files.Close()
	for files.Next() {
		var file FileInfo
		if err := files.Scan(&file.ID, &file.Name, &file.SizeBytes, &file.SHA256, &file.VersionNumber); err != nil {
			return Document{}, err
		}
		doc.Files = append(doc.Files, file)
	}
	if doc.Files == nil {
		doc.Files = []FileInfo{}
	}

	if viewer != "" {
		taskRows, err := q.Query(ctx, `
			SELECT t.id::text, s.kind, t.state, t.plan_due_at
			FROM step_tasks t
			JOIN route_steps s ON s.id = t.step_id
			WHERE t.document_id = $1::uuid AND t.assignee_user_id = $2 AND t.state = 'назначена'
		`, id, viewer)
		if err != nil {
			return Document{}, err
		}
		defer taskRows.Close()
		for taskRows.Next() {
			var task TaskInfo
			if err := taskRows.Scan(&task.ID, &task.Kind, &task.State, &task.PlanDueAt); err != nil {
				return Document{}, err
			}
			doc.MyTasks = append(doc.MyTasks, task)
		}
	}
	if doc.MyTasks == nil {
		doc.MyTasks = []TaskInfo{}
	}

	routePath, err := loadRoutePath(ctx, q, doc)
	if err != nil {
		return Document{}, err
	}
	doc.RoutePath = routePath

	if err := loadDocumentExtras(ctx, q, &doc, viewer, viewerDept); err != nil {
		return Document{}, err
	}

	auditRows, err := q.Query(ctx, `
		SELECT at, user_id, action, COALESCE(from_state, ''), COALESCE(to_state, ''), COALESCE(reason, '')
		FROM audit_log
		WHERE document_id = $1::uuid
		  AND action <> 'просмотр'
		  AND NOT (action = 'файл' AND COALESCE(reason, '') LIKE 'скачивание %')
		ORDER BY at DESC
		LIMIT 30
	`, id)
	if err != nil {
		return Document{}, err
	}
	defer auditRows.Close()
	for auditRows.Next() {
		var entry AuditEntry
		if err := auditRows.Scan(&entry.At, &entry.UserID, &entry.Action, &entry.FromState, &entry.ToState, &entry.Reason); err != nil {
			return Document{}, err
		}
		doc.AuditTrail = append(doc.AuditTrail, entry)
	}
	if doc.AuditTrail == nil {
		doc.AuditTrail = []AuditEntry{}
	}

	return doc, files.Err()
}

func loadRoutePath(ctx context.Context, q querier, doc Document) ([]RouteStepView, error) {
	var routeID string
	if doc.RouteInstanceID != nil {
		routeID = *doc.RouteInstanceID
	} else {
		err := q.QueryRow(ctx, `
			SELECT r.id::text FROM routes r
			JOIN document_types t ON t.id = r.type_id
			WHERE t.code = $1 AND r.is_template = true AND r.status = 'действует'
			LIMIT 1
		`, doc.TypeCode).Scan(&routeID)
		if errors.Is(err, pgx.ErrNoRows) {
			return []RouteStepView{}, nil
		}
		if err != nil {
			return nil, err
		}
	}

	rows, err := q.Query(ctx, `
		SELECT s.id::text, COALESCE(t.id::text, ''), s.order_no, s.parallel_group, s.kind, COALESCE(s.caption, ''), s.is_blocking, s.is_mandatory, s.is_order_locked,
		       COALESCE(s.due_days, 3), s.condition_rule,
		       COALESCE(s.assignee_role, ''), COALESCE(t.assignee_user_id, COALESCE(s.assignee_user_id, '')),
		       COALESCE(t.deputy_for_user_id, ''), COALESCE(t.delegated_from_user_id, ''),
		       COALESCE(t.remark_addressed, false), t.accepted_at,
		       COALESCE(t.state, ''), t.plan_due_at, t.decided_at, COALESCE(t.comment, '')
		FROM route_steps s
		LEFT JOIN step_tasks t ON t.step_id = s.id AND t.document_id = $2::uuid AND t.state <> 'снята'
		WHERE s.route_id = $1::uuid
		ORDER BY s.order_no, s.kind, s.id
	`, routeID, doc.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var path []RouteStepView
	for rows.Next() {
		var v RouteStepView
		if err := rows.Scan(
			&v.StepID, &v.TaskID, &v.OrderNo, &v.ParallelGroup, &v.Kind, &v.Caption, &v.IsBlocking, &v.IsMandatory, &v.IsOrderLocked,
			&v.DueDays, &v.ConditionRule,
			&v.AssigneeRole, &v.AssigneeUserID,
			&v.DeputyForUserID, &v.DelegatedFromUserID, &v.RemarkAddressed, &v.AcceptedAt,
			&v.TaskState, &v.PlanDueAt, &v.DecidedAt, &v.Comment,
		); err != nil {
			return nil, err
		}
		switch v.TaskState {
		case "назначена", "ожидает_подтверждения":
			v.StageState = "текущий"
		case "согласовано", "подписано", "исполнено", "ознакомлен":
			v.StageState = "пройден"
		case "пропущен":
			v.StageState = "пропущен"
		case "замечания", "отклонено":
			v.StageState = "возврат_или_отказ"
		default:
			if v.ConditionRule != "" && !conditionMet(v.ConditionRule, doc) {
				v.StageState = "пропущен"
			} else {
				v.StageState = "предстоит"
			}
		}
		path = append(path, v)
	}
	if path == nil {
		path = []RouteStepView{}
	}
	for i := range path {
		if path[i].StageState != "текущий" {
			continue
		}
		for _, prev := range path {
			if prev.OrderNo < path[i].OrderNo && prev.IsBlocking && prev.StageState != "пройден" && prev.StageState != "пропущен" {
				path[i].StageState = "предстоит"
				path[i].PlanDueAt = nil
				break
			}
		}
	}
	return path, rows.Err()
}

func nullTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}

func nullUUID(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (s *Store) SaveFile(ctx context.Context, actor Actor, documentID, name string, body io.Reader) (Document, error) {
	if name == "" {
		return Document{}, fmt.Errorf("%w: нет имени файла", ErrInvalid)
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return Document{}, fmt.Errorf("%w: имя файла", ErrInvalid)
	}
	tmp, err := os.CreateTemp(s.root, "upload-*")
	if err != nil {
		return Document{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(tmp, hasher), io.LimitReader(body, s.maxFile+1))
	tmp.Close()
	if err != nil {
		return Document{}, err
	}
	if written > s.maxFile {
		return Document{}, ErrTooLarge
	}
	sum := hex.EncodeToString(hasher.Sum(nil))
	key := time.Now().UTC().Format("2006/01") + "/" + sum[:2] + "/" + sum
	dest := filepath.Join(s.root, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return Document{}, err
	}
	if _, err := os.Stat(dest); err != nil {
		if err := os.Rename(tmpName, dest); err != nil {
			src, openErr := os.Open(tmpName)
			if openErr != nil {
				return Document{}, openErr
			}
			defer src.Close()
			out, createErr := os.Create(dest)
			if createErr != nil {
				return Document{}, createErr
			}
			if _, copyErr := io.Copy(out, src); copyErr != nil {
				out.Close()
				return Document{}, copyErr
			}
			out.Close()
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Document{}, err
	}
	defer tx.Rollback(ctx)
	doc, err := loadDocument(ctx, tx, documentID, actor.UserID, actor.DepartmentCode)
	if err != nil {
		return Document{}, err
	}
	if err := s.ensureRead(ctx, tx, actor, doc); err != nil {
		return Document{}, err
	}
	if doc.AuthorUserID != actor.UserID && !actor.canWrite() && !actor.canEdit() && !actor.hasContour(doc.BusinessDomain, ContourLevelLead) {
		return Document{}, ErrForbidden
	}
	if doc.Status == "Отклонён" || doc.Status == "Закрыт (в деле)" {
		return Document{}, fmt.Errorf("%w: документ закрыт для файлов", ErrConflict)
	}
	versionID := doc.VersionID
	if doc.RouteStarted {
		var next int
		if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(number), 0) + 1 FROM document_versions WHERE document_id = $1::uuid`, documentID).Scan(&next); err != nil {
			return Document{}, err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO document_versions (id, document_id, number, created_by)
			VALUES (gen_random_uuid(), $1::uuid, $2, $3) RETURNING id::text
		`, documentID, next, actor.UserID).Scan(&versionID); err != nil {
			return Document{}, err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE documents SET current_version_id = $2::uuid, updated_at = now() WHERE id = $1::uuid
		`, documentID, versionID); err != nil {
			return Document{}, err
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO files (id, version_id, document_id, name, size_bytes, sha256, shard_volume, storage_key, uploaded_by)
		VALUES (gen_random_uuid(), $1::uuid, $2::uuid, $3, $4, $5, 0, $6, $7)
	`, versionID, documentID, name, written, sum, key, actor.UserID); err != nil {
		return Document{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE document_versions SET content_hash = $2 WHERE id = $1::uuid`, versionID, sum); err != nil {
		return Document{}, err
	}
	if err := auditField(ctx, tx, documentID, actor.UserID, "файл", "", name); err != nil {
		return Document{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Document{}, err
	}
	return loadDocument(ctx, s.pool, documentID, actor.UserID, actor.DepartmentCode)
}

func (s *Store) OpenFile(ctx context.Context, actor Actor, fileID string) (string, string, error) {
	var documentID, name, key string
	err := s.pool.QueryRow(ctx, `
		SELECT document_id::text, name, storage_key FROM files WHERE id = $1::uuid
	`, fileID).Scan(&documentID, &name, &key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	doc, err := loadDocument(ctx, s.pool, documentID, actor.UserID, actor.DepartmentCode)
	if err != nil {
		return "", "", err
	}
	if err := s.ensureRead(ctx, s.pool, actor, doc); err != nil {
		return "", "", err
	}
	if strings.Contains(key, "..") {
		return "", "", ErrInvalid
	}
	return filepath.Join(s.root, filepath.FromSlash(key)), name, nil
}

func (s *Store) DeleteDocument(ctx context.Context, actor Actor, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	doc, err := loadDocument(ctx, tx, id, actor.UserID, actor.DepartmentCode)
	if err != nil {
		return err
	}
	if doc.OrganizationCode != actor.OrganizationCode || !actor.has("admin") {
		return ErrForbidden
	}
	switch doc.Status {
	case "Закрыт (в деле)", "Зарегистрирован", "Подписан":
		return fmt.Errorf("%w: карточку в статусе «%s» удалить нельзя", ErrConflict, doc.Status)
	}
	var exported bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM outbox_events WHERE document_id = $1::uuid)`, id).Scan(&exported); err != nil {
		return err
	}
	if exported {
		return fmt.Errorf("%w: карточка уже ушла в учёт, удалить её нельзя", ErrConflict)
	}
	var keys []string
	keyRows, err := tx.Query(ctx, `SELECT storage_key FROM files WHERE document_id = $1::uuid`, id)
	if err != nil {
		return err
	}
	for keyRows.Next() {
		var key string
		if err := keyRows.Scan(&key); err != nil {
			keyRows.Close()
			return err
		}
		if key != "" && !strings.Contains(key, "..") {
			keys = append(keys, key)
		}
	}
	keyRows.Close()
	if err := keyRows.Err(); err != nil {
		return err
	}
	statements := []string{
		`DELETE FROM step_tasks WHERE document_id = $1::uuid`,
		`DELETE FROM document_links WHERE from_document_id = $1::uuid OR to_document_id = $1::uuid`,
		`DELETE FROM comments_reviews WHERE document_id = $1::uuid`,
		`DELETE FROM paper_movements WHERE document_id = $1::uuid`,
		`DELETE FROM document_tags WHERE document_id = $1::uuid`,
		`DELETE FROM user_bookmarks WHERE document_id = $1::uuid`,
		`DELETE FROM document_grants WHERE document_id = $1::uuid`,
		`DELETE FROM files WHERE document_id = $1::uuid`,
		`DELETE FROM document_versions WHERE document_id = $1::uuid`,
		`DELETE FROM route_steps WHERE route_id IN (SELECT id FROM routes WHERE document_id = $1::uuid)`,
		`DELETE FROM routes WHERE document_id = $1::uuid`,
		`DELETE FROM work_grids WHERE owner_kind = 'document' AND owner_id = $1::uuid`,
		`UPDATE calendar_events SET protocol_document_id = NULL WHERE protocol_document_id = $1::uuid`,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement, id); err != nil {
			return err
		}
	}
	if err := writeAudit(ctx, tx, id, actor.UserID, "удаление", doc.Status, "", doc.Title); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM documents WHERE id = $1::uuid`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	for _, key := range keys {
		_ = os.Remove(filepath.Join(s.root, filepath.FromSlash(key)))
	}
	return nil
}
