package domain

import "errors"

var (
	ErrNoRelevantChanges  = errors.New("no relevant changes")
	ErrNoMeaningfulDiff   = errors.New("no meaningful diff")
	ErrInvalidACPResponse = errors.New("invalid ACP response")
	ErrForbiddenPath      = errors.New("forbidden path")
	ErrEmptyDocument      = errors.New("empty document")
	ErrMissingSections    = errors.New("missing required sections")
	ErrStateNotFound      = errors.New("state not found")
	ErrOpenMRExists       = errors.New("open bot MR already exists")
)
