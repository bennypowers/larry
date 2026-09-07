# Ebuild Helper (T9)

**Why:** Every command needs ebuild metadata.
**Precondition:** T3 (logger)

### Status: ✅ **COMPLETE** — 2,238 lines impl, 1,909 lines tests, 4 packages, 0 failures

**Tests:**
- ✅ `TestPortageq_EnvVar_PN` / `PV` / `P` — `portageq envvar` → package name/version
- ✅ `TestPortageq_GetRepoPath` — resolves overlay path
- ✅ `TestPortageq_MissingTool` — `portageq` not found → descriptive error
- ✅ `TestPortageq_PortageTreeUnavailable` — error distinguishes main tree vs. overlay missing
- ✅ `TestEbuildInfo_Parse` — `ebuild info` output → struct with SRC_URI, DEPEND, RDEPEND, HOMEPAGE
- ✅ `TestSRC_URI_Resolve_Npm` — `mirror://npm/...` → `https://registry.npmjs.org/...`
- ✅ `TestSRC_URI_Resolve_CustomVars` — `${MY_P}` expanded
- ✅ `TestVariableResolution_Nested` — `${MY_VAR="${OTHER}-suffix"}`
- ✅ `TestVariableResolution_BashManip` — `${PV//./_}`
- ✅ `TestDetectType_Npm` / `Cargo` / `Go` / `RpiKernel` / `Generic` / `Force` — SRC_URI/inherit → type

**Implementation:**
1. ✅ `internal/ebuild/ebuild.go` — `EbuildInfo` struct: `Path, Category, Pkg, PV, PN, P, SRC_URI, DEPEND, RDEPEND, HOMEPAGE, Inherit []string`
2. ✅ `internal/ebuild/portageq.go` — `portageq envvar` + `get_repo_path` wrappers. Check tree accessibility before calling.
3. ✅ `internal/ebuild/info.go` — parse `ebuild <path> info` output
4. ✅ `internal/ebuild/srcuri.go` — expand vars, rewrite `mirror://npm/`, return resolved URIs
5. ✅ `internal/ebuild/detect.go` — `DetectType(ebuildPath) (PackageType, error)` (sole owner of detection logic)

**Acceptance:** ✅ Correct variable expansion, SRC_URI resolution, type detection. Clear error when Portage tree unavailable.
