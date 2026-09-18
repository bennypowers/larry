package output

import (
	"fmt"
	"os"

	"github.com/muesli/termenv"
)

// ColorHelper provides color output using termenv.
type ColorHelper struct {
	mode    string
	enabled bool
	profile termenv.Profile
}

// NewColorHelper creates a new ColorHelper with the given mode.
func NewColorHelper(mode string) (*ColorHelper, error) {
	if mode != "auto" && mode != "always" && mode != "never" {
		return nil, &ConfigError{fmt.Sprintf("invalid color mode: %q (must be auto, always, or never)", mode)}
	}
	return &ColorHelper{
		mode:    mode,
		enabled: mode == "always",
		profile: termenv.Ascii,
	}, nil
}

// Detect auto-detects terminal color support.
func (c *ColorHelper) Detect() error {
	if c.mode != "auto" {
		return nil
	}

	if _, noColor := os.LookupEnv("NO_COLOR"); noColor {
		c.enabled = false
		c.profile = termenv.Ascii
		return nil
	}

	output := termenv.DefaultOutput()
	c.profile = output.Profile
	c.enabled = c.profile != termenv.Ascii

	return nil
}

// Enabled returns whether color output is currently enabled.
func (c *ColorHelper) Enabled() bool {
	if c.mode == "never" {
		return false
	}
	return c.enabled
}

// Mode returns the current color mode.
func (c *ColorHelper) Mode() string {
	return c.mode
}

// Red returns red-colored text.
func (c *ColorHelper) Red(text string) string {
	if !c.enabled {
		return text
	}
	return termenv.String(text).Foreground(termenv.ANSIRed).String()
}

// Cyan returns cyan-colored text.
func (c *ColorHelper) Cyan(text string) string {
	if !c.enabled {
		return text
	}
	return termenv.String(text).Foreground(termenv.ANSICyan).String()
}

// Green returns green-colored text.
func (c *ColorHelper) Green(text string) string {
	if !c.enabled {
		return text
	}
	return termenv.String(text).Foreground(termenv.ANSIGreen).String()
}

// Yellow returns yellow-colored text.
func (c *ColorHelper) Yellow(text string) string {
	if !c.enabled {
		return text
	}
	return termenv.String(text).Foreground(termenv.ANSIYellow).String()
}

// Blue returns blue-colored text.
func (c *ColorHelper) Blue(text string) string {
	if !c.enabled {
		return text
	}
	return termenv.String(text).Foreground(termenv.ANSIBlue).String()
}

// Dim returns dimmed text.
func (c *ColorHelper) Dim(text string) string {
	if !c.enabled {
		return text
	}
	return termenv.String(text).Foreground(termenv.ANSIBrightBlack).String()
}

// Hyperlink creates an OSC 8 hyperlink.
func (c *ColorHelper) Hyperlink(text, url string) string {
	if !c.enabled {
		return text
	}
	return "\033]8;;" + url + "\033\\" + text + "\033]8;;\033\\"
}

// ConfigError represents a configuration validation error.
type ConfigError struct {
	msg string
}

func (e *ConfigError) Error() string {
	return e.msg
}
