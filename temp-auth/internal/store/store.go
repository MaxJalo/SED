package store

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

var (
	ErrUserNotFound   = errors.New("user not found")
	ErrSessionRevoked = errors.New("session revoked or unknown")
	ErrUserBlocked    = errors.New("user is blocked")
	ErrBadCredentials = errors.New("неверный логин или пароль")
)

type User struct {
	UserID           string   `json:"user_id"`
	FullName         string   `json:"full_name"`
	Position         string   `json:"position"`
	DepartmentCode   string   `json:"department_code"`
	DepartmentName   string   `json:"department_name"`
	OrganizationCode string   `json:"organization_code"`
	ManagerID        string   `json:"manager_id,omitempty"`
	TopManagerID     string   `json:"top_manager_id,omitempty"`
	Roles            []string `json:"roles"`
	HasPOA           bool     `json:"has_power_of_attorney,omitempty"`
}

type Session struct {
	JTI       string
	UserID    string
	CreatedAt time.Time
	ExpiresAt time.Time
	Revoked   bool
}

type Store struct {
	mu        sync.RWMutex
	users     map[string]User
	order     []string
	sessions  map[string]*Session
	blocked   map[string]bool
	passwords map[string][]byte
}

func NewDefaultStore() *Store {
	return NewStore(defaultUsers())
}

func NewStore(users []User) *Store {
	s := &Store{
		users:     make(map[string]User, len(users)),
		order:     make([]string, 0, len(users)),
		sessions:  make(map[string]*Session),
		blocked:   map[string]bool{},
		passwords: map[string][]byte{},
	}
	for _, u := range users {
		s.users[u.UserID] = u
		s.order = append(s.order, u.UserID)
	}
	return s
}

func defaultUsers() []User {
	return []User{
		{
			UserID:           "usr-org1-admin",
			FullName:         "Администратор Системы",
			Position:         "Администратор сервиса",
			DepartmentCode:   "IT",
			DepartmentName:   "Управление ИТ",
			OrganizationCode: "ORG-01",
			ManagerID:        "usr-org1-ceo",
			TopManagerID:     "usr-org1-ceo",
			Roles:            []string{"admin"},
		},
		{
			UserID:           "usr-org1-ceo",
			FullName:         "Генеральный Директор",
			Position:         "Руководитель юридического лица",
			DepartmentCode:   "EXEC",
			DepartmentName:   "Дирекция",
			OrganizationCode: "ORG-01",
			Roles:            []string{"manager", "signer"},
			HasPOA:           true,
		},
		{
			UserID:           "usr-org1-clerk",
			FullName:         "Сотрудник Канцелярии",
			Position:         "Делопроизводитель",
			DepartmentCode:   "OFFICE",
			DepartmentName:   "Канцелярия",
			OrganizationCode: "ORG-01",
			ManagerID:        "usr-org1-ceo",
			TopManagerID:     "usr-org1-ceo",
			Roles:            []string{"clerk"},
		},
		{
			UserID:           "usr-org1-head-sales",
			FullName:         "Руководитель Отдела",
			Position:         "Начальник отдела продаж",
			DepartmentCode:   "SALES",
			DepartmentName:   "Отдел продаж",
			OrganizationCode: "ORG-01",
			ManagerID:        "usr-org1-ceo",
			TopManagerID:     "usr-org1-ceo",
			Roles:            []string{"manager", "signer", "author"},
			HasPOA:           true,
		},
		{
			UserID:           "usr-org1-author",
			FullName:         "Инициатор Документов",
			Position:         "Ведущий специалист",
			DepartmentCode:   "SALES",
			DepartmentName:   "Отдел продаж",
			OrganizationCode: "ORG-01",
			ManagerID:        "usr-org1-head-sales",
			TopManagerID:     "usr-org1-ceo",
			Roles:            []string{"author"},
		},
		{
			UserID:           "usr-org1-lawyer",
			FullName:         "Юрист Согласующий",
			Position:         "Ведущий юрисконсульт",
			DepartmentCode:   "LEGAL",
			DepartmentName:   "Юридический отдел",
			OrganizationCode: "ORG-01",
			ManagerID:        "usr-org1-ceo",
			TopManagerID:     "usr-org1-ceo",
			Roles:            []string{"approver"},
		},
		{
			UserID:           "usr-org1-executor",
			FullName:         "Исполнитель Поручений",
			Position:         "Инженер",
			DepartmentCode:   "TECH",
			DepartmentName:   "Технический отдел",
			OrganizationCode: "ORG-01",
			ManagerID:        "usr-org1-ceo",
			TopManagerID:     "usr-org1-ceo",
			Roles:            []string{"executor"},
		},
		{
			UserID:           "usr-org1-controller",
			FullName:         "Контролёр Сроков",
			Position:         "Специалист контроля исполнения",
			DepartmentCode:   "CTRL",
			DepartmentName:   "Служба контроля",
			OrganizationCode: "ORG-01",
			ManagerID:        "usr-org1-ceo",
			TopManagerID:     "usr-org1-ceo",
			Roles:            []string{"controller"},
		},
		{
			UserID:           "usr-org1-observer",
			FullName:         "Наблюдатель Профиля",
			Position:         "Аудитор-наблюдатель",
			DepartmentCode:   "CTRL",
			DepartmentName:   "Служба контроля",
			OrganizationCode: "ORG-01",
			ManagerID:        "usr-org1-controller",
			TopManagerID:     "usr-org1-ceo",
			Roles:            []string{"observer"},
		},
		{
			UserID:           "usr-org2-author",
			FullName:         "Сотрудник Второго Юрлица",
			Position:         "Менеджер",
			DepartmentCode:   "OPS",
			DepartmentName:   "Операционный отдел",
			OrganizationCode: "ORG-02",
			Roles:            []string{"author"},
		},
	}
}

func (s *Store) FindUserByID(userID string) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[userID]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return u, nil
}

func (s *Store) FindUserByRole(role string) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, id := range s.order {
		u := s.users[id]
		for _, r := range u.Roles {
			if r == role {
				return u, nil
			}
		}
	}
	return User{}, ErrUserNotFound
}

func (s *Store) ListDirectory(orgCode string) []User {
	return s.listDirectory(orgCode, false)
}

func (s *Store) ListPublicDirectory(orgCode string) []User {
	return s.listDirectory(orgCode, true)
}

func (s *Store) listDirectory(orgCode string, hidePasswordUsers bool) []User {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make([]User, 0, len(s.order))
	for _, id := range s.order {
		if hidePasswordUsers && s.passwords[id] != nil {
			continue
		}
		u := s.users[id]
		if orgCode != "" && u.OrganizationCode != orgCode {
			continue
		}
		res = append(res, u)
	}
	return res
}

func (s *Store) UpsertPasswordUser(user User, password string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[user.UserID]; !ok {
		s.order = append(s.order, user.UserID)
	}
	s.users[user.UserID] = user
	sum := sha256.Sum256([]byte(password))
	s.passwords[user.UserID] = append([]byte(nil), sum[:]...)
}

func (s *Store) HasPassword(userID string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.passwords[userID] != nil
}

func (s *Store) Authenticate(login, password string) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.users[login]
	sum := sha256.Sum256([]byte(password))
	stored := s.passwords[login]
	if !ok || stored == nil || subtle.ConstantTimeCompare(stored, sum[:]) != 1 {
		return User{}, ErrBadCredentials
	}
	if s.blocked[login] {
		return User{}, ErrUserBlocked
	}
	return user, nil
}

func (s *Store) CreateSession(userID string, ttl time.Duration, now time.Time) (Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[userID]; !ok {
		return Session{}, ErrUserNotFound
	}
	if s.blocked[userID] {
		return Session{}, ErrUserBlocked
	}
	jti := randomJTI()
	sess := &Session{
		JTI:       jti,
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: now.Add(ttl),
		Revoked:   false,
	}
	s.sessions[jti] = sess
	return *sess, nil
}

func (s *Store) ValidateSession(jti, userID string, now time.Time) (User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.blocked[userID] {
		return User{}, ErrUserBlocked
	}
	sess, ok := s.sessions[jti]
	if !ok || sess.Revoked || sess.UserID != userID || now.After(sess.ExpiresAt) {
		return User{}, ErrSessionRevoked
	}
	u, ok := s.users[userID]
	if !ok {
		return User{}, ErrUserNotFound
	}
	return u, nil
}

func (s *Store) RevokeSession(jti string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[jti]
	if !ok {
		return false
	}
	sess.Revoked = true
	return true
}

func (s *Store) SetBlocked(userID string, blocked bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[userID]; !ok {
		return ErrUserNotFound
	}
	if blocked {
		s.blocked[userID] = true
	} else {
		delete(s.blocked, userID)
	}
	return nil
}

func randomJTI() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
