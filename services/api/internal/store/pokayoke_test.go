package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCheckLinkPairMatrix(t *testing.T) {
	if err := checkLinkPair("d1", "договор", "Подписан", "d2", "допсоглашение", "Черновик", "допсоглашение"); err != nil {
		t.Fatalf("expected valid contract->addendum link, got %v", err)
	}
	if err := checkLinkPair("d1", "счет", "Черновик", "d2", "допсоглашение", "Черновик", "допсоглашение"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for non-contract parent of addendum, got %v", err)
	}
	if err := checkLinkPair("d1", "протокол", "Подписан", "d2", "поручение", "Черновик", "пункт_протокола"); err != nil {
		t.Fatalf("expected valid protocol->item link, got %v", err)
	}
	if err := checkLinkPair("d1", "служебная_записка", "Черновик", "d2", "поручение", "Черновик", "пункт_протокола"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for non-protocol parent, got %v", err)
	}
	if err := checkLinkPair("d1", "входящий", "Зарегистрирован", "d2", "исходящий", "Черновик", "ответ"); err != nil {
		t.Fatalf("expected valid incoming->outgoing reply, got %v", err)
	}
	if err := checkLinkPair("d1", "договор", "Подписан", "d2", "исходящий", "Черновик", "ответ"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for reply on contract, got %v", err)
	}
	if err := checkLinkPair("d1", "договор", "Подписан", "d1", "договор", "Подписан", "основание"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for self-link, got %v", err)
	}
	if err := checkLinkPair("d1", "входящий", "Отклонён", "d2", "исходящий", "Черновик", "ответ"); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict for rejected document link, got %v", err)
	}
}

func TestAuditPairShowsOnlyRealChanges(t *testing.T) {
	if _, _, changed := auditPair("  Договор  ", "Договор"); changed {
		t.Fatal("same text must not be logged")
	}
	from, to, changed := auditPair("", "120000")
	if !changed || from != "" || to != "120000" {
		t.Fatalf("new value must be kept, got %q -> %q changed=%v", from, to, changed)
	}
	long := strings.Repeat("я", 250)
	_, clipped, changed := auditPair("коротко", long)
	if !changed || len([]rune(clipped)) != 241 {
		t.Fatalf("long value must be clipped to 240 runes plus ellipsis, got %d", len([]rune(clipped)))
	}
}

func TestValidateFormatFields(t *testing.T) {
	pos := 100.5
	zero := 0.0
	neg := -10.0

	if err := validateFormatFields("", nil); err != nil {
		t.Fatalf("empty inn/amount should pass, got %v", err)
	}
	if err := validateFormatFields("7707083893", &pos); err != nil {
		t.Fatalf("10-digit INN and positive amount should pass, got %v", err)
	}
	if err := validateFormatFields("770708389301", &pos); err != nil {
		t.Fatalf("12-digit INN should pass, got %v", err)
	}
	if err := validateFormatFields("12345", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for short INN, got %v", err)
	}
	if err := validateFormatFields("770708389A", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for non-digit INN, got %v", err)
	}
	if err := validateFormatFields("", &zero); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for zero amount, got %v", err)
	}
	if err := validateFormatFields("", &neg); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid for negative amount, got %v", err)
	}
}

func TestNoSelfVisaAndNoBlindPick(t *testing.T) {
	for _, kind := range []string{"approval", "signing", "resolution", "review"} {
		if err := checkNoSelfVisa(kind, "u-author", "u-author"); !errors.Is(err, ErrConflict) {
			t.Fatalf("expected ErrConflict for self-visa on %s, got %v", kind, err)
		}
		if err := checkNoSelfVisa(kind, "u-author", "u-other"); err != nil {
			t.Fatalf("unexpected error for non-author on %s: %v", kind, err)
		}
	}
	if err := checkNoSelfVisa("execution", "u-author", "u-author"); err != nil {
		t.Fatalf("execution by author should be allowed, got %v", err)
	}
	if err := checkNoSelfVisa("acknowledgment", "u-author", "u-author"); err != nil {
		t.Fatalf("acknowledgment by author should be allowed, got %v", err)
	}

	colleagues := []Colleague{
		{UserID: "u-top", OrganizationCode: "ORG-01", Roles: []string{"signer"}},
		{UserID: "u-other", OrganizationCode: "ORG-01", Roles: []string{"approver"}},
	}
	doc := Document{OrganizationCode: "ORG-01", AuthorUserID: "u-top"}
	if _, err := resolveStepAssignee(stepRow{Kind: "approval", AssigneeRole: "approver"}, doc, colleagues); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict when step has no explicit assignee (no blind pickColleague), got %v", err)
	}
	if conditionMet("skip_no_manager", doc) {
		t.Fatal("skip_no_manager should evaluate to false")
	}
}

func TestDecisionAndCloseToCaseGuards(t *testing.T) {
	if _, err := decisionState("approval", Decision{Decision: "return", Reason: ""}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid when return reason is empty, got %v", err)
	}
	if _, err := decisionState("signing", Decision{Decision: "reject", Reason: ""}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("expected ErrInvalid when reject reason is empty, got %v", err)
	}

	now := time.Now()
	if err := canCloseToCase("Черновик", &now, 0); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict for closing draft, got %v", err)
	}
	if err := canCloseToCase("Подписан", nil, 0); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict when author has not completed doc, got %v", err)
	}
	if err := canCloseToCase("Подписан", &now, 2); !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict when active tasks remain, got %v", err)
	}
	if err := canCloseToCase("Подписан", &now, 0); err != nil {
		t.Fatalf("expected closing completed doc with 0 active tasks to succeed, got %v", err)
	}
	if err := canCloseToCase("Отклонён", nil, 0); err != nil {
		t.Fatalf("expected closing rejected doc with 0 active tasks to succeed, got %v", err)
	}
}
