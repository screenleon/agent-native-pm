// Package prompts is the canonical home for agent prompts — both the
// planner prompts (backlog, whatsnext) and the future execution role
// library (see roles/). The package exists because:
//
//  1. Go's //go:embed path cannot reach outside the containing package
//     directory. Placing the markdown here lets the connector binary ship
//     prompts baked in at build time.
//
//  2. The Python reference adapters (adapters/*.py) and the Go built-in
//     adapter (internal/connector/builtin_adapter.go) both render prompts
//     from this directory. Phase 5 (see docs/phase5-plan.md) removed the
//     prior duplication where each language carried its own copy of the
//     prompt text.
//
// Public API:
//
//   - Render — load + strip frontmatter + substitute {{VAR}} placeholders.
//     This is the primary entry point used by the adapters.
//   - Load — return the raw markdown source INCLUDING frontmatter. Used
//     by callers that want to parse the metadata themselves.
//   - Exists — report whether a named prompt is bundled.
//   - ListRoles — enumerate the role ids under roles/. Used by tooling
//     that needs the catalog (e.g. Phase 6 dispatcher validation).
//
// Frontmatter stripping, regex substitution, and embed.FS are private
// implementation details.
package prompts

import (
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
)

//go:embed *.md roles/*.md
var fsys embed.FS

// templateVar matches our single template syntax: {{VAR_NAME}} where
// VAR_NAME is [A-Z0-9_]+. We deliberately avoid Go's text/template so
// that JSON schema examples in the prompt body (e.g. `{ "a": 1 }`) are
// unambiguously literal — single braces are never treated as template.
var templateVar = regexp.MustCompile(`\{\{([A-Z][A-Z0-9_]*)\}\}`)

// frontmatterDelim finds YAML-ish frontmatter at the top of a prompt
// file: the sentinel `---\n` opens, the next `---\n` (on its own line)
// closes. Anything after the closing delimiter is the prompt body.
// Non-greedy + anchored at start so nested `---` in the body is ignored.
var frontmatterDelim = regexp.MustCompile(`(?s)\A---\s*\n.*?\n---\s*\n`)

// Render loads the named prompt (e.g. "backlog" or "roles/backend-architect"),
// strips any frontmatter, and substitutes {{VAR}} placeholders with the
// provided values.
//
// Substitution is single-pass: a value that itself contains `{{OTHER}}`
// is inserted verbatim and NOT re-expanded. This prevents both infinite
// loops and surprise double-substitution if user-supplied data happens
// to include template syntax.
//
// Unknown variables are left as-is in the output rather than silently
// erased. This surfaces prompt/adapter mismatches during review rather
// than producing a subtly-empty prompt at runtime.
func Render(name string, vars map[string]string) (string, error) {
	raw, err := fs.ReadFile(fsys, name+".md")
	if err != nil {
		return "", fmt.Errorf("prompts: load %q: %w", name, err)
	}
	body := stripFrontmatter(string(raw))
	return substitute(body, vars), nil
}

// Load returns the full prompt source (with frontmatter) — used by
// callers that want to parse the frontmatter themselves.
func Load(name string) (string, error) {
	raw, err := fs.ReadFile(fsys, name+".md")
	if err != nil {
		return "", fmt.Errorf("prompts: load %q: %w", name, err)
	}
	return string(raw), nil
}

// Exists reports whether a prompt with the given name is bundled. Used
// by the role-library catalog lookup.
func Exists(name string) bool {
	_, err := fs.ReadFile(fsys, name+".md")
	return err == nil
}

// ListRoles returns the sorted list of role ids that exist under roles/.
// Filenames like "backend-architect.md" → id "backend-architect".
func ListRoles() ([]string, error) {
	entries, err := fs.ReadDir(fsys, "roles")
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".md") {
			continue
		}
		if name == "README.md" {
			continue
		}
		out = append(out, strings.TrimSuffix(name, ".md"))
	}
	return out, nil
}

func stripFrontmatter(body string) string {
	loc := frontmatterDelim.FindStringIndex(body)
	if loc == nil {
		return body
	}
	return body[loc[1]:]
}

func substitute(body string, vars map[string]string) string {
	return templateVar.ReplaceAllStringFunc(body, func(match string) string {
		submatch := templateVar.FindStringSubmatch(match)
		if len(submatch) < 2 {
			return match
		}
		key := submatch[1]
		if v, ok := vars[key]; ok {
			return v
		}
		return match
	})
}
