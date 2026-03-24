// Package markdown provides section-aware diffing and patching for markdown documents.
// It parses ATX headings (# … ######) as section boundaries and merges two
// versions of a document at the section level instead of replacing the whole file.
package markdown

import (
	"strings"
)

// Section represents a block of markdown content anchored to a heading.
// The preamble (content before the first heading) is represented with Level 0
// and an empty Heading.
type Section struct {
	Level   int    // 1–6, or 0 for preamble
	Title   string // heading text (trimmed), used for matching
	Heading string // original heading line, e.g. "## Installation"
	Body    string // content following the heading, including blank lines
}

// key returns a normalised matching key (case-insensitive).
func (s Section) key() string {
	return strings.ToLower(strings.TrimSpace(s.Title))
}

// text reconstructs the full section text: heading line + body.
func (s Section) text() string {
	if s.Heading == "" {
		return s.Body
	}
	return s.Heading + "\n" + s.Body
}

// ParseSections splits a markdown document into sections.
// The first element may be a preamble section (Level 0) when the document
// contains content before its first heading.
func ParseSections(doc string) []Section {
	var sections []Section
	var bodyBuf strings.Builder
	var current *Section

	flush := func() {
		body := bodyBuf.String()
		bodyBuf.Reset()
		if current == nil {
			if body != "" {
				sections = append(sections, Section{Body: body})
			}
		} else {
			current.Body = body
			sections = append(sections, *current)
			current = nil
		}
	}

	// Strip one trailing newline so it does not produce a spurious blank body
	// line; we restore it on the last section below.
	trimmed := strings.TrimSuffix(doc, "\n")
	for _, line := range strings.Split(trimmed, "\n") {
		if level, title, ok := parseHeading(line); ok {
			flush()
			current = &Section{Level: level, Title: title, Heading: line}
		} else {
			bodyBuf.WriteString(line)
			bodyBuf.WriteByte('\n')
		}
	}
	flush()

	// Restore the trailing newline on the last section.
	if len(sections) > 0 && strings.HasSuffix(doc, "\n") {
		last := &sections[len(sections)-1]
		if !strings.HasSuffix(last.Body, "\n") {
			last.Body += "\n"
		}
	}

	return sections
}

// PatchDocument merges the sections of updated into original at the section level.
//
// Rules:
//   - A section present in both documents is replaced with the updated version.
//   - A section only in original is kept at its original position.
//   - A section only in updated is appended at the end.
//   - The preamble (content before the first heading) is replaced when the
//     updated version is non-empty, otherwise the original preamble is kept.
//   - If original is empty, updated is returned unchanged.
//   - Matching is case-insensitive by heading title.
func PatchDocument(original, updated string) string {
	if strings.TrimSpace(original) == "" {
		return updated
	}
	if strings.TrimSpace(updated) == "" {
		return original
	}

	origSections := ParseSections(original)
	updSections := ParseSections(updated)

	origPreamble, origHeadings := splitPreamble(origSections)
	updPreamble, updHeadings := splitPreamble(updSections)

	// Index updated heading sections by normalised key.
	updByKey := make(map[string]Section, len(updHeadings))
	for _, s := range updHeadings {
		if k := s.key(); k != "" {
			updByKey[k] = s
		}
	}
	consumed := make(map[string]bool, len(updHeadings))

	var sb strings.Builder

	// Preamble: prefer updated when it carries non-whitespace content.
	switch {
	case updPreamble != nil && strings.TrimSpace(updPreamble.Body) != "":
		sb.WriteString(updPreamble.text())
	case origPreamble != nil:
		sb.WriteString(origPreamble.text())
	}

	// Heading sections in original order; replace where updated has a match.
	for _, orig := range origHeadings {
		if upd, ok := updByKey[orig.key()]; ok {
			sb.WriteString(upd.text())
			consumed[upd.key()] = true
		} else {
			sb.WriteString(orig.text())
		}
	}

	// Append sections from updated that do not appear in original.
	for _, s := range updHeadings {
		if k := s.key(); k != "" && !consumed[k] {
			sb.WriteString(s.text())
		}
	}

	result := sb.String()
	// Preserve the trailing-newline convention of the original.
	if strings.HasSuffix(original, "\n") && !strings.HasSuffix(result, "\n") {
		result += "\n"
	}
	return result
}

// parseHeading parses an ATX heading line (e.g. "## Installation").
// Returns the level (1–6), trimmed title text, and whether it is a heading.
func parseHeading(line string) (level int, title string, ok bool) {
	if len(line) == 0 || line[0] != '#' {
		return 0, "", false
	}
	i := 0
	for i < len(line) && line[i] == '#' {
		i++
	}
	if i > 6 {
		return 0, "", false
	}
	// Must be followed by a space (or be the entire line for an empty heading).
	if i < len(line) && line[i] != ' ' {
		return 0, "", false
	}
	return i, strings.TrimSpace(line[i:]), true
}

// splitPreamble separates the optional Level-0 preamble from heading sections.
func splitPreamble(sections []Section) (preamble *Section, headings []Section) {
	if len(sections) > 0 && sections[0].Level == 0 {
		return &sections[0], sections[1:]
	}
	return nil, sections
}
