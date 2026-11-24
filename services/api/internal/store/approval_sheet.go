package store

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"strings"
)

func (s *Store) BuildApprovalSheetDocx(ctx context.Context, actor Actor, docID string, names map[string]string) ([]byte, string, error) {
	doc, err := loadDocument(ctx, s.pool, docID, actor.UserID, actor.DepartmentCode)
	if err != nil {
		return nil, "", err
	}
	if err := s.ensureRead(ctx, s.pool, actor, doc); err != nil {
		return nil, "", err
	}
	data, err := RenderApprovalSheetDocx(doc, names)
	if err != nil {
		return nil, "", err
	}
	filename := fmt.Sprintf("approval-sheet-%s.docx", doc.ID[:8])
	return data, filename, nil
}

func RenderApprovalSheetDocx(doc Document, names map[string]string) ([]byte, error) {
	var bodyXML strings.Builder
	addPara := func(text string, bold bool) {
		var esc bytes.Buffer
		_ = xml.EscapeText(&esc, []byte(text))
		rPr := ""
		if bold {
			rPr = "<w:rPr><w:b/></w:rPr>"
		}
		fmt.Fprintf(&bodyXML, "<w:p><w:r>%s<w:t xml:space=\"preserve\">%s</w:t></w:r></w:p>", rPr, esc.String())
	}

	userLabel := func(id string) string {
		if id == "" {
			return "—"
		}
		if label, ok := names[id]; ok && label != "" {
			return label
		}
		return id
	}

	reg := "без номера"
	if doc.RegNumber != nil && *doc.RegNumber != "" {
		reg = *doc.RegNumber
	}
	addPara("ЛИСТ СОГЛАСОВАНИЯ", true)
	addPara(fmt.Sprintf("Документ: %s (%s)", doc.Title, doc.TypeName), false)
	addPara(fmt.Sprintf("Рег. номер: %s | Версия: %d | Статус: %s | Важность: %s", reg, doc.VersionNumber, doc.Status, doc.Importance), false)
	addPara(fmt.Sprintf("Организация: %s | Автор: %s | Контур: %s", doc.OrganizationCode, userLabel(doc.AuthorUserID), doc.BusinessDomain), false)
	addPara(" ", false)
	addPara("Этапы маршрута и решения:", true)

	for _, step := range doc.RoutePath {
		who := userLabel(step.AssigneeUserID)
		if step.AssigneeUserID == "" && step.AssigneeRole != "" {
			who = "Роль: " + step.AssigneeRole
		}
		var extras []string
		if step.DeputyForUserID != "" {
			extras = append(extras, "замещает: "+userLabel(step.DeputyForUserID))
		}
		if step.DelegatedFromUserID != "" {
			extras = append(extras, "делегировано от: "+userLabel(step.DelegatedFromUserID))
		}
		if step.RemarkAddressed {
			extras = append(extras, "замечания учтены")
		}
		plan := "—"
		if step.PlanDueAt != nil {
			plan = step.PlanDueAt.Format("2006-01-02 15:04")
		}
		fact := "—"
		if step.DecidedAt != nil {
			fact = step.DecidedAt.Format("2006-01-02 15:04")
		}
		slaNote := "в срок"
		if step.PlanDueAt != nil && step.DecidedAt != nil && step.DecidedAt.After(*step.PlanDueAt) {
			slaNote = "просрочка SLA"
		}
		line := fmt.Sprintf(
			"Шаг %d [%s] | %s | Состояние: %s (%s) | План: %s | Факт: %s | SLA: %s",
			step.OrderNo, step.Kind, who, step.StageState, step.TaskState, plan, fact, slaNote,
		)
		if len(extras) > 0 {
			line += " | " + strings.Join(extras, ", ")
		}
		if step.Comment != "" {
			line += " | Комментарий: " + step.Comment
		}
		addPara(line, false)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"[Content_Types].xml": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`,
		"_rels/.rels": `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`,
		"word/document.xml": fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>%s<w:sectPr/></w:body>
</w:document>`, bodyXML.String()),
	}
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(content)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
