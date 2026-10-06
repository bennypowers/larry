package main

import (
	"os"
	"strings"
	"testing"
)

// readContainerfile reads and returns the Containerfile content.
func readContainerfile(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("Containerfile.larry-test")
	if err != nil {
		t.Fatalf("read Containerfile.larry-test: %v", err)
	}
	return string(b)
}

// stripComments returns lines with everything from the first '#' stripped.
func stripComments(content string) []string {
	var out []string
	for _, line := range strings.Split(content, "\n") {
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		out = append(out, line)
	}
	return out
}

// --- TestContainerfile_Present ---

func TestContainerfile_Present(t *testing.T) {
	info, err := os.Stat("Containerfile.larry-test")
	if err != nil {
		t.Fatalf("Containerfile.larry-test does not exist or is not accessible: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("Containerfile.larry-test is empty")
	}
}

// --- TestContainerfile_UsesGentooStage3 (D1) ---

func TestContainerfile_UsesGentooStage3(t *testing.T) {
	content := readContainerfile(t)
	for _, line := range strings.Split(content, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "FROM" {
			want := "docker.io/gentoo/stage3:amd64-openrc"
			if fields[1] != want {
				t.Errorf("first FROM image = %q; want %q", fields[1], want)
			}
			return
		}
	}
	t.Fatal("no FROM instruction found in Containerfile.larry-test")
}

// --- TestContainerfile_NotUbuntu (D2) ---

func TestContainerfile_NotUbuntu(t *testing.T) {
	content := readContainerfile(t)
	lower := strings.ToLower(content)
	for _, word := range []string{"ubuntu", "apt-get"} {
		if strings.Contains(lower, word) {
			t.Errorf("Containerfile must not reference %s", word)
		}
	}
	for _, instr := range []string{"useradd", "EPREFIX"} {
		if strings.Contains(content, instr) {
			t.Errorf("Containerfile must not contain %q", instr)
		}
	}
}

// --- TestContainerfile_ConfiguresReposConf ---

func TestContainerfile_ConfiguresReposConf(t *testing.T) {
	content := readContainerfile(t)
	if !strings.Contains(content, "/etc/portage/repos.conf") {
		t.Error("Containerfile must configure /etc/portage/repos.conf")
	}
	if !strings.Contains(content, "location = /var/db/repos/") {
		t.Error("Containerfile must set location = /var/db/repos/")
	}
}

// --- TestContainerfile_SetsDistdirAndPkgdir ---

func TestContainerfile_SetsDistdirAndPkgdir(t *testing.T) {
	content := readContainerfile(t)
	if !strings.Contains(content, `DISTDIR="/var/cache/distfiles"`) {
		t.Error(`Containerfile must set DISTDIR="/var/cache/distfiles"`)
	}
	if !strings.Contains(content, `PKGDIR="/var/cache/binpkgs"`) {
		t.Error(`Containerfile must set PKGDIR="/var/cache/binpkgs"`)
	}
}

// --- TestContainerfile_SyncsPortageTree ---

func TestContainerfile_SyncsPortageTree(t *testing.T) {
	content := readContainerfile(t)
	for _, line := range stripComments(content) {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "RUN ") {
			cmd := strings.TrimSpace(trimmed[4:])
			if strings.HasPrefix(cmd, "emaint sync -r gentoo") {
				return
			}
		}
	}
	t.Error("Containerfile must sync the gentoo tree with emaint")
}

// --- TestContainerfile_HasRepoArg ---

func TestContainerfile_HasRepoArg(t *testing.T) {
	content := readContainerfile(t)
	var foundRepo, foundVCards, foundGpu bool
	for _, line := range stripComments(content) {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "ARG REPO="):
			foundRepo = true
			if strings.TrimSpace(trimmed[8:]) == "" {
				t.Error("ARG REPO= must have a default value")
			}
		case strings.HasPrefix(trimmed, "ARG VIDEO_CARDS="):
			foundVCards = true
			if strings.TrimSpace(trimmed[14:]) == "" {
				t.Error("ARG VIDEO_CARDS= must have a default value")
			}
		case strings.HasPrefix(trimmed, "ARG AMDGPU_TARGETS="):
			foundGpu = true
			if strings.TrimSpace(trimmed[19:]) == "" {
				t.Error("ARG AMDGPU_TARGETS= must have a default value")
			}
		}
	}
	if !foundRepo {
		t.Error("Containerfile must have ARG REPO=")
	}
	if !foundVCards {
		t.Error("Containerfile must have ARG VIDEO_CARDS=")
	}
	if !foundGpu {
		t.Error("Containerfile must have ARG AMDGPU_TARGETS=")
	}
}
