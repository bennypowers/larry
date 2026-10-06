# Copyright 2026 Gentoo Authors
# Distributed under the terms of the GNU General Public License v2

EAPI=8

inherit cargo

DESCRIPTION="Test cargo package without existing CRATES"
HOMEPAGE="https://example.com/test-crate"
SRC_URI="
	https://example.com/test-crate/releases/download/v1.0.0/test-crate-1.0.0.tar.gz -> ${P}.tar.gz
	${CARGO_CRATE_URIS}
"
S="${WORKDIR}/${P}"

LICENSE="MIT"
SLOT="0"
KEYWORDS="~amd64"
