# Contributing to larry

Thank you for your interest in contributing to larry! This document covers
how to add providers, ebuild types, phases, and general contribution guidelines.

## Development Workflow

### Prerequisites

```bash
# Build tools
make test    # Run all tests
make build   # Compile binary
make install # Install to /usr/local/bin
make lint    # go vet + staticcheck
```

### TDD Discipline

All changes follow strict red-green-refactor:

1. **Red** — Write a failing test that captures the desired behavior
2. **Green** — Implement minimum code to make the test pass
3. **Refactor** — Clean up while keeping tests green

Tests must be runnable on the host Gentoo environment without actual network
calls where possible. Use `httptest.Server` for provider tests.

## Adding a New Provider

Providers resolve the latest upstream version from a source (PyPI, npm, GitHub, etc.).

### Step 1: Implement the Provider Interface

```go
// internal/providers/newprovider.go
package providers

type newProvider struct{}

func (p *newProvider) Name() string {
	return "newprovider"
}

func (p *newProvider) Resolve(ctx context.Context, client *http.Client, uri string, prereleases bool) (string, error) {
	// Extract package name from URI
	// Make HTTP request to upstream API
	// Return latest version string
	return "1.0.0", nil
}
```

### Step 2: Register the Provider

In `internal/provider/factory.go`:

```go
func NewProviderRegistry() *ProviderRegistry {
	registry := NewRegistry()
	registry.Register(&GitHub{})
	registry.Register(&GitLab{})
	registry.Register(&PyPI{})
	registry.Register(&NPM{})
	registry.Register(&Codeberg{})
	registry.Register(&SourceHut{})
	// Add your provider
	registry.Register(&NewProvider{})
	return registry
}
```

### Step 3: Add URI Prefix Mapping

In `internal/providers/urlresolve.go`:

```go
var uriPrefixes = map[string]string{
	"mirror://pypi/":      "pypi",
	"mirror://npm/":       "npm",
	"mirror://github/":    "github",
	"mirror://gitlab/":    "gitlab",
	"mirror://codeberg/":  "codeberg",
	"mirror://sourcehut/": "sourcehut",
	"mirror://newprovider/": "newprovider",
}
```

### Step 4: Write Tests

```go
// internal/providers/newprovider_test.go
func TestNewProvider_Resolve(t *testing.T) {
	// Use httptest.Server for deterministic testing
	server := httptest.NewServer(http.HandlerFunc(...))
	defer server.Close()

	// Test resolution
	version, err := (&newProvider{}).Resolve(ctx, client, server.URL, false)
	if err != nil {
		t.Fatal(err)
	}
	if version != "1.0.0" {
		t.Errorf("version = %q, want 1.0.0", version)
	}
}
```

## Adding a New Ebuild Type

Ebuild types determine which prepare pipeline runs when a package is prepared.

### Step 1: Add Type Detection

In `internal/ebuild/detect.go`:

```go
func DetectType(info *EbuildInfo) Type {
	for _, inherit := range info.Inherit {
		switch inherit {
		case "cargo":
			return TypeCargo
		// Add new type
		case "mytype":
			return TypeMyType
		}
	}
	return TypeGeneric
}
```

### Step 2: Implement the Pipeline

In `internal/prepare/mytype.go`:

```go
func prepareMyType(info *EbuildInfo, cfg *PrepareConfig) error {
	// Implement the pipeline
	// Follow staging → execute → commit pattern
	return nil
}
```

### Step 3: Wire into Dispatcher

In `internal/prepare/prepare.go`:

```go
func Prepare(info *EbuildInfo, cfg *PrepareConfig) error {
	switch info.Type {
	case TypeCargo:
		return prepareCargo(info, cfg)
	case TypeNPM:
		return prepareNPM(info, cfg)
	case TypeGoModule:
		return prepareGoModule(info, cfg)
	case TypeVendored:
		return prepareVendored(info, cfg)
	case TypeMyType:
		return prepareMyType(info, cfg)
	default:
		return prepareGeneric(info, cfg)
	}
}
```

### Step 4: Write Tests

```go
// internal/prepare/mytype_test.go
func TestPrepareMyType(t *testing.T) {
	// Test staging, execution, commit, and rollback
}
```

## Adding a New Command

### Step 1: Define the Command

In `cmd/larry/newcmd.go`:

```go
var newCmd = cobra.Command{
	Use:   "new [args]",
	Short: "New command description",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Implementation
		return nil
	},
}

func init() {
	rootCmd.AddCommand(&newCmd)
	newCmd.Flags().StringVar(&newFlag, "flag", "default", "flag description")
}
```

### Step 2: Register with Root

```go
func init() {
	rootCmd.AddCommand(&newCmd)
}
```

### Step 3: Add Tests

Tests go in `internal/newcmd/newcmd_test.go`.

## Shell Completion

Generate completion scripts:

```bash
# Bash
larry completion bash > /etc/bash_completion.d/larry

# Zsh
larry completion zsh > ~/.zfunc/_larry

# Fish
larry completion fish > ~/.config/fish/completions/larry.fish
```

## Testing on Host

All tests must run on the host Gentoo environment:

```bash
go test ./... -count=1
```

The test suite targets:
- 562+ unit tests across 18 packages
- 12 integration tests
- 0 failures expected

## Coding Standards

- Use `internal/` for package-private code
- Keep functions small and focused
- Write table-driven tests
- Document exported functions
- Follow Go formatting (`gofmt`)
- No `t.Skip` unless environment genuinely lacks required tools

## Submitting Changes

1. Create a feature branch
2. Write tests first (red)
3. Implement (green)
4. Run full test suite (`make test`)
5. Ensure `make lint` passes
6. Commit with clear message
7. Push and open PR

## Environment Variables

| Variable | Purpose |
|----------|---------|
| `LARRY_OVERLAY` | Default overlay path |
| `LARRY_DISTDIR` | Default distdir path |
| `LARRY_CACHE_DIR` | Cache directory |
| `LARRY_COLOR` | Color mode override |
| `GITHUB_TOKEN` | GitHub PAT |
| `NO_COLOR` | Disable color output |
