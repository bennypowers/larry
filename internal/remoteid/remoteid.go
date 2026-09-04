package remoteid

// RemoteIdType represents the type of upstream provider.
type RemoteIdType string

const (
	TypeGitHub   RemoteIdType = "github"
	TypeGitLab   RemoteIdType = "gitlab"
	TypePyPI     RemoteIdType = "pypi"
	TypeNpm      RemoteIdType = "npm"
	TypeCodeberg RemoteIdType = "codeberg"
	TypeSourceHut RemoteIdType = "sourcehut"
	TypeUnknown  RemoteIdType = "unknown"
)

// RemoteId identifies an upstream source.
type RemoteId struct {
	Type RemoteIdType
	Org  string
	Repo string
}

// String returns the RemoteId as a string.
func (r *RemoteId) String() string {
	return string(r.Type) + ":" + r.Org + "/" + r.Repo
}
