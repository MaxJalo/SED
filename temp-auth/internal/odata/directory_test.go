package odata

import "testing"

func TestBuildDirectoryManagerChain(t *testing.T) {
	orgs := map[string]orgRow{
		"org-1": {Ref: "org-1", Name: "Юрлицо"},
	}
	depts := []deptRow{
		{Ref: "root", Name: "Дирекция", Code: "01", Owner: "org-1"},
		{Ref: "shop", Name: "Цех", Code: "02", Owner: "org-1", Parent: "root"},
	}
	positions := []namedRow{
		{Ref: "p-dir", Name: "Генеральный директор"},
		{Ref: "p-head", Name: "Начальник цеха"},
		{Ref: "p-work", Name: "Инженер"},
	}
	employees := []namedRow{
		{Ref: "e-dir", Name: "Директоров А.А."},
		{Ref: "e-head", Name: "Начальников Б.Б."},
		{Ref: "e-work", Name: "Инженеров В.В."},
	}
	rows := []assignment{
		{Employee: "e-dir", Org: "org-1", Dept: "root", Position: "p-dir", Event: "Прием", Rates: 1.0, Head: true},
		{Employee: "e-head", Org: "org-1", Dept: "shop", Position: "p-head", Event: "Перемещение", Rates: 1.0, Head: true},
		{Employee: "e-work", Org: "org-1", Dept: "shop", Position: "p-work", Event: "Прием", Rates: 1.0, Head: true},
		{Employee: "e-work", Org: "org-1", Dept: "shop", Position: "p-work", Event: "Увольнение", Rates: 0.0},
	}
	users := buildDirectory(orgs, depts, positions, employees, rows, "2026-11-20")
	byID := map[string]string{}
	managers := map[string]string{}
	for _, user := range users {
		byID[user.UserID] = user.FullName
		managers[user.UserID] = user.ManagerID
	}
	if managers["e-work"] != "e-head" {
		t.Fatalf("инженер должен подчиняться начальнику цеха, получили %s", managers["e-work"])
	}
	if managers["e-head"] != "e-dir" {
		t.Fatalf("начальник цеха должен подчиняться директору, получили %s", managers["e-head"])
	}
	if len(users) != 3 {
		t.Fatalf("увольнение не должно добавлять человека, людей %d", len(users))
	}
}
