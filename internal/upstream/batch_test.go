package upstream

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"bennypowers.dev/larry/internal/provider"
	"bennypowers.dev/larry/internal/remoteid"
	"bennypowers.dev/larry/internal/version"
)

// batchProvider implements both UpstreamProvider and BatchResolver so tests can
// observe how many repositories were answered in a batch versus per repo.
type batchProvider struct {
	mu           sync.Mutex
	perRepo      []string
	batchCalls   int
	batchReqs    int
	hints        map[string]*provider.Resolution
	hintsFor     func(reqs []provider.BatchRequest) map[string]*provider.Resolution
	batchErr     error
	perRepoVer   string
	perRepoDelay time.Duration
}

func (p *batchProvider) Type() remoteid.RemoteIdType          { return remoteid.TypeGitHub }
func (p *batchProvider) Metadata() provider.ProviderMetadata  { return provider.ProviderMetadata{} }
func (p *batchProvider) Name() string                         { return "batch-mock" }
func (p *batchProvider) IsPrerelease(v *version.Version) bool { return v.IsPrerelease() }

func (p *batchProvider) FetchLatestVersion(rid remoteid.RemoteId, _ bool) (*version.Version, error) {
	p.mu.Lock()
	p.perRepo = append(p.perRepo, rid.Repo)
	delay := p.perRepoDelay
	ver := p.perRepoVer
	p.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	if ver == "" {
		ver = "1.0.0"
	}
	return version.Parse(ver)
}

func (p *batchProvider) BatchResolve(_ context.Context, reqs []provider.BatchRequest, _ bool) (map[string]*provider.Resolution, error) {
	p.mu.Lock()
	p.batchCalls++
	p.batchReqs += len(reqs)
	hints := p.hints
	if p.hintsFor != nil {
		hints = p.hintsFor(reqs)
	}
	err := p.batchErr
	p.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return hints, nil
}

func (p *batchProvider) counts() (batch, reqs int, perRepo []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.batchCalls, p.batchReqs, append([]string(nil), p.perRepo...)
}

func githubOverlay(t *testing.T, packages ...string) string {
	t.Helper()
	overlay := setupTestOverlay(t)
	for _, pkg := range packages {
		writeEbuild(t, overlay, "app-shells/"+pkg+"/"+pkg+"-1.0.0.ebuild", fmt.Sprintf(`EAPI=8
DESCRIPTION="%s"
SRC_URI="https://github.com/acme/%s/archive/v1.0.0.tar.gz"
LICENSE="MIT"
SLOT="0"
`, pkg, pkg))
		writeMetadata(t, overlay, filepath.Join("app-shells", pkg), "acme/"+pkg)
	}
	return overlay
}

func hintsForAll(ver string) func([]provider.BatchRequest) map[string]*provider.Resolution {
	return func(reqs []provider.BatchRequest) map[string]*provider.Resolution {
		out := make(map[string]*provider.Resolution, len(reqs))
		for _, req := range reqs {
			out[provider.BatchKey(req.ID)] = &provider.Resolution{Version: ver}
		}
		return out
	}
}

func TestPrefetchBatches_SingleBatchCallForAllRepos(t *testing.T) {
	overlay := githubOverlay(t, "alpha", "beta")
	p := &batchProvider{
		hintsFor:   hintsForAll("2.0.0"),
		perRepoVer: "9.9.9",
	}
	reg := provider.NewProviderRegistry()
	reg.Register(p)

	results, err := RunWithTargets(RunOptions{
		OverlayDir: overlay,
		Registry:   reg,
		CacheDir:   t.TempDir(),
		Jobs:       2,
	})
	if err != nil {
		t.Fatalf("RunWithTargets: %v", err)
	}
	batchCalls, batchReqs, perRepo := p.counts()
	if batchCalls != 1 {
		t.Errorf("BatchResolve calls = %d, want 1", batchCalls)
	}
	if batchReqs != 2 {
		t.Errorf("batched requests = %d, want 2", batchReqs)
	}
	if len(perRepo) != 0 {
		t.Errorf("per-repo fetches = %v, want none (batch answered both)", perRepo)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	for _, r := range results {
		if r.Latest != "2.0.0" {
			t.Errorf("%s Latest = %q, want batched 2.0.0", r.Package, r.Latest)
		}
		if r.Status != StatusOutdated {
			t.Errorf("%s Status = %v, want OUTDATED", r.Package, r.Status)
		}
	}
}

func TestPrefetchBatches_FallsBackPerRepoOnError(t *testing.T) {
	overlay := githubOverlay(t, "alpha", "beta")
	p := &batchProvider{
		batchErr:   fmt.Errorf("graphql exploded"),
		perRepoVer: "3.0.0",
	}
	reg := provider.NewProviderRegistry()
	reg.Register(p)

	results, err := RunWithTargets(RunOptions{
		OverlayDir: overlay,
		Registry:   reg,
		CacheDir:   t.TempDir(),
		Jobs:       2,
	})
	if err != nil {
		t.Fatalf("RunWithTargets: %v", err)
	}
	_, _, perRepo := p.counts()
	if len(perRepo) != 2 {
		t.Errorf("per-repo fetches = %v, want both repos", perRepo)
	}
	for _, r := range results {
		if r.Latest != "3.0.0" {
			t.Errorf("%s Latest = %q, want per-repo 3.0.0", r.Package, r.Latest)
		}
	}
}

func TestPrefetchBatches_SkipsSingleRepo(t *testing.T) {
	overlay := githubOverlay(t, "alpha")
	p := &batchProvider{perRepoVer: "3.0.0"}
	reg := provider.NewProviderRegistry()
	reg.Register(p)

	if _, err := RunWithTargets(RunOptions{
		OverlayDir: overlay,
		Registry:   reg,
		CacheDir:   t.TempDir(),
		Jobs:       2,
	}); err != nil {
		t.Fatalf("RunWithTargets: %v", err)
	}
	batchCalls, _, perRepo := p.counts()
	if batchCalls != 0 {
		t.Errorf("BatchResolve calls = %d, want 0 for a single repo", batchCalls)
	}
	if len(perRepo) != 1 {
		t.Errorf("per-repo fetches = %v, want 1", perRepo)
	}
}

// barrierProvider blocks each fetch until a second fetch is in flight, proving
// two workers actually overlap.
type barrierProvider struct {
	started  atomic.Int32
	timeouts atomic.Int32
	ready    chan struct{}
	once     sync.Once
}

func newBarrierProvider() *barrierProvider {
	return &barrierProvider{ready: make(chan struct{})}
}

func (p *barrierProvider) Type() remoteid.RemoteIdType          { return remoteid.TypeGitHub }
func (p *barrierProvider) Metadata() provider.ProviderMetadata  { return provider.ProviderMetadata{} }
func (p *barrierProvider) Name() string                         { return "barrier-mock" }
func (p *barrierProvider) IsPrerelease(v *version.Version) bool { return v.IsPrerelease() }

func (p *barrierProvider) FetchLatestVersion(remoteid.RemoteId, bool) (*version.Version, error) {
	if p.started.Add(1) >= 2 {
		p.once.Do(func() { close(p.ready) })
	}
	select {
	case <-p.ready:
	case <-time.After(2 * time.Second):
		p.timeouts.Add(1)
	}
	return version.Parse("1.0.0")
}

func TestResolveNodes_RunsFetchesConcurrently(t *testing.T) {
	overlay := githubOverlay(t, "alpha", "beta")
	p := newBarrierProvider()
	reg := provider.NewProviderRegistry()
	reg.Register(p)

	if _, err := RunWithTargets(RunOptions{
		OverlayDir: overlay,
		Registry:   reg,
		CacheDir:   t.TempDir(),
		Jobs:       2,
	}); err != nil {
		t.Fatalf("RunWithTargets: %v", err)
	}
	if n := p.timeouts.Load(); n != 0 {
		t.Errorf("%d fetches timed out waiting for a concurrent peer; worker pool is not parallel", n)
	}
}

func TestRunWithOptions_PreservesResultOrderUnderParallelism(t *testing.T) {
	overlay := githubOverlay(t, "delta", "alpha", "gamma", "beta")
	p := &batchProvider{
		perRepoVer:   "1.0.0",
		perRepoDelay: 5 * time.Millisecond,
	}
	reg := provider.NewProviderRegistry()
	reg.Register(p)

	results, err := RunWithTargets(RunOptions{
		OverlayDir: overlay,
		Registry:   reg,
		CacheDir:   t.TempDir(),
		Jobs:       4,
	})
	if err != nil {
		t.Fatalf("RunWithTargets: %v", err)
	}
	want := []string{
		"app-shells/alpha",
		"app-shells/beta",
		"app-shells/delta",
		"app-shells/gamma",
	}
	if len(results) != len(want) {
		t.Fatalf("got %d results, want %d", len(results), len(want))
	}
	for i, w := range want {
		if results[i].Package != w {
			t.Errorf("results[%d] = %q, want %q (order must match discovery)", i, results[i].Package, w)
		}
	}
}

func TestRunWithOptions_JobsHonored(t *testing.T) {
	overlay := githubOverlay(t, "alpha", "beta", "gamma", "delta")

	var inFlight, maxInFlight atomic.Int32
	p := &countingProvider{
		onFetch: func() {
			n := inFlight.Add(1)
			for {
				old := maxInFlight.Load()
				if n <= old || maxInFlight.CompareAndSwap(old, n) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			inFlight.Add(-1)
		},
	}
	reg := provider.NewProviderRegistry()
	reg.Register(p)

	if _, err := RunWithTargets(RunOptions{
		OverlayDir: overlay,
		Registry:   reg,
		CacheDir:   t.TempDir(),
		Jobs:       1,
	}); err != nil {
		t.Fatalf("RunWithTargets: %v", err)
	}
	if got := maxInFlight.Load(); got != 1 {
		t.Errorf("max in-flight with Jobs=1 = %d, want 1", got)
	}

	maxInFlight.Store(0)
	if _, err := RunWithTargets(RunOptions{
		OverlayDir: overlay,
		Registry:   reg,
		CacheDir:   t.TempDir(),
		Jobs:       3,
	}); err != nil {
		t.Fatalf("RunWithTargets: %v", err)
	}
	if got := maxInFlight.Load(); got < 2 {
		t.Errorf("max in-flight with Jobs=3 = %d, want at least 2", got)
	}
}

// countingProvider runs onFetch around every FetchLatestVersion call.
type countingProvider struct {
	onFetch func()
}

func (p *countingProvider) Type() remoteid.RemoteIdType          { return remoteid.TypeGitHub }
func (p *countingProvider) Metadata() provider.ProviderMetadata  { return provider.ProviderMetadata{} }
func (p *countingProvider) Name() string                         { return "counting-mock" }
func (p *countingProvider) IsPrerelease(v *version.Version) bool { return v.IsPrerelease() }

func (p *countingProvider) FetchLatestVersion(remoteid.RemoteId, bool) (*version.Version, error) {
	p.onFetch()
	return version.Parse("1.0.0")
}
