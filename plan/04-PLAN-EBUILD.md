# Ebuild Helper Task (T9)

**What this covers:** Variable resolution via `portageq`, `ebuild info` parsing, SRC_URI resolution, type detection.
**Why fourth:** Every command needs ebuild metadata to function.

---

### T9 — Ebuild helper: variable resolution via `portageq` + `ebuild info`

**Precondition:** T3 complete (has logger)

**Tests:**
- `TestPortageq_EnvVar` — `portageq envvar <ebuild> PN` → package name
- `TestPortageq_EnvVar_PV` — → version string
- `TestPortageq_EnvVar_P` → `${PN}-${PV}`
- `TestPortageq_GetRepoPath` — resolves overlay path from ebuild location
- `TestPortageq_MissingTool` — `portageq` not found → descriptive error
- `TestPortageq_PortageTreeUnavailable` — clarify scope: test against **both** the hardcoded `/var/db/repos/gentoo` (main tree) **and** `cfg.Overlay` (target overlay). These are different repos with different registration; the error message must distinguish which one is missing.
- `TestEbuildInfo_Parse` — `ebuild <path> info` output → parsed struct with SRC_URI, DEPEND, RDEPEND, HOMEPAGE
- `TestSRC_URI_Resolve_Npm` — `mirror://npm/<pkg>/<P>.tar.gz` → `https://registry.npmjs.org/<pkg>/<P>.tar.gz`
- `TestSRC_URI_Resolve_CustomVars` — `${MY_P}` expanded from custom variable lines
- `TestVariableResolution_Nested` — `${MY_VAR="${OTHER}-suffix"}` resolved correctly
- `TestVariableResolution_BashManip` — `${PV//./_}` handled
- `TestDetectType_Npm` — SRC_URI contains `mirror://npm/` → `TypeNPM`
- `TestDetectType_Cargo` — contains `CRATES=` → `TypeCargo`
- `TestDetectType_Go` — inherits `go-module` → `TypeGo`
- `TestDetectType_RpiKernel` — PV matches `*_pYYYYMMDD` → `TypeRpiKernel`
- `TestDetectType_Generic` — anything else → `TypeGeneric`
- `TestDetectType_Force` — `--type` flag overrides auto-detection

**Implementation:**
1. `internal/ebuild/ebuild.go`:
   ```go
   type EbuildInfo struct {
       Path      string
       Category  string
       Pkg       string
       PV        string
       PN        string
       P         string
       SRC_URI   string
       DEPEND    string
       RDEPEND   string
       HOMEPAGE  string
       Inherit   []string
   }
   ```
2. `internal/ebuild/portageq.go` — wrappers around `portageq envvar` and `portageq get_repo_path`:
   - Check that `/var/db/repos/gentoo` (or equivalent Portage tree) is accessible before calling `portageq`
   - If `portageq` fails because the Portage tree is unavailable, return a descriptive error indicating which overlay path is missing
   - Do not silently fall back to raw bash parsing — this is fragile and error-prone
3. `internal/ebuild/info.go` — parse `ebuild <path> info` output
4. `internal/ebuild/srcuri.go` — resolve SRC_URI:
   - Expand `${PN}`, `${PV}`, `${P}`, and custom variables
   - Rewrite `mirror://npm/` → `https://registry.npmjs.org/`
   - Return fully-resolved URI list
5. `internal/ebuild/detect.go` — `DetectType(ebuildPath string) (PackageType, error)` from spec table

**Acceptance:** Variable resolution produces correct expanded values for `${PN}`, `${PV}`, `${P}`, and custom vars. SRC_URI resolution handles `mirror://npm/`. Type detection matches spec table. Clear error when Portage tree is unavailable.

