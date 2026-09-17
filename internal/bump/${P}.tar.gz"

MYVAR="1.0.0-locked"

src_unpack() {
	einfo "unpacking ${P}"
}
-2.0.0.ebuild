EAPI=8
PV=2.0.0
SRC_URI="mirror://test/${P}.tar.gz"

MYVAR="1.0.0-locked"

src_unpack() {
	einfo "unpacking ${P}"
}
