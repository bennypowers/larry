# Import Command (T19)

> **Phase 1 (historical).** The ✅ marks below mean "code and tests exist",
> not "behaviour matches the reference script". A 2026-10-05 audit found defects
> in this area. See `plan/PLAN.md` for the corrected status and `28a-PLAN-IMPORT-PATHS.md`, `28b-PLAN-IMPORT-METADATA.md`.

**Why:** Relatively standalone; depends on ebuild helpers + prepare.
**Precondition:** T9 (T13 implicit — import calls prepare)

### Status: ✅ **COMPLETE** — 3 files, 22 tests

**Tests:** 22 passing
- ✅ `TestImport_FindSource_Explicit` — `--from` specifies source overlay
- ✅ `TestImport_FindSource_AutoDetect` — auto-detect from `/var/db/repos/`
- ✅ `TestImport_FindEbuild_LatestVersion` — finds highest stable version
- ✅ `TestImport_FindEbuild_LiveVersion` — finds live ebuilds when no stable
- ✅ `TestImport_FindEbuild_NotFound` — missing package → error
- ✅ `TestImport_FindEbuild_NoLive` — `liveOnly=true` with no live → error
- ✅ `TestImport_CopyEbuild` — copies ebuild to target
- ✅ `TestImport_CopyFilesDir` — copies `files/` if present
- ✅ `TestImport_CopyFilesDir_NoFilesDir` — no files/ → no error
- ✅ `TestImport_CreateMetadata` — creates new metadata.xml with maintainer + remote-id
- ✅ `TestImport_CreateMetadata_MultipleRemotes` — multiple remote-ids supported
- ✅ `TestImport_RetainRemoteId` — upstream `<remote-id>` preserved on merge
- ✅ `TestImport_AutoDetectOverlay` — finds overlay containing package
- ✅ `TestImport_AutoDetectOverlay_NotFound` — package not in overlay → empty
- ✅ `TestImport_SourceNotFound` — no source overlay → error
- ✅ `TestImport_TargetMetadataExists` — merge maintainers, preserve remote-id
- ✅ `TestImport_SourceNewerVersion` — version comparison (2.0.0 > 1.0.0)
- ✅ `TestImport_CallPrepare` — ebuild parsing works after import
- ✅ `TestImport_FromFlag` — `--from` flag behavior
- ✅ `TestImport_AutoDetectSource` — searches `/var/db/repos/` for overlays
- ✅ `TestImport_FullWorkflow` — end-to-end import (source → copy → metadata)
- ✅ `TestImport_PortageqIntegration` — portageq available check

**Implementation:**
1. ✅ `internal/importcmd/import.go` — `Import(catPkg, cfg) (*ImportResult, error)` + helpers
2. ✅ `internal/importcmd/import_test.go` — 22 tests
3. ✅ `cmd/larry/import.go` — cobra subcommand with `--from`, `--dry-run`, `--force` flags
4. Steps:
   a. Locate source overlay (`--from` or auto-detect from `/var/db/repos/`)
   b. Find latest stable ebuild (skip live versions unless no stable available)
   c. Parse source metadata.xml for remote-ids
   d. Copy ebuild + files/ to target overlay
   e. Create or merge metadata.xml (preserve upstream remote-id, add local maintainer)
   f. Print result paths and suggest `larry prepare` next step

**Data Model:**
- `ImportConfig{SourceOverlay, TargetOverlay, DryRun, Force}`
- `ImportResult{EbuildPath, MetaPath, SourcePath, SourceMeta, Prepared, Warned}`
- `ebInfo{name, pv}` — parsed from `PN-PV.ebuild` filename

**Version Comparison:**
- `9999` and `*_p*` are live versions (always > stable)
- Dotted numeric: `2.0.0 > 1.9.0 > 1.0.0`
- Live vs stable: any live > any stable

**Metadata Handling:**
- Manual XML generation (not encoding/xml) for proper Gentoo format
- Merge: preserves existing maintainers + remote-ids, adds new maintainer
- Duplicate detection via `RemoteId.String()` key
- Escape hatch: `files/prepare.sh` can override metadata

**Acceptance:** ✅ Copies ebuild, copies files/, creates/merges metadata.xml with maintainers + remote-id. Dry-run supported. `--from` flag works. Portageq integration verified.

---

### T20 — `larry test` — test command

> Placeholder for future test command that runs ebuild tests (ebuild test).

---
