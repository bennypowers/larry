# Ebuild with npm dependencies for testing
EAPI=8

inherit nodejs

MY_PN="my-web-tool"
MY_P="${MY_PN}-0.75.3"

DESCRIPTION="A web tool that uses npm packages"
HOMEPAGE="https://example.com/my-web-tool"
SRC_URI="mirror://npm/${MY_PN}/${MY_PN}-${PV}.tgz"

LICENSE="MIT"
SLOT="0"
KEYWORDS="~amd64 ~arm64"

DEPEND=""
RDEPEND="
	dev-lang/node
	app-arch/xz-utils
"

S="${WORKDIR}/${MY_P}"
