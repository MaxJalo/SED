package odata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Maxjalo/SED/temp-auth/internal/store"
)

const emptyKey = "00000000-0000-0000-0000-000000000000"

type Config struct {
	BaseURL  string
	Username string
	Password string
	OrgMatch string
	Timeout  time.Duration
}

type orgRow struct {
	Ref     string `json:"Ref_Key"`
	Name    string `json:"Description"`
	Code    string `json:"Code"`
	Deleted bool   `json:"DeletionMark"`
}

type deptRow struct {
	Ref       string `json:"Ref_Key"`
	Name      string `json:"Description"`
	Code      string `json:"Code"`
	Owner     string `json:"Owner_Key"`
	Parent    string `json:"Parent_Key"`
	HeadOrg   string `json:"ГоловнаяОрганизация_Key"`
	Deleted   bool   `json:"DeletionMark"`
	Disbanded bool   `json:"Расформировано"`
}

type namedRow struct {
	Ref     string `json:"Ref_Key"`
	Name    string `json:"Description"`
	Deleted bool   `json:"DeletionMark"`
	Archive bool   `json:"ВАрхиве"`
}

type assignment struct {
	Employee string `json:"Сотрудник_Key"`
	Org      string `json:"Организация_Key"`
	Dept     string `json:"Подразделение_Key"`
	Position string `json:"Должность_Key"`
	Event    string `json:"ВидСобытия"`
	Until    string `json:"ДействуетДо"`
	Rates    any    `json:"КоличествоСтавок"`
	Head     bool   `json:"ЭтоГоловнойСотрудник"`
}

type page struct {
	Value []json.RawMessage `json:"value"`
}

func LoadDirectory(ctx context.Context, cfg Config) ([]store.User, error) {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 60 * time.Second
	}
	client := &http.Client{Timeout: cfg.Timeout}
	asOf := time.Now().Format("2006-01-02")

	var orgs []orgRow
	if err := getAll(ctx, client, cfg, "Catalog_Организации", "$select=Ref_Key,Description,DeletionMark", &orgs); err != nil {
		return nil, err
	}
	selected := map[string]orgRow{}
	needle := normalize(cfg.OrgMatch)
	for _, org := range orgs {
		if org.Deleted || org.Ref == "" || org.Ref == emptyKey {
			continue
		}
		if needle != "" && !strings.Contains(normalize(org.Name), needle) {
			continue
		}
		selected[strings.ToLower(org.Ref)] = org
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("odata: ни одно юрлицо не подошло под фильтр")
	}

	var depts []deptRow
	if err := getAll(ctx, client, cfg, "Catalog_ПодразделенияОрганизаций", "$select=Ref_Key,Description,Code,Owner_Key,Parent_Key,ГоловнаяОрганизация_Key,DeletionMark,Расформировано", &depts); err != nil {
		return nil, err
	}
	var positions []namedRow
	if err := getAll(ctx, client, cfg, "Catalog_Должности", "$select=Ref_Key,Description,DeletionMark", &positions); err != nil {
		return nil, err
	}
	var employees []namedRow
	if err := getAll(ctx, client, cfg, "Catalog_Сотрудники", "$select=Ref_Key,Description,DeletionMark,ВАрхиве", &employees); err != nil {
		return nil, err
	}
	var assignments []assignment
	path := "InformationRegister_КадроваяИсторияСотрудников_RecordType/SliceLast(Period=datetime'" + asOf + "T00:00:00')"
	if err := getAll(ctx, client, cfg, path, "$select=Сотрудник_Key,Организация_Key,Подразделение_Key,Должность_Key,ВидСобытия,ДействуетДо,КоличествоСтавок,ЭтоГоловнойСотрудник", &assignments); err != nil {
		return nil, err
	}
	return buildDirectory(selected, depts, positions, employees, assignments, asOf), nil
}

func getAll(ctx context.Context, client *http.Client, cfg Config, entity, query string, dest any) error {
	const pageSize = 500
	var raw []json.RawMessage
	for skip := 0; ; skip += pageSize {
		q := query + fmt.Sprintf("&$format=json&$top=%d&$skip=%d", pageSize, skip)
		body, err := get(ctx, client, cfg, entity+"?"+q)
		if err != nil {
			return err
		}
		var chunk page
		if err := json.Unmarshal(body, &chunk); err != nil {
			return fmt.Errorf("odata %s: %w", entity, err)
		}
		raw = append(raw, chunk.Value...)
		if len(chunk.Value) < pageSize {
			break
		}
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, dest)
}

func get(ctx context.Context, client *http.Client, cfg Config, pathAndQuery string) ([]byte, error) {
	base := strings.TrimRight(cfg.BaseURL, "/")
	full, err := url.Parse(base + "/" + pathAndQuery)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full.String(), nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(cfg.Username, cfg.Password)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		text := string(body)
		if len(text) > 300 {
			text = text[:300]
		}
		return nil, fmt.Errorf("odata %s: http %d: %s", pathAndQuery, resp.StatusCode, text)
	}
	return body, nil
}

func buildDirectory(orgs map[string]orgRow, depts []deptRow, positions, employees []namedRow, assignments []assignment, asOf string) []store.User {
	deptByID := map[string]deptRow{}
	for _, dept := range depts {
		if dept.Deleted || dept.Disbanded || dept.Ref == "" {
			continue
		}
		owner := strings.ToLower(dept.Owner)
		head := strings.ToLower(dept.HeadOrg)
		if _, ok := orgs[owner]; !ok {
			if _, ok = orgs[head]; !ok {
				continue
			}
		}
		deptByID[strings.ToLower(dept.Ref)] = dept
	}
	posName := map[string]string{}
	for _, row := range positions {
		if !row.Deleted {
			posName[strings.ToLower(row.Ref)] = strings.TrimSpace(row.Name)
		}
	}
	empName := map[string]namedRow{}
	for _, row := range employees {
		if !row.Deleted && !row.Archive {
			empName[strings.ToLower(row.Ref)] = row
		}
	}

	type seat struct {
		userID   string
		orgKey   string
		deptKey  string
		position string
		head     bool
	}
	seats := map[string]seat{}
	for _, row := range assignments {
		orgKey := strings.ToLower(row.Org)
		if _, ok := orgs[orgKey]; !ok {
			continue
		}
		empKey := strings.ToLower(row.Employee)
		person, ok := empName[empKey]
		if !ok || !currentAssignment(row, asOf) {
			continue
		}
		next := seat{
			userID:   person.Ref,
			orgKey:   orgKey,
			deptKey:  strings.ToLower(row.Dept),
			position: posName[strings.ToLower(row.Position)],
			head:     row.Head,
		}
		if prev, exists := seats[empKey]; exists && prev.head && !next.head {
			continue
		}
		seats[empKey] = next
	}

	parent := map[string]string{}
	for id, dept := range deptByID {
		p := strings.ToLower(dept.Parent)
		if p == "" || p == emptyKey || p == id {
			continue
		}
		parent[id] = p
	}
	heads := map[string]string{}
	headRank := map[string]int{}
	for _, seat := range seats {
		rank := headRankOf(seat.position)
		if rank == 0 || seat.deptKey == "" {
			continue
		}
		if rank > headRank[seat.deptKey] {
			heads[seat.deptKey] = seat.userID
			headRank[seat.deptKey] = rank
		}
	}

	managerOf := func(userID, deptKey string) string {
		seen := map[string]bool{}
		current := deptKey
		first := true
		for current != "" && !seen[current] {
			seen[current] = true
			if head := heads[current]; head != "" && (head != userID || !first) {
				if head != userID {
					return head
				}
			}
			first = false
			current = parent[current]
		}
		return ""
	}

	users := make([]store.User, 0, len(seats))
	byID := map[string]int{}
	for _, seat := range seats {
		org := orgs[seat.orgKey]
		dept := deptByID[seat.deptKey]
		person := empName[strings.ToLower(seat.userID)]
		orgCode := strings.TrimSpace(org.Name)
		user := store.User{
			UserID:           seat.userID,
			FullName:         strings.TrimSpace(person.Name),
			Position:         seat.position,
			DepartmentCode:   strings.TrimSpace(dept.Code),
			DepartmentName:   strings.TrimSpace(dept.Name),
			OrganizationCode: orgCode,
			Roles:            rolesFor(seat.position),
		}
		byID[user.UserID] = len(users)
		users = append(users, user)
	}
	for i := range users {
		seat := seats[strings.ToLower(users[i].UserID)]
		users[i].ManagerID = managerOf(users[i].UserID, seat.deptKey)
	}
	for i := range users {
		users[i].TopManagerID = topManager(users[i].UserID, users, byID)
	}
	return users
}

func topManager(userID string, users []store.User, byID map[string]int) string {
	seen := map[string]bool{}
	current := userID
	last := ""
	for current != "" && !seen[current] {
		seen[current] = true
		idx, ok := byID[current]
		if !ok {
			break
		}
		next := users[idx].ManagerID
		if next == "" || next == current {
			return current
		}
		last = next
		current = next
	}
	if last == "" {
		return userID
	}
	return last
}

func currentAssignment(row assignment, asOf string) bool {
	event := normalize(row.Event)
	if strings.Contains(event, "увольн") || event == "неподтвержденныеданные" {
		return false
	}
	until := row.Until
	if len(until) >= 10 && !strings.HasPrefix(until, "0001-01-01") && until[:10] < asOf {
		return false
	}
	switch value := row.Rates.(type) {
	case float64:
		if value <= 0 {
			return false
		}
	case string:
		if value == "0" || value == "0.0" {
			return false
		}
	}
	return row.Employee != "" && row.Employee != emptyKey
}

func headRankOf(position string) int {
	name := normalize(position)
	switch {
	case strings.Contains(name, "генеральн"):
		return 4
	case strings.Contains(name, "директор"):
		return 3
	case strings.Contains(name, "руководител"), strings.Contains(name, "заведующ"):
		return 2
	case strings.Contains(name, "начальник"):
		return 1
	default:
		return 0
	}
}

func rolesFor(position string) []string {
	name := normalize(position)
	roles := []string{"author", "executor"}
	if headRankOf(position) >= 3 {
		roles = append(roles, "manager", "signer")
	} else if headRankOf(position) > 0 {
		roles = append(roles, "manager")
	}
	if strings.Contains(name, "делопроизвод") || strings.Contains(name, "канцеляр") {
		roles = append(roles, "clerk")
	}
	if strings.Contains(name, "юрист") || strings.Contains(name, "юрисконсульт") {
		roles = append(roles, "approver")
	}
	if strings.Contains(name, "контрол") {
		roles = append(roles, "controller")
	}
	if strings.Contains(name, "администратор") {
		roles = append(roles, "admin")
	}
	return roles
}

func normalize(value string) string {
	return strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "ё", "е"))
}
