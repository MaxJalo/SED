package store

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound  = errors.New("не найдено")
	ErrForbidden = errors.New("недостаточно прав")
	ErrConflict  = errors.New("конфликт")
	ErrInvalid   = errors.New("некорректный запрос")
	ErrTooLarge  = errors.New("файл больше 50 МБ")
)

type Store struct {
	pool    *pgxpool.Pool
	root    string
	maxFile int64
}

func New(pool *pgxpool.Pool, fileRoot string, maxFile int64) *Store {
	return &Store{pool: pool, root: fileRoot, maxFile: maxFile}
}

type Colleague struct {
	UserID           string
	FullName         string
	Position         string
	DepartmentCode   string
	DepartmentName   string
	OrganizationCode string
	ManagerID        string
	TopManagerID     string
	Roles            []string
}

type Effect struct {
	Kind       string    `json:"kind"`
	DocumentID string    `json:"document_id"`
	TaskID     string    `json:"task_id,omitempty"`
	AssigneeID string    `json:"assignee_id,omitempty"`
	AuthorID   string    `json:"author_id,omitempty"`
	Title      string    `json:"title,omitempty"`
	RegNumber  string    `json:"reg_number,omitempty"`
	Due        time.Time `json:"due,omitempty"`
}

type Document struct {
	ID                  string          `json:"id"`
	TypeCode            string          `json:"type_code"`
	TypeName            string          `json:"type_name"`
	SubtypeCode         string          `json:"subtype_code,omitempty"`
	SubtypeName         string          `json:"subtype_name,omitempty"`
	BusinessDomain      string          `json:"business_domain"`
	StreamKind          string          `json:"stream_kind"`
	Title               string          `json:"title"`
	Body                string          `json:"body"`
	Status              string          `json:"status"`
	Importance          string          `json:"importance"`
	OrganizationCode    string          `json:"organization_code"`
	AuthorUserID        string          `json:"author_user_id"`
	AddresseeUserID     string          `json:"addressee_user_id,omitempty"`
	AddresseeDepartment string          `json:"addressee_department_code,omitempty"`
	CounterpartyName    string          `json:"counterparty_name,omitempty"`
	CounterpartyINN     string          `json:"counterparty_inn,omitempty"`
	ServiceName         string          `json:"service_name,omitempty"`
	DueDate             *string         `json:"due_date,omitempty"`
	Amount              *float64        `json:"amount,omitempty"`
	RegNumber           *string         `json:"reg_number"`
	RegDate             *time.Time      `json:"reg_date"`
	CaseID              *string         `json:"case_id,omitempty"`
	CaseLabel           string          `json:"case_label,omitempty"`
	CompletedByAuthorAt *time.Time      `json:"completed_by_author_at,omitempty"`
	CustomerContract    bool            `json:"customer_contract"`
	OnecSyncStatus      string          `json:"onec_sync_status"`
	RouteStarted        bool            `json:"route_started"`
	RouteInstanceID     *string         `json:"route_instance_id"`
	RouteStatus         *string         `json:"route_status"`
	VersionID           string          `json:"version_id"`
	VersionNumber       int             `json:"version_number"`
	Bookmarked          bool            `json:"bookmarked,omitempty"`
	BookmarkNote        string          `json:"bookmark_note,omitempty"`
	Files               []FileInfo      `json:"files,omitempty"`
	MyTasks             []TaskInfo      `json:"my_tasks,omitempty"`
	RoutePath           []RouteStepView `json:"route_path,omitempty"`
	Links               []DocumentLink  `json:"links,omitempty"`
	CommentsReviews     []CommentReview `json:"comments_reviews,omitempty"`
	PaperMovements      []PaperMovement `json:"paper_movements,omitempty"`
	Tags                []TagInfo       `json:"tags,omitempty"`
	Grants              []GrantInfo     `json:"grants,omitempty"`
	AuditTrail          []AuditEntry    `json:"audit_trail,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
}

type RouteStepView struct {
	StepID              string     `json:"step_id,omitempty"`
	TaskID              string     `json:"task_id,omitempty"`
	OrderNo             int        `json:"order_no"`
	ParallelGroup       *int       `json:"parallel_group,omitempty"`
	Kind                string     `json:"kind"`
	Caption             string     `json:"caption,omitempty"`
	IsBlocking          bool       `json:"is_blocking"`
	IsMandatory         bool       `json:"is_mandatory"`
	IsOrderLocked       bool       `json:"is_order_locked"`
	DueDays             int        `json:"due_days"`
	ConditionRule       string     `json:"condition_rule,omitempty"`
	AssigneeRole        string     `json:"assignee_role,omitempty"`
	AssigneeUserID      string     `json:"assignee_user_id,omitempty"`
	DeputyForUserID     string     `json:"deputy_for_user_id,omitempty"`
	DelegatedFromUserID string     `json:"delegated_from_user_id,omitempty"`
	RemarkAddressed     bool       `json:"remark_addressed,omitempty"`
	AcceptedAt          *time.Time `json:"accepted_at,omitempty"`
	StageState          string     `json:"stage_state"` // "пройден" | "текущий" | "предстоит" | "пропущен" | "возврат_или_отказ"
	TaskState           string     `json:"task_state,omitempty"`
	PlanDueAt           *time.Time `json:"plan_due_at,omitempty"`
	DecidedAt           *time.Time `json:"decided_at,omitempty"`
	Comment             string     `json:"comment,omitempty"`
}

type DocumentLink struct {
	ID             string    `json:"id"`
	Direction      string    `json:"direction"` // "parent" | "child"
	OtherDocID     string    `json:"other_doc_id"`
	OtherTitle     string    `json:"other_title"`
	OtherStatus    string    `json:"other_status"`
	OtherTypeName  string    `json:"other_type_name"`
	OtherRegNumber string    `json:"other_reg_number,omitempty"`
	LinkType       string    `json:"link_type"`
	CreatedAt      time.Time `json:"created_at"`
}

type CommentReview struct {
	ID           string    `json:"id"`
	EntryKind    string    `json:"entry_kind"` // "comment" | "review"
	AuthorUserID string    `json:"author_user_id"`
	TargetUserID string    `json:"target_user_id,omitempty"`
	Body         string    `json:"body"`
	CreatedAt    time.Time `json:"created_at"`
}

type PaperMovement struct {
	ID           string    `json:"id"`
	HolderUserID string    `json:"holder_user_id"`
	MovedAt      time.Time `json:"moved_at"`
	Note         string    `json:"note,omitempty"`
	RecordedBy   string    `json:"recorded_by"`
}

type TagInfo struct {
	ID             string `json:"id"`
	Scope          string `json:"scope"`
	DepartmentCode string `json:"department_code,omitempty"`
	Name           string `json:"name"`
}

type CaseInfo struct {
	ID             string `json:"id"`
	Code           string `json:"code"`
	Title          string `json:"title"`
	Year           int    `json:"year"`
	RetentionYears int    `json:"retention_years"`
	Storage        string `json:"storage"`
}

type TemplateInfo struct {
	ID                 string `json:"id"`
	TypeCode           string `json:"type_code"`
	Name               string `json:"name"`
	DefaultTitle       string `json:"default_title"`
	BodyTemplate       string `json:"body_template"`
	DefaultServiceName string `json:"default_service_name,omitempty"`
}

type DeputyInfo struct {
	ID           string `json:"id"`
	UserID       string `json:"user_id"`
	DeputyUserID string `json:"deputy_user_id"`
	ValidFrom    string `json:"valid_from"`
	ValidTo      string `json:"valid_to"`
	Active       bool   `json:"active"`
	CreatedBy    string `json:"created_by"`
}

type AuditEntry struct {
	At        time.Time `json:"at"`
	UserID    string    `json:"user_id"`
	Action    string    `json:"action"`
	FromState string    `json:"from_state,omitempty"`
	ToState   string    `json:"to_state,omitempty"`
	Reason    string    `json:"reason,omitempty"`
}

type FileInfo struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	SizeBytes     int64  `json:"size_bytes"`
	SHA256        string `json:"sha256"`
	VersionNumber int    `json:"version_number"`
}

type SubtypeInfo struct {
	ID                 string `json:"id"`
	TypeCode           string `json:"type_code"`
	Code               string `json:"code"`
	Name               string `json:"name"`
	BusinessDomain     string `json:"business_domain"`
	DefaultTitle       string `json:"default_title"`
	BodyTemplate       string `json:"body_template"`
	DefaultImportance  string `json:"default_importance"`
	DefaultServiceName string `json:"default_service_name,omitempty"`
}

type GrantInfo struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	GrantedBy string    `json:"granted_by"`
	ExpiresAt time.Time `json:"expires_at"`
}

type TaskSummary struct {
	NewCount       int `json:"new_count"`
	OverdueCount   int `json:"overdue_count"`
	TotalToMe      int `json:"total_to_me"`
	ForExecution   int `json:"for_execution"`
	PendingConfirm int `json:"pending_confirm"`
	OnControl      int `json:"on_control"`
	FromMe         int `json:"from_me"`
	Bookmarks      int `json:"bookmarks"`
}

type BookmarkItem struct {
	DocumentID string    `json:"document_id"`
	Title      string    `json:"title"`
	Status     string    `json:"status"`
	TypeName   string    `json:"type_name"`
	NoteText   string    `json:"note_text"`
	CreatedAt  time.Time `json:"created_at"`
}

type RouteStepEditInput struct {
	StepID         string `json:"step_id,omitempty"`
	OrderNo        int    `json:"order_no"`
	Kind           string `json:"kind"`
	DueDays        int    `json:"due_days"`
	AssigneeRole   string `json:"assignee_role,omitempty"`
	AssigneeUserID string `json:"assignee_user_id,omitempty"`
	IsBlocking     bool   `json:"is_blocking"`
}

type TaskInfo struct {
	ID                  string     `json:"id"`
	DocumentID          string     `json:"document_id,omitempty"`
	Title               string     `json:"title,omitempty"`
	DocumentStatus      string     `json:"document_status,omitempty"`
	AuthorUserID        string     `json:"author_user_id,omitempty"`
	AssigneeUserID      string     `json:"assignee_user_id,omitempty"`
	DeputyForUserID     string     `json:"deputy_for_user_id,omitempty"`
	DelegatedFromUserID string     `json:"delegated_from_user_id,omitempty"`
	Kind                string     `json:"kind"`
	State               string     `json:"state"`
	Comment             string     `json:"comment,omitempty"`
	AcceptedAt          *time.Time `json:"accepted_at,omitempty"`
	StartedAt           *time.Time `json:"started_at,omitempty"`
	PlanDueAt           *time.Time `json:"plan_due_at,omitempty"`
}

type ListPage struct {
	Items      []Document `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
}

func encodeCursor(t time.Time, id string) string {
	raw := t.UTC().Format(time.RFC3339Nano) + "|" + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func DecodeCursor(value string) (time.Time, string, error) {
	if value == "" {
		return time.Time{}, "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return time.Time{}, "", ErrInvalid
	}
	stamp, id, ok := strings.Cut(string(raw), "|")
	if !ok || id == "" {
		return time.Time{}, "", ErrInvalid
	}
	t, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return time.Time{}, "", ErrInvalid
	}
	return t, id, nil
}

func isUnique(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

func (s *Store) loadIdempotency(ctx context.Context, userID, key, hash string) ([]byte, int, bool, error) {
	var body []byte
	var code int
	var stored string
	err := s.pool.QueryRow(ctx, `
		SELECT body_hash, response_code, response_body
		FROM idempotency_keys
		WHERE user_id = $1 AND idem_key = $2
	`, userID, key).Scan(&stored, &code, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, 0, false, nil
	}
	if err != nil {
		return nil, 0, false, err
	}
	if stored != hash {
		return nil, 0, false, fmt.Errorf("%w: тот же Idempotency-Key использован с другим телом", ErrConflict)
	}
	return body, code, true, nil
}

func insertIdempotency(ctx context.Context, tx pgx.Tx, userID, key, hash string, code int, body []byte) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO idempotency_keys (user_id, idem_key, body_hash, response_code, response_body)
		VALUES ($1, $2, $3, $4, $5::jsonb)
	`, userID, key, hash, code, body)
	return err
}

func writeAudit(ctx context.Context, tx pgx.Tx, documentID, userID, action, from, to, reason string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO audit_log (id, document_id, user_id, action, from_state, to_state, reason)
		VALUES (gen_random_uuid(), $1::uuid, $2, $3, NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''))
	`, documentID, userID, action, from, to, reason)
	return err
}

func pickColleague(list []Colleague, org, role string) (string, bool) {
	for _, person := range list {
		if person.OrganizationCode != org {
			continue
		}
		for _, item := range person.Roles {
			if item == role {
				return person.UserID, true
			}
		}
	}
	return "", false
}

func streamKind(typeCode string) string {
	switch typeCode {
	case "входящий", "счет":
		return "входящий"
	case "исходящий", "ткп":
		return "исходящий"
	default:
		return "внутренний"
	}
}

func orgIndex(org string) string {
	low := strings.ToLower(org)
	if strings.Contains(low, "нпо") || strings.Contains(low, "турбулент") || strings.EqualFold(org, "ORG-01") {
		return "НПО"
	}
	return "ОРГ"
}

func numberCode(typeCode, subtype string) (string, int) {
	switch typeCode {
	case "входящий":
		return "ВХ", 4
	case "исходящий":
		return "ИСХ", 4
	case "служебная_записка":
		return "СЗ", 4
	case "приказ":
		if subtype == "приказ_лс" || strings.Contains(strings.ToLower(subtype), "лич") {
			return "ЛС", 3
		}
		return "П", 3
	case "распоряжение":
		return "Р", 3
	case "протокол":
		return "ПР", 3
	case "поручение":
		return "ПОР", 4
	case "заявка":
		return "ЗВ", 4
	case "договор":
		return "ДГ", 4
	case "допсоглашение":
		return "ДС", 3
	case "лна":
		return "ЛНА", 3
	case "тз":
		return "ТЗ", 3
	case "ткп":
		return "ТКП", 3
	case "счет":
		return "СЧ", 4
	case "акт":
		return "АКТ", 3
	default:
		return "ДОК", 4
	}
}

func formatRegNumber(org, typeCode, subtype string, year, seq int) string {
	code, width := numberCode(typeCode, subtype)
	return fmt.Sprintf("%s-%s-%0*d/%02d", orgIndex(org), code, width, seq, year%100)
}

func assignTypeNumber(ctx context.Context, tx pgx.Tx, docID, org, typeCode, subtype string) (string, int, error) {
	year := time.Now().UTC().Year()
	code, _ := numberCode(typeCode, subtype)
	var seq int
	if err := tx.QueryRow(ctx, `
		INSERT INTO type_number_counters (organization_code, type_code, year, last_value)
		VALUES ($1, $2, $3, 1)
		ON CONFLICT (organization_code, type_code, year)
		DO UPDATE SET last_value = type_number_counters.last_value + 1
		RETURNING last_value
	`, org, code, year).Scan(&seq); err != nil {
		return "", 0, err
	}
	number := formatRegNumber(org, typeCode, subtype, year, seq)
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `
		UPDATE documents
		SET reg_number = $2, reg_year = $3, reg_date = COALESCE(reg_date, $4), updated_at = now()
		WHERE id = $1::uuid AND (reg_number IS NULL OR reg_number = '')
	`, docID, number, year, now); err != nil {
		return "", 0, err
	}
	return number, year, nil
}

func ensureJournal(ctx context.Context, tx pgx.Tx, org, kind string) (string, error) {
	prefix := "ВН"
	name := "Внутренние"
	switch kind {
	case "входящий":
		prefix, name = "ВХ", "Входящие"
	case "исходящий":
		prefix, name = "ИСХ", "Исходящие"
	}
	var id string
	err := tx.QueryRow(ctx, `
		INSERT INTO registration_journals (id, organization_code, stream_kind, name, prefix)
		VALUES (gen_random_uuid(), $1, $2, $3, $4)
		ON CONFLICT (organization_code, stream_kind) DO UPDATE SET name = registration_journals.name
		RETURNING id::text
	`, org, kind, name+" "+org, prefix).Scan(&id)
	return id, err
}

func mustJSON(v any) []byte {
	body, err := json.Marshal(v)
	if err != nil {
		return []byte(`{"error":"json"}`)
	}
	return body
}
