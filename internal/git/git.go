package git

import (
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// Repo wraps a go-git repository.
type Repo struct {
	repo *git.Repository
}

// Open opens a git repository at the given path.
func Open(path string) (*Repo, error) {
	repo, err := git.PlainOpen(path)
	if err != nil {
		return nil, err
	}
	return &Repo{repo: repo}, nil
}

// Remotes returns the list of remote names.
func (r *Repo) Remotes() ([]string, error) {
	remotes, err := r.repo.Remotes()
	if err != nil {
		return nil, err
	}
	names := make([]string, len(remotes))
	for i, rem := range remotes {
		names[i] = rem.Config().Name
	}
	return names, nil
}

// Tags returns all tag names.
func (r *Repo) Tags() ([]string, error) {
	refIter, err := r.repo.Tags()
	if err != nil {
		return nil, err
	}
	defer refIter.Close()

	var names []string
	err = refIter.ForEach(func(ref *plumbing.Reference) error {
		names = append(names, ref.Name().Short())
		return nil
	})
	if err != nil {
		return nil, err
	}
	return names, nil
}

// LatestCommit returns the hash of the latest commit on the given ref.
func (r *Repo) LatestCommit(ref string) (string, error) {
	head, err := r.repo.Head()
	if err != nil {
		return "", err
	}
	return head.Hash().String(), nil
}
