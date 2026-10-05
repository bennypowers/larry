# 22a — `prepare`: SRC_URI entry parsing + the npm front half

**Why:** The npm pipeline handles only the first SRC_URI line, bails out on
USE-conditional entries, and discards `->` rename targets — which every scoped package
needs. It also omits the script's hard guard against the `${P}`-vs-`${PN}` release-tag
mistake, so a mis-tagged ebuild silently produces a dangling download URL.

**Precondition:** 20a (shared expander), 21 (manifest).

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish). Cited below as
`script <func> (L…)`. **If that file is absent on this machine, this plan's text is
authoritative — do not guess at the script's behaviour.**
Functions: `prepare_npm` (L199–298), in particular the `${P}` guard at L257–261.

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting. Known-red baseline: `go vet` at
> `internal/git/git.go:318`, plus `internal/prepare TestExtractVar/multi_line` **if plan
> 20a has not yet deleted it**. Do not chase anything else you did not break.

---

## Precondition check — run this first

```bash
go doc bennypowers.dev/larry/internal/ebuild BuildVariableMap
# expect: func BuildVariableMap(raw string, pe *atom.ParsedEbuild) VariableMap

go doc bennypowers.dev/larry/internal/ebuild ExpandVariables
# expect: func ExpandVariables(input string, vars VariableMap) string

go doc bennypowers.dev/larry/internal/prepare ManifestRunner
# expect: var ManifestRunner func(bin string, args []string, dir string, env []string) error

grep -n 'type manifestRequest' internal/prepare/manifest.go
# expect: one hit
```

**If `BuildVariableMap` still takes `*EbuildInfo`, plan 20a has not landed. If
`manifestRequest` is missing, plan 21 has not landed. STOP and report in either case —
do not implement the missing piece yourself.**

---

## File ownership — read this before you touch `srcuri.go`

`internal/ebuild/srcuri.go` is shared. **Plan 20a owns everything above
`ResolveSRCURI`** (the expander, `extractVarName`, `applyBashManip`,
`BuildVariableMap`). This plan **appends** `SrcUriEntry` and `ParseSrcUri` *below*
`ResolveSRCURI` and changes nothing above it. In `internal/ebuild/srcuri_test.go` add
only new `TestParseSrcUri_*` functions; do not edit or reorder plan 20a's tests.

---

## Defects being fixed

| ID | Location | Defect |
|----|----------|--------|
| D1 | `internal/prepare/npm.go:189-192` | takes only the **first line** of SRC_URI. Optional npm tarballs (`webui? ( … )`) are never fetched, so pkgdev cannot checksum them. Real case: `dev-util/pi-coding-agent/pi-coding-agent-1.0.2.ebuild:15` |
| D2 | `internal/prepare/npm.go:194-198` | requires the line to start with `mirror://npm/` after `TrimSpace`; a USE-conditional or non-npm first entry → `npm: could not resolve SRC_URI to npm registry URL`. The script strips `^\s*[A-Za-z0-9_+-]+\? ?\( ` and a trailing `\) ` first |
| D3 | `internal/prepare/npm.go:200-207` + `npmExtractDistfileName` (`:210-223`) | strips `" -> name"` and then derives the DISTDIR filename from the URL. Scoped packages always need the rename target, so the file lands under the wrong name. `dev-util/vtsls/vtsls-0.3.0.ebuild:11` is `mirror://npm/@vtsls/${MY_NPM_NAME}/-/${MY_NPM_NAME}-${PV}.tgz -> ${P}.tgz`: URL base is `language-server-0.3.0.tgz`, correct target is `vtsls-0.3.0.tgz`. (Unscoped renames like `dev-util/eslint_d`'s `mirror://npm/${PN}/-/${PN}-${PV}.tgz -> ${P}.tgz` happen to be no-ops, which is why this defect went unnoticed.) |
| D4 | not implemented | the `${P}`-tag guard. The deps tarball is uploaded under the `${PN}` tag, so a SRC_URI referencing `releases/download/${P}/` can never resolve. `grep -rn 'releases/download/\${P}' --include=*.go .` → zero hits |

---

## Target behaviour

### `SrcUriEntry` + `ParseSrcUri` — exact specification

Append to `internal/ebuild/srcuri.go`:

```go
// SrcUriEntry is one downloadable item parsed out of a SRC_URI value.
type SrcUriEntry struct {
	URL      string // variable-expanded, MIRROR-UNRESOLVED (still "mirror://npm/…")
	Filename string // the "-> name" target, else path.Base(URL)
	UseFlag  string // "webui" for `webui? ( … )`, "!foo" for `!foo? ( … )`, "" when unconditional
}

// ParseSrcUri splits a SRC_URI value into entries, stripping USE-conditional
// wrappers and resolving `->` renames.
func ParseSrcUri(s string, vars VariableMap) []SrcUriEntry
```

Algorithm, in this exact order. Do not improvise:

1. `s = ExpandVariables(s, vars)` — expand **first**, so a variable holding a whole
   entry is tokenised correctly.
2. `s = strings.ReplaceAll(s, "\\\n", " ")` — join backslash continuations.
3. `tokens := strings.Fields(s)` — tokenise on whitespace. Every rule below operates on
   whole tokens.
4. Walk the tokens left to right with a single `currentFlag string`:
   - A token matching ``regexp.MustCompile(`^!?[A-Za-z0-9_][A-Za-z0-9_+-]*\?$`)``
     **followed by** a `(` token **opens a group**: set `currentFlag` to the token with
     its trailing `?` removed (a leading `!` is **preserved**), skip both tokens.
   - A `)` token **closes the group**: set `currentFlag = ""`.
   - **Groups do not nest.** A second group-opening token while `currentFlag != ""` just
     overwrites `currentFlag`; do not build a stack.
   - A `->` token consumes the **next** token as the `Filename` of the entry that was
     just emitted. If there is no previous entry or no next token, ignore the `->`.
   - Any other token becomes a new entry with `URL = token`,
     `Filename = path.Base(URL)` (use `path`, not `filepath` — these are URLs),
     `UseFlag = currentFlag`.
5. Skip tokens that `isPureAssignment` already rejects (`srcuri.go:269`), so a stray
   `MY_P="x"` inside a SRC_URI value does not become an entry.

**`URL` is mirror-unresolved on purpose.** Callers that need a real URL call
`resolveMirror` (`srcuri.go:311-332`) themselves — that function is unexported, so
within `internal/ebuild` call it directly, and from `internal/prepare` use the existing
`ebuild.ResolveNpmDistfile` (`srcuri.go:347`) or do the `mirror://npm/` →
`https://registry.npmjs.org/` rewrite inline. Keeping `ParseSrcUri` mirror-unresolved is
what lets the npm pipeline test `strings.HasPrefix(e.URL, "mirror://npm/")`.

### npm pipeline, front half

Replace `npmPrepare`'s steps 1–3 (`npm.go:45-70`):

1. Build `vars := ebuild.BuildVariableMap(ei.Raw, pe)` (where `pe` comes from
   `atom.ParseEbuildPath(ei.Path)`), then
   `entries := ebuild.ParseSrcUri(ei.SRC_URI, vars)`.
2. **Guard (D4), before any network or filesystem work.** If the **raw, unexpanded**
   ebuild text contains the literal substring `releases/download/${P}/`, return an error
   immediately:
   ```
   npm: SRC_URI uses ${P} as release tag but deps upload to the ${PN} tag; change releases/download/${P}/ to releases/download/${PN}/
   ```
   Check `ei.Raw`, **not** the expanded SRC_URI — after expansion the `${P}` is gone.
3. `npmEntries := entries` filtered to `strings.HasPrefix(e.URL, "mirror://npm/")`.
   Error `npm: no mirror://npm/ entries in SRC_URI` only when the filtered slice is
   empty (**D1, D2**). A USE-conditional entry counts; `UseFlag` is recorded but does
   **not** filter anything in this plan (larry has no USE state).
4. For each npm entry: rewrite `mirror://npm/` → `https://registry.npmjs.org/` and
   download to `filepath.Join(distdir, e.Filename)` — **the rename target, not the URL
   base** (**D3**). Skip the download if the file already exists and is non-empty.
5. The "main" distfile for the rest of the pipeline is `npmEntries[0].Filename`.
   Delete `npmExtractDistfileName` (`npm.go:210-223`) and its test
   `TestNpm_ExtractDistfileName` — `ParseSrcUri` supersedes it.
6. Delete `npm.go:124-126`:
   ```go
   // Remove stale Manifest (first run failed before fetching)
   manifestPath := filepath.Join(filepath.Dir(ei.Path), "Manifest")
   _ = os.Remove(manifestPath)
   ```
   Plan 21's `runManifest` owns Manifest removal and does it **after** the prefetch
   hook. Removing it here re-introduces exactly the ordering bug AGENTS.md warns about
   under "Manifest integrity depends on pipeline order".

**There is no step that computes an npm *package name*.** An earlier draft of this plan
asked for `strings.Split(pathAfterRegistry, "/-/")[0]`; nothing consumes it. Do not
write it.

---

## Tests (write first — every one must be red before you implement)

### `internal/ebuild/srcuri_test.go` (append only)

`vars` for all rows: `{"PN":"vtsls", "PV":"0.3.0", "P":"vtsls-0.3.0",
"MY_NPM_NAME":"language-server", "MY_NPM_SCOPE":"@earendil-works",
"MY_WEBUI_P":"pi-web-ui-0.75.3"}`.

- `TestParseSrcUri_Unconditional` — input
  `"\n\tmirror://npm/${PN}/-/${P}.tgz\n"` → one entry:
  `{URL:"mirror://npm/vtsls/-/vtsls-0.3.0.tgz", Filename:"vtsls-0.3.0.tgz", UseFlag:""}`.
- `TestParseSrcUri_RenameTarget` (**D3**) — input
  `"mirror://npm/@vtsls/${MY_NPM_NAME}/-/${MY_NPM_NAME}-${PV}.tgz -> ${P}.tgz"` → one
  entry: `{URL:"mirror://npm/@vtsls/language-server/-/language-server-0.3.0.tgz",
  Filename:"vtsls-0.3.0.tgz", UseFlag:""}`.
- `TestParseSrcUri_UseConditional` (**D2**) — input
  `"webui? ( mirror://npm/a/-/a-1.0.tgz -> a.tgz )"` → one entry:
  `{URL:"mirror://npm/a/-/a-1.0.tgz", Filename:"a.tgz", UseFlag:"webui"}`.
- `TestParseSrcUri_MultiEntryUseGroup` — input, modelled on
  `dev-util/pi-coding-agent/pi-coding-agent-1.0.2.ebuild:12-17`:
  ```
  	mirror://npm/${MY_NPM_SCOPE}/${PN}/-/${P}.tgz -> ${P}.tgz
  	https://github.com/o/r/releases/download/pi/${P}-deps.tar.xz
  	webui? ( mirror://npm/${MY_NPM_SCOPE}/pi-web-ui/-/${MY_WEBUI_P}.tgz -> ${MY_WEBUI_P}.tgz
  		https://github.com/o/r/releases/download/pi/${MY_WEBUI_P}-deps.tar.xz )
  ```
  with `PN=vtsls`, `P=vtsls-0.3.0` → **four** entries in source order. Entries 1 and 2
  have `UseFlag:""`; entries **3 and 4 both** have `UseFlag:"webui"` — the flag applies
  to every entry until the matching `)`, not just the first. Entry 4's `Filename` is
  `pi-web-ui-0.75.3-deps.tar.xz` (no `->`, so `path.Base`).
- `TestParseSrcUri_NegatedUseFlag` — input
  `"!minimal? ( https://e.com/x-1.0.tar.gz )"` → one entry with `UseFlag:"!minimal"`
  (the leading `!` is preserved, the trailing `?` is not).
- `TestParseSrcUri_ScopedNpm` — input
  `"mirror://npm/@vtsls/language-server/-/language-server-0.3.0.tgz"` → `Filename` is
  `language-server-0.3.0.tgz`.
- `TestParseSrcUri_MultipleEntriesMixedProviders` — one `mirror://npm/` entry and one
  `https://github.com/...` entry → two entries, both `UseFlag:""`, in source order.
- `TestParseSrcUri_UrlIsMirrorUnresolved` — a `mirror://npm/…` input's `URL` still
  starts with `mirror://npm/`; it must **not** have been rewritten to
  `https://registry.npmjs.org/`.
- `TestParseSrcUri_IgnoresBareAssignment` — input `"MY_P=\"x\" mirror://npm/a/-/a.tgz"`
  → exactly one entry.
- `TestParseSrcUri_Continuation` — input
  `"mirror://npm/a/-/a.tgz \\\n\thttps://e.com/b.tar.gz"` → two entries.
- `TestParseSrcUri_Empty` — `""` → `nil` or a zero-length slice, no panic.

### `internal/prepare/npm_test.go`

Use `httptest` for the registry and `t.TempDir()` for DISTDIR. No network, ever.

- `TestNpm_DownloadsAllEntries` (**D1, D2**) — SRC_URI with two `mirror://npm/` entries,
  the second inside `webui? ( … )`. Point the pipeline at an `httptest` server (inject
  via `cfg.HTTPClient`, which `PrepareConfig` already carries at `prepare.go:25`).
  Assert **both** files exist in the fake DISTDIR afterwards.
- `TestNpm_UsesRenameTargetAsFilename` (**D3**) — SRC_URI
  `mirror://npm/@vtsls/language-server/-/language-server-0.3.0.tgz -> vtsls-0.3.0.tgz`
  → the DISTDIR contains `vtsls-0.3.0.tgz` and **not** `language-server-0.3.0.tgz`.
- `TestNpm_SkipsExistingDistfile` — pre-create a non-empty `vtsls-0.3.0.tgz` in DISTDIR;
  assert the `httptest` server received **zero** requests for it.
- `TestNpm_RejectsPTagInSrcUri` (**D4**) — `ei.Raw` containing
  `https://github.com/o/r/releases/download/${P}/${P}-deps.tar.xz` → a non-nil error
  whose message contains both `${P}` and `${PN}`; assert the `httptest` server received
  **zero** requests and nothing was written to DISTDIR.
- `TestNpm_AcceptsPNTagInSrcUri` — the same ebuild with `releases/download/${PN}/` does
  **not** trigger the guard.
- `TestNpm_NoNpmEntriesIsError` — SRC_URI with only an `https://` entry → error
  containing `mirror://npm/`.

Delete `TestNpm_ExtractDistfileName` together with `npmExtractDistfileName`. Note the
deletion in your handback.

---

## Implementation steps

Each step has its test named above. Write the test, watch it fail, then implement.

1. Append `SrcUriEntry` and `ParseSrcUri` to `internal/ebuild/srcuri.go` **below**
   `ResolveSRCURI`, exactly per the five-step algorithm.
   Tests: the eleven `TestParseSrcUri_*`.
2. Add the `${P}` guard at the very top of `npmPrepare`, before `Backup`.
   Tests: `TestNpm_RejectsPTagInSrcUri`, `TestNpm_AcceptsPNTagInSrcUri`.
3. Rewrite `npmPrepare` steps 1–3 around `ParseSrcUri`; download every npm entry to
   `distdir/<Filename>`. Delete `npmExtractDistfileName` (plan 20a already deleted
   `npmResolveSRCURI`; if it is still present, delete it here and say so).
   Tests: `TestNpm_DownloadsAllEntries`, `TestNpm_UsesRenameTargetAsFilename`,
   `TestNpm_SkipsExistingDistfile`, `TestNpm_NoNpmEntriesIsError`.
4. Delete the mid-pipeline Manifest removal at `npm.go:124-126`.
   Test: `TestRunManifest_PrefetchRunsBeforeManifestRemoval` from plan 21 still passes,
   and no `os.Remove(... "Manifest")` remains in `npm.go`.

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
go test ./internal/ebuild/... ./internal/prepare/... -count=1

grep -rn 'func npmExtractDistfileName\|func npmResolveSRCURI' internal/prepare/
# expect: no output

grep -n 'Manifest' internal/prepare/npm.go
# expect: no os.Remove of a Manifest path — plan 21's runManifest owns that
```

**Tier 2 — read-only manual. Safe to run.**

```bash
# Which overlay packages exercise the D4 guard (they are the ones that must FAIL)?
grep -rln 'releases/download/\${P}/' ~/Developer/gentoo-overlay --include=*.ebuild
# expect: dev-util/vtsls/vtsls-0.3.0.ebuild  (line 12)

# Which exercise the correct ${PN} tag (the ones that must SUCCEED)?
grep -rln 'releases/download/\${PN}/' ~/Developer/gentoo-overlay --include=*.ebuild
```

**Tier 3 — MUTATING. DO NOT RUN. Report these verbatim for a human.**

> **DO NOT RUN.** `larry prepare` on an npm package calls `gh release upload`, which
> **publishes a real asset to a real GitHub repo** and is not reversible by
> `git checkout`.

```bash
# Tier 3 — DO NOT RUN. The D4 guard: this must FAIL, loudly, before any download.
# dev-util/vtsls/vtsls-0.3.0.ebuild:12 is literally
#   https://github.com/bennypowers/gentoo-overlay/releases/download/${P}/${P}-deps.tar.xz
# so the ${P} guard fires. A non-zero exit naming ${PN} IS the acceptance signal here.
cd ~/Developer/gentoo-overlay
larry prepare dev-util/vtsls/vtsls-0.3.0.ebuild
```

```bash
# Tier 3 — DO NOT RUN. The scoped-rename + USE-group path, on a package whose release
# tag is the literal "pi-coding-agent" (not ${P}), so the D4 guard does not fire.
# dev-util/pi-coding-agent/pi-coding-agent-1.0.2.ebuild:12-17 has a scoped `->` rename,
# a `webui? ( … )` group containing TWO entries, and releases/download/pi-coding-agent/.
cd ~/Developer/gentoo-overlay
larry prepare dev-util/pi-coding-agent/pi-coding-agent-1.0.2.ebuild
grep -c '^DIST' dev-util/pi-coding-agent/Manifest   # 4 entries, incl. both webui ones
git checkout dev-util/pi-coding-agent                # restore (does NOT undo the upload)
```

Prove D1, D2 and D3 at Tier 1 with `TestNpm_DownloadsAllEntries` and
`TestNpm_UsesRenameTargetAsFilename` instead. Note: among the `releases/download/${PN}/`
packages in the overlay, every `->` rename happens to be a no-op (`${PN}-${PV}.tgz` and
`${P}.tgz` are the same string), so none of them is a useful D3 acceptance target.

---

## Out of scope

Manifest mechanics (plan 21). Prefetch, tarball compression, `--ignore-scripts`,
`ghReleaseCreate` and the `npm-cache` asset contract — all plan **22b**. cargo and go
pipelines (plans 23, 24). Do not fix unrelated bugs you notice — report them.
