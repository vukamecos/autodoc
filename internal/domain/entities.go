package domain

import "time"

type ChangeStatus string

const (
	ChangeStatusAdded    ChangeStatus = "added"
	ChangeStatusModified ChangeStatus = "modified"
	ChangeStatusDeleted  ChangeStatus = "deleted"
	ChangeStatusRenamed  ChangeStatus = "renamed"
)

type FileCategory string

const (
	FileCategoryCode           FileCategory = "code"
	FileCategoryConfig         FileCategory = "config"
	FileCategoryInfrastructure FileCategory = "infrastructure"
	FileCategoryDocumentation  FileCategory = "documentation"
	FileCategoryTest           FileCategory = "test"
)

type ImpactZone string

const (
	ImpactZoneModule       ImpactZone = "module"
	ImpactZoneService      ImpactZone = "service"
	ImpactZoneAPI          ImpactZone = "api"
	ImpactZoneConfig       ImpactZone = "config"
	ImpactZoneArchitecture ImpactZone = "architecture"
)

type RunStatus string

const (
	RunStatusSuccess   RunStatus = "success"
	RunStatusFailed    RunStatus = "failed"
	RunStatusRetryable RunStatus = "retryable"
	RunStatusSkipped   RunStatus = "skipped"
)

type FileDiff struct {
	Path    string
	OldPath string
	Status  ChangeStatus
	Patch   string
}

type AnalyzedChange struct {
	Diff        FileDiff
	Category    FileCategory
	ImpactZones []ImpactZone
	Priority    int
}

type Document struct {
	Path    string
	Content string
}

type MergeRequest struct {
	ID          string
	Title       string
	Description string
	Branch      string
	URL         string
}

type RunState struct {
	LastProcessedSHA string
	LastRunAt        time.Time
	Status           RunStatus
	OpenMRIDs        []string
	ContextHash      string
}

type ACPRequest struct {
	CorrelationID string
	Instructions  string
	ChangeSummary string
	Diff          string
	Documents     []Document
}

type ACPResponse struct {
	Summary string    `json:"summary"`
	Files   []ACPFile `json:"files"`
	Notes   []string  `json:"notes"`
}

type ACPFile struct {
	Path    string `json:"path"`
	Action  string `json:"action"` // "update" | "create"
	Content string `json:"content"`
}
