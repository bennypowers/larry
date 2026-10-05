# 23b — `prepare`: Cargo.lock fetching, forges, tags, and pycargoebuild

**Why:** The cargo lock fetcher silently falls back to the upstream **default branch**
when the version tag is missing, producing a wrong-but-plausible ebuild. It writes an
undecoded base64 blob to `Cargo.lock` when the GitHub contents API returns one. It
probes four subdirectories the script never probes and misses the one it does. SourceHut
— which the reference overlay uses — is unsupported entirely. And the pycargoebuild
branch writes `Cargo.toml` **into the overlay package directory**, then looks for a
`Cargo.lock` that was downloaded somewhere else, so pycargoebuild never actually runs.

**Precondition:** 23a (`lookPath` seam, `formatCRATES`, `updateCargoEbuildNative(int, error)`).

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish). Cited below as
`script <func> (L…)`. **If that file is absent on this machine, this plan's text is
authoritative — do not guess at the script's behaviour.**
Functions: `prepare_cargo` (L357–441) — forge resolution L361–384, SourceHut tag
L387–393, hard failure L424–428.

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting. Known-red baseline: `go vet` at
> `internal/git/git.go:318`. Do not chase anything else you did not break.

---

## Precondition check — run this first

```bash
go doc bennypowers.dev/larry/internal/prepare 2>/dev/null | head -1   # package compiles

grep -n 'var lookPath' internal/prepare/cargo.go
# expect: one hit (plan 23a)

grep -n 'func updateCargoEbuildNative' internal/prepare/cargo.go
# expect: func updateCargoEbuildNative(ebuildPath string, crates []CargoCrate) (int, error)

grep -n 'registry+' internal/prepare/cargo.go
# expect: one hit inside parseCargoLock
```

**If `lookPath` is missing or `updateCargoEbuildNative` still returns a bare `error`,
plan 23a has not landed. STOP and report — do not implement 23a's work here.**

---

## Step 0 — the fetcher seam (do this first; four tests are unwritable without it)

There is **no function called `fetchCargoLock`**. The real one is
`findCargoLockFromTreeAPI` (`cargo.go:108`) and it shells out through
`client.Gh("api", apiCall)` at `cargo.go:143`. An `httptest` server **cannot** intercept
a `gh` subprocess, so `TestFetchCargoLock_NoDefaultBranchFallback`,
`_BindingsPythonSubdir`, `_NoOtherSubdirsProbed` and `_WritesRawBody` are all
unwritable against today's code.

Replace it with an HTTP-only function behind an interface:

```go
// fetcher is the minimal HTTP surface fetchCargoLock needs.
// *httpclient.Client already satisfies it.
type fetcher interface {
	Get(url string) (*http.Response, error)
}

// fetchCargoLock downloads Cargo.lock for the given forge/owner/repo/tag into a fresh
// temp dir and returns the file path. fetch may be nil, in which case
// httpclient.NewClient() is used.
func fetchCargoLock(fetch fetcher, forge forgeKind, owner, repo, tag string) (string, error)
```

Then:

- **Delete** `findCargoLockFromTreeAPI` (`cargo.go:104-193`) and its `gh api` call.
- **Delete** the dead passthrough `findCargoLockFromTree` (`cargo.go:195-198`). It has
  no production caller, but it **does** have seven test callers at
  `internal/prepare/cargo_test.go:84, 101, 116, 129, 146, 162, 177`. Repoint those
  tests at `fetchCargoLock` or delete the ones that only assert the old `gh`-based
  behaviour, and **list every test you deleted in your handback**.
- **Delete** `findCargoLockAndDownload` (`cargo.go:94-102`) — it exists only to wrap the
  deleted function. `PrepareCargo` calls `fetchCargoLock` directly.
- Keep `downloadCargoLock` (`cargo.go:200-230`) if `fetchCargoLock` still wants it;
  otherwise inline it and delete it.

---

## Defects being fixed

| ID | Location | Defect |
|----|----------|--------|
| D4 | `internal/prepare/cargo.go:157-176` | when the contents API returns `{"content":"<base64>","encoding":"base64"}`, the **raw base64** is written to `Cargo.lock`; the parser then finds 0 crates. **Fixed by deletion** — see the decision below |
| D5 | `internal/prepare/cargo.go:131` | `refs = append(refs, "")` — the ref list ends with the empty string (no `?ref=`), so a missing version tag silently yields the **default branch's** Cargo.lock. The script hard-fails (`Failed to download Cargo.lock`, L424–428) |
| D6 | `internal/prepare/cargo.go:70-92` | `findCargoRepo` matches `github.com` only. No SourceHut, and no `https://sr.ht/~u/p` → `https://git.sr.ht/~u/p` rewrite (script L370–377). Non-GitHub cargo packages fail with `could not find Cargo.lock: no owner/repo`. It also returns `("", "")` rather than an error |
| D7 | `internal/prepare/cargo.go:109-116` | subdirectory probes are `Cargo.lock`, `src/`, `rust/`, `lib/`, `crates/`; the script probes `""` then `bindings/python/`. The sets do not overlap beyond the root, so `bindings/python/`-rooted locks fail and four pointless requests are made per ref |
| D8 | `internal/prepare/cargo.go:297-328` | the pycargoebuild branch sets `tmpDir := filepath.Dir(ebuildPath)` at `:301` and writes `Cargo.toml` **into the overlay package directory** at `:308-309`, then looks for a `Cargo.lock` at `:316` that was downloaded to a *different* temp dir — so the `os.Stat` fails, pycargoebuild never runs, and the overlay is transiently polluted (cleanup is `defer`-only, so a panic or a hard exit leaves `Cargo.toml` behind). It also builds its runner inline with `github.NewClient().Runner()` at `:320`, which no test can replace |
| D10 | `internal/prepare/cargo.go:118-131` | tag derivation probes `{tag, "v"+tag, tag-without-v}` — three HTTP round trips per subdir. The script builds `v$PV` with `-rN` stripped, once. SourceHut's rule (tag from the `archive/<tag>.tar` URL, else `$PN-$PV`, L387–393) has no analogue at all |

---

## Decision: raw URLs only

An earlier draft of this plan contradicted itself — it specified raw URLs in one
sentence and "if the contents API is used instead of raw, decode base64" in the next.
**Decided: raw URLs, exclusively. The GitHub contents API is not used at all.**

| forge | URL template |
|---|---|
| `forgeGitHub` | `https://raw.githubusercontent.com/<owner>/<repo>/<tag>/<subdir>Cargo.lock` |
| `forgeSourceHut` | `https://git.sr.ht/<owner>/<repo>/blob/<tag>/<subdir>Cargo.lock` |

`<owner>` for SourceHut **includes the leading `~`** (e.g. `~tsdh`), so the template
needs no extra tilde. `<subdir>` is `""` or `"bindings/python/"` — note the trailing
slash is part of the subdir value, so concatenation is correct with no separator.

This makes **D4 fixed by deletion**: there is no contents-API branch left, so there is
no base64 to decode. Remove `cargo.go:148-176` entirely (the `json.Unmarshal`, the
`DownloadURL` check, and the base64 fallback). `fetchCargoLock` reads the HTTP body and
writes it verbatim.

Replace the planned `TestFetchCargoLock_DecodesBase64` with
`TestFetchCargoLock_WritesRawBody`.

---

## Target behaviour

### Forge resolution (D6)

```go
type forgeKind int

const (
	forgeGitHub forgeKind = iota
	forgeSourceHut
)

// findCargoRepo resolves the upstream forge from the variable-expanded ebuild text.
// owner includes the leading "~" for SourceHut. Returns an error when nothing matches.
func findCargoRepo(raw string, vars ebuild.VariableMap) (forge forgeKind, owner, repo string, err error)
```

Expand `raw` with `ebuild.ExpandVariables(raw, vars)` once, then try these patterns in
order and take the **first pattern** that matches anywhere in the text:

1. ``regexp.MustCompile(`https://github\.com/([^/\s"]+)/([^/\s"]+)`)`` → `forgeGitHub`
2. ``regexp.MustCompile(`https://git\.sr\.ht/(~[^/\s"]+)/([^/\s"]+)`)`` → `forgeSourceHut`
3. ``regexp.MustCompile(`https://sr\.ht/(~[^/\s"]+)/([^/\s"]+)`)`` → `forgeSourceHut`
   (the HOMEPAGE form; the `git.sr.ht` rewrite is implicit in the URL template above)
4. no match → `fmt.Errorf("cargo: no github.com or sr.ht URL in ebuild")`

Strip a trailing `.git` and any trailing `/` from `repo`.

**Precedence is by URL form, not by which variable the URL lives in.** That is the
intentional change from today's behaviour at `cargo.go:72-89`, which checks
`ei.HOMEPAGE` **first** and `ei.SRC_URI` second. For `gui-apps/nirius/nirius-0.7.1.ebuild`
this is exactly what matters: `HOMEPAGE="https://sr.ht/~tsdh/nirius/"` is on line 54 and
`SRC_URI` contains `https://git.sr.ht/~tsdh/${PN}/archive/…` on line 56. Pattern 2 is
tried before pattern 3, so the `git.sr.ht` URL from SRC_URI wins — which is the one that
actually resolves.

### Tag derivation (D10)

One tag, no probing:

- `forgeGitHub`: `"v" + atom.StripRevision(PV)`. For PV `1.2.3-r1` → `v1.2.3`.
- `forgeSourceHut`: capture group 1 of
  ``regexp.MustCompile(`archive/([^/\s"]+)\.tar(\.[A-Za-z0-9]+)?`)`` against the
  **expanded** text; if there is no match, `PN + "-" + atom.StripRevision(PV)`.
  For nirius the expanded SRC_URI contains `archive/nirius-0.7.1.tar.gz`, so the tag is
  `nirius-0.7.1`.

Delete the `refs := []string{tag, "v"+tag, tag[1:]}` construction at `cargo.go:118-130`.

### Fetch (D5, D7)

```go
for _, subdir := range []string{"", "bindings/python/"} {
	url := buildURL(forge, owner, repo, tag, subdir)
	resp, err := fetch.Get(url)
	// non-200 or transport error -> try the next subdir
	// 200 -> write resp.Body verbatim to <os.MkdirTemp>/Cargo.lock and return
}
return "", fmt.Errorf("cargo: failed to download Cargo.lock for %s/%s at tag %s", owner, repo, tag)
```

Exactly two subdirs, exactly one tag → **at most two requests**. No empty-ref retry
(**D5**). No `src/`, `rust/`, `lib/` or `crates/` (**D7**). Always close `resp.Body`.

### pycargoebuild in a real temp dir (D8)

```go
func updateCargoEbuild(ei *ebuild.EbuildInfo, lockPath string, crates []CargoCrate,
	cfg *PrepareConfig, runner github.CmdRunner) (int, error)
```

- `runner == nil` ⇒ `github.NewClient().Runner()`. This mirrors the pattern already used
  by the `gh` helpers in `vendored.go` (see `TestVendored_GhReleaseView_Exists` at
  `internal/prepare/vendored_test.go:257`, which threads a `github.CmdRunner` in
  explicitly).
- Create `work, err := os.MkdirTemp("", "larry-cargo-*")`, `defer os.RemoveAll(work)`.
  **Never** `filepath.Dir(ebuildPath)`.
- Copy the downloaded `lockPath` to `filepath.Join(work, "Cargo.lock")` and write the
  synthetic `Cargo.toml` to `filepath.Join(work, "Cargo.toml")`.
- Call `runPycargoebuild(work, ebuildPath, runner)` — it already sets `cmd.Dir = dir`
  (`cargo.go:337`), so `cmd.Dir` becomes `work`.
- On any pycargoebuild error, fall back to `updateCargoEbuildNative(ebuildPath, crates)`.
- When `lookPath("pycargoebuild")` fails, go straight to the native path (plan 23a).

`PrepareCargo` passes `nil` for `runner`.

---

## Tests (write first — every one must be red before you implement)

### `internal/prepare/cargo_test.go`

All fetch tests use `httptest.NewServer` plus a tiny `fetcher` stub that rewrites the
host, or an `*httpclient.Client` pointed at the test server. Record every requested
path so "never requested" can be asserted.

- `TestFetchCargoLock_WritesRawBody` (**D4**) — the server returns the literal bytes
  ```toml
  [[package]]
  name = "a"
  version = "1.0"
  source = "registry+https://github.com/rust-lang/crates.io-index"
  ```
  with `Content-Type: text/plain`. Assert the written file is **byte-identical** to the
  response body — no base64 decode, no JSON unwrapping.
- `TestFetchCargoLock_GitHubRawURL` — `forgeGitHub`, owner `tsdh`, repo `x`, tag
  `v1.0`, subdir `""` → the recorded request path is
  `/tsdh/x/v1.0/Cargo.lock` (host `raw.githubusercontent.com` in production).
- `TestFetchCargoLock_SourceHutBlobURL` — `forgeSourceHut`, owner `~tsdh`, repo
  `nirius`, tag `nirius-0.7.1` → the recorded request path is
  `/~tsdh/nirius/blob/nirius-0.7.1/Cargo.lock`.
- `TestFetchCargoLock_NoDefaultBranchFallback` (**D5**) — the server 404s **everything**.
  Assert: a non-nil error, and the recorded request list contains **no** path lacking a
  tag segment. Concretely: exactly two requests, both containing the tag.
- `TestFetchCargoLock_BindingsPythonSubdir` (**D7**) — root 404, `bindings/python/Cargo.lock`
  200 → success, and the returned file holds the second response's body.
- `TestFetchCargoLock_NoOtherSubdirsProbed` (**D7**) — assert **no** recorded path
  contains `/src/`, `/rust/`, `/lib/` or `/crates/`, and that exactly two requests were
  made.
- `TestFindCargoRepo_GitHub` — raw containing
  `HOMEPAGE="https://github.com/yioneko/vtsls"` → `(forgeGitHub, "yioneko", "vtsls", nil)`.
- `TestFindCargoRepo_GitSrHt` (**D6**) — raw containing
  `SRC_URI="https://git.sr.ht/~tsdh/nirius/archive/nirius-0.7.1.tar.gz"` →
  `(forgeSourceHut, "~tsdh", "nirius", nil)`.
- `TestFindCargoRepo_SrHtHomepageRewrite` (**D6**) — raw containing **only**
  `HOMEPAGE="https://sr.ht/~tsdh/nirius/"` → `(forgeSourceHut, "~tsdh", "nirius", nil)`.
- `TestFindCargoRepo_SrcUriBeatsHomepage` (**D6**) — raw with
  `HOMEPAGE="https://sr.ht/~tsdh/nirius/"` on one line and
  `SRC_URI="…https://git.sr.ht/~tsdh/nirius/archive/nirius-0.7.1.tar.gz…"` on a later
  line → `forgeSourceHut`, `~tsdh`, `nirius`. Both resolve to the same repo here; the
  point is that the `git.sr.ht` pattern is tried first, reversing `cargo.go:72-89`.
- `TestFindCargoRepo_NoMatchIsError` — raw with no forge URL → non-nil error, and the
  returned owner/repo are empty.
- `TestCargoTag_GitHubStripsRevision` (**D10**) — `forgeGitHub`, PV `1.2.3-r1` → `v1.2.3`.
- `TestCargoTag_SourceHutFromArchiveURL` (**D10**) — expanded SRC_URI
  `https://git.sr.ht/~tsdh/nirius/archive/nirius-0.7.1.tar.gz` → `nirius-0.7.1`
  (the `.gz` is not part of the tag).
- `TestCargoTag_SourceHutFallsBackToPnPv` (**D10**) — no `archive/` URL, PN `nirius`,
  PV `0.7.1-r2` → `nirius-0.7.1`.
- `TestCargo_PycargoebuildRunsInTempDir` (**D8**) — a `t.TempDir()` package dir holding
  a copy of plan 23a's `cargo-literal-crates.ebuild`; stub `lookPath` to report
  pycargoebuild present; pass a `github.NewStubRunner()` as `runner`. Assert:
  `cmd.Dir != filepath.Dir(ebuildPath)`, `cmd.Dir` contains `larry-cargo-`, a
  `Cargo.lock` and a `Cargo.toml` exist **inside `cmd.Dir`** at the moment the runner is
  invoked, and **no** `Cargo.toml` or `Cargo.lock` remains in the package dir after the
  call returns.
- `TestCargo_PycargoebuildFailureFallsBackToNative` — stub runner returns an error →
  the ebuild's `CRATES=` block is nevertheless updated from `crates`.

---

## Implementation steps

**Steps 1–4 must land together.** Removing the default-branch fallback (the old step 4)
before adding SourceHut support would leave every SourceHut package hard-failing in the
middle of the plan. Do not commit between them.

1. **Step 0** — introduce `fetcher` and the `fetchCargoLock` skeleton; delete
   `findCargoLockFromTreeAPI`, `findCargoLockFromTree` and `findCargoLockAndDownload`;
   repoint or delete the seven `cargo_test.go` callers.
2. **D6** — add `forgeKind` and rewrite `findCargoRepo(raw, vars)` with the four-step
   pattern precedence.
   Tests: the five `TestFindCargoRepo_*`.
3. **D10** — add the tag derivation helper for both forges.
   Tests: the three `TestCargoTag_*`.
4. **D4, D5, D7** — implement `fetchCargoLock`'s body: two subdirs, one tag, raw URLs,
   verbatim body write, no base64, no empty-ref retry.
   Tests: the six `TestFetchCargoLock_*`.
5. **D8** — thread `runner github.CmdRunner` through `updateCargoEbuild`, move the work
   into `os.MkdirTemp`, copy the lock in.
   Tests: `TestCargo_PycargoebuildRunsInTempDir`,
   `TestCargo_PycargoebuildFailureFallsBackToNative`.
6. Rewire `PrepareCargo` (`cargo.go:29-68`): `findCargoRepo` → tag → `fetchCargoLock` →
   `parseCargoLock` → `updateCargoEbuild(…, nil)` → `runManifest`. Keep plan 23a's
   `Backup` / `defer os.Remove(.bak)` / `Restore` wrapping intact.
7. `go test ./internal/prepare/ -count=1`.

---

## Acceptance

> Tiers per `plan/PLAN.md` → **Verification tiers**.

**Tier 1 — automated, required. Run all of these.**

```bash
go build ./...
go vet ./...                  # expect ONLY internal/git/git.go:318 (known baseline)
go test ./... -count=1        # no NEW failures vs. your recorded baseline
golangci-lint run ./...       # per AGENTS.md, required before any push
```

```bash
go test ./internal/prepare/ -count=1 -run 'Cargo'

grep -n 'client.Gh\|findCargoLockFromTree\|encoding/base64\|"content"' internal/prepare/cargo.go
# expect: no output

grep -n 'src/Cargo.lock\|rust/Cargo.lock\|lib/Cargo.lock\|crates/Cargo.lock' internal/prepare/cargo.go
# expect: no output

grep -n 'filepath.Dir(ebuildPath)' internal/prepare/cargo.go
# expect: no output — pycargoebuild runs in os.MkdirTemp

grep -c 'raw.githubusercontent.com' internal/prepare/cargo.go
# expect: 1
```

**Tier 1 — the alternative to the overlay check.** Copy
`internal/prepare/testdata/ebuilds/cargo-literal-crates.ebuild` (plan 23a's copy of
`nirius-0.7.1.ebuild`) into a `t.TempDir()`, serve a small Cargo.lock from `httptest`,
and run the whole pipeline with a stubbed `ManifestRunner`. Assert:

- `findCargoRepo` returned `(forgeSourceHut, "~tsdh", "nirius")`;
- the tag was `nirius-0.7.1`;
- exactly one HTTP request was made, to `/~tsdh/nirius/blob/nirius-0.7.1/Cargo.lock`;
- the rewritten ebuild's `CRATES=` block holds exactly the sorted registry crates from
  the served lock, with no `git+` sources, and starts **before** the `inherit` line;
- no `.bak`, `Cargo.toml` or `Cargo.lock` remains in the temp package dir.

Do **not** use `grep -c '@'` on the nirius file as a check — it already returns 40 on
the unmodified file, so it passes whether or not your code ran.

**Tier 2 — read-only manual. Safe to run.**

```bash
grep -n 'HOMEPAGE=\|SRC_URI=\|git.sr.ht\|archive/' ~/Developer/gentoo-overlay/gui-apps/nirius/nirius-0.7.1.ebuild
# expect: HOMEPAGE sr.ht on 54, git.sr.ht archive URL on 56 — the precedence case
```

**Tier 3 — MUTATING. DO NOT RUN. Report verbatim for a human.**

> **DO NOT RUN.** This rewrites real ebuilds inside `~/Developer/gentoo-overlay`,
> regenerates real `Manifest` files, and makes real network requests to sr.ht and
> GitHub.

```bash
# Tier 3 — DO NOT RUN.
cd ~/Developer/gentoo-overlay
cp gui-apps/nirius/nirius-0.7.1.ebuild /tmp/nirius.before
larry prepare gui-apps/nirius/nirius-0.7.1.ebuild        # SourceHut, replace CRATES
diff /tmp/nirius.before gui-apps/nirius/nirius-0.7.1.ebuild  # only the CRATES block
git diff --stat                                           # only the intended ebuild
find . -name Cargo.toml -o -name Cargo.lock               # expect: nothing under the overlay
git checkout gui-apps/nirius                              # restore
```

```bash
# Tier 3 — DO NOT RUN. Proves the no-default-branch rule against a real forge.
# Copy an ebuild to a scratch overlay, bump PV to a version that does not exist
# upstream, and confirm larry FAILS instead of writing default-branch crates.
cp -r ~/Developer/gentoo-overlay/gui-apps /tmp/scratch-overlay/
mv /tmp/scratch-overlay/nirius/nirius-0.7.1.ebuild /tmp/scratch-overlay/nirius/nirius-99.0.0.ebuild
larry prepare --overlay /tmp/scratch-overlay gui-apps/nirius/nirius-99.0.0.ebuild
# expect: non-zero exit, "failed to download Cargo.lock"
```

`TestFetchCargoLock_NoDefaultBranchFallback` proves the same thing at Tier 1.

---

## Out of scope

Lock parsing, the `CRATES=` block rewrite and rollback (plan 23a). Manifest mechanics
(plan 21). Version comparison (plan 14). Do not fix unrelated bugs you notice — report
them.
