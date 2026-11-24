package store

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestValidateRouteStepsEdit(t *testing.T) {
	existing := []existingStepMeta{
		{ID: "s1", OrderNo: 1, Kind: "approval", Mandatory: false, OrderLocked: false},
		{ID: "s2", OrderNo: 2, Kind: "signing", Mandatory: true, OrderLocked: true},
	}

	// Попытка удалить обязательный шаг s2 -> ErrConflict
	err := validateRouteStepsEdit(existing, []RouteStepEditInput{
		{StepID: "s1", OrderNo: 1, Kind: "approval", DueDays: 2},
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict when deleting mandatory step, got %v", err)
	}

	// Попытка поменять порядок заблокированного шага s2 -> ErrConflict
	err = validateRouteStepsEdit(existing, []RouteStepEditInput{
		{StepID: "s1", OrderNo: 1, Kind: "approval", DueDays: 2},
		{StepID: "s2", OrderNo: 3, Kind: "signing", DueDays: 2},
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict when moving locked step, got %v", err)
	}

	// Корректное удаление необязательного s1 и добавление нового шага при сохранении s2 на месте
	err = validateRouteStepsEdit(existing, []RouteStepEditInput{
		{StepID: "", OrderNo: 1, Kind: "approval", DueDays: 1},
		{StepID: "s2", OrderNo: 2, Kind: "signing", DueDays: 2},
	})
	if err != nil {
		t.Fatalf("expected valid edit to succeed, got %v", err)
	}
}

func TestRenderApprovalSheetDocx(t *testing.T) {
	now := time.Now().UTC()
	reg := "ВН-12/2026"
	doc := Document{
		ID:               "11111111-1111-4111-8111-111111111111",
		TypeName:         "Договор",
		BusinessDomain:   "продажи",
		Title:            "Договор поставки №1",
		Status:           "Подписан",
		Importance:       "обычная",
		OrganizationCode: "ORG-01",
		AuthorUserID:     "u1",
		RegNumber:        &reg,
		VersionNumber:    2,
		RoutePath: []RouteStepView{
			{
				OrderNo:         1,
				Kind:            "approval",
				AssigneeUserID:  "u2",
				DeputyForUserID: "u3",
				RemarkAddressed: true,
				StageState:      "пройден",
				TaskState:       "согласовано",
				PlanDueAt:       &now,
				DecidedAt:       &now,
				Comment:         "Замечания сняты",
			},
		},
	}
	names := map[string]string{
		"u1": "Иванов И.И.",
		"u2": "Петров П.П.",
		"u3": "Сидоров С.С.",
	}

	raw, err := RenderApprovalSheetDocx(doc, names)
	if err != nil {
		t.Fatalf("RenderApprovalSheetDocx failed: %v", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("invalid zip/docx: %v", err)
	}
	var docXML string
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			b, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				t.Fatal(err)
			}
			docXML = string(b)
		}
	}
	if !strings.Contains(docXML, "ЛИСТ СОГЛАСОВАНИЯ") || !strings.Contains(docXML, "Петров П.П.") || !strings.Contains(docXML, "замечания учтены") {
		t.Fatalf("unexpected document.xml content: %s", docXML)
	}
}
