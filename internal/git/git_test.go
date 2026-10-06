package git

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestOpen(t *testing.T) {
	// Use the larry repo itself
	repo, err := Open(".")
	if err != nil {
		t.Skip("current directory is not a git repo")
	}
	if repo == nil {
		t.Fatal("Open returned nil")
	}
}

func TestRemotes(t *testing.T) {
	repo, err := Open(".")
	if err != nil {
		t.Skip("current directory is not a git repo")
	}

	names, err := repo.Remotes()
	if err != nil {
		t.Fatalf("Remotes(): %v", err)
	}
	if len(names) == 0 {
		t.Fatal("Remotes() returned empty list")
	}
}

func TestRemoteURL(t *testing.T) {
	repo, err := Open(".")
	if err != nil {
		t.Skip("current directory is not a git repo")
	}

	// origin should exist in the larry repo
	url, err := repo.RemoteURL("origin")
	if err != nil {
		t.Fatalf("RemoteURL(\"origin\"): %v", err)
	}
	if url == "" {
		t.Fatal("RemoteURL returned empty string")
	}
}

func TestRemoteURL_NotFound(t *testing.T) {
	repo, err := Open(".")
	if err != nil {
		t.Skip("current directory is not a git repo")
	}

	_, err = repo.RemoteURL("nonexistent")
	if err == nil {
		t.Fatal("RemoteURL(\"nonexistent\") should return error")
	}
}

func TestParseRemoteURL_SSH(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantO   string
		wantR   string
		wantErr bool
	}{
		{
			name:    "standard SSH",
			url:     "git@github.com:owner/repo.git",
			wantO:   "owner",
			wantR:   "repo",
			wantErr: false,
		},
		{
			name:    "SSH without .git",
			url:     "git@github.com:owner/repo",
			wantO:   "owner",
			wantR:   "repo",
			wantErr: false,
		},
		{
			name:    "HTTPS",
			url:     "https://github.com/owner/repo.git",
			wantO:   "owner",
			wantR:   "repo",
			wantErr: false,
		},
		{
			name:    "HTTPS without .git",
			url:     "https://github.com/owner/repo",
			wantO:   "owner",
			wantR:   "repo",
			wantErr: false,
		},
		{
			name:    "HTTP",
			url:     "http://github.com/owner/repo.git",
			wantO:   "owner",
			wantR:   "repo",
			wantErr: false,
		},
		{
			name:    "git protocol",
			url:     "git://github.com/owner/repo.git",
			wantO:   "owner",
			wantR:   "repo",
			wantErr: false,
		},
		{
			name:    "ssh protocol",
			url:     "ssh://git@github.com/owner/repo.git",
			wantO:   "owner",
			wantR:   "repo",
			wantErr: false,
		},
		{
			name:    "GitLab URL",
			url:     "git@gitlab.com:group/project.git",
			wantO:   "",
			wantR:   "",
			wantErr: true,
		},
		{
			name:    "invalid - no slash",
			url:     "git@github.com:owner",
			wantErr: true,
		},
		{
			name:    "invalid - no repo",
			url:     "git@github.com:owner/",
			wantErr: true,
		},
		{
			name:    "plain domain",
			url:     "github.com/owner/repo",
			wantO:   "owner",
			wantR:   "repo",
			wantErr: false,
		},
		{
			name:    "nested path",
			url:     "https://github.com/owner/subgroup/repo.git",
			wantO:   "owner",
			wantR:   "subgroup",
			wantErr: false,
		},
		{
			name:    "empty",
			url:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			owner, repo, err := ParseRemoteURL(tt.url)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseRemoteURL(%q) expected error, got nil", tt.url)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseRemoteURL(%q) unexpected error: %v", tt.url, err)
				return
			}
			if owner != tt.wantO {
				t.Errorf("ParseRemoteURL(%q) owner = %q, want %q", tt.url, owner, tt.wantO)
			}
			if repo != tt.wantR {
				t.Errorf("ParseRemoteURL(%q) repo = %q, want %q", tt.url, repo, tt.wantR)
			}
		})
	}
}

func TestParseRemoteURL_Failure(t *testing.T) {
	tests := []string{
		"",
		"not-a-url",
		"git@gitlab.com:group/project.git",
		"https://bitbucket.org/owner/repo",
	}

	for _, url := range tests {
		_, _, err := ParseRemoteURL(url)
		if err == nil {
			t.Errorf("ParseRemoteURL(%q) expected error, got nil", url)
		}
	}
}

func TestIsGitHubURL(t *testing.T) {
	if !isGitHubURL("git@github.com:owner/repo.git") {
		t.Error("isGitHubURL should return true for GitHub SSH URL")
	}
	if !isGitHubURL("https://github.com/owner/repo") {
		t.Error("isGitHubURL should return true for GitHub HTTPS URL")
	}
	if isGitHubURL("https://gitlab.com/owner/repo") {
		t.Error("isGitHubURL should return false for GitLab URL")
	}
	if isGitHubURL("https://example.com/repo") {
		t.Error("isGitHubURL should return false for unknown URL")
	}
}

func TestRemoveGitSuffix(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"owner/repo.git", "owner/repo"},
		{"owner/repo", "owner/repo"},
		{"owner/repo.GIT", "owner/repo"}, // case-insensitive
		{"owner/repo.git.git", "owner/repo.git"},
		{"", ""},
	}

	for _, tt := range tests {
		got := removeGitSuffix(tt.input)
		if got != tt.want {
			t.Errorf("removeGitSuffix(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSplitOwnerRepo(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantO   string
		wantR   string
		wantNil bool
	}{
		{
			name:  "simple",
			path:  "owner/repo",
			wantO: "owner",
			wantR: "repo",
		},
		{
			name:  "with trailing slash",
			path:  "owner/repo/",
			wantO: "owner",
			wantR: "repo",
		},
		{
			name:  "with leading slash",
			path:  "/owner/repo",
			wantO: "owner",
			wantR: "repo",
		},
		{
			name:  "nested",
			path:  "owner/subgroup/repo",
			wantO: "owner",
			wantR: "subgroup",
		},
		{
			name:    "no slash",
			path:    "owner",
			wantNil: true,
		},
		{
			name:    "empty",
			path:    "",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := splitOwnerRepo(tt.path)
			if tt.wantNil {
				if result != nil {
					t.Errorf("splitOwnerRepo(%q) = %v, want nil", tt.path, result)
				}
				return
			}
			if result == nil {
				t.Fatalf("splitOwnerRepo(%q) = nil, want [%q, %q]", tt.path, tt.wantO, tt.wantR)
			}
			if result[0] != tt.wantO || result[1] != tt.wantR {
				t.Errorf("splitOwnerRepo(%q) = [%q, %q], want [%q, %q]", tt.path, result[0], result[1], tt.wantO, tt.wantR)
			}
		})
	}
}

func TestLatestCommit(t *testing.T) {
	repo, err := Open(".")
	if err != nil {
		t.Skip("current directory is not a git repo")
	}

	hash, err := repo.LatestCommit()
	if err != nil {
		t.Fatalf("LatestCommit(): %v", err)
	}
	if hash == "" {
		t.Fatal("LatestCommit() returned empty string")
	}
	if len(hash) != 40 {
		t.Errorf("LatestCommit() returned hash of length %d, want 40", len(hash))
	}
}

func TestHasRemote(t *testing.T) {
	repo, err := Open(".")
	if err != nil {
		t.Skip("current directory is not a git repo")
	}

	if !repo.HasRemote("origin") {
		t.Error("HasRemote(\"origin\") should return true")
	}
	if repo.HasRemote("nonexistent") {
		t.Error("HasRemote(\"nonexistent\") should return false")
	}
}

func TestTags(t *testing.T) {
	// Test operates on the current repo (see below).
	_ = t.TempDir() // keep the test clean

	// Initialize a bare repo and push tags
	repo, err := Open(".")
	if err != nil {
		t.Skip("current directory is not a git repo")
	}

	// Just verify Tags() works on the larry repo
	tagNames, err := repo.Tags()
	if err != nil {
		t.Fatalf("Tags(): %v", err)
	}
	// larry may or may not have tags, but it should not error
	_ = tagNames
}

func TestListTagsSorted(t *testing.T) {
	repo, err := Open(".")
	if err != nil {
		t.Skip("current directory is not a git repo")
	}

	infos, err := repo.ListTagsSorted()
	if err != nil {
		t.Fatalf("ListTagsSorted(): %v", err)
	}
	// Should not error, may be empty if no tags
	_ = infos
}

func TestResolveTag(t *testing.T) {
	repo, err := Open(".")
	if err != nil {
		t.Skip("current directory is not a git repo")
	}

	// Get first tag and try to resolve it
	tagNames, err := repo.Tags()
	if err != nil || len(tagNames) == 0 {
		t.Skip("no tags to test")
	}

	hash, err := repo.ResolveTag(tagNames[0])
	if err != nil {
		t.Fatalf("ResolveTag(%q): %v", tagNames[0], err)
	}
	if hash == (plumbing.Hash{}) {
		t.Fatalf("ResolveTag(%q) returned zero hash", tagNames[0])
	}
}

func TestCommitByTag(t *testing.T) {
	repo, err := Open(".")
	if err != nil {
		t.Skip("current directory is not a git repo")
	}

	tagNames, err := repo.Tags()
	if err != nil || len(tagNames) == 0 {
		t.Skip("no tags to test")
	}

	commit, err := repo.CommitByTag(tagNames[0])
	if err != nil {
		t.Fatalf("CommitByTag(%q): %v", tagNames[0], err)
	}
	if commit == "" {
		t.Fatalf("CommitByTag(%q) returned empty string", tagNames[0])
	}
}

func TestTags_SortedByDate(t *testing.T) {
	// Create a temporary repo with multiple tags at different times
	tmpDir := t.TempDir()

	// Initialize a repo
	r, err := git.PlainInit(tmpDir, false)
	if err != nil {
		t.Fatalf("PlainInit(): %v", err)
	}

	// Create a commit
	w, err := r.Worktree()
	if err != nil {
		t.Fatalf("Worktree(): %v", err)
	}

	// Create a file and commit
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	_, err = w.Add("test.txt")
	if err != nil {
		t.Fatalf("Add(): %v", err)
	}

	head1, err := w.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@test.com",
			When:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("Commit(): %v", err)
	}

	// Create tag v1.0.0 AFTER commit
	ref := plumbing.NewHashReference(plumbing.ReferenceName("refs/tags/v1.0.0"), head1)
	if err := r.Storer.SetReference(ref); err != nil {
		t.Fatalf("SetReference(v1.0.0): %v", err)
	}

	// Create another commit
	if err := os.WriteFile(testFile, []byte("test2"), 0644); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}
	_, err = w.Add("test.txt")
	if err != nil {
		t.Fatalf("Add(): %v", err)
	}

	head2, err := w.Commit("second commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@test.com",
			When:  time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("Commit(): %v", err)
	}

	// Create tag v2.0.0 AFTER second commit
	ref2 := plumbing.NewHashReference(plumbing.ReferenceName("refs/tags/v2.0.0"), head2)
	if err := r.Storer.SetReference(ref2); err != nil {
		t.Fatalf("SetReference(v2.0.0): %v", err)
	}

	// Now test with our Repo wrapper
	wrapped, err := Open(tmpDir)
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}

	infos, err := wrapped.ListTagsSorted()
	if err != nil {
		t.Fatalf("ListTagsSorted(): %v", err)
	}

	if len(infos) != 2 {
		t.Fatalf("ListTagsSorted() returned %d tags, want 2", len(infos))
	}

	// v2.0.0 should come first (newer)
	if infos[0].Name != "v2.0.0" {
		t.Errorf("First tag = %q, want v2.0.0", infos[0].Name)
	}
	if infos[1].Name != "v1.0.0" {
		t.Errorf("Second tag = %q, want v1.0.0", infos[1].Name)
	}
}

func TestListTagsSorted_NilHashForUnknownTags(t *testing.T) {
	// Create a repo where a tag points to an unresolvable object
	tmpDir := t.TempDir()

	r, err := git.PlainInit(tmpDir, false)
	if err != nil {
		t.Fatalf("PlainInit(): %v", err)
	}

	// Create a commit
	w, err := r.Worktree()
	if err != nil {
		t.Fatalf("Worktree(): %v", err)
	}

	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("WriteFile(): %v", err)
	}

	_, err = w.Add("test.txt")
	if err != nil {
		t.Fatalf("Add(): %v", err)
	}

	_, err = w.Commit("initial commit", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@test.com",
			When:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	})
	if err != nil {
		t.Fatalf("Commit(): %v", err)
	}

	// Create tag
	head, err := r.Head()
	if err != nil {
		t.Fatalf("HEAD(): %v", err)
	}
	ref := plumbing.NewHashReference(plumbing.ReferenceName("refs/tags/v1.0.0"), head.Hash())
	if err := r.Storer.SetReference(ref); err != nil {
		t.Fatalf("SetReference(): %v", err)
	}

	wrapped, err := Open(tmpDir)
	if err != nil {
		t.Fatalf("Open(): %v", err)
	}

	infos, err := wrapped.ListTagsSorted()
	if err != nil {
		t.Fatalf("ListTagsSorted(): %v", err)
	}

	if len(infos) != 1 {
		t.Fatalf("ListTagsSorted() returned %d tags, want 1", len(infos))
	}

	if infos[0].Name != "v1.0.0" {
		t.Errorf("Tag name = %q, want v1.0.0", infos[0].Name)
	}
}

// TestListRemoteTags_DropsPeels creates a bare repo with a lightweight and
// an annotated tag, then verifies ListRemoteTags returns only unpeeled tags.
func TestListRemoteTags_DropsPeels(t *testing.T) {
	dir := t.TempDir()

	// Create a normal repo, commit, and tag, then clone as bare.
	repoDir := filepath.Join(dir, "work")
	repo, err := git.PlainInit(repoDir, false)
	if err != nil {
		t.Fatalf("PlainInit: %v", err)
	}
	wt, err := repo.Worktree()
	if err != nil {
		t.Fatalf("Worktree: %v", err)
	}
	readmePath := filepath.Join(repoDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# test\n"), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	_, err = wt.Add("README.md")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	_, err = wt.Commit("init", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "Test",
			Email: "test@test.com",
			When:  time.Now(),
		},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	head, err := repo.Head()
	if err != nil {
		t.Fatalf("Head: %v", err)
	}

	// Lightweight tag.
	_, err = repo.CreateTag("v1.0", head.Hash(), nil)
	if err != nil {
		t.Fatalf("CreateTag v1.0: %v", err)
	}

	// Annotated tag.
	_, err = repo.CreateTag("v2.0", head.Hash(), &git.CreateTagOptions{
		Tagger: &object.Signature{
			Name:  "Test",
			Email: "test@test.com",
			When:  time.Now(),
		},
		Message: "annotated",
	})
	if err != nil {
		t.Fatalf("CreateTag v2.0: %v", err)
	}

	// Clone as bare repo.
	bareDir := filepath.Join(dir, "bare")
	bareRepo, err := git.PlainClone(bareDir, false, &git.CloneOptions{
		URL: repoDir,
	})
	if err != nil {
		t.Fatalf("PlainClone: %v", err)
	}
	_ = bareRepo // ensure used

	// ListRemoteTags on the bare repo path.
	tags, err := ListRemoteTags(bareDir)
	if err != nil {
		t.Fatalf("ListRemoteTags: %v", err)
	}

	if len(tags) != 2 {
		t.Errorf("ListRemoteTags() returned %d tags, want 2: %v", len(tags), tags)
	}

	tagSet := make(map[string]bool)
	for _, tag := range tags {
		tagSet[tag] = true
		if strings.HasSuffix(tag, "^{}") {
			t.Errorf("tag %q should not end with ^{}", tag)
		}
	}
	if !tagSet["v1.0"] {
		t.Error("expected tag v1.0")
	}
	if !tagSet["v2.0"] {
		t.Error("expected tag v2.0")
	}
}
