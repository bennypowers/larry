# Import Command Task (T19)

**What this covers:** `larry import` — import ebuild from another overlay.
**Why eighth:** Relatively standalone; depends on ebuild helpers and prepare.

---

### T19 — `larry import` — import pipeline

**Precondition:** T9 complete (T13 implicitly — import calls prepare, and T13 is phase 7 before T19 phase 8, but T13 is listed explicitly here to avoid the same undocumented-dependency bug that caused the original bump issue)

**Tests:
- `TestImport_FindSource` — finds latest ebuild in source overlay
- `TestImport_CopyEbuild` — copies ebuild to local overlay
- `TestImport_CopyFilesDir` — copies `files/` dir if present
- `TestImport_CreateMetadata` — creates new `metadata.xml` with local maintainers + upstream remote-id
- `TestImport_RetainRemoteId` — upstream `<remote-id>` preserved in new `metadata.xml`
- `TestImport_CallPrepare` — after import, calls prepare pipeline
- `TestImport_FromFlag` — `--from` specifies source overlay
- `TestImport_AutoDetectSource` — searches `/var/db/repos/` for overlay containing package
- `TestImport_SourceNotFound` — no source overlay found → error

**Implementation:**
1. `cmd/larry/import.go` — cobra subcommand
2. `internal/import/import.go`:
   ```go
   func Import(catPkg, sourceOverlay string, cfg config.Config) error
   ```
3. Steps:
   a. Locate source overlay (`--from` or search `/var/db/repos/`)
   b. Find latest non-live ebuild in source
   c. Copy ebuild + `files/` to local overlay
   d. Create new `metadata.xml` with local maintainer + upstream `<remote-id>`
   e. Call prepare pipeline on new ebuild
4. Metadata rewriting: preserve upstream `<remote-id>`, add local maintainer info

**Acceptance:** Import copies ebuild, creates metadata.xml with correct maintainers and remote-id. Prepare called on new ebuild.
