# Copyright 2026 Gentoo Authors
# Distributed under the terms of the GNU General Public License v2

EAPI=8

CRATES="
	crate-a-0.1.0.crate
	crate-b-0.2.0.crate
	crate-c-0.3.0.crate
"

inherit cargo

DESCRIPTION="A Rust package with literal CRATES block"
HOMEPAGE="https://example.com"
SRC_URI="https://github.com/example/repo/archive/v${PV}.tar.gz -> ${P}.tar.gz"

LICENSE="MIT"
SLOT="0"
KEYWORDS="~amd64"
