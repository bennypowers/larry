package ebuild

// PortageQ wraps the portageq binary.
type PortageQ struct{}

// NewPortageQ creates a new PortageQ wrapper.
func NewPortageQ() *PortageQ {
	return &PortageQ{}
}

// GetVar returns a Portage variable value.
func (p *PortageQ) GetVar(pkgDir, varName string) (string, error) {
	return "", nil
}
