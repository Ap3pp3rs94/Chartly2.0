package exporters

import (

"bytes"

"encoding/csv"

"encoding/json"

"fmt"

"sort"

"strconv"

"strings"

"unicode/utf8"


"github.com/Ap3pp3rs94/Chartly2.0/services/analytics/internal/reports"
)

type CSVRenderer struct {

OmitCharts         bool // default false

IncludeSectionMeta bool // default true

MaxTableRows       int  // default 50

MaxTextLines       int  // default 2000

MaxCellChars       int  // default 30000
}

func (CSVRenderer) Name() string        { return "csv" }
func (CSVRenderer) ContentType() string { return "text/csv" }

func (cr CSVRenderer) Render(r reports.Report) ([]byte, error) {

// Minimal validation (avoid relying on unexported validators)

if strings.TrimSpace(r.ID) == "" {


return nil, fmt.Errorf("%w: report id missing", reports.ErrRender)

}

if strings.TrimSpace(r.Title) == "" {


return nil, fmt.Errorf("%w: report title missing", reports.ErrRender)

}


opts := normalizeCSVOpts(cr)


// Determine max table column count across all sections deterministically

maxCols := 0

for i := range r.Sections {


s := r.Sections[i]


if strings.ToLower(strings.TrimSpace(s.Kind)) != "table" {



continue


}


if s.Table == nil {



continue


}


if len(s.Table.Columns) > maxCols {



maxCols = len(s.Table.Columns)


}

}


var buf bytes.Buffer

w := csv.NewWriter(&buf)

w.UseCRLF = false


// Header: record_type, section_id, section_title, kind, key, value, c1..cN

header := []string{"record_type", "section_id", "section_title", "kind", "key", "value"}

for i := 1; i <= maxCols; i++ {


header = append(header, fmt.Sprintf("c%d", i))

}

if err := w.Write(header); err != nil {


return nil, fmt.Errorf("%w: %v", reports.ErrRender, err)

}


emit := func(recordType, sectionID, sectionTitle, kind, key, value string, cols []string) error {


row := make([]string, 0, 6+maxCols)


row = append(row,



opts.cap(recordType),



opts.cap(sectionID),



opts.cap(sectionTitle),



opts.cap(kind),



opts.cap(key),



opts.cap(value),


)


// pad columns to maxCols

for i := 0; i < maxCols; i++ {



if cols != nil && i < len(cols) {




row = append(row, opts.cap(cols[i]))



} else {




row = append(row, "")



}


}


if err := w.Write(row); err != nil {



return err


}


return nil

}


// Report meta (deterministic order)

baseFields := [][2]string{


{"report_id", strings.TrimSpace(r.ID)},


{"title", strings.TrimSpace(r.Title)},


{"subtitle", strings.TrimSpace(r.Subtitle)},


{"summary", strings.TrimSpace(r.Summary)},


{"tenant_id", strings.TrimSpace(r.TenantID)},


{"request_id", strings.TrimSpace(r.RequestID)},


{"generated_at", strings.TrimSpace(r.GeneratedAt)},

}

for _, kv := range baseFields {


if err := emit("report_meta", "", "", "", kv[0], kv[1], nil); err != nil {



w.Flush()



return nil, fmt.Errorf("%w: %v", reports.ErrRender, err)


}

}


if r.Meta != nil {


keys := csvSortedKeys(r.Meta)


for _, k := range keys {



if err := emit("report_meta", "", "", "", "meta."+k, strings.TrimSpace(r.Meta[k]), nil); err != nil {




w.Flush()




return nil, fmt.Errorf("%w: %v", reports.ErrRender, err)



}


}

}


// Sections (in order)

for i := range r.Sections {


s := r.Sections[i]


secID := strings.TrimSpace(s.ID)


if secID == "" {



secID = fmt.Sprintf("s_%03d", i+1)


}


secTitle := strings.TrimSpace(s.Title)


if secTitle == "" {



secTitle = fmt.Sprintf("Section %d", i+1)


}


kind := strings.ToLower(strings.TrimSpace(s.Kind))


if kind == "" {



kind = "text"


}



// Section header row


if err := emit("section", secID, secTitle, kind, "title", secTitle, nil); err != nil {



w.Flush()



return nil, fmt.Errorf("%w: %v", reports.ErrRender, err)


}


if err := emit("section", secID, secTitle, kind, "kind", kind, nil); err != nil {



w.Flush()



return nil, fmt.Errorf("%w: %v", reports.ErrRender, err)


}



switch kind {


case "text":



text := normalizeNewlines(s.Text)



lines := strings.Split(text, "\n")



if len(lines) == 1 && strings.TrimSpace(lines[0]) == "" {




lines = nil



}



if lines == nil {




_ = emit("text_line", secID, secTitle, kind, "0", "", nil)




break



}



n := len(lines)



if opts.MaxTextLines > 0 && n > opts.MaxTextLines {




n = opts.MaxTextLines



}



for li := 0; li < n; li++ {




key := strconv.Itoa(li)




val := strings.TrimRight(lines[li], " \t")




if err := emit("text_line", secID, secTitle, kind, key, val, nil); err != nil {





w.Flush()





return nil, fmt.Errorf("%w: %v", reports.ErrRender, err)




}



}



if opts.MaxTextLines > 0 && len(lines) > n {




if err := emit("text_note", secID, secTitle, kind, "truncated", fmt.Sprintf("showing %d of %d", n, len(lines)), nil); err != nil {





w.Flush()





return nil, fmt.Errorf("%w: %v", reports.ErrRender, err)




}



}


case "table":



if s.Table == nil {




if err := emit("table_note", secID, secTitle, kind, "missing", "true", nil); err != nil {





w.Flush()





return nil, fmt.Errorf("%w: %v", reports.ErrRender, err)




}




break



}



cols := make([]string, 0, len(s.Table.Columns))



for _, c := range s.Table.Columns {




cols = append(cols, strings.TrimSpace(c))



}



if err := emit("table_header", secID, secTitle, kind, "columns", "", cols); err != nil {




w.Flush()




return nil, fmt.Errorf("%w: %v", reports.ErrRender, err)



}



rows := s.Table.Rows



trunc := false



if opts.MaxTableRows > 0 && len(rows) > opts.MaxTableRows {




rows = rows[:opts.MaxTableRows]




trunc = true



}



for ri := 0; ri < len(rows); ri++ {




row := rows[ri]




cells := make([]string, 0, len(cols))




for ci := 0; ci < len(cols); ci++ {





var v any





if ci < len(row) {






v = row[ci]





}





cells = append(cells, csvStringifyCell(v))




}




if err := emit("table_row", secID, secTitle, kind, strconv.Itoa(ri), "", cells); err != nil {





w.Flush()





return nil, fmt.Errorf("%w: %v", reports.ErrRender, err)




}



}



if trunc {




if err := emit("table_note", secID, secTitle, kind, "truncated", fmt.Sprintf("showing %d of %d", len(rows), len(s.Table.Rows)), nil); err != nil {





w.Flush()





return nil, fmt.Errorf("%w: %v", reports.ErrRender, err)




}



}


case "chart":



if opts.OmitCharts {




if err := emit("chart_note", secID, secTitle, kind, "omitted", "true", nil); err != nil {





w.Flush()





return nil, fmt.Errorf("%w: %v", reports.ErrRender, err)




}




break



}



if s.Chart == nil {




if err := emit("chart_note", secID, secTitle, kind, "missing", "true", nil); err != nil {





w.Flush()





return nil, fmt.Errorf("%w: %v", reports.ErrRender, err)




}




break



}



b, err := json.MarshalIndent(s.Chart, "", "  ")



if err != nil {




if err := emit("chart_note", secID, secTitle, kind, "invalid", "true", nil); err != nil {





w.Flush()





return nil, fmt.Errorf("%w: %v", reports.ErrRender, err)




}




break



}



if err := emit("chart_json", secID, secTitle, kind, "spec", string(b), nil); err != nil {




w.Flush()




return nil, fmt.Errorf("%w: %v", reports.ErrRender, err)



}


case "json":



b, err := json.MarshalIndent(s.JSON, "", "  ")



if err != nil {




if err := emit("json_note", secID, secTitle, kind, "invalid", "true", nil); err != nil {





w.Flush()





return nil, fmt.Errorf("%w: %v", reports.ErrRender, err)




}




break



}



if err := emit("json", secID, secTitle, kind, "payload", string(b), nil); err != nil {




w.Flush()




return nil, fmt.Errorf("%w: %v", reports.ErrRender, err)



}



default:



if err := emit("section_note", secID, secTitle, kind, "unsupported_kind", kind, nil); err != nil {




w.Flush()




return nil, fmt.Errorf("%w: %v", reports.ErrRender, err)



}


}



if opts.IncludeSectionMeta && len(s.Meta) > 0 {



keys := csvSortedKeys(s.Meta)



for _, k := range keys {




if err := emit("section_meta", secID, secTitle, kind, "meta."+k, strings.TrimSpace(s.Meta[k]), nil); err != nil {





w.Flush()





return nil, fmt.Errorf("%w: %v", reports.ErrRender, err)




}



}


}

}


w.Flush()

if err := w.Error(); err != nil {


return nil, fmt.Errorf("%w: %v", reports.ErrRender, err)

}

return buf.Bytes(), nil
}

////////////////////////////////////////////////////////////////////////////////
// Options + helpers
////////////////////////////////////////////////////////////////////////////////

type csvOpts struct {

OmitCharts         bool

IncludeSectionMeta bool

MaxTableRows       int

MaxTextLines       int

MaxCellChars       int
}

func normalizeCSVOpts(cr CSVRenderer) csvOpts {


o := csvOpts{


OmitCharts:         cr.OmitCharts,


IncludeSectionMeta: cr.IncludeSectionMeta,


MaxTableRows:       cr.MaxTableRows,


MaxTextLines:       cr.MaxTextLines,


MaxCellChars:       cr.MaxCellChars,

}


if o.MaxTableRows <= 0 {



o.MaxTableRows = 50

}


if o.MaxTextLines <= 0 {



o.MaxTextLines = 2000

}


if o.MaxCellChars <= 0 {



o.MaxCellChars = 30000

}


if o.MaxCellChars > 32767 {



o.MaxCellChars = 32767

}


// default include meta = true

if cr.IncludeSectionMeta == false {



o.IncludeSectionMeta = false

} else {



o.IncludeSectionMeta = true

}


return o
}

func (o csvOpts) cap(s string) string {


s = normalizeNewlines(s)


s = strings.ReplaceAll(s, "\t", " ")


s = strings.TrimRight(s, " ")



if s == "" {



return ""


}



if csvRuneLen(s) <= o.MaxCellChars {



return s


}


return csvTruncateRunes(s, o.MaxCellChars) + "..."
}

func normalizeNewlines(s string) string {


s = strings.ReplaceAll(s, "\r\n", "\n")


s = strings.ReplaceAll(s, "\r", "\n")


// remove NULs deterministically

s = strings.ReplaceAll(s, "\x00", "")


return s
}

func csvSortedKeys(m map[string]string) []string {


keys := make([]string, 0, len(m))


for k := range m {



k = strings.TrimSpace(k)



if k == "" {




continue



}



keys = append(keys, k)


}


sort.Strings(keys)


return keys
}

func csvStringifyCell(v any) string {


if v == nil {



return ""


}


switch t := v.(type) {


case string:



return t


case bool:



if t {




return "true"



}



return "false"


case float64:



return strconv.FormatFloat(t, 'g', -1, 64)


case float32:



return strconv.FormatFloat(float64(t), 'g', -1, 64)


case int:



return strconv.Itoa(t)


case int64:



return strconv.FormatInt(t, 10)


case int32:



return strconv.FormatInt(int64(t), 10)


case uint:



return strconv.FormatUint(uint64(t), 10)


case uint64:



return strconv.FormatUint(t, 10)


case uint32:



return strconv.FormatUint(uint64(t), 10)


default:



b, err := json.Marshal(t)



if err != nil {




return ""



}



return string(b)


}
}

func csvRuneLen(s string) int {


return utf8.RuneCountInString(s)
}

func csvTruncateRunes(s string, max int) string {


if max <= 0 {



return ""


}


rs := []rune(s)


if len(rs) <= max {



return s


}


return string(rs[:max])
}

////////////////////////////////////////////////////////////////////////////////
// Interface guard
////////////////////////////////////////////////////////////////////////////////

var _ reports.Renderer = CSVRenderer{}
