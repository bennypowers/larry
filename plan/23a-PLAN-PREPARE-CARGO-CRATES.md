# 23a — `prepare`: Cargo.lock parsing, the `CRATES=` rewrite, and rollback

**Why:** The cargo pipeline emits git-sourced crates into `CRATES=` (which the cargo
eclass cannot fetch), emits duplicates, and does **nothing at all** for ebuilds that use
`${CARGO_CRATE_URIS}` without a literal `CRATES=` — the regex rewrite is a silent no-op
in that case. It also has no `.bak` rollback, unlike every other pipeline.

**Precondition:** 20a (shared expander), 20b (detection), 21 (manifest).
**This half needs no network.** Everything here is pure string manipulation over
`t.TempDir()` fixtures.

**Reference script:** `~/Developer/gentoo-overlay/scripts/larry` (fish). Cited below as
`script <func> (L…)`. **If that file is absent on this machine, this plan's text is
authoritative — do not guess at the script's behaviour.**
Functions: `update_ebuild_crates` (L302–355), in particular the `sort -u` pipe at L324
and the append branch at L350–352.

> Read `plan/PLAN.md` → **Rules that apply to every Phase 2 plan** and
> **Verification tiers** before starting. Known-red baseline: `go vet` at
> `internal/git/git.go:318`, plus `internal/prepare TestExtractVar/multi_line` **if plan
> 20a has not yet deleted it**. Do not chase anything else you did not break.

---

## Precondition check — run this first

```bash
go doc bennypowers.dev/larry/internal/ebuild BuildVariableMap
# expect: func BuildVariableMap(raw string, pe *atom.ParsedEbuild) VariableMap

go doc bennypowers.dev/larry/internal/ebuild ParsePackageType
# expect: func ParsePackageType(s string) (PackageType, error)

grep -n 'type manifestRequest' internal/prepare/manifest.go
# expect: one hit

grep -n 'func Backup\|func Restore' internal/prepare/rollback.go
# expect: 9 and 18
```

**If `BuildVariableMap` still takes `*EbuildInfo`, plan 20a has not landed. If
`ParsePackageType` is missing, plan 20b has not landed. If `manifestRequest` is missing,
plan 21 has not landed. STOP and report in every case.**

---

## Correct the field evidence before you start

An earlier draft of this plan claimed `gui-apps/nirius/nirius-0.7.1.ebuild` is a
"`inherit cargo`, **no** literal `CRATES=`" package. **That is false.** Verify:

```bash
grep -n 'CRATES=\|^inherit' ~/Developer/gentoo-overlay/gui-apps/nirius/nirius-0.7.1.ebuild
# 6:CRATES="
# 49:inherit cargo
```

`nirius` has a literal **40-entry** `CRATES=` block spanning lines 6–47, placed
**before** `inherit cargo` on line 49. It is therefore:

- the **replace** fixture for D3, not the append fixture;
- the canonical proof of the ordering rule below (CRATES must precede inherit);
- plan 23b's SourceHut fixture (D6, D10).

For the **append/insert** case you must **hand-write** a fixture. No ebuild in the
reference overlay exercises it.

---

## Defects being fixed

| ID | Location | Defect |
|----|----------|--------|
| D1 | `internal/prepare/cargo.go:264-267` | accepts **any** non-empty `source`; the script's awk requires `source ~ /^registry\+/`. Git-sourced deps land in `CRATES=` and the cargo eclass cannot resolve them |
| D2 | `internal/prepare/cargo.go:279-287` | `formatCRATES` does no `sort -u`; duplicate `name@version` entries from multi-workspace locks are emitted twice (script pipes through `sort -u`, L324) |
| D3 | `internal/prepare/cargo.go:341-359` | `updateCargoEbuildNative` uses `cratesRe.ReplaceAllString`, which is a **no-op** when `CRATES=` is absent — it rewrites the file with identical content and returns nil. Reachable for every `inherit cargo` + `${CARGO_CRATE_URIS}` ebuild |
| D9 | `internal/prepare/cargo.go:29-68` | `PrepareCargo` never calls `Backup`/`Restore` around the ebuild rewrite, unlike `prepareGeneric` (`prepare.go:124-134`), and never cleans up `.bak` — `prepareGeneric` has `defer os.Remove(ei.Path + ".bak")` at `prepare.go:127`, `PrepareCargo` has nothing |
| D11 | `internal/prepare/cargo.go:297-328` | **anti-pattern.** When `pycargoebuild` is on `PATH` **and** a `Cargo.lock` sits next to the ebuild, `updateCargoEbuild` returns at `:325` and **neither `parseCargoLock` nor `formatCRATES` ever runs**. AGENTS.md states pycargoebuild is installed on the build workstation, so D1 and D2's tests would validate a code path production routinely skips. `exec.LookPath` must be stubbable so the native path is provably reachable |

---

## Target behaviour

### D1 + D2 — lock parsing

`parseCargoLock(content string) []CargoCrate` keeps its signature. Add, inside the
per-block loop, immediately after the existing `source == "" || name == "" || version
== ""` check:

```go
if !strings.HasPrefix(source, "registry+") {
	continue
}
```

`formatCRATES(crates []CargoCrate) string` becomes deduplicating and sorting:

1. Build `name + "@" + version` for every crate.
2. De-duplicate into a `map[string]struct{}`, then `slices.Sort` the keys
   (plain byte-wise `sort.Strings`/`slices.Sort`, matching `sort -u`; **not** version-aware
   ordering).
3. Emit `"\n"` then one `"\t" + entry + "\n"` per entry. That is today's shape at
   `:282-285` — keep the leading newline and the tab indentation so existing ebuilds
   diff cleanly.

### D3 — insert, do **not** append

`CRATES=` must appear **before** `inherit cargo`, because `cargo.eclass` evaluates
`CARGO_CRATE_URIS` **at inherit time**. Appending the block at end of file produces an
ebuild whose `SRC_URI` still expands `${CARGO_CRATE_URIS}` to the empty string — a
silently broken package. `nirius` demonstrates the correct layout: CRATES at line 6,
`inherit` at line 49.

```go
// updateCargoEbuildNative replaces an existing CRATES="…" block, or inserts one when
// absent. Returns the number of crate entries written.
func updateCargoEbuildNative(ebuildPath string, crates []CargoCrate) (int, error)
```

Algorithm:

1. Read the file.
2. If ``regexp.MustCompile(`(?s)^CRATES\s*=\s*"[^"]*"`)`` with the `(?m)` multiline
   anchor — i.e. ``regexp.MustCompile(`(?sm)^CRATES\s*=\s*"[^"]*"`)`` — matches,
   **replace** that match with `CRATES="` + `formatCRATES(crates)` + `"`. Done.
   (Anchor at line start: today's `:351` regex is unanchored and would match a `CRATES=`
   appearing inside a comment or a heredoc.)
3. Otherwise **insert**. Find the first line matching ``regexp.MustCompile(`(?m)^inherit\b`)``.
   Insert `CRATES="` + block + `"` + `"\n\n"` immediately **before** that line, so the
   result reads:
   ```
   CRATES="
   	foo@1.0
   "

   inherit cargo
   ```
4. If there is no `^inherit` line at all, insert immediately after the
   ``regexp.MustCompile(`(?m)^EAPI=`)`` line, separated by a blank line on both sides.
5. If there is neither, prepend to the file.
6. Write with `0644` and return `len(deduped entries)`.

Callers: `updateCargoEbuild` (`cargo.go:290`) and through it `PrepareCargo`. Propagate
the count — `PrepareCargo` should log it with `logger.Debug`. The count exists so the
`_Append` test can assert it without re-parsing.

### D9 — backup and rollback

In `PrepareCargo` (`cargo.go:29-68`), mirroring `prepareGeneric`:

```go
if err := Backup(ei.Path); err != nil {
	return fmt.Errorf("backup: %w", err)
}
defer os.Remove(ei.Path + ".bak")
```

and call `Restore(ei.Path)` before returning on **any** error after that point —
including the lock-parse error at `:51-53`, the update error at `:58-60`, and the
manifest error at `:63-65`. The `defer os.Remove` must fire on both the success and the
failure path; `Restore` leaves the `.bak` in place, so without the defer every failed
`larry prepare` litters the overlay with a `.bak` file.

### D11 — make the native path reachable and provable

Add a package-level seam next to the existing ones:

```go
// lookPath is exec.LookPath, stubbable so tests can exercise the native parser
// even on a workstation that has pycargoebuild installed.
var lookPath = exec.LookPath
```

and use it at `cargo.go:298`. Nothing else about the pycargoebuild branch changes in
this plan — plan **23b** owns the temp-directory fix (D8) and the runner seam.

---

## Fixtures

Create `internal/prepare/testdata/ebuilds/` (the package already has a `testdata/`
holding `pi-coding-agent-0.87.0-r1.ebuild`; add the subdirectory, do not disturb that
file).

| File | Provenance | Must contain |
|---|---|---|
| `cargo-literal-crates.ebuild` | copy of `~/Developer/gentoo-overlay/gui-apps/nirius/nirius-0.7.1.ebuild` (76 lines) | `CRATES="` at line 6, `inherit cargo` at line 49 |
| `cargo-no-crates.ebuild` | **hand-write** | `EAPI=8`, `inherit cargo`, `SRC_URI="…\n\t${CARGO_CRATE_URIS}\n"`, **no** `CRATES=` anywhere |
| `cargo-no-inherit.ebuild` | **hand-write** | `EAPI=8` and nothing else type-bearing; used for the "no inherit line" insert branch |

Copying from the reference overlay is a **read**; that is Tier 2 and allowed. Tests must
copy the fixture into a `t.TempDir()` before mutating it — never write to `testdata/`.

---

## Tests (write first — every one must be red before you implement)

### `internal/prepare/cargo_test.go`

- `TestParseCargoLock_OnlyRegistryCrates` (**D1**) — input:
  ```toml
  [[package]]
  name = "aho-corasick"
  version = "1.1.4"
  source = "registry+https://github.com/rust-lang/crates.io-index"

  [[package]]
  name = "local-fork"
  version = "0.1.0"
  source = "git+https://github.com/x/y?rev=abc#abc"

  [[package]]
  name = "nirius"
  version = "0.7.1"
  ```
  → exactly **one** crate: `{Name:"aho-corasick", Version:"1.1.4"}`.
- `TestFormatCRATES_DedupeAndSort` (**D2**) — input crates in the order
  `[{b,2.0},{a,1.0},{b,2.0}]` → output exactly `"\n\ta@1.0\n\tb@2.0\n"`.
- `TestFormatCRATES_Empty` — `nil` → `"\n"`, no panic.
- `TestUpdateCratesBlock_Replace` — copy `cargo-literal-crates.ebuild` to
  `t.TempDir()`, run with crates `[{a,1.0},{b,2.0}]`. Assert: the new file contains
  exactly `CRATES="\n\ta@1.0\n\tb@2.0\n"`, contains **none** of the original 40 entries
  (`grep` for `aho-corasick@1.1.4` → absent), and the returned count is `2`.
- `TestUpdateCratesBlock_Append` (**D3**) — copy `cargo-no-crates.ebuild` to
  `t.TempDir()`, run with crates `[{a,1.0}]`. Assert, on the resulting bytes `b`:
  - `strings.Contains(string(b), "CRATES=\"\n\ta@1.0\n\"")`
  - **`strings.Index(string(b), "CRATES=") < strings.Index(string(b), "inherit")`** —
    this is the ordering guard; the block is **inserted before** `inherit`, never
    appended at EOF.
  - the returned count is `1`.
  Today this test fails because the function is a no-op: `CRATES=` never appears.
- `TestUpdateCratesBlock_NoInheritLine` — `cargo-no-inherit.ebuild` → the block lands
  after the `EAPI=8` line, and `strings.Index(b, "CRATES=") > strings.Index(b, "EAPI=")`.
- `TestUpdateCratesBlock_PreservesRestOfFile` — copy `cargo-literal-crates.ebuild`,
  record the original bytes, run the replace. Define "preserved" precisely and assert
  it: let `pre` be the original bytes before the `CRATES=` match start and `post` the
  original bytes after the match end; the new file must equal
  `pre + "CRATES=\"" + newBlock + "\"" + post`, **byte for byte**. Nothing outside the
  replaced span may change — not whitespace, not the trailing newline, not the
  copyright header.
- `TestCargo_FallsBackToNativeWhenPycargoebuildAbsent` (**D11**) — stub
  `prepare.lookPath` to return `("", exec.ErrNotFound)`; run `updateCargoEbuild` over a
  `t.TempDir()` copy of `cargo-no-crates.ebuild` with crates `[{a,1.0}]`. Assert the
  file now contains the inserted `CRATES=` block. Restore `lookPath` with `t.Cleanup`.
  **Without this test, D1/D2/D3 are only ever exercised on a machine that lacks
  pycargoebuild — which the AGENTS.md workstation is not.**
- `TestCargo_RollbackOnManifestFailure` (**D9**) — `t.TempDir()` overlay with a copy of
  `cargo-literal-crates.ebuild`; stub `prepare.ManifestRunner` to return an error; stub
  the lock fetch so `PrepareCargo` gets as far as the manifest step. Assert: the ebuild
  on disk is **byte-identical** to the original, and `<path>.bak` does **not** exist.
- `TestCargo_NoBakFileOnSuccess` (**D9**) — same setup with a succeeding
  `ManifestRunner`. Assert `<path>.bak` does not exist afterwards.

---

## Implementation steps

Each step has its test named above. Write the test, watch it fail, then implement.

1. Create `internal/prepare/testdata/ebuilds/` and the three fixtures.
2. **D11** — add the `lookPath` package var and use it at `cargo.go:298`.
   Test: `TestCargo_FallsBackToNativeWhenPycargoebuildAbsent` (will still fail on the
   insert behaviour until step 4 — that is expected and correct TDD ordering).
3. **D1 + D2** — add the `registry+` filter to `parseCargoLock`; make `formatCRATES`
   dedupe and sort.
   Tests: `TestParseCargoLock_OnlyRegistryCrates`, `TestFormatCRATES_DedupeAndSort`,
   `TestFormatCRATES_Empty`.
4. **D3** — rewrite `updateCargoEbuildNative` as replace-or-insert returning
   `(int, error)`; update `updateCargoEbuild`'s two return sites (`:323`, `:331`) and
   `PrepareCargo`'s call at `:57`.
   Tests: the four `TestUpdateCratesBlock_*`.
5. **D9** — add `Backup` / `defer os.Remove(.bak)` / `Restore` to `PrepareCargo`.
   Tests: `TestCargo_RollbackOnManifestFailure`, `TestCargo_NoBakFileOnSuccess`.
6. `go test ./internal/prepare/ -count=1`. Existing cargo tests that call
   `updateCargoEbuildNative` with the old one-value return will not compile — update
   their call sites and list the changes in your handback.

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
go test ./internal/prepare/ -count=1 -run 'Cargo|CratesBlock|FormatCRATES'

grep -n 'registry+' internal/prepare/cargo.go
# expect: one hit inside parseCargoLock

grep -n 'ReplaceAllString' internal/prepare/cargo.go
# expect: no output — the rewrite is replace-or-insert now, not a blind regex replace

grep -rn 'exec.LookPath("pycargoebuild")' internal/prepare/
# expect: no output — it goes through the lookPath var
```

**Tier 1 — the alternative to the overlay check.** Prove the whole rewrite against a
`t.TempDir()` copy of `cargo-literal-crates.ebuild`:

- the resulting `CRATES=` block contains exactly **N** `name@version` lines, where N is
  the de-duplicated crate count returned by `updateCargoEbuildNative`;
- those lines are **sorted** byte-wise and contain **no** `git+` source;
- `strings.Index(out, "CRATES=") < strings.Index(out, "inherit")`.

Do **not** use `grep -c '@'` as the check — the unmodified `nirius-0.7.1.ebuild` already
contains 40 `@` lines, so that count passes whether or not your code ran.

**Tier 2 — read-only manual. Safe to run.**

```bash
grep -n 'CRATES=\|^inherit' ~/Developer/gentoo-overlay/gui-apps/nirius/nirius-0.7.1.ebuild
# expect: 6:CRATES="   and   49:inherit cargo  — the ordering this plan enforces
```

**Tier 3 — MUTATING. DO NOT RUN. Report verbatim for a human.**

> **DO NOT RUN.** This rewrites real ebuilds inside `~/Developer/gentoo-overlay` and
> regenerates real `Manifest` files.

```bash
# Tier 3 — DO NOT RUN.
cd ~/Developer/gentoo-overlay
cp gui-apps/nirius/nirius-0.7.1.ebuild /tmp/nirius.before
larry prepare gui-apps/nirius/nirius-0.7.1.ebuild
diff /tmp/nirius.before gui-apps/nirius/nirius-0.7.1.ebuild   # only the CRATES block
git diff --stat                                                # only the intended ebuild
ls gui-apps/nirius/*.bak 2>/dev/null                           # expect: nothing (D9)
git checkout gui-apps/nirius                                   # restore
```

---

## Out of scope

Everything involving the network: the `Cargo.lock` fetcher, forge resolution, SourceHut,
tag derivation, the subdirectory probe list, and the pycargoebuild temp directory — all
plan **23b**. Manifest mechanics (plan 21). Version comparison (plan 14). Do not fix
unrelated bugs you notice — report them.
