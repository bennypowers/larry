# Copyright 2025 Benny Powers
# Distributed under the terms of the GNU General Public License v2

EAPI=8

DESCRIPTION="The most over-engineered cursor theme"
HOMEPAGE="https://github.com/phisch/phinger-cursors"
SRC_URI="https://github.com/phisch/phinger-cursors/releases/download/v${PV}/phinger-cursors-variants.tar.bz2 -> ${P}.tar.bz2"

LICENSE="CC-BY-SA-4.0"
SLOT="0"
KEYWORDS="~amd64 ~x86"

S="${WORKDIR}"

src_install() {
  insinto /usr/share/icons
  doins -r phinger-cursors*
}
