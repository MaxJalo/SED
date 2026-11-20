package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var forbiddenExtensions = map[string]bool{
	".docx": true,
	".doc":  true,
	".pem":  true,
	".key":  true,
	".pfx":  true,
	".p12":  true,
	".crt":  true,
	".jks":  true,
	".dump": true,
}

var forbiddenExactNames = map[string]bool{
	".env":       true,
	".env.local": true,
	".env.prod":  true,
}

// SHA-256 hashes of lower-cased corporate / internal terms that must not leak into public repo.
var forbiddenTermHashes = map[string]bool{
	"166c111baaf1e10e4fc1ecc580cdf6e97c3cf7f96c7361c3dc8f7f3b5393a2e6": true,
	"23186aced7b9086c2d1df19abdf889355eafc8d614bdb338b487206c4293d45f": true,
	"9925277fb7f780cb00527cac3fe02ab082db9544dfc8e5a954f4da386aa2ba08": true,
	"2bccaa351d4937c38d829dead4b2beb7f470079e7a746dbd9cc1d73480eb8821": true,
	"3248275a2f37d183ca171d56455c0053f20add6d9ae07bc78445be4958c009a5": true,
	"812fdac6bf76de62525dbf5d058dad9054008c93715b60134d4ec5e45ff8162d": true,
}

var secretPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	{"Private Key Block", regexp.MustCompile(`-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`)},
	{"GitHub Classic Token", regexp.MustCompile(`ghp_[A-Za-z0-9]{30,}`)},
	{"GitHub Fine-Grained Token", regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`)},
	{"AWS Access Key ID", regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{"Slack Token", regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`)},
	{"Generic OpenAI/API Key", regexp.MustCompile(`\bsk-[A-Za-z0-9]{24,}\b`)},
	{"Hardcoded Private IPv4", regexp.MustCompile(`\b(?:192\.168\.\d{1,3}\.\d{1,3}|10\.\d{1,3}\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3})\b`)},
}

var pgDsnRe = regexp.MustCompile(`postgres(?:ql)?://([^:\s]+):([^@\s]+)@`)
var wordSplitRe = regexp.MustCompile(`[^\p{L}\p{N}_./-]+`)

func main() {
	staged := flag.Bool("staged", false, "Scan only git staged files")
	all := flag.Bool("all", false, "Scan all tracked/non-ignored files")
	flag.Parse()

	if !*staged && !*all {
		*all = true
	}

	var files []string
	var err error
	if *staged {
		files, err = getStagedFiles()
	} else {
		files, err = getRepoFiles()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "secret_guard: failed to list files: %v\n", err)
		os.Exit(2)
	}

	var violations []string
	for _, f := range files {
		if f == "" {
			continue
		}
		if strings.HasPrefix(f, "scripts/secret_guard.go") {
			continue
		}
		violations = append(violations, scanFile(f)...)
	}

	if len(violations) > 0 {
		fmt.Fprintln(os.Stderr, "SECRET/LEAK GUARD FAILED:")
		for _, v := range violations {
			fmt.Fprintln(os.Stderr, "  -", v)
		}
		os.Exit(1)
	}
	fmt.Printf("secret_guard: OK (%d files scanned)\n", len(files))
}

func getStagedFiles() ([]string, error) {
	cmd := exec.Command("git", "diff", "--cached", "--name-only", "--diff-filter=ACM")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	return lines, nil
}

func getRepoFiles() ([]string, error) {
	if _, err := os.Stat(".git"); err == nil {
		cmd := exec.Command("git", "ls-files")
		out, err := cmd.Output()
		if err == nil && len(bytes.TrimSpace(out)) > 0 {
			return strings.Split(strings.TrimSpace(string(out)), "\n"), nil
		}
	}

	var result []string
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if name == ".git" || name == "node_modules" || name == "вспомогателньые" || name == "вспомогательные" {
				return filepath.SkipDir
			}
			return nil
		}
		norm := filepath.ToSlash(path)
		if norm == "ТЗ_Сервис_Документооборота.md" || norm == "ТЗ_Сервис_Документооборота.docx" {
			return nil
		}
		result = append(result, norm)
		return nil
	})
	return result, err
}

func scanFile(path string) []string {
	var out []string
	base := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(path))

	if forbiddenExactNames[base] || (strings.HasPrefix(base, ".env.") && base != ".env.example") {
		out = append(out, fmt.Sprintf("%s: forbidden environment file", path))
		return out
	}
	if forbiddenExtensions[ext] {
		out = append(out, fmt.Sprintf("%s: forbidden extension %s", path, ext))
		return out
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	if bytes.IndexByte(data, 0) >= 0 {
		// Binary file; skip text regexes
		return out
	}

	content := string(data)
	for _, p := range secretPatterns {
		if p.re.MatchString(content) {
			out = append(out, fmt.Sprintf("%s: matched %s", path, p.name))
		}
	}

	for _, m := range pgDsnRe.FindAllStringSubmatch(content, -1) {
		pass := m[2]
		if !isTemplateCredential(pass) {
			out = append(out, fmt.Sprintf("%s: non-template password in PostgreSQL DSN", path))
		}
	}

	lower := strings.ToLower(content)
	for _, tok := range wordSplitRe.Split(lower, -1) {
		if len(tok) < 4 {
			continue
		}
		sum := sha256.Sum256([]byte(tok))
		h := hex.EncodeToString(sum[:])
		if forbiddenTermHashes[h] {
			out = append(out, fmt.Sprintf("%s: matched forbidden internal/corporate term (sha256:%s)", path, h[:12]))
		}
	}

	return out
}

func isTemplateCredential(val string) bool {
	l := strings.ToLower(val)
	return strings.Contains(l, "change_me") ||
		strings.Contains(l, "example") ||
		strings.Contains(l, "dev") ||
		strings.Contains(l, "test") ||
		strings.Contains(l, "sed_user") ||
		strings.HasPrefix(val, "${") ||
		strings.HasPrefix(val, "<")
}
