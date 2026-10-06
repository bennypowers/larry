# Copyright 2026 Gentoo Authors
# Distributed under the terms of the GNU General Public License v2

EAPI=8

CRATES="
	aho-corasick@1.1.4
	anstream@0.6.21
	anstyle-parse@0.2.7
	anstyle-query@1.1.5
	anstyle-wincon@3.0.11
	anstyle@1.0.13
	clap@4.5.60
	clap_builder@4.5.60
	clap_derive@4.5.55
	clap_lex@1.0.0
	colorchoice@1.0.4
	env_filter@1.0.0
	env_logger@0.11.9
	heck@0.5.0
	is_terminal_polyfill@1.70.2
	itoa@1.0.17
	jiff-static@0.2.20
	jiff@0.2.20
	log@0.4.29
	memchr@2.8.0
	niri-ipc@25.11.0
	once_cell_polyfill@1.70.2
	portable-atomic-util@0.2.5
	portable-atomic@1.13.1
	proc-macro2@1.0.106
	quote@1.0.44
	regex-automata@0.4.14
	regex-syntax@0.8.9
	regex@1.12.3
	serde@1.0.228
	serde_core@1.0.228
	serde_derive@1.0.228
	serde_json@1.0.149
	strsim@0.11.1
	syn@2.0.117
	unicode-ident@1.0.24
	utf8parse@0.2.2
	windows-link@0.2.1
	windows-sys@0.61.2
	zmij@1.0.21
"

inherit cargo

MY_P="${PN}-${PN}-${PV}"

DESCRIPTION="Utility commands for the niri wayland compositor"
HOMEPAGE="https://sr.ht/~tsdh/nirius/"
SRC_URI="
	https://git.sr.ht/~tsdh/${PN}/archive/${PN}-${PV}.tar.gz -> ${P}.tar.gz
	${CARGO_CRATE_URIS}
"
S="${WORKDIR}/${MY_P}"

LICENSE="GPL-3+"
LICENSE+=" Apache-2.0 MIT Unicode-3.0"
SLOT="0"
KEYWORDS="~amd64"

RDEPEND="gui-wm/niri"

QA_FLAGS_IGNORED="
	usr/bin/nirius
	usr/bin/niriusd
"

src_install() {
	cargo_src_install
	dodoc README.md
}
