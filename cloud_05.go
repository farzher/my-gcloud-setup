package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func gcloudExists(ctx context.Context, args ...string) bool {
	args = append(args, "--format=value(name)")
	_, e := run(ctx, "gcloud", args...)
	return e == nil
}

func mergeResult(a, b commandResult) commandResult {
	if a.Stdout != "" && b.Stdout != "" {
		a.Stdout += "\n"
	}
	a.Stdout += b.Stdout
	if a.Stderr != "" && b.Stderr != "" {
		a.Stderr += "\n"
	}
	a.Stderr += b.Stderr
	if b.Command != "" {
		a.Command = b.Command
	}
	return a
}

func usefulOutput(r commandResult) string {
	var x []string
	if r.Stdout != "" {
		x = append(x, r.Stdout)
	}
	if r.Stderr != "" {
		x = append(x, r.Stderr)
	}
	return strings.Join(x, "\n")
}

func shellDisplay(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if strings.ContainsAny(a, " \t\n\"'") {
			out[i] = fmt.Sprintf("%q", a)
		} else {
			out[i] = a
		}
	}
	return strings.Join(out, " ")
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

func hasExecutable(name string) bool { _, e := exec.LookPath(name); return e == nil }

func firstLine(s string) string {
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

func nonEmptyLines(s string) []string {
	var o []string
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			o = append(o, l)
		}
	}
	return o
}

func uniqueLines(s string) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range nonEmptyLines(s) {
		if !seen[l] {
			seen[l] = true
			out = append(out, l)
		}
	}
	return out
}

func looksNotFound(s string) bool {
	x := strings.ToLower(s)
	return strings.Contains(x, "not found") || strings.Contains(x, "was not found")
}

func shortError(err error) string {
	if err == nil {
		return ""
	}
	s := strings.TrimSpace(err.Error())
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 64 {
		s = s[:61] + "..."
	}
	return s
}

func parseSite(input string) (name, domain string, err error) {
	s := strings.TrimSpace(input)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimSuffix(s, "/")
	if s == "" {
		return "", "", errors.New("enter a domain or name")
	}
	if strings.Contains(s, ".") && !strings.ContainsAny(s, " /\\") {
		domain = strings.ToLower(s)
		if !validDomain(domain) {
			return "", "", errors.New("invalid domain")
		}
		name = strings.ReplaceAll(domain, ".", "-")
		name, err = cleanProjectName(name)
		return name, domain, err
	}
	name, err = cleanProjectName(s)
	return name, "", err
}

func validDomain(s string) bool {
	if len(s) < 3 || len(s) > 253 || strings.HasPrefix(s, ".") || strings.HasSuffix(s, ".") {
		return false
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		if len(p) < 1 || len(p) > 63 || p[0] == '-' || p[len(p)-1] == '-' {
			return false
		}
		for _, r := range p {
			if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-') {
				return false
			}
		}
	}
	return true
}

func cleanProjectName(input string) (string, error) {
	input = strings.TrimSpace(input)
	var b strings.Builder
	lastDash := false
	for _, r := range input {
		allowed := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == ' ' || r == '-' || r == '\'' || r == '"' || r == '!'
		if strings.ContainsRune("._/\\:", r) {
			r = '-'
			allowed = true
		}
		if !allowed {
			continue
		}
		if r == '-' {
			if lastDash {
				continue
			}
			lastDash = true
		} else {
			lastDash = false
		}
		b.WriteRune(r)
	}
	name := strings.TrimSpace(b.String())
	rr := []rune(name)
	if len(rr) > 30 {
		rr = rr[:30]
		name = strings.TrimSpace(string(rr))
	}
	name = strings.TrimRight(name, " -'\"!")
	if len([]rune(name)) < 4 {
		return "", errors.New("4–30 chars")
	}
	first := []rune(name)[0]
	if !((first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z')) {
		return "", errors.New("start with a letter")
	}
	return name, nil
}

func repoSlug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	dash := false
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > 80 {
		slug = strings.Trim(slug[:80], "-")
	}
	return slug
}

func accountHash(account string) string {
	s := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(account))))
	return hex.EncodeToString(s[:])[:12]
}

func randomHex(n int) string {
	b := make([]byte, (n+1)/2)
	if _, e := rand.Read(b); e != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())[:n]
	}
	return hex.EncodeToString(b)[:n]
}

func configPath() string {
	dir, e := os.UserConfigDir()
	if e != nil || dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "cloud-charm", "config.json")
}

func loadConfig() config {
	c := config{Projects: map[string]string{}, Names: map[string]string{}, Domains: map[string]string{}, Billing: map[string]string{}, Repos: map[string]string{}, Disabled: map[string]bool{}}
	if data, e := os.ReadFile(configPath()); e == nil {
		_ = json.Unmarshal(data, &c)
	}
	if c.Projects == nil {
		c.Projects = map[string]string{}
	}
	if c.Names == nil {
		c.Names = map[string]string{}
	}
	if c.Domains == nil {
		c.Domains = map[string]string{}
	}
	if c.Billing == nil {
		c.Billing = map[string]string{}
	}
	if c.Repos == nil {
		c.Repos = map[string]string{}
	}
	if c.Disabled == nil {
		c.Disabled = map[string]bool{}
	}
	return c
}

func saveConfig(c config) error {
	p := configPath()
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		return e
	}
	data, e := json.MarshalIndent(c, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(p, data, 0600)
}
