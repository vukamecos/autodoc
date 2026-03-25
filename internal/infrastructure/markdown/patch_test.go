package markdown

import (
	"testing"
)

// ---------------------------------------------------------------------------
// ParseSections
// ---------------------------------------------------------------------------

func TestParseSections_Empty(t *testing.T) {
	// strings.Split("", "\n") yields one empty string, so ParseSections returns
	// a single preamble section with a newline body. The empty-input guard lives
	// in PatchDocument (TrimSpace check), not in ParseSections itself.
	sections := ParseSections("")
	if len(sections) != 1 {
		t.Fatalf("expected 1 preamble section for empty input, got %d", len(sections))
	}
	if sections[0].Level != 0 {
		t.Errorf("expected level-0 preamble, got level %d", sections[0].Level)
	}
}

func TestParseSections_PreambleOnly(t *testing.T) {
	doc := "Just some text\nno headings here\n"
	sections := ParseSections(doc)
	if len(sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(sections))
	}
	if sections[0].Level != 0 {
		t.Errorf("expected level 0 preamble, got %d", sections[0].Level)
	}
}

func TestParseSections_HeadingsOnly(t *testing.T) {
	doc := "# Alpha\nfoo\n## Beta\nbar\n"
	sections := ParseSections(doc)
	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(sections))
	}
	if sections[0].Level != 1 || sections[0].Title != "Alpha" {
		t.Errorf("unexpected first section: %+v", sections[0])
	}
	if sections[1].Level != 2 || sections[1].Title != "Beta" {
		t.Errorf("unexpected second section: %+v", sections[1])
	}
}

func TestParseSections_PreambleThenHeadings(t *testing.T) {
	doc := "intro line\n# Heading\nbody\n"
	sections := ParseSections(doc)
	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(sections))
	}
	if sections[0].Level != 0 {
		t.Errorf("expected level-0 preamble, got level %d", sections[0].Level)
	}
	if sections[1].Title != "Heading" {
		t.Errorf("unexpected heading title: %q", sections[1].Title)
	}
}

func TestParseSections_TrailingNewlinePreserved(t *testing.T) {
	doc := "# Title\nbody\n"
	sections := ParseSections(doc)
	last := sections[len(sections)-1]
	if last.Body == "" || last.Body[len(last.Body)-1] != '\n' {
		t.Errorf("trailing newline not preserved in last section body: %q", last.Body)
	}
}

// ---------------------------------------------------------------------------
// PatchDocument
// ---------------------------------------------------------------------------

func TestPatchDocument_BothEmpty(t *testing.T) {
	result := PatchDocument("", "")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestPatchDocument_OriginalEmpty(t *testing.T) {
	updated := "# New\ncontent\n"
	result := PatchDocument("", updated)
	if result != updated {
		t.Errorf("expected updated content when original is empty, got %q", result)
	}
}

func TestPatchDocument_UpdatedEmpty(t *testing.T) {
	original := "# Existing\ncontent\n"
	result := PatchDocument(original, "")
	if result != original {
		t.Errorf("expected original content when updated is empty, got %q", result)
	}
}

func TestPatchDocument_ReplaceExistingSection(t *testing.T) {
	original := "# Alpha\nold content\n# Beta\nkeep this\n"
	updated := "# Alpha\nnew content\n"

	result := PatchDocument(original, updated)

	if !contains(result, "new content") {
		t.Error("expected new content to be present")
	}
	if !contains(result, "keep this") {
		t.Error("expected Beta section to be preserved")
	}
	if contains(result, "old content") {
		t.Error("expected old content to be removed")
	}
}

func TestPatchDocument_AppendNewSection(t *testing.T) {
	original := "# Alpha\ncontent\n"
	updated := "# NewSection\nbrand new\n"

	result := PatchDocument(original, updated)

	if !contains(result, "content") {
		t.Error("expected original Alpha section to be preserved")
	}
	if !contains(result, "brand new") {
		t.Error("expected new section to be appended")
	}
}

func TestPatchDocument_CaseInsensitiveMatch(t *testing.T) {
	original := "# Installation\nold steps\n"
	updated := "# INSTALLATION\nnew steps\n"

	result := PatchDocument(original, updated)

	if !contains(result, "new steps") {
		t.Error("expected case-insensitive section replacement")
	}
	if contains(result, "old steps") {
		t.Error("expected old steps to be replaced")
	}
}

func TestPatchDocument_PreambleReplaced(t *testing.T) {
	original := "preamble text\n# Section\nbody\n"
	updated := "new preamble\n# Section\nbody\n"

	result := PatchDocument(original, updated)

	if !contains(result, "new preamble") {
		t.Error("expected preamble to be updated")
	}
	if contains(result, "preamble text") {
		t.Error("expected old preamble to be removed")
	}
}

func TestPatchDocument_TrailingNewlineConvention(t *testing.T) {
	original := "# Title\nbody\n"
	updated := "# Title\nupdated body\n"
	result := PatchDocument(original, updated)
	if len(result) == 0 || result[len(result)-1] != '\n' {
		t.Errorf("expected trailing newline, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// parseHeading
// ---------------------------------------------------------------------------

func TestParseHeading(t *testing.T) {
	tests := []struct {
		line  string
		level int
		title string
		ok    bool
	}{
		{"# Title", 1, "Title", true},
		{"## Sub", 2, "Sub", true},
		{"###### Deep", 6, "Deep", true},
		{"####### Too deep", 0, "", false},
		{"#NoSpace", 0, "", false},
		{"not a heading", 0, "", false},
		{"", 0, "", false},
	}
	for _, tc := range tests {
		level, title, ok := parseHeading(tc.line)
		if ok != tc.ok || level != tc.level || title != tc.title {
			t.Errorf("parseHeading(%q) = (%d, %q, %v), want (%d, %q, %v)",
				tc.line, level, title, ok, tc.level, tc.title, tc.ok)
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}
