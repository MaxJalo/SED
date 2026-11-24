package store

import "testing"

func TestCanManageDeputy(t *testing.T) {
	colleagues := []Colleague{
		{UserID: "u-dir", OrganizationCode: "ORG-01", DepartmentCode: "EXEC", Roles: []string{"manager", "signer"}},
		{UserID: "u-mgr", OrganizationCode: "ORG-01", DepartmentCode: "IT", ManagerID: "u-dir", Roles: []string{"manager"}},
		{UserID: "u-emp", OrganizationCode: "ORG-01", DepartmentCode: "IT", ManagerID: "u-mgr", Roles: []string{"executor"}},
		{UserID: "u-fin", OrganizationCode: "ORG-01", DepartmentCode: "FIN", ManagerID: "u-dir", Roles: []string{"approver"}},
		{UserID: "u-clerk", OrganizationCode: "ORG-01", DepartmentCode: "CHANCERY", Roles: []string{"clerk"}},
	}

	clerk := Actor{UserID: "u-clerk", OrganizationCode: "ORG-01", DepartmentCode: "CHANCERY", Roles: []string{"clerk"}}
	if !canManageDeputy(clerk, colleagues, "u-fin") {
		t.Fatal("clerk should be able to manage deputies for any org user")
	}

	mgr := Actor{UserID: "u-mgr", OrganizationCode: "ORG-01", DepartmentCode: "IT", Roles: []string{"manager"}}
	if !canManageDeputy(mgr, colleagues, "u-mgr") {
		t.Fatal("manager should manage own deputy")
	}
	if !canManageDeputy(mgr, colleagues, "u-emp") {
		t.Fatal("manager should manage deputy for subordinate in same department")
	}
	if canManageDeputy(mgr, colleagues, "u-fin") {
		t.Fatal("manager should NOT manage deputy for another department")
	}

	exec := Actor{UserID: "u-emp", OrganizationCode: "ORG-01", DepartmentCode: "IT", Roles: []string{"executor"}}
	if canManageDeputy(exec, colleagues, "u-emp") {
		t.Fatal("executor should not manage deputies")
	}
}

func TestResolveStepAssignee(t *testing.T) {
	colleagues := []Colleague{
		{UserID: "u-mgr", OrganizationCode: "ORG-01", Roles: []string{"manager"}},
		{UserID: "u-emp", OrganizationCode: "ORG-01", ManagerID: "u-mgr", Roles: []string{"author"}},
		{UserID: "u-sign", OrganizationCode: "ORG-01", Roles: []string{"signer"}},
	}
	doc := Document{
		OrganizationCode: "ORG-01",
		AuthorUserID:     "u-emp",
		AddresseeUserID:  "u-sign",
	}

	got, err := resolveStepAssignee(stepRow{Kind: "approval", AssigneeRole: "manager"}, doc, colleagues)
	if err != nil || got != "u-mgr" {
		t.Fatalf("expected u-mgr, got %q, err=%v", got, err)
	}

	got, err = resolveStepAssignee(stepRow{Kind: "execution", AssigneeRole: "executor"}, doc, colleagues)
	if err != nil || got != "u-sign" {
		t.Fatalf("expected addressee u-sign for execution, got %q, err=%v", got, err)
	}
}
