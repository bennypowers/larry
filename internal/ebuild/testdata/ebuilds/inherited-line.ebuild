# Copyright 2026 Gentoo Authors
# Distributed under the terms of the GNU General Public License v2

EAPI=8

INHERITED="toolchain-funcs"

DESCRIPTION="An ebuild with only INHERITED=, no type-bearing content"
HOMEPAGE="https://example.com"
SRC_URI="https://github.com/example/repo/archive/v${PV}.tar.gz -> ${P}.tar.gz"

LICENSE="MIT"
SLOT="0"
KEYWORDS="~amd64"
