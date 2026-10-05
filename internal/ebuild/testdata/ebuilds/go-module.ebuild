# Copyright 2026 Gentoo Authors
# Distributed under the terms of the GNU General Public License v2

EAPI=8

inherit go-module systemd

DESCRIPTION="An Open Source Zero Trust Networking platform"
HOMEPAGE="https://netbird.io/ https://github.com/netbirdio/netbird"
SRC_URI="https://github.com/netbirdio/netbird/archive/refs/tags/v${PV}.tar.gz -> ${P}.tar.gz"
SRC_URI+=" https://github.com/gentoo-zh-drafts/netbird/releases/download/v${PV}/${P}-vendor.tar.xz"

LICENSE="BSD"
SLOT="0"
KEYWORDS="~amd64 ~arm ~arm64 ~loong ~riscv ~x86"
RESTRICT="test" # fails with network-sandbox
IUSE="wayland"

BDEPEND="
	>=dev-lang/go-1.26.0
	wayland? (
		>=dev-lang/nodejs-20
		sys-apps/pnpm-bin
		x11-libs/gtk:4
		net-libs/webkit-gtk:6
	)
"

PATCHES=( "${FILESDIR}"/${P}-systemd-service-sbin.patch )

src_compile() {
	mkdir build || die
	ego build -o build -ldflags="
		-X 'github.com/netbirdio/netbird/version.version=${PV}'
		-extldflags '${LDFLAGS}'
		" ./client

	if use wayland; then
		# Build the Wails-based GTK4 GUI
		# 1. Build the React/TypeScript frontend
		pnpm --prefix client/ui/frontend install --ignore-scripts || die "frontend deps failed"
		pnpm --prefix client/ui/frontend run build || die "frontend build failed"

		# 2. Build the Go binary (CGO required for GTK4/WebKitGTK)
		local -x CGO_ENABLED=1
		ego build -tags production -trimpath -ldflags="
			-X 'github.com/netbirdio/netbird/version.version=${PV}'
			-extldflags '${LDFLAGS}'
			-s -w
		" -o build/netbird-ui ./client/ui || die "GUI build failed"
	fi
}

src_install() {
	newsbin build/client netbird
	fperms 0700 /usr/sbin/netbird

	systemd_dounit "release_files/systemd/netbird@.service"
	newinitd "${FILESDIR}"/netbird.initd netbird

	einstalldocs

	if use wayland; then
		newbin build/netbird-ui netbird-ui
		insinto /usr/share/applications
		doins client/ui/build/linux/netbird-ui.desktop
		insinto /usr/share/icons/hicolor/128x128/apps
		doins client/ui/build/appicon.png
	fi
}
