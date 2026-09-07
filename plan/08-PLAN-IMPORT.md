# Import Command (T19)

**Why:** Relatively standalone; depends on ebuild helpers + prepare.
**Precondition:** T9 (T13 implicit — import calls prepare)

### Status: 🔴 **EMPTY** — No `internal/import/` directory, 0 files

**Tests:**
- `TestImport_FindSource` — finds latest ebuild in source overlay
- `TestImport_CopyEbuild` — copies to local overlay
- `TestImport_CopyFilesDir` — copies `files/` if present
- `TestImport_CreateMetadata` — creates `metadata.xml` with local maintainers + upstream remote-id
- `TestImport_RetainRemoteId` — upstream `<remote-id>` preserved
- `TestImport_CallPrepare` — prepare called after import
- `TestImport_FromFlag` — `--from` specifies source overlay
- `TestImport_AutoDetectSource` — searches `/var/db/repos/` for overlay containing package
- `TestImport_SourceNotFound` — no source overlay → error
- `TestImport_TargetMetadataExists` — `metadata.xml` already present in target → merge maintainers, preserve remote-id (no overwrite)
- `TestImport_SourceNewerVersion` — source has newer version than local target → overwrite with warning or error? (specify behavior)

**Implementation:**
1. `cmd/larry/import.go` — cobra subcommand
2. `internal/import/import.go` — `Import(catPkg, sourceOverlay, cfg) error`
3. Steps: locate source (`--from` or search `/var/db/repos/`) → find latest non-live ebuild → copy ebuild + `files/` to local overlay → create new `metadata.xml` (preserve upstream remote-id, add local maintainer) → call prepare pipeline

**Acceptance:** Import copies ebuild, creates metadata.xml with maintainers + remote-id. Prepare called. Metadata collision handled.
