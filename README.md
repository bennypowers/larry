![Larry the Cow](https://wiki.gentoo.org/images/thumb/b/b8/Larry-nefarius-v2.svg/600px-Larry-nefarius-v2.svg.png)

> [!important]
> Install Gentoo. Do a Linux

# larry

CLI tool for managing a Gentoo overlay repository.

| Command | Description |
|---------|-------------|
| `larry upstream` | Check packages for upstream version updates |
| `larry bump` | Create a new versioned ebuild from the latest |
| `larry prepare` | Download distfiles, generate deps tarballs, upload, manifest |
| `larry import` | Import an ebuild from another overlay |
| `larry test` | Test-build an ebuild locally or in a podman container |

### Custom prepare scripts

For packages with non-standard prepare logic, drop a `files/prepare.sh` script
into the package directory. Larry executes it instead of any built-in pipeline.

```bash
# sys-kernel/raspberrypi-kernel/files/prepare.sh
#!/bin/sh
date=$(echo "$PV" | grep -oP '_p\K\d{8}$')
date=$(echo $date | sed 's/^\(.\{4\}\)\(.\{2\}\)\(.\{2\}\)$/\1-\2-\3/')
commit=$(curl -s "api.github.com/repos/raspberrypi/firmware/releases" \
         | jq -r ".[] | select(.created_at | startswith(\"$date\")) | .body" \
         | grep -oP '(?:commit|hash)\s*[:=]\s*\K[0-9a-f]{8,40}' | head -1)
sed -i "s/COMMIT=\"\([^\"]*\)\"/COMMIT=\"$commit\"/" "$EBUILD"
```

**Environment variables:** `EBUILD`, `PN`, `PV`, `P`, `CATEGORY`, `DISTDIR`, `LARRY_PREPARE=1`

**Working directory:** the ebuild's parent directory (`filepath.Dir(ebuildPath)`)

**Dry-run:** detected but script is NOT executed.

### Dependencies

| Tool                       | Purpose                                |
| -------------------------- | -------------------------------------- |
| `ebuild` (Portage)         | Phase execution, manifest generation   |
| `pkgdev` (Portage contrib) | `pkgdev manifest`                      |
| `gh` (GitHub CLI)          | GitHub API auth + releases             |
| `podman`                   | Container testing with GPU passthrough |
| `npm`                      | npm dependency resolution              |
| `go`                       | Go module vendoring                    |
| `git`                      | Remote resolution, tag operations      |
| `pycargoebuild`            | Cargo lockfile → CRATES parsing        |

