// Package markdown provides section-aware document patching for autodoc.
//
// [PatchDocument] merges an updated Markdown document produced by the LLM
// into the original file without overwriting sections the LLM did not touch.
// It identifies sections by their ATX heading lines (e.g. "## Section Name")
// and replaces only the sections that appear in the updated content, leaving
// all other sections intact.
//
// This prevents common LLM failure modes such as silently dropping sections
// that were not relevant to the current diff.
package markdown
