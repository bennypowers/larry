package remoteid

import (
	"testing"
)

func TestRemoteId_String(t *testing.T) {
	r := RemoteId{
		Type: TypeGitHub,
		Org:  "golang",
		Repo: "go",
	}

	expected := "github:golang/go"
	if got := r.String(); got != expected {
		t.Errorf("RemoteId.String() = %q, want %q", got, expected)
	}
}

func TestRemoteIdTypeConstants(t *testing.T) {
	expected := []RemoteIdType{
		TypeGitHub, TypeGitLab, TypePyPI, TypeNpm, TypeCodeberg, TypeSourceHut, TypeUnknown,
	}

	for _, tt := range expected {
		if string(tt) == "" {
			t.Errorf("RemoteIdType %q is empty", tt)
		}
	}
}
