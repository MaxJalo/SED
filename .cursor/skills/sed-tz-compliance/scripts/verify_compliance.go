package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func main() {
	var failures []string

	// 1. Verify required test directories for all non-internal boundaries
	requiredDirs := []string{
		"tests/api-v1",
		"tests/worker",
		"tests/orchestrator-gateway",
		"tests/onec-outbox",
		"tests/reverse-proxy",
		"tests/web-client",
		"tests/thin-client",
		"tests/thick-client",
		"tests/acceptance-ac",
		"temp-auth/tests",
		"devtools/mock-external/tests",
	}
	for _, dir := range requiredDirs {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			failures = append(failures, fmt.Sprintf("missing non-internal test directory: %s", dir))
		}
	}

	// 2. Verify temp-auth isolation in docker-compose.yml
	if composeBytes, err := os.ReadFile("docker-compose.yml"); err == nil {
		lower := strings.ToLower(string(composeBytes))
		if strings.Contains(lower, "temp-auth") {
			failures = append(failures, "docker-compose.yml must NOT contain temp-auth (must live in docker-compose.dev-auth.yml)")
		}
	} else {
		failures = append(failures, "missing docker-compose.yml")
	}

	if _, err := os.Stat("docker-compose.dev-auth.yml"); err != nil {
		failures = append(failures, "missing docker-compose.dev-auth.yml for isolated temp-auth")
	}

	// 3. Scan services/ Go code & SQL migrations for TZ invariant violations
	auditMutateRe := regexp.MustCompile(`(?i)\b(UPDATE|DELETE\s+FROM)\s+(?:public\.)?(audit_log|journal_audit|ж_аудита)\b`)
	offsetRe := regexp.MustCompile(`(?i)\bOFFSET\s+\$\d+|\bOFFSET\s+\d+`)
	tempAuthImportRe := regexp.MustCompile(`github\.com/Maxjalo/SED/temp-auth`)

	for _, root := range []string{"services", "web", "clients"} {
		if _, err := os.Stat(root); err != nil {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if ext != ".go" && ext != ".sql" && ext != ".ts" && ext != ".tsx" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			text := string(data)
			norm := filepath.ToSlash(path)

			if tempAuthImportRe.MatchString(text) {
				failures = append(failures, fmt.Sprintf("%s: forbidden direct import of temp-auth", norm))
			}
			if auditMutateRe.MatchString(text) {
				failures = append(failures, fmt.Sprintf("%s: forbidden UPDATE/DELETE on append-only audit table", norm))
			}
			if ext == ".sql" || ext == ".go" {
				if offsetRe.MatchString(text) && !strings.Contains(norm, "_test.go") {
					failures = append(failures, fmt.Sprintf("%s: OFFSET pagination detected (TZ requires cursor pagination)", norm))
				}
			}
			return nil
		})
	}

	if len(failures) > 0 {
		fmt.Fprintln(os.Stderr, "TZ COMPLIANCE AUDIT FAILED:")
		for _, f := range failures {
			fmt.Fprintln(os.Stderr, "  -", f)
		}
		os.Exit(1)
	}
	fmt.Println("verify_compliance: OK (all TZ invariants & isolation checks passed)")
}
