# Ebuild with Cargo dependencies for testing
EAPI=8

inherit cargo

CRATES="
	crates.io/aho-corasick/aho_corasick-1.1.2 
	crates.io/atty/atty-0.2.14
"

DESCRIPTION="A Rust CLI tool"
HOMEPAGE="https://example.com/rust-cli"
SRC_URI="mirror://github/${PN}/${P}.tar.gz"

LICENSE="Apache-2.0"
SLOT="0"
KEYWORDS="~amd64"

DEPEND=""
RDEPEND=""
