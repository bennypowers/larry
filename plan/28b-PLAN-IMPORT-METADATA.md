# 28b — `larry import`: metadata.xml, maintainer, provenance and prepare

**Why:** The metadata.xml import writes is not valid Gentoo metadata — root
`<pkg-metadata>` instead of `<pkgmetadata>`, no DOCTYPE, `<maintainer>` without
`type="person"`, and `<remote-id>` as a direct child instead of inside `<upstream>`.
The maintainer fields are passed as two empty strings, so every imported package gets
`<email></email><name></name>`. The source metadata parse error is swallowed, so the
upstream remote-id is always lost. There is no provenance line. And `prepare` is never
invoked despite `ImportResult.Prepared` existing for it.

**Precondition:** **28a** (source resolution and target path — this plan edits the same
function and will conflict otherwise), plus 15 (`metadata.xml` parser), 13a
(`13a-PLAN-CLI-HARNESS.md` — maintainer config keys + the `cmd/larry` test harness) and
13b (`13b-PLAN-CLI-OUTPUT.md` — the `*output.Logger` `Msg`/`Warn`/`Error` methods).

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish, ~1860 lines).
Cited below as `script <func> (L…)`. **If that file is absent on this machine, this
plan's text is authoritative — do not guess at the script's behaviour.**
Function: `do_import` (L1615–1737); the metadata template is L1712–1729, the provenance
lines are L1679–1680, the prepare call is L1734–1736.

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting. In particular: the known-red baseline is
> exactly `TestExtractVar/multi_line` in `internal/prepare` plus one `go vet` finding at
> `internal/git/git.go:318`. Do not chase either.

---

## Precondition check — run these first

```bash
grep -n 'func resolveSourceOverlay' internal/importcmd/import.go
go doc ./internal/remoteid Metadata
go doc ./internal/remoteid ParseMetadataFile
go doc ./internal/remoteid RemoteId
go doc ./internal/config Config | grep -i maintainer
grep -n 'func execCmd\|func fixtureOverlay' cmd/larry/testhelp_test.go
```

Expected (plan 28a): `resolveSourceOverlay(from, targetOverlay, catPkg, reposRoot string) (path, repoName string, err error)` exists. **If it does not, 28a has not landed — STOP and report.**

Expected (plan 15): `remoteid.ParseMetadataFile` accepts the canonical
`<pkgmetadata><upstream><remote-id>` nesting and returns a non-empty
`GetRemoteIds()` for it.

Expected (plan 13a) — **flat fields on `config.Config`, not a nested struct:**

```go
type Config struct {
    // …
    MaintainerName  string   // viper key `maintainer.name`,  env LARRY_MAINTAINER_NAME
    MaintainerEmail string   // viper key `maintainer.email`, env LARRY_MAINTAINER_EMAIL
}
```

**There is no `config.Maintainer{Name,Email}` type and plan 13a does not create one.**
(Plan 13a adds them in its step 5, read from the viper keys `maintainer.name` /
`maintainer.email` with `LARRY_MAINTAINER_NAME` / `LARRY_MAINTAINER_EMAIL` overrides.)
An earlier draft of this plan said there was. If you find yourself writing
`config.Maintainer{…}`, stop and re-read this block.

```bash
grep -rn 'MaintainerName\|MaintainerEmail' --include='*.go' internal/config/
# expect: both fields present. If absent, plan 13a has not landed — STOP and report.
```

**If `cmd/larry/testhelp_test.go` with `execCmd` and `fixtureOverlay` does not exist,
plan 13a has not landed. STOP and report — do not hand-roll a second `cmd/larry`
harness.**

---

## Defects being fixed

| ID | Location | Defect |
|----|----------|--------|
| D7 | `internal/importcmd/import.go:399-425` | `writeMetadata` emits root `<pkg-metadata>` (`:403`, `:421`) with no DOCTYPE, `<maintainer>` without `type="person"` (`:408`), and `<remote-id>` as a direct child of the root (`:417-419`) instead of inside `<upstream>`. Indentation is two spaces; Gentoo convention is tabs |
| D8 | `internal/importcmd/import.go:515-519` + `internal/remoteid/metadata.go:20` | source metadata parsing always errors (`<pkgmetadata>` vs the parser's expectations) and the error is discarded by the `if …; err == nil` idiom at `:517`, so `srcMeta == nil` and the upstream remote-id is **always** lost. Plan 15 fixes the parser; this plan must stop swallowing the error |
| D9 | `internal/importcmd/import.go:559, 568` | the maintainer is passed as `""`, `""` → `<email></email><name></name>`. `internal/config` has no maintainer fields at HEAD |
| D10 | `internal/importcmd/import.go:559` | `srcMeta.GetRemoteIds()` is called on a possibly-nil `srcMeta` — safe only by a nil-receiver guard — while `:565-567` four lines later does check `if srcMeta != nil`. Inconsistent, and a trap for the next editor |
| D11 | `internal/importcmd/import.go:29`; `cmd/larry/import.go:82-84` | prepare is never invoked; `ImportResult.Prepared` (`:29`) is declared and never set. Script ends with `do_prepare_one` (L1734–1736) |
| D14 | `internal/importcmd/import.go` | no `Importing <atom> from ::<repo>` provenance line and no `Source:` line (script L1679–1680) |
| D16 | `cmd/larry/import.go:43-44, 82-84` | the `Long` text ("After importing, run 'larry prepare' …") and the trailing `fmt.Printf("\nRun 'larry prepare %s' …")` contradict D11's fix: once import runs prepare itself, both are wrong |

---

## Target behaviour

### 1. metadata.xml — exact bytes

The script writes this (L1712–1729), and the golden file must match it **byte for
byte**:

- `<?xml version="1.0" encoding="UTF-8"?>` then a newline;
- `<!DOCTYPE pkgmetadata SYSTEM "https://www.gentoo.org/dtd/metadata.dtd">` then a
  newline;
- root element `pkgmetadata` (no hyphen);
- **one TAB per indent level** (Gentoo convention; the current `writeMetadata` uses two
  spaces at `:408-412`);
- exactly **one** trailing `\n` at end of file;
- **`<upstream>` is OMITTED ENTIRELY when there are no remote-ids** — do not emit an
  empty `<upstream></upstream>`.

With one remote-id, `testdata/metadata/imported.golden` is exactly (`→` = TAB):

```
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE pkgmetadata SYSTEM "https://www.gentoo.org/dtd/metadata.dtd">
<pkgmetadata>
→<maintainer type="person">
→→<email>web@bennypowers.com</email>
→→<name>Benny Powers</name>
→</maintainer>
→<upstream>
→→<remote-id type="github">owner/repo</remote-id>
→</upstream>
</pkgmetadata>
```

With no remote-ids, `testdata/metadata/imported-no-upstream.golden` is exactly:

```
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE pkgmetadata SYSTEM "https://www.gentoo.org/dtd/metadata.dtd">
<pkgmetadata>
→<maintainer type="person">
→→<email>web@bennypowers.com</email>
→→<name>Benny Powers</name>
→</maintainer>
</pkgmetadata>
```

Write the golden files with literal tab characters. Compare with `bytes.Equal` on the
whole file, not line-by-line contains-checks.

### 2. Maintainer (**D9**)

`ImportConfig` gains:

```go
MaintainerName  string
MaintainerEmail string
```

fed from `config.Config.MaintainerName` / `.MaintainerEmail` in
`cmd/larry/import.go`. If **either** is empty, `Import` returns
`maintainer not configured: set maintainer.name and maintainer.email in the config file, or LARRY_MAINTAINER_NAME / LARRY_MAINTAINER_EMAIL`
**before writing anything**. Never emit an empty `<email>` or `<name>`.

Thread them through `createMetadata` (`:341`) and `mergeMetadata` (`:354`), replacing
the two `""` literals at `:559` and `:568`.

### 3. Remote-ids (**D8, D10**)

```go
srcMetaPath := filepath.Join(srcPkgDir, "metadata.xml")
var remoteIds []*remoteid.RemoteId
if _, err := os.Stat(srcMetaPath); err == nil {
    meta, err := remoteid.ParseMetadataFile(srcMetaPath)
    if err != nil {
        return nil, fmt.Errorf("parse source metadata %s: %w", srcMetaPath, err)
    }
    remoteIds = meta.GetRemoteIds()
}
```

A **missing** source metadata.xml is fine (empty `remoteIds`). A **malformed** one is a
hard error. Never call `GetRemoteIds()` on a possibly-nil value — this shape removes the
question entirely.

### 4. Provenance (**D14**)

Immediately after source resolution, via `Logger.Msg` (plan 13b), matching script
L1679–1680 including the column alignment:

```
Importing <category>/<package> from ::<repoName>
Source:   <source package dir>
```

Then, matching L1697 and L1701 and L1731:

```
Copied:   <ebuild basename>
Copied:   files/            (only when a files/ dir was copied)
Created:  metadata.xml
```

### 5. Run prepare (**D11, D16**)

`ImportConfig` gains an injectable hook — **a direct call to `prepare.PrepareFromFile`
cannot be stubbed, and the real one shells out to `pkgdev`** (today's suite already
prints `pkgdev manifest: error: not in ebuild repo` eight times):

```go
// Prepare runs the prepare pipeline on the freshly imported ebuild.
// Nil means prepare.PrepareFromFile with the command's PrepareConfig.
Prepare func(ebuildPath string) error
```

In `Import`, after metadata is written and when not `--dry-run`:

```go
run := cfg.Prepare
if run == nil {
    run = func(p string) error { return prepare.PrepareFromFile(p, prepareCfg) }
}
if err := run(result.EbuildPath); err != nil {
    return result, fmt.Errorf("prepare imported ebuild: %w", err)
}
result.Prepared = true
```

(Return the partially-populated `result` alongside the error so the caller can still
report what was written.)

Then delete the two now-false statements in `cmd/larry/import.go`:
- the `Long` sentence at `:43-44` ("After importing, run 'larry prepare' on the imported
  ebuild to run the appropriate type-specific pipeline…") — replace it with "The
  imported ebuild is prepared automatically; use `larry prepare` to re-run it.";
- the trailing `fmt.Printf("\nRun 'larry prepare %s' to prepare the ebuild for
  commit.\n", ebuildRel)` at `:82-84`, together with the now-unused `ebuildDir` /
  `ebuildRel` locals at `:82-83`. (**D16**)

---

## Tests (write first — every one must fail before you implement)

### `internal/importcmd/import_test.go`

| Test | Exact input | Exact expected output |
|---|---|---|
| `TestImport_MetadataShape` (**D7**) | source metadata.xml containing `<?xml …?><pkgmetadata><upstream><remote-id type="github">owner/repo</remote-id></upstream></pkgmetadata>`; maintainer `Benny Powers` / `web@bennypowers.com` | the written `metadata.xml` is **byte-identical** to `testdata/metadata/imported.golden` (the block above). Use `bytes.Equal`; on mismatch print both with `%q` so tabs are visible |
| `TestImport_MetadataShape_NoRemoteIds` (**D7**) | source package dir with **no** metadata.xml | byte-identical to `testdata/metadata/imported-no-upstream.golden`; additionally assert the output does **not** contain the substring `<upstream>` |
| `TestImport_PreservesUpstreamRemoteId` (**D8**) | source metadata.xml as in the first row | the written file contains exactly one `<remote-id type="github">owner/repo</remote-id>`, nested inside `<upstream>` |
| `TestImport_MalformedSourceMetadataIsReported` (**D8**) | source metadata.xml whose bytes are `<pkgmetadata><unclosed>` | `Import` returns a non-nil error whose text contains `parse source metadata`; no metadata.xml is written to the target |
| `TestImport_MissingSourceMetadataIsNotAnError` (**D8**) | no source metadata.xml | returns nil; the target metadata.xml matches the no-upstream golden |
| `TestImport_NeverCallsGetRemoteIdsOnNil` (**D10**) | same as the previous row, run with `-race` | no panic; this is covered by the previous row but keep it as an explicit `-race` case |
| `TestImport_MissingMaintainerConfigIsError` (**D9**) | `MaintainerName: ""`, `MaintainerEmail: "x@y"` | error containing `maintainer not configured`; the target overlay dir has **zero** entries afterwards (nothing was written) |
| `TestImport_MissingMaintainerEmailIsError` (**D9**) | `MaintainerName: "X"`, `MaintainerEmail: ""` | same |
| `TestImport_MaintainerFromConfig` (**D9**) | both set | the written metadata contains `<email>web@bennypowers.com</email>` and `<name>Benny Powers</name>` with no empty tags |
| `TestImport_RunsPrepare` (**D11**) | `cfg.Prepare` set to a recording stub returning nil | the stub was called **exactly once**, with the target ebuild path `<target>/dev-lisp/foo/foo-1.0.0.ebuild`; `result.Prepared == true` |
| `TestImport_PrepareFailureIsReported` (**D11**) | `cfg.Prepare` returns `errors.New("boom")` | `Import` returns an error wrapping `boom`; `result != nil` and `result.EbuildPath` is populated; `result.Prepared == false` |
| `TestImport_DryRunDoesNotPrepare` (**D11**) | `dryRun = true`, recording stub | the stub recorded **zero** calls |

### `cmd/larry/import_test.go` (plan 13a's harness)

| Test | Exact input | Exact expected output |
|---|---|---|
| `TestImportCmd_PrintsProvenance` (**D14**) | `execCmd(t,"import","--dry-run","dev-lisp/foo")` against a fixture whose source repo dir is named `guru` | stdout contains the exact line `Importing dev-lisp/foo from ::guru` (modulo plan 13b's `>>> ` prefix) and a line starting `Source:   ` |
| `TestImportCmd_NoStaleRunPrepareHint` (**D16**) | `execCmd(t,"import","dev-lisp/foo")` with prepare stubbed | stdout does **not** contain the substring `Run 'larry prepare` |
| `TestImportCmd_LongHelpHasNoStaleHint` (**D16**) | `execCmd(t,"import","--help")` | output does **not** contain `After importing, run 'larry prepare'` |

---

## Implementation steps

Every step has a test above. Write the test red first.

1. Write the two golden files under `testdata/metadata/` with literal tabs, then rewrite
   `writeMetadata` (`import.go:399-425`) to emit exactly those bytes: `pkgmetadata`
   root, DOCTYPE, tabs, `type="person"`, `<upstream>` wrapper, `<upstream>` omitted when
   `len(remotes) == 0`, one trailing newline.
   Covers: `TestImport_MetadataShape`, `_MetadataShape_NoRemoteIds`,
   `_PreservesUpstreamRemoteId`. (**D7**)
2. Add `MaintainerName` / `MaintainerEmail` to `ImportConfig`; validate both at the top
   of `Import`, before any write; thread them into `createMetadata` / `mergeMetadata`,
   replacing the `""` literals at `:559` and `:568`. Feed them from
   `config.Config.MaintainerName` / `.MaintainerEmail` in `cmd/larry/import.go`.
   Covers: `TestImport_MissingMaintainerConfigIsError`, `_MissingMaintainerEmailIsError`,
   `_MaintainerFromConfig`. (**D9**)
3. Replace `import.go:515-519` with the stat-then-parse-then-propagate shape above.
   Covers: `TestImport_MalformedSourceMetadataIsReported`,
   `_MissingSourceMetadataIsNotAnError`, `_NeverCallsGetRemoteIdsOnNil`. (**D8, D10**)
4. Add the provenance and progress lines via `Logger.Msg` — these run in
   `internal/importcmd`, so add a `Log *output.Logger` field to `ImportConfig`
   (defaulting to a stdout/stderr logger when nil) rather than a package global.
   Covers: `TestImportCmd_PrintsProvenance`. (**D14**)
5. Add `Prepare func(ebuildPath string) error` to `ImportConfig`; call it (or the real
   `prepare.PrepareFromFile`) after metadata; set `Prepared`.
   Covers: `TestImport_RunsPrepare`, `_PrepareFailureIsReported`,
   `_DryRunDoesNotPrepare`. (**D11**)
6. Delete the two stale "run larry prepare" statements in `cmd/larry/import.go`
   (`:43-44` and `:82-84`) and the orphaned locals.
   Covers: `TestImportCmd_NoStaleRunPrepareHint`, `_LongHelpHasNoStaleHint`. (**D16**)
7. Hand back listing every function and test you deleted.

**Checkpoint — after step 3, `go test ./internal/importcmd/ -count=1` must be green.**
If budget is short, stop there and hand back; steps 4–6 are a clean second session.

---

## Acceptance

> Tiers per `plan/PLAN.md` → **Verification tiers**.

**Tier 1 — run these. All four are required.**

```bash
go build ./...
go vet ./...                      # expect ONLY internal/git/git.go:318 (baseline)
go test ./... -count=1            # no NEW failures vs. your recorded baseline
golangci-lint run ./...           # per AGENTS.md, required before any push
```

Also Tier 1 — the real proof, entirely against `t.TempDir()`:

```bash
go test ./internal/importcmd/ ./cmd/larry/ -count=1 -race -v
```

Validating the golden against the real DTD is **optional and offline-first**. The
`xmllint` binary is not in AGENTS.md's verified-tools list, so:

```bash
# Preferred: offline, against a DTD copy vendored into testdata/.
command -v xmllint >/dev/null && test -f testdata/metadata/metadata.dtd \
  && xmllint --noout --dtdvalid testdata/metadata/metadata.dtd \
       testdata/metadata/imported.golden \
  || echo "SKIP: xmllint or local DTD unavailable"
```

Do **not** use `xmllint --dtdvalid https://www.gentoo.org/dtd/metadata.dtd` — it hits
the network on every run. If `/usr/share/xml/gentoo/dtd/metadata.dtd` exists on this
machine (it ships with `app-portage/gentoolkit-dev`), copy it to
`testdata/metadata/metadata.dtd` and commit it; otherwise skip the check and say so in
your report. The byte-exact golden comparison is the binding assertion either way.

**Tier 2 — read-only, safe to run:**

```bash
grep -n 'pkg-metadata' internal/importcmd/import.go
# expect: no output

grep -rn "Run 'larry prepare" cmd/larry/
# expect: no output

grep -rn 'config.Maintainer{' --include='*.go' ./cmd ./internal
# expect: no output — the fields are flat on config.Config
```

**Tier 3 — DO NOT RUN. Report verbatim for a human.**

```bash
# Tier 3 — DO NOT RUN. Report verbatim.
# Writes a new package into ~/Developer/gentoo-overlay AND runs the real prepare
# pipeline on it (which may upload a GitHub release asset).
larry import dev-lisp/sbcl-unicode-dictionary
cat ~/Developer/gentoo-overlay/dev-lisp/sbcl-unicode-dictionary/metadata.xml
#   expect: <pkgmetadata>, DOCTYPE, tab indents, the configured maintainer,
#           and the upstream remote-id carried over from the source repo
xmllint --noout --dtdvalid /usr/share/xml/gentoo/dtd/metadata.dtd \
  ~/Developer/gentoo-overlay/dev-lisp/sbcl-unicode-dictionary/metadata.xml
git -C ~/Developer/gentoo-overlay clean -fd dev-lisp/sbcl-unicode-dictionary   # restore
```

## Out of scope

Source resolution, target path, version selection, `--force`, `--dry-run` write
suppression and the dead overlay-detection functions — all of that is plan 28a. The
prepare pipeline's own behaviour (plans 21, 22a/22b, 23a/23b, 24, 25a/25b) — only the invocation is in scope here.
Do not fix unrelated bugs you notice — report them.
