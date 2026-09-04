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

