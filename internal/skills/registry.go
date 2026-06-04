package skills

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

type skill struct {
	Name        string
	Description string
	Content     string
	Path        string
}
type Registry struct {
	skills map[string]skill
}

func Scan(dir string) (*Registry, error) {
	registry := &Registry{
		skills: make(map[string]skill),
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return registry, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		manifestPath := filepath.Join(dir, entry.Name(), "SKILL.md")

		rawBytes, err := os.ReadFile(manifestPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}

		raw := string(rawBytes)
		meta, body := parseFrontmatter(raw)

		name := strings.TrimSpace(meta["name"])
		if name == "" {
			name = entry.Name()
		}

		description := strings.TrimSpace(meta["description"])
		if description == "" {
			description = fallbackDescription(body, raw)
		}

		registry.skills[name] = skill{
			Name:        name,
			Description: description,
			Content:     raw,
			Path:        manifestPath,
		}

	}
	return registry, nil
}
func (r *Registry) List() string {
	if r == nil || len(r.skills) == 0 {
		return "(no skills found)"
	}
	names := make([]string, 0, len(r.skills))
	for name := range r.skills {
		names = append(names, name)
	}

	lines := make([]string, 0, len(names))
	for _, name := range names {
		skill := r.skills[name]
		lines = append(lines, "- **"+skill.Name+"**: "+skill.Description)
	}

	return strings.Join(lines, "\n")
}
func (r *Registry) Load(name string) (string, bool) {
	if r == nil {
		return "", false
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}

	skill, ok := r.skills[name]
	if !ok {
		return "", false
	}

	return skill.Content, true
}

func parseFrontmatter(text string) (map[string]string, string) {
	meta := make(map[string]string)

	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---") {
		return meta, normalized
	}

	parts := strings.SplitN(normalized, "---", 3)
	if len(parts) < 3 || strings.TrimSpace(parts[0]) != "" {
		return meta, normalized
	}

	metaText := strings.TrimSpace(parts[1])
	body := strings.TrimSpace(parts[2])

	parseMeta(metaText, meta)

	return meta, body
}

func parseMeta(metaText string, meta map[string]string) {
	if strings.TrimSpace(metaText) == "" {
		return
	}

	lines := strings.Split(metaText, "\n")
	if len(lines) == 1 {
		parseInlineMeta(metaText, meta)
		if len(meta) > 0 {
			return
		}
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}

		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])

		if key != "" {
			meta[key] = unquote(value)
		}
	}
}
func parseInlineMeta(metaText string, meta map[string]string) {
	text := strings.TrimSpace(metaText)

	nameKey := "name:"
	descriptionKey := "description:"

	nameIdx := strings.Index(text, nameKey)
	descriptionIdx := strings.Index(text, descriptionKey)

	if nameIdx >= 0 {
		nameStart := nameIdx + len(nameKey)
		nameEnd := len(text)
		if descriptionIdx > nameIdx {
			nameEnd = descriptionIdx
		}

		name := strings.TrimSpace(text[nameStart:nameEnd])
		if name != "" {
			meta["name"] = unquote(name)
		}
	}

	if descriptionIdx >= 0 {
		descriptionStart := descriptionIdx + len(descriptionKey)
		description := strings.TrimSpace(text[descriptionStart:])
		if description != "" {
			meta["description"] = unquote(description)
		}
	}
}

func fallbackDescription(body string, raw string) string {
	if heading := firstMarkdownHeading(body); heading != "" {
		return heading
	}

	if heading := firstMarkdownHeading(raw); heading != "" {
		return heading
	}

	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && line != "---" {
			return strings.TrimPrefix(line, "#")
		}
	}

	return "(no description)"
}

func firstMarkdownHeading(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			return strings.TrimSpace(strings.TrimLeft(line, "#"))
		}
	}

	return ""
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return s
	}

	first := s[0]
	last := s[len(s)-1]

	if first == '"' && last == '"' {
		return s[1 : len(s)-1]
	}

	if first == '\'' && last == '\'' {
		return s[1 : len(s)-1]
	}

	return s
}
