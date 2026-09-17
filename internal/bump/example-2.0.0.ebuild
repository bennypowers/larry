EAPI=8
PV=2.0.0
P=example-2.0.0
SRC_URI="mirror://github/example/example-2.0.0.tar.gz"

cat() {
	tar xzf ${A}
}
