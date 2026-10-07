# 30 — `upstream`: skip acct-group/* and acct-user/* in dependency scan

**Why:** `ScanDependencies` (plan 18) follows in-overlay deps without filtering
`acct-group`/`acct-user`, producing UNKNOWN depth-1 rows for `acct-group/lemonade`
and `acct-user/lemonade` (children of `sci-ml/lemonade`). Plan 16 explicitly says
"Skip acct-group/* and acct-user/*" — `discoverTargets` does it, `ScanDependencies`
does not.

**Precondition:** 18 (`ScanDependencies`), 16 (`shouldSkip`).

**Reference script:** `check_package` (L1504–1511): `is_in_overlay` checks the dir,
then `check_package -a atom` — the script never calls `check_package` on acct-* from
within a dependency recursion.

---

## Defects

| ID | Location | Defect |
|----|----------|--------|
| D1 | `dependencies.go:133` | no `shouldSkip(parsed.Category)` — every in-overlay dep recurses, including acct-* |

---

## Target behaviour

In `ScanDependencies`, after `atom.ParseAtom` succeeds and before `resolveOne`, add:

```go
if shouldSkip(parsed.Category) {
    continue
}
```

This is identical to the check in `discoverTargets` (upstream.go:46). One function,
one filter.

---

## Tests

### `internal/upstream/dependencies_test.go` — append

- `TestScanDependencies_SkipsAcctPackages` — overlay with `app-misc/root/root-1.0.0.ebuild`
  (`RDEPEND="acct-group/foo acct-user/foo dev-libs/bar"`) where `acct-group/foo` and
  `acct-user/foo` exist as directories, and `dev-libs/bar/bar-2.0.0.ebuild` exists.
  Expect exactly one row: `dev-libs/bar` (Depth 1). No acct-group/acct-user rows.

---

## Implementation steps

1. In `ScanDependencies` (dependencies.go), after the `atom.ParseAtom` call at `:128`,
   before the `visited` check at `:133`, add:
   ```go
   if shouldSkip(parsed.Category) {
       continue
   }
   ```
2. Run the new test.
3. Run full suite: `go test ./internal/upstream/... ./cmd/... -count=1`.

---

## Acceptance

### Tier 1 — automated, required

```bash
go build ./...
go vet ./...
go test ./internal/upstream/... ./cmd/larry/... -count=1
```

Grep assertions (must print nothing):
```bash
grep -rn 'acct-group\|acct-user' --include='*.go' ./internal/upstream/dependencies.go
```

### Tier 2 — read-only manual

```bash
larry upstream --fresh --overlay ~/Developer/gentoo-overlay -v | grep -c 'acct-'
# expect: 0
```

### Tier 3 — DO NOT RUN

Nothing in this plan is Tier 3.
