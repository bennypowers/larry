# Copyright 2026 Benny Powers
# Distributed under the terms of the GNU General Public License v2

EAPI=8

PYTHON_COMPAT=( python3_{12..14} )

inherit pam python-single-r1 systemd tmpfiles

DESCRIPTION="Web-based server administration interface"
HOMEPAGE="https://cockpit-project.org/ https://github.com/cockpit-project/cockpit"
SRC_URI="
	https://github.com/cockpit-project/${PN}/releases/download/${PV}/${P}.tar.xz
	https://github.com/cockpit-project/${PN}/releases/download/${PV}/${PN}-node-${PV}.tar.xz
"

LICENSE="LGPL-2.1+ GPL-3+ MIT BSD CC-BY-SA-3.0"
SLOT="0"
KEYWORDS="~amd64"

IUSE="doc selinux"
REQUIRED_USE="${PYTHON_REQUIRED_USE}"

DEPEND="
	${PYTHON_DEPS}
	>=app-crypt/mit-krb5-1.11
	>=dev-libs/glib-2.68:2
	>=dev-libs/json-glib-1.4
	>=net-libs/gnutls-3.6.0:=
	sys-apps/systemd:=
	sys-libs/pam
	virtual/libcrypt:=
"

RDEPEND="
	${DEPEND}
	net-misc/openssh
	net-libs/glib-networking[ssl]
"

BDEPEND="
	${PYTHON_DEPS}
	$(python_gen_cond_dep 'dev-python/installer[${PYTHON_USEDEP}]')
	>=sys-devel/gettext-0.21
	virtual/pkgconfig
	doc? ( dev-ruby/asciidoctor )
"

src_prepare() {
	default
	# The node modules tarball extracts to ${WORKDIR}/node_modules
	ln -s "${WORKDIR}/node_modules" "${S}/node_modules" || die
	# Disable the pip-based install-python target; we install the
	# wheel ourselves in src_install using dev-python/installer
	sed -i '/^install-python:/,/^[^[:space:]]/{
		/^install-python:/!{/^[^[:space:]]/!d}
	}' Makefile.in || die
}

src_configure() {
	local myeconfargs=(
		--localstatedir="${EPREFIX}/var"
		--with-pamdir="$(getpam_mod_dir)"
		--with-systemdunitdir="$(systemd_get_systemunitdir)"
		$(use_enable doc)
		$(use_enable selinux selinux-policy targeted)
	)

	econf "${myeconfargs[@]}"
}

src_install() {
	# Build the Python wheel before install (needed by install-python target)
	local wheel
	wheel=$("${PYTHON}" src/build_backend.py --wheel "${S}" tmp/wheel) || die

	default

	# Install the wheel using Gentoo's dev-python/installer
	"${PYTHON}" -m installer --destdir="${D}" --prefix="${EPREFIX}/usr" "${wheel}" || die

	# Upstream moves cockpit-askpass from bindir to libexecdir
	dodir /usr/libexec
	mv "${D}${EPREFIX}/usr/bin/cockpit-askpass" \
		"${D}${EPREFIX}/usr/libexec/cockpit-askpass" || die

	python_optimize

	keepdir /etc/cockpit/ws-certs.d
	keepdir /etc/cockpit/machines.d
}

pkg_postinst() {
	tmpfiles_process cockpit-ws.conf

	elog "To enable Cockpit, run:"
	elog "  systemctl enable --now cockpit.socket"
	elog ""
	elog "Then visit https://localhost:9090 in your browser."
	elog ""
	elog "For TLS certificates, place your cert and key in"
	elog "/etc/cockpit/ws-certs.d/ or install app-crypt/sscg"
	elog "for automatic self-signed certificate generation."
}
