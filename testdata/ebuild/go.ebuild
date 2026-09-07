# Ebuild with Go dependencies for testing
EAPI=8

inherit go-module

DESCRIPTION="A Go CLI tool"
HOMEPAGE="https://example.com/go-cli"
SRC_URI="mirror://github/${PN}/${P}.tar.gz"

LICENSE="MIT"
SLOT="0"
KEYWORDS="~amd64"

DEPEND=""
RDEPEND=""
