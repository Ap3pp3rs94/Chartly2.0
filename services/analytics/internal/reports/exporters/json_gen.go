package exporters

import (

"encoding/json"

"fmt"

"strings"


"github.com/Ap3pp3rs94/Chartly2.0/services/analytics/internal/reports"
)

type JSONRenderer struct {

// Indent controls json.MarshalIndent indentation.

// If empty/whitespace, defaults to two spaces.

Indent string


// OmitCharts drops Section.Chart payloads from the output.

// Useful for smaller payloads or when chart specs are considered redundant.

OmitCharts bool
}

func (JSONRenderer) Name() string        { return "json" }
func (JSONRenderer) ContentType() string { return "application/json" }

func (jr JSONRenderer) Render(r reports.Report) ([]byte, error) {

if strings.TrimSpace(r.ID) == "" {


return nil, fmt.Errorf("%w: report id missing", reports.ErrRender)

}

if strings.TrimSpace(r.Title) == "" {


return nil, fmt.Errorf("%w: report title missing", reports.ErrRender)

}


indent := jr.Indent

if strings.TrimSpace(indent) == "" {


indent = "  "

}


out := r

if jr.OmitCharts && len(r.Sections) > 0 {


out.Sections = make([]reports.Section, len(r.Sections))


for i := range r.Sections {



s := r.Sections[i] // copy by value



s.Chart = nil



out.Sections[i] = s


}

}


b, err := json.MarshalIndent(out, "", indent)

if err != nil {


return nil, fmt.Errorf("%w: %v", reports.ErrRender, err)

}

return b, nil
}

var _ reports.Renderer = JSONRenderer{}
