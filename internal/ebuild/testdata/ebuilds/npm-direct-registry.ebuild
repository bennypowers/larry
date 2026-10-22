# Copyright 2026 Gentoo Authors
# Distributed under the terms of the GNU General Public License v2

EAPI=8

MY_NPM_SCOPE="@earendil-works"

DESCRIPTION="AI coding agent CLI with interactive TUI, tool calling, and session management"
HOMEPAGE="https://pi.dev https://github.com/earendil-works/pi"
SRC_URI="
	https://registry.npmjs.org/${MY_NPM_SCOPE}/${PN}/-/${P}.tgz -> ${P}.tgz
	https://github.com/bennypowers/gentoo-overlay/releases/download/pi-coding-agent/${P}-deps.tar.xz
	webui? ( https://registry.npmjs.org/${MY_NPM_SCOPE}/pi-web-ui/-/pi-web-ui-0.75.3.tgz -> pi-web-ui-0.75.3.tgz
		https://github.com/bennypowers/gentoo-overlay/releases/download/pi-coding-agent/pi-web-ui-0.75.3-deps.tar.xz )
"
S="${WORKDIR}"

LICENSE="MIT"
SLOT="0"
KEYWORDS="~amd64"

IUSE="webui"

RDEPEND=">=net-libs/nodejs-22"
BDEPEND=">=net-libs/nodejs-22[npm]"

src_unpack() {
	cd "${T}" || die "Could not cd to temporary directory"
	unpack ${P}-deps.tar.xz
	if use webui; then
		unpack pi-web-ui-0.75.3-deps.tar.xz
	fi
}

src_install() {
	npm \
		--offline \
		--verbose \
		--progress false \
		--foreground-scripts \
		--global \
		--prefix "${ED}"/usr \
		--cache "${T}"/npm-cache \
		install "${DISTDIR}"/${P}.tgz || die "npm install failed"

	if use webui; then
		npm \
			--offline \
			--verbose \
			--progress false \
			--foreground-scripts \
			--global \
			--prefix "${ED}"/usr \
			--cache "${T}"/npm-cache \
			install "${DISTDIR}"/pi-web-ui-0.75.3.tgz || die "npm install web-ui failed"
	fi
}
