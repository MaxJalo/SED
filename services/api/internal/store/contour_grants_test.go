package store

import "testing"

func TestContourLevelsHierarchy(t *testing.T) {
	actor := Actor{
		UserID:           "u-1",
		OrganizationCode: "ORG-01",
		Roles:            []string{"author"},
		ContourLevels: map[string]string{
			"общий":        ContourLevelView,
			"продажи":      ContourLevelCards,
			"закупки":      ContourLevelLead,
			"производство": ContourLevelManage,
		},
	}

	if !actor.hasContour("общий", ContourLevelView) {
		t.Fatal("общий should satisfy view")
	}
	if actor.hasContour("общий", ContourLevelCards) {
		t.Fatal("общий view should not satisfy cards")
	}
	if actor.hasContour("кб", ContourLevelView) {
		t.Fatal("общий should not open other contours")
	}
	if !actor.hasContour("продажи", ContourLevelView) || !actor.hasContour("продажи", ContourLevelCards) {
		t.Fatal("продажи cards should satisfy view and cards")
	}
	if actor.hasContour("продажи", ContourLevelLead) {
		t.Fatal("продажи cards should not satisfy lead")
	}
	if !actor.hasContour("закупки", ContourLevelLead) || !actor.hasContour("закупки", ContourLevelCards) {
		t.Fatal("закупки lead should satisfy cards and lead")
	}
	if actor.canManageContour("закупки") {
		t.Fatal("закупки lead should not manage contour")
	}
	if !actor.canManageContour("производство") {
		t.Fatal("производство manage should manage производство")
	}
	if actor.canManageContour("продажи") {
		t.Fatal("производство manage should not manage продажи")
	}
}

func TestCanCreateInDomain(t *testing.T) {
	leadOnly := Actor{
		UserID:        "u-lead",
		Roles:         []string{"observer"},
		ContourLevels: map[string]string{"продажи": ContourLevelLead, "общий": ContourLevelCards},
	}
	if !leadOnly.CanCreateInDomain("служебная_записка", "продажи") {
		t.Fatal("lead in продажи should create in продажи")
	}
	if leadOnly.CanCreateInDomain("служебная_записка", "закупки") {
		t.Fatal("lead in продажи should not create in закупки")
	}
	if leadOnly.CanCreateInDomain("служебная_записка", "общий") {
		t.Fatal("cards in общий should not create in общий")
	}

	orgWrite := Actor{
		UserID: "u-write",
		Roles:  []string{"write"},
	}
	if !orgWrite.CanCreateInDomain("служебная_записка", "кб") {
		t.Fatal("org write should create in any contour")
	}
}

func TestValidateDomainAndLevel(t *testing.T) {
	for _, d := range []string{"общий", "продажи", "закупки", "производство", "кб", "качество"} {
		if err := validateDomain(d); err != nil {
			t.Fatalf("expected valid domain %s: %v", d, err)
		}
	}
	if err := validateDomain("unknown"); err == nil {
		t.Fatal("expected error for unknown domain")
	}
	for _, lvl := range []string{"view", "cards", "lead", "manage"} {
		if err := validateContourLevel(lvl, false); err != nil {
			t.Fatalf("expected valid level %s: %v", lvl, err)
		}
	}
	if err := validateContourLevel("", false); err == nil {
		t.Fatal("expected error for empty level when allowEmpty=false")
	}
	if err := validateContourLevel("", true); err != nil {
		t.Fatalf("expected empty level to be allowed when allowEmpty=true: %v", err)
	}
}
