package roles

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestCatalogMatchesPromptDir is the SoT-drift detector. It walks the
// markdown files under backend/internal/prompts/roles/ and asserts that
// (a) the set of role_ids in the markdown frontmatter matches the set
// of IDs in the hand-maintained catalog, and (b) each role's title /
// version / use_case agree across the two locations.
//
// This test fires before any code consuming the catalog can run; if it
// fails, the developer who added a markdown file forgot to update
// catalog.go (or vice versa).
func TestCatalogMatchesPromptDir(t *testing.T) {
	rolesDir := promptsRolesDir(t)
	entries, err := os.ReadDir(rolesDir)
	if err != nil {
		t.Fatalf("read prompts/roles dir %q: %v", rolesDir, err)
	}

	type fmRole struct {
		title   string
		version int
		useCase string
	}
	fileRoles := map[string]fmRole{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") || name == "README.md" {
			continue
		}
		path := filepath.Join(rolesDir, name)
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatalf("read %q: %v", path, readErr)
		}
		fm := parseFrontmatter(t, string(body), path)
		roleID := fm["role_id"]
		if roleID == "" {
			t.Fatalf("%s: frontmatter missing role_id", name)
		}
		if filename := strings.TrimSuffix(name, ".md"); filename != roleID {
			t.Fatalf("%s: filename %q must equal role_id %q", name, filename, roleID)
		}
		ver, _ := strconv.Atoi(fm["version"])
		fileRoles[roleID] = fmRole{
			title:   fm["title"],
			version: ver,
			useCase: fm["use_case"],
		}
	}

	catalogRoles := map[string]fmRole{}
	for _, r := range catalog {
		catalogRoles[r.ID] = fmRole{title: r.Title, version: r.Version, useCase: r.UseCase}
	}

	missing := setDiff(keys(fileRoles), keys(catalogRoles))
	if len(missing) > 0 {
		t.Errorf("catalog drift: prompts/roles/ has roles not in catalog.go: %v", missing)
	}
	extra := setDiff(keys(catalogRoles), keys(fileRoles))
	if len(extra) > 0 {
		t.Errorf("catalog drift: catalog.go has roles without a prompt file: %v", extra)
	}

	for id, fileR := range fileRoles {
		catR, ok := catalogRoles[id]
		if !ok {
			continue
		}
		if fileR.title != catR.title {
			t.Errorf("%s: title mismatch — prompt %q vs catalog %q", id, fileR.title, catR.title)
		}
		if fileR.version != catR.version {
			t.Errorf("%s: version mismatch — prompt %d vs catalog %d", id, fileR.version, catR.version)
		}
		if fileR.useCase != catR.useCase {
			t.Errorf("%s: use_case mismatch\nprompt:  %q\ncatalog: %q", id, fileR.useCase, catR.useCase)
		}
	}

	// Every role MUST have a positive DefaultTimeoutSec. Zero would
	// silently mean "no timeout" at the call site (per TimeoutFor's
	// contract for env=0), which is the wrong default.
	for _, r := range catalog {
		if r.DefaultTimeoutSec <= 0 {
			t.Errorf("%s: DefaultTimeoutSec must be > 0, got %d", r.ID, r.DefaultTimeoutSec)
		}
	}
}

func TestIsKnown(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"backend-architect", true},                     // T-6c-C1-1 (covered here in catalog tests)
		{"Backend-Architect", false},                    // T-6c-C1-2 case-sensitive
		{"../../../etc/passwd", false},                  // T-6c-C1-3 path traversal
		{"", false},                                     // T-6c-C1-4 empty
		{"code-reviewer", true},
		{"nonexistent", false},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			if got := IsKnown(c.id); got != c.want {
				t.Errorf("IsKnown(%q) = %v, want %v", c.id, got, c.want)
			}
		})
	}
}

func TestTimeoutForCatalogDefault(t *testing.T) {
	// T-6c-C2-12: backend-architect default is 90 min
	t.Setenv("ANPM_DISPATCH_TIMEOUT", "")
	if got := TimeoutFor("backend-architect"); got != 90*time.Minute {
		t.Errorf("TimeoutFor(backend-architect) = %v, want 90m", got)
	}
}

func TestTimeoutForUnknownFallback(t *testing.T) {
	// T-6c-C2-13: unknown role falls back to 30 min
	t.Setenv("ANPM_DISPATCH_TIMEOUT", "")
	if got := TimeoutFor("nonexistent"); got != 30*time.Minute {
		t.Errorf("TimeoutFor(nonexistent) = %v, want 30m (fallback)", got)
	}
}

func TestTimeoutForEnvOverride(t *testing.T) {
	// T-6c-C2-14: env=120 overrides catalog
	t.Setenv("ANPM_DISPATCH_TIMEOUT", "120")
	if got := TimeoutFor("backend-architect"); got != 120*time.Second {
		t.Errorf("TimeoutFor with env=120 = %v, want 120s", got)
	}
}

func TestTimeoutForEnvDisabled(t *testing.T) {
	// env=0 means "disabled" — caller must treat returned 0 as "no timeout"
	t.Setenv("ANPM_DISPATCH_TIMEOUT", "0")
	if got := TimeoutFor("backend-architect"); got != 0 {
		t.Errorf("TimeoutFor with env=0 = %v, want 0 (disabled)", got)
	}
}

func TestTimeoutForEnvNegativeFallsThrough(t *testing.T) {
	// negative env values fall through to catalog default
	t.Setenv("ANPM_DISPATCH_TIMEOUT", "-1")
	if got := TimeoutFor("backend-architect"); got != 90*time.Minute {
		t.Errorf("TimeoutFor with env=-1 = %v, want catalog default 90m", got)
	}
}

func TestTimeoutForEnvGarbageFallsThrough(t *testing.T) {
	t.Setenv("ANPM_DISPATCH_TIMEOUT", "abc")
	if got := TimeoutFor("backend-architect"); got != 90*time.Minute {
		t.Errorf("TimeoutFor with garbage env = %v, want catalog default", got)
	}
}

func TestTimeoutForEnvWhitespaceFallsThrough(t *testing.T) {
	// Risk-reviewer L8: env="  120  " currently fails strconv.Atoi and
	// falls through to the catalog default. Pin this behaviour so a
	// future fix that adds TrimSpace is an intentional decision and
	// documented somewhere.
	t.Setenv("ANPM_DISPATCH_TIMEOUT", "  120  ")
	if got := TimeoutFor("backend-architect"); got != 90*time.Minute {
		t.Errorf("TimeoutFor with whitespace env = %v, want catalog default 90m (current TrimSpace-free behaviour)", got)
	}
}

func TestByIDDefensiveCopy(t *testing.T) {
	r1, ok := ByID("backend-architect")
	if !ok {
		t.Fatal("ByID(backend-architect) not found")
	}
	r1.Title = "MUTATED"
	r2, _ := ByID("backend-architect")
	if r2.Title == "MUTATED" {
		t.Error("ByID returned a reference, not a copy — catalog is mutable from outside")
	}
}

// promptsRolesDir locates the prompts/roles directory relative to this
// test file. Using runtime.Caller keeps the test working under any
// working directory (go test invokes with module root, but `go test
// ./backend/internal/roles` invokes with the package dir).
func promptsRolesDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// thisFile = .../backend/internal/roles/catalog_test.go
	// target   = .../backend/internal/prompts/roles
	return filepath.Join(filepath.Dir(thisFile), "..", "prompts", "roles")
}

// frontmatterLine matches a single `key: value` line. Quoted values
// have surrounding double-quotes stripped. This is intentionally
// permissive — the catalog drift test only needs title / version /
// use_case / role_id, all of which are flat string scalars in the
// project's role frontmatter convention.
var frontmatterLine = regexp.MustCompile(`^([a-z_]+):\s*(.*)$`)

func parseFrontmatter(t *testing.T, body, path string) map[string]string {
	t.Helper()
	out := map[string]string{}
	lines := strings.Split(body, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		t.Fatalf("%s: expected --- on first line", path)
	}
	for i := 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "---" {
			return out
		}
		m := frontmatterLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		key := m[1]
		val := strings.TrimSpace(m[2])
		// strip surrounding quotes
		if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
			val = val[1 : len(val)-1]
		}
		out[key] = val
	}
	t.Fatalf("%s: frontmatter never closed with ---", path)
	return nil
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func setDiff(a, b []string) []string {
	bset := map[string]bool{}
	for _, x := range b {
		bset[x] = true
	}
	var out []string
	for _, x := range a {
		if !bset[x] {
			out = append(out, x)
		}
	}
	return out
}
