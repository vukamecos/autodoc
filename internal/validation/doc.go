// Package validation implements the six-step document validation pipeline
// that guards every document update before it is written to disk.
//
// The checks run in order and short-circuit on the first failure:
//
//  1. allowed_path    — the target path must match documentation.allowed_paths.
//  2. not_empty       — the updated document must not be empty.
//  3. forbid_non_doc  — if validation.forbid_non_doc_changes is true, only
//     documentation files may be touched.
//  4. required_sections — all sections listed in documentation.required_sections
//     for the target path must be present in the updated content.
//  5. content_shrink  — the updated document must not shrink below
//     validation.min_content_ratio of the original size.
//  6. markdown_lint   — basic Markdown hygiene checks (unclosed fences,
//     empty headings, etc.).
//
// Each failure is recorded in the autodoc_validation_failures_total Prometheus
// counter, partitioned by the check name.
package validation
