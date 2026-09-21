package git

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
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

// RemoteURL returns the URL of the named remote (e.g., "origin").
func (r *Repo) RemoteURL(name string) (string, error) {
	remote, err := r.repo.Remote(name)
	if err != nil {
		return "", fmt.Errorf("remote %q: %w", name, err)
	}
	cfg := remote.Config()
	if cfg == nil || len(cfg.URLs) == 0 {
		return "", fmt.Errorf("remote %q has no URL", name)
	}
	return cfg.URLs[0], nil
}

// TagInfo holds a tag name and its resolved commit hash.
type TagInfo struct {
	Name string
	Hash plumbing.Hash
}

// Tags returns all tag names sorted by commit date (newest first).
func (r *Repo) Tags() ([]string, error) {
	infos, err := r.ListTagsSorted()
	if err != nil {
		return nil, err
	}
	names := make([]string, len(infos))
	for i, t := range infos {
		names[i] = t.Name
	}
	return names, nil
}

// ListTagsSorted returns all tags with their commit hashes, sorted by commit date (newest first).
func (r *Repo) ListTagsSorted() ([]TagInfo, error) {
	refIter, err := r.repo.Tags()
	if err != nil {
		return nil, err
	}
	defer refIter.Close()

	type tagWithTime struct {
		name string
		hash plumbing.Hash
		when time.Time
	}
	var tags []tagWithTime

	err = refIter.ForEach(func(ref *plumbing.Reference) error {
		name := ref.Name().Short()
		hash := ref.Hash()

		// Try to get commit time by resolving through tag/commit objects
		var when time.Time

		// Try direct commit (lightweight tag)
		commit, err := r.repo.CommitObject(hash)
		if err == nil {
			when = commit.Author.When
			tags = append(tags, tagWithTime{name: name, hash: hash, when: when})
			return nil
		}

		// Try tag object -> commit (annotated tag)
		tagObj, err := r.repo.TagObject(hash)
		if err == nil {
			commit, err := tagObj.Commit()
			if err == nil {
				when = commit.Author.When
				tags = append(tags, tagWithTime{name: name, hash: hash, when: when})
				return nil
			}
		}

		// Fallback: include with zero time
		tags = append(tags, tagWithTime{name: name, hash: hash})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(tags, func(i, j int) bool {
		if tags[i].when.Equal(tags[j].when) {
			return tags[i].name < tags[j].name
		}
		return tags[i].when.After(tags[j].when)
	})

	result := make([]TagInfo, len(tags))
	for i, t := range tags {
		result[i].Name = t.name
		result[i].Hash = t.hash
	}
	return result, nil
}

// ResolveOverlayDir finds the root of the git repository containing
// the current working directory. Returns the absolute path, or an error
// if not inside a git repository.
func ResolveOverlayDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	repo, err := Open(cwd)
	if err != nil {
		return "", err
	}
	w, err := repo.Worktree()
	if err != nil {
		return "", err
	}
	return w.Filesystem.Root(), nil
}

// ResolveTag resolves a tag name to its commit hash.
// For annotated tags, this follows the tag object to the underlying commit.
func (r *Repo) ResolveTag(tag string) (plumbing.Hash, error) {
	ref, err := r.repo.Reference(plumbing.ReferenceName("refs/tags/"+tag), true)
	if err != nil {
		return plumbing.Hash{}, fmt.Errorf("tag %q: %w", tag, err)
	}

	hash := ref.Hash()

	// Lightweight tag (directly points to commit)
	if ref.Type() == plumbing.HashReference {
		return hash, nil
	}

	// Annotated tag — resolve through tag object to commit
	tagObj, err := r.repo.TagObject(hash)
	if err == nil {
		commit, err := tagObj.Commit()
		if err != nil {
			return plumbing.Hash{}, fmt.Errorf("tag %q: failed to resolve commit: %w", tag, err)
		}
		return commit.Hash, nil
	}

	// Maybe it's a commit directly (rare edge case)
	_, err = r.repo.CommitObject(hash)
	if err == nil {
		return hash, nil
	}

	return plumbing.Hash{}, fmt.Errorf("tag %q is not a commit or tag object", tag)
}

// readGitAuthor reads user.name and user.email from git config.
// Checks repo config first, then XDG global (~/.config/git/config),
// then falls back to empty string.
func readGitAuthor(repo *git.Repository) (name, email string) {
	// Try repo config
	if cfg, err := repo.Config(); err == nil && cfg != nil {
		name = cfg.User.Name
		email = cfg.User.Email
	}

	// Try XDG global git config (~/.config/git/config)
	if name == "" || email == "" {
		xdgName, xdgEmail := readXDGGitConfig()
		if name == "" {
			name = xdgName
		}
		if email == "" {
			email = xdgEmail
		}
	}
	return
}

// readXDGGitConfig parses the XDG git config file for user.name and user.email.
func readXDGGitConfig() (name, email string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	path := filepath.Join(home, ".config", "git", "config")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}

	// Simple INI parser for [user] section
	inUser := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inUser = line == "[user]"
			continue
		}
		if !inUser {
			continue
		}
		if name == "" && strings.HasPrefix(line, "name") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "name"))
			name = strings.Trim(name, " =")
		}
		if email == "" && strings.HasPrefix(line, "email") {
			email = strings.TrimSpace(strings.TrimPrefix(line, "email"))
			email = strings.Trim(email, " =")
		}
	}
	return
}

// ParseRemoteURL parses a GitHub remote URL into owner and repo.
// Supports formats:
//
//	git@github.com:owner/repo.git
//	https://github.com/owner/repo.git
//	https://github.com/owner/repo
//	http://github.com/owner/repo.git
//	git://github.com/owner/repo.git
//	ssh://git@github.com/owner/repo.git
//	https://git@github.com/owner/repo.git
//
// Returns an error for non-GitHub URLs or malformed paths.
func ParseRemoteURL(url string) (owner, repo string, err error) {
	url = removeGitSuffix(url)

	// SSH format: git@github.com:owner/repo or git@github.com:owner/repo
	sshRe := regexp.MustCompile(`^[^@]+@github\.com[:/](.+)$`)
	if matches := sshRe.FindStringSubmatch(url); len(matches) == 2 {
		parts := splitOwnerRepo(matches[1])
		if parts == nil {
			return "", "", fmt.Errorf("invalid GitHub URL %q: expected owner/repo", url)
		}
		return parts[0], parts[1], nil
	}

	// Protocol-based: protocol://github.com/owner/repo
	protoRe := regexp.MustCompile(`^[a-z]+://github\.com/(.+)$`)
	if matches := protoRe.FindStringSubmatch(url); len(matches) == 2 {
		parts := splitOwnerRepo(matches[1])
		if parts == nil {
			return "", "", fmt.Errorf("invalid GitHub URL %q: expected owner/repo", url)
		}
		return parts[0], parts[1], nil
	}

	// Plain domain: github.com/owner/repo
	plainRe := regexp.MustCompile(`^github\.com/(.+)$`)
	if matches := plainRe.FindStringSubmatch(url); len(matches) == 2 {
		parts := splitOwnerRepo(matches[1])
		if parts == nil {
			return "", "", fmt.Errorf("invalid GitHub URL %q: expected owner/repo", url)
		}
		return parts[0], parts[1], nil
	}

	return "", "", fmt.Errorf("not a GitHub URL: %q", url)
}

// isGitHubURL reports whether url is a GitHub URL (returns true if ParseRemoteURL would succeed).
func isGitHubURL(url string) bool {
	_, _, err := ParseRemoteURL(url)
	return err == nil
}

// splitOwnerRepo splits a "owner/repo" string into its components.
// Returns nil if the path doesn't have exactly one slash (owner/repo).
func splitOwnerRepo(s string) []string {
	// Handle possible nested paths — take first two segments
	parts := splitPath(s)
	if len(parts) >= 2 {
		return []string{parts[0], parts[1]}
	}
	return nil
}

// splitPath splits a URL path into segments, ignoring leading/trailing slashes.
func splitPath(s string) []string {
	s = s
	// Remove trailing slash
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	// Remove leading slash
	for len(s) > 0 && s[0] == '/' {
		s = s[1:]
	}
	return splitNonEmpty(s, '/')
}

// splitNonEmpty splits s by rune c, returning non-empty parts.
func splitNonEmpty(s string, c rune) []string {
	var parts []string
	var current string
	for _, r := range s {
		if r == c {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(r)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

// removeGitSuffix strips a trailing .git from url (case-insensitive).
func removeGitSuffix(url string) string {
	lower := ".git"
	if len(url) >= len(lower) && strings.EqualFold(url[len(url)-len(lower):], lower) {
		return url[:len(url)-len(lower)]
	}
	return url
}

// LatestCommit returns the hash of the HEAD commit.
func (r *Repo) LatestCommit() (string, error) {
	head, err := r.repo.Head()
	if err != nil {
		return "", err
	}
	return head.Hash().String(), nil
}

// CommitByTag returns the commit hash for a given tag name.
// For annotated tags, this follows the tag object to the underlying commit.
// This is a convenience wrapper around ResolveTag that returns a string hash.
func (r *Repo) CommitByTag(tag string) (string, error) {
	hash, err := r.ResolveTag(tag)
	if err != nil {
		return "", err
	}
	return hash.String(), nil
}

// Worktree returns the worktree for this repository.
func (r *Repo) Worktree() (*git.Worktree, error) {
	return r.repo.Worktree()
}

// HasRemote checks if the repository has a remote with the given name.
func (r *Repo) HasRemote(name string) bool {
	_, err := r.repo.Remote(name)
	return err == nil
}

// GetRemote returns the *git.Remote for the given name.
func (r *Repo) GetRemote(name string) (*git.Remote, error) {
	return r.repo.Remote(name)
}

// Commit stages all tracked changes and creates a commit with the given message.
// Uses git config (user.name / user.email) for author; falls back to "larry" / "larry@localhost"
// if git config is not set.
// Returns the new commit hash, or an error if nothing to commit or the commit fails.
func (r *Repo) Commit(msg string) (string, error) {
	return r.CommitWithDir(msg, "")
}

// CommitWithDir stages all changes in the given directory (or "." if empty) and creates a commit.
// Only files within the specified directory are staged, preventing stale files
// from previous failed bumps from being included.
func (r *Repo) CommitWithDir(msg, dir string) (string, error) {
	if dir == "" {
		dir = "."
	}
	w, err := r.Worktree()
	if err != nil {
		return "", err
	}

	// Add only files within the specified directory
	_, err = w.Add(dir)
	if err != nil {
		return "", err
	}

	// Check if there are actually changes
	status, err := w.Status()
	if err != nil {
		return "", err
	}
	if status.IsClean() {
		return "", fmt.Errorf("nothing to commit (working tree clean)")
	}

	// Read author from git config — try repo config first, then XDG global, then fall back.
	cfgName, cfgEmail := readGitAuthor(r.repo)
	if cfgName == "" {
		cfgName = "larry"
	}
	if cfgEmail == "" {
		cfgEmail = "larry@localhost"
	}

	h, err := w.Commit(msg, &git.CommitOptions{
		Author: &object.Signature{
			Name:  cfgName,
			Email: cfgEmail,
			When:  time.Now(),
		},
	})
	if err != nil {
		return "", err
	}
	return h.String(), nil
}

// DebugReadAuthor is a test helper.
func DebugReadAuthor(repoPath string) (name, email string) {
	repo, err := Open(repoPath)
	if err != nil {
		fmt.Println("Open error:", err)
		return
	}
	cfg, _ := repo.repo.Config()
	fmt.Printf("repo.User.Name: %q\n", cfg.User.Name)
	fmt.Printf("repo.User.Email: %q\n", cfg.User.Email)
	name, email = readGitAuthor(repo.repo)
	fmt.Printf("readGitAuthor -> name: %q, email: %q\n", name, email)
	return
}
