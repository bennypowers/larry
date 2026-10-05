# Copyright 2026 Gentoo Authors
# Distributed under the terms of the GNU General Public License v2

EAPI=8

inherit npm

DESCRIPTION="A package with vendored deps in GitHub releases"
HOMEPAGE="https://example.com"
SRC_URI="https://github.com/o/r/releases/download/${PN}/${P}-vendor.tar.xz
	https://github.com/o/r/releases/download/${PN}/${P}-deps.tar.xz"

LICENSE="MIT"
SLOT="0"
KEYWORDS="~amd64"
