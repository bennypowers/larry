EAPI=8
PV=2.0.0
SRC_URI="mirror://test/my-pkg-2.0.0.tar.gz"

cat() {
	tar xzf ${A}
}
