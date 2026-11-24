package store

import "testing"

func TestDepartmentHead(t *testing.T) {
	people := []Colleague{
		{UserID: "dir", DepartmentCode: "SHOP", Roles: []string{"manager", "signer"}},
		{UserID: "eng", DepartmentCode: "SHOP", ManagerID: "dir", Roles: []string{"executor"}},
		{UserID: "tech", DepartmentCode: "SHOP", ManagerID: "dir", Roles: []string{"author"}},
		{UserID: "other", DepartmentCode: "OFFICE", ManagerID: "dir", Roles: []string{"clerk"}},
	}
	if got := DepartmentHead(people, "SHOP"); got != "dir" {
		t.Fatalf("руководитель отдела SHOP: %s", got)
	}
	outside := []Colleague{
		{UserID: "boss", DepartmentCode: "EXEC", Roles: []string{"manager"}},
		{UserID: "eng", DepartmentCode: "LAB", ManagerID: "boss", Roles: []string{"executor"}},
		{UserID: "tech", DepartmentCode: "LAB", ManagerID: "boss", Roles: []string{"author"}},
	}
	if got := DepartmentHead(outside, "LAB"); got != "boss" {
		t.Fatalf("руководитель может быть выше отдела, получили %s", got)
	}
}
