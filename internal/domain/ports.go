package domain

import "context"

type RepositoryPort interface {
	Fetch(ctx context.Context) error
	Diff(ctx context.Context, fromSHA, toSHA string) ([]FileDiff, error)
	HeadSHA(ctx context.Context) (string, error)
}

type MRCreatorPort interface {
	CreateBranch(ctx context.Context, name string) error
	CommitFiles(ctx context.Context, branch string, docs []Document, message string) error
	CreateMR(ctx context.Context, mr MergeRequest) (string, error)
	OpenBotMRs(ctx context.Context) ([]MergeRequest, error)
}

type StateStorePort interface {
	LoadState(ctx context.Context) (*RunState, error)
	SaveState(ctx context.Context, state *RunState) error
}

type DocumentStorePort interface {
	ReadDocument(ctx context.Context, path string) (*Document, error)
}

type DocumentWriterPort interface {
	WriteDocument(ctx context.Context, doc Document) error
}

type ACPClientPort interface {
	Generate(ctx context.Context, req ACPRequest) (*ACPResponse, error)
}

type ChangeAnalyzerPort interface {
	Analyze(ctx context.Context, diffs []FileDiff) ([]AnalyzedChange, error)
}

type DocumentMapperPort interface {
	MapToDocs(ctx context.Context, changes []AnalyzedChange) ([]string, error)
}

type ValidationPort interface {
	Validate(ctx context.Context, original, updated Document) error
}
