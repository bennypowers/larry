package upstream

import (
	"encoding/json"
	"io"
)

// JSONSchemaVersion is bumped when the JSON result shape changes
// incompatibly. Consumers should check it before relying on fields.
const JSONSchemaVersion = 1

// JSONOutput is the machine-readable form of an upstream check.
type JSONOutput struct {
	SchemaVersion int          `json:"schema_version"`
	Results       []JSONResult `json:"results"`
}

// JSONResult is one package row. Status is a stable snake_case slug rather
// than the internal iota value. The display glyph is deliberately omitted.
type JSONResult struct {
	Package string     `json:"package"`
	Current string     `json:"current"`
	Latest  string     `json:"latest"`
	Status  string     `json:"status"`
	Source  JSONSource `json:"source"`
	Target  string     `json:"target,omitempty"`
	Error   string     `json:"error,omitempty"`
	Depth   int        `json:"depth"`
}

// JSONSource is the provider label and canonical browse URL.
type JSONSource struct {
	Label string `json:"label"`
	URL   string `json:"url,omitempty"`
}

// JSONName returns the stable snake_case slug for a status.
func (s Status) JSONName() string {
	switch s {
	case StatusOutdated:
		return "outdated"
	case StatusUpToDate:
		return "up_to_date"
	case StatusAhead:
		return "ahead"
	case StatusNewerTag:
		return "newer_tag"
	case StatusLive:
		return "live"
	case StatusDeprecated:
		return "deprecated"
	case StatusHasRelease:
		return "has_release"
	case StatusCheckTag:
		return "check_tag"
	case StatusManual:
		return "manual"
	default:
		return "unknown"
	}
}

// NewJSONOutput converts internal results into the public JSON shape.
// Every result is included; significance filtering is a text-mode concern.
func NewJSONOutput(results []UpstreamResult) JSONOutput {
	out := JSONOutput{
		SchemaVersion: JSONSchemaVersion,
		Results:       make([]JSONResult, 0, len(results)),
	}
	for _, r := range results {
		out.Results = append(out.Results, JSONResult{
			Package: r.Package,
			Current: r.Current,
			Latest:  r.Latest,
			Status:  r.Status.JSONName(),
			Source: JSONSource{
				Label: r.Source.Label,
				URL:   r.Source.URL,
			},
			Target: r.Target,
			Error:  r.Error,
			Depth:  r.Depth,
		})
	}
	return out
}

// RenderJSON writes the complete result set as indented JSON. It emits no
// colour, hyperlinks, or diagnostics.
func RenderJSON(w io.Writer, results []UpstreamResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(NewJSONOutput(results))
}
