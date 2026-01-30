package exporters

import (

"archive/zip"

"bytes"

"encoding/json"

"encoding/xml"

"errors"

"fmt"

"math"

"sort"

"strconv"

"strings"

"time"

"unicode/utf8"


"github.com/Ap3pp3rs94/Chartly2.0/services/analytics/internal/reports"
)

type ExcelRenderer struct {

// OmitCharts controls whether chart sections embed the chart JSON spec.

// Default false => include.

OmitCharts bool


// MaxTableRows caps the number of rows rendered for any table section. Default 50.

MaxTableRows int


// MaxTextLines caps the number of emitted rows/lines per section sheet. Default 2000.

MaxTextLines int


// MaxCellChars caps per-cell text length (Excel max ~32767). Default 30000.

MaxCellChars int


// MaxSheets caps total sheets. If exceeded, sections are rendered into a single "Sections" sheet.

// Default 0 => unlimited.

MaxSheets int
}

func (ExcelRenderer) Name() string { return "xlsx" }

func (ExcelRenderer) ContentType() string {

return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
}

func (er ExcelRenderer) Render(r reports.Report) ([]byte, error) {

if strings.TrimSpace(r.ID) == "" {


return nil, fmt.Errorf("%w: report id missing", reports.ErrRender)

}

if strings.TrimSpace(r.Title) == "" {


return nil, fmt.Errorf("%w: report title missing", reports.ErrRender)

}


opts := normalizeExcelOpts(er)


wb := newWorkbook(opts)


wb.addSummary(r)


// Determine sheet strategy deterministically

sectionSheetsNeeded := len(r.Sections)

totalSheets := 1 + sectionSheetsNeeded // summary + per section


if opts.MaxSheets > 0 && totalSheets > opts.MaxSheets {


wb.addAllSectionsCombined(r)

} else {


for i := range r.Sections {



wb.addSectionSheet(r, i)


}

}


out, err := wb.bytes(r)

if err != nil {


return nil, fmt.Errorf("%w: %v", reports.ErrRender, err)

}


return out, nil
}

type excelOpts struct {

OmitCharts   bool

MaxTableRows int

MaxTextLines int

MaxCellChars int

MaxSheets    int
}

func normalizeExcelOpts(er ExcelRenderer) excelOpts {


o := excelOpts{


OmitCharts:   er.OmitCharts,


MaxTableRows: er.MaxTableRows,


MaxTextLines: er.MaxTextLines,


MaxCellChars: er.MaxCellChars,


MaxSheets:    er.MaxSheets,

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


if o.MaxSheets < 0 {



o.MaxSheets = 0

}


return o
}

////////////////////////////////////////////////////////////////////////////////
// Workbook model
////////////////////////////////////////////////////////////////////////////////

type workbook struct {

opts      excelOpts

sheets    []sheet

nameCount map[string]int
}

type sheet struct {

name string

rows [][]cell
}

type cell struct {

// typ: "s" = inline string; "n" = number.

typ   string

style int

val   string
}

func newWorkbook(opts excelOpts) *workbook {

return &workbook{


opts:      opts,


sheets:    make([]sheet, 0, 8),


nameCount: make(map[string]int),

}
}

func (wb *workbook) addSheet(name string) *sheet {

base := sanitizeSheetName(name)

if base == "" {


base = "Sheet"

}


n := wb.nameCount[base]

wb.nameCount[base] = n + 1


final := base

if n > 0 {


suffix := fmt.Sprintf("_%d", n+1)


final = truncateRunes(base, 31-lenRunes(suffix)) + suffix

}


final = strings.TrimSuffix(final, "'")

if final == "" {


final = "Sheet"

}


sh := sheet{name: final, rows: make([][]cell, 0, 256)}

wb.sheets = append(wb.sheets, sh)

return &wb.sheets[len(wb.sheets)-1]
}

func (s *sheet) addRow(cells ...cell) {

if len(cells) == 0 {


// preserve spacing with one empty cell


s.rows = append(s.rows, []cell{{typ: "s", style: 0, val: ""}})


return

}

s.rows = append(s.rows, cells)
}

func (s *sheet) addRowStrings(style int, vals ...string) {

row := make([]cell, 0, len(vals))

for _, v := range vals {


row = append(row, cell{typ: "s", style: style, val: v})

}

s.addRow(row...)
}

func (s *sheet) addKV(key, value string) {

s.addRow(


cell{typ: "s", style: 1, val: key},


cell{typ: "s", style: 0, val: value},

)
}

////////////////////////////////////////////////////////////////////////////////
// Content builders
////////////////////////////////////////////////////////////////////////////////

func (wb *workbook) addSummary(r reports.Report) {

sh := wb.addSheet("Summary")


sh.addRow(cell{typ: "s", style: 1, val: wb.capText(r.Title)})

if strings.TrimSpace(r.Subtitle) != "" {


sh.addRow(cell{typ: "s", style: 0, val: wb.capText(r.Subtitle)})

}

if strings.TrimSpace(r.Summary) != "" {


sh.addRow()


for _, ln := range splitLinesPreserve(r.Summary, wb.opts.MaxCellChars) {



sh.addRow(cell{typ: "s", style: 0, val: wb.capText(ln)})


}

}


// Metadata

sh.addRow()

sh.addRow(cell{typ: "s", style: 1, val: "Metadata"})

sh.addRow(cell{typ: "s", style: 1, val: "key"}, cell{typ: "s", style: 1, val: "value"})


meta := buildMetaMap(r)

keys := sortedKeys(meta)

for _, k := range keys {


sh.addKV(wb.capText(k), wb.capText(meta[k]))

}


// Section index

sh.addRow()

sh.addRow(cell{typ: "s", style: 1, val: "Sections"})

sh.addRow(


cell{typ: "s", style: 1, val: "index"},


cell{typ: "s", style: 1, val: "title"},


cell{typ: "s", style: 1, val: "kind"},

)


for i, sec := range r.Sections {


title := strings.TrimSpace(sec.Title)


if title == "" {




title = fmt.Sprintf("Section %d", i+1)


}


kind := strings.TrimSpace(sec.Kind)


if kind == "" {



kind = "text"


}


sh.addRow(



cell{typ: "n", style: 0, val: strconv.Itoa(i + 1)},



cell{typ: "s", style: 0, val: wb.capText(title)},



cell{typ: "s", style: 0, val: wb.capText(kind)},


)

}
}

func (wb *workbook) addSectionSheet(r reports.Report, idx int) {

sec := r.Sections[idx]

title := strings.TrimSpace(sec.Title)

if title == "" {


title = fmt.Sprintf("Section %d", idx+1)

}

kind := strings.ToLower(strings.TrimSpace(sec.Kind))

if kind == "" {


kind = "text"

}


base := fmt.Sprintf("S%02d_%s", idx+1, kind)

hint := sanitizeSheetName(title)

if hint != "" {


base = base + "_" + hint

}

sh := wb.addSheet(base)


wb.renderSectionIntoSheet(sh, title, kind, sec)
}

func (wb *workbook) addAllSectionsCombined(r reports.Report) {

sh := wb.addSheet("Sections")

sh.addRow(cell{typ: "s", style: 1, val: "All Sections (combined due to sheet cap)"})

sh.addRow()


for i := range r.Sections {


sec := r.Sections[i]


title := strings.TrimSpace(sec.Title)


if title == "" {




title = fmt.Sprintf("Section %d", i+1)


}


kind := strings.ToLower(strings.TrimSpace(sec.Kind))


if kind == "" {



kind = "text"


}



sh.addRow(cell{typ: "s", style: 1, val: fmt.Sprintf("S%02d: %s", i+1, wb.capText(title))})


sh.addRow(cell{typ: "s", style: 0, val: "kind"}, cell{typ: "s", style: 0, val: wb.capText(kind)})


sh.addRow()



wb.renderSectionBody(sh, kind, sec)



if len(sec.Meta) > 0 {




sh.addRow()




sh.addRow(cell{typ: "s", style: 1, val: "section_meta"})




keys := sortedKeys(sec.Meta)




for _, k := range keys {





sh.addKV(wb.capText(k), wb.capText(sec.Meta[k]))




}



}



sh.addRow()


sh.addRow()

}
}

func (wb *workbook) renderSectionIntoSheet(sh *sheet, title, kind string, sec reports.Section) {

sh.addRow(cell{typ: "s", style: 1, val: wb.capText(title)})

sh.addRow(cell{typ: "s", style: 0, val: "kind"}, cell{typ: "s", style: 0, val: wb.capText(kind)})

sh.addRow()


wb.renderSectionBody(sh, kind, sec)


if len(sec.Meta) > 0 {


sh.addRow()


sh.addRow(cell{typ: "s", style: 1, val: "Section meta"})


keys := sortedKeys(sec.Meta)


for _, k := range keys {



sh.addKV(wb.capText(k), wb.capText(sec.Meta[k]))


}

}


// Cap per section sheet lines deterministically

if wb.opts.MaxTextLines > 0 && len(sh.rows) > wb.opts.MaxTextLines {


sh.rows = sh.rows[:wb.opts.MaxTextLines]


sh.addRow(cell{typ: "s", style: 0, val: "... (section truncated)"})

}
}

func (wb *workbook) renderSectionBody(sh *sheet, kind string, sec reports.Section) {

switch kind {

case "text":


if strings.TrimSpace(sec.Text) == "" {



sh.addRow(cell{typ: "s", style: 0, val: "(no text)"})



return


}


for _, ln := range splitLinesPreserve(sec.Text, wb.opts.MaxCellChars) {



sh.addRow(cell{typ: "s", style: 0, val: wb.capText(ln)})


}


case "table":


if sec.Table == nil {



sh.addRow(cell{typ: "s", style: 0, val: "(no table)"})



return


}


wb.renderTable(sh, *sec.Table)


case "chart":


if wb.opts.OmitCharts {



sh.addRow(cell{typ: "s", style: 0, val: "(chart spec omitted)"})



return


}


if sec.Chart == nil {



sh.addRow(cell{typ: "s", style: 0, val: "(no chart spec)"})



return


}


b, err := json.MarshalIndent(sec.Chart, "", "  ")


if err != nil {



sh.addRow(cell{typ: "s", style: 0, val: "(invalid chart json)"})



return


}


for _, ln := range splitLinesPreserve(string(b), wb.opts.MaxCellChars) {



sh.addRow(cell{typ: "s", style: 0, val: wb.capText(ln)})


}


case "json":


b, err := json.MarshalIndent(sec.JSON, "", "  ")


if err != nil {



sh.addRow(cell{typ: "s", style: 0, val: "(invalid json)"})



return


}


for _, ln := range splitLinesPreserve(string(b), wb.opts.MaxCellChars) {



sh.addRow(cell{typ: "s", style: 0, val: wb.capText(ln)})


}



default:


sh.addRow(cell{typ: "s", style: 0, val: "(unsupported section kind)"})

}
}

func (wb *workbook) renderTable(sh *sheet, t reports.Table) {

cols := make([]string, 0, len(t.Columns))

for _, c := range t.Columns {


c = strings.TrimSpace(c)


if c != "" {



cols = append(cols, c)


}

}

if len(cols) == 0 {


sh.addRow(cell{typ: "s", style: 0, val: "(empty table columns)"})


return

}


// header

hdr := make([]cell, 0, len(cols))

for _, c := range cols {


hdr = append(hdr, cell{typ: "s", style: 1, val: wb.capText(c)})

}

sh.addRow(hdr...)


rows := t.Rows

trunc := false

if wb.opts.MaxTableRows > 0 && len(rows) > wb.opts.MaxTableRows {


rows = rows[:wb.opts.MaxTableRows]


trunc = true

}


for _, row := range rows {


out := make([]cell, 0, len(cols))


for i := 0; i < len(cols); i++ {



var v any



if i < len(row) {





v = row[i]



}



out = append(out, wb.cellFromAny(v, 0))


}


sh.addRow(out...)

}


if trunc {


sh.addRow(cell{typ: "s", style: 0, val: fmt.Sprintf("... (showing %d of %d rows)", len(rows), len(t.Rows))})

}
}

func (wb *workbook) cellFromAny(v any, style int) cell {

if v == nil {


return cell{typ: "s", style: style, val: ""}

}

switch t := v.(type) {

case string:


return cell{typ: "s", style: style, val: wb.capText(t)}

case bool:


if t {



return cell{typ: "s", style: style, val: "true"}


}


return cell{typ: "s", style: style, val: "false"}

case float64:


if math.IsNaN(t) || math.IsInf(t, 0) {



return cell{typ: "s", style: style, val: "NaN"}


}


return cell{typ: "n", style: style, val: formatFloat(t)}

case float32:


f := float64(t)


if math.IsNaN(f) || math.IsInf(f, 0) {



return cell{typ: "s", style: style, val: "NaN"}


}


return cell{typ: "n", style: style, val: formatFloat(f)}

case int:


return cell{typ: "n", style: style, val: strconv.Itoa(t)}

case int64:


return cell{typ: "n", style: style, val: strconv.FormatInt(t, 10)}

case int32:


return cell{typ: "n", style: style, val: strconv.FormatInt(int64(t), 10)}

case uint:


return cell{typ: "n", style: style, val: strconv.FormatUint(uint64(t), 10)}

case uint64:


return cell{typ: "n", style: style, val: strconv.FormatUint(t, 10)}

case uint32:


return cell{typ: "n", style: style, val: strconv.FormatUint(uint64(t), 10)}


default:


b, err := json.Marshal(t)


if err != nil {



return cell{typ: "s", style: style, val: ""}


}


return cell{typ: "s", style: style, val: wb.capText(string(b))}

}
}

func formatFloat(f float64) string {

// deterministic compact format

return strconv.FormatFloat(f, 'g', -1, 64)
}

func (wb *workbook) capText(s string) string {

s = xlsxSanitizeText(s)

if s == "" {


return ""

}

if xlsxRuneLen(s) <= wb.opts.MaxCellChars {


return s

}


tr := truncateRunes(s, wb.opts.MaxCellChars)

return strings.TrimRight(tr, " ") + "..."
}

func splitLinesPreserve(s string, maxCellChars int) []string {

s = strings.ReplaceAll(s, "\r\n", "\n")

s = strings.ReplaceAll(s, "\r", "\n")

raw := strings.Split(s, "\n")


out := make([]string, 0, len(raw))

for _, ln := range raw {


ln = strings.TrimRight(ln, " \t")


if ln == "" {



out = append(out, "")



continue


}


if maxCellChars > 0 && xlsxRuneLen(ln) > maxCellChars {



out = append(out, chunkRunes(ln, maxCellChars)...)



continue


}


out = append(out, ln)

}

return out
}

func chunkRunes(s string, n int) []string {

if n <= 0 {


return []string{s}

}

rs := []rune(s)

if len(rs) <= n {


return []string{s}

}

out := make([]string, 0, (len(rs)/n)+1)

for i := 0; i < len(rs); i += n {


j := i + n


if j > len(rs) {



j = len(rs)


}


out = append(out, string(rs[i:j]))

}

return out
}

////////////////////////////////////////////////////////////////////////////////
// XLSX writer
////////////////////////////////////////////////////////////////////////////////

func (wb *workbook) bytes(r reports.Report) ([]byte, error) {

if len(wb.sheets) == 0 {


return nil, errors.New("no sheets")

}


// Deterministic zip timestamps (no time.Now)

fixedZipTime := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)


created := fixedZipTime

if ts, ok := parseGeneratedAt(r.GeneratedAt); ok {


created = ts

}


var buf bytes.Buffer

zw := zip.NewWriter(&buf)


writeFile := func(name string, content []byte) error {


h := &zip.FileHeader{



Name:     name,



Method:   zip.Deflate,



Modified: fixedZipTime,


}


w, err := zw.CreateHeader(h)


if err != nil {



return err


}


_, err = w.Write(content)


return err

}


// Build parts deterministically

contentTypes := wb.buildContentTypes()

relsRoot := buildRootRels()

coreProps := buildCorePropsXML(r.Title, created)

appProps := wb.buildAppPropsXML()

workbookXML := wb.buildWorkbookXML()

workbookRels := wb.buildWorkbookRelsXML()

styles := buildStylesXML()


type file struct {


name    string


content []byte

}

files := []file{


{name: "[Content_Types].xml", content: contentTypes},


{name: "_rels/.rels", content: relsRoot},


{name: "docProps/core.xml", content: coreProps},


{name: "docProps/app.xml", content: appProps},


{name: "xl/workbook.xml", content: workbookXML},


{name: "xl/_rels/workbook.xml.rels", content: workbookRels},


{name: "xl/styles.xml", content: styles},

}


for i := range wb.sheets {


sx := wb.buildSheetXML(wb.sheets[i])


files = append(files, file{



name:    fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1),



content: sx,


})

}


// Write in fixed order

for _, f := range files {


if err := writeFile(f.name, f.content); err != nil {



_ = zw.Close()



return nil, err


}

}

if err := zw.Close(); err != nil {


return nil, err

}

return buf.Bytes(), nil
}

func (wb *workbook) buildContentTypes() []byte {

var b bytes.Buffer

b.WriteString(xmlHeader())

b.WriteString(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` + "\n")

b.WriteString(`  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` + "\n")

b.WriteString(`  <Default Extension="xml" ContentType="application/xml"/>` + "\n")

b.WriteString(`  <Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` + "\n")

b.WriteString(`  <Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>` + "\n")

b.WriteString(`  <Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>` + "\n")

b.WriteString(`  <Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/>` + "\n")

for i := range wb.sheets {


b.WriteString(fmt.Sprintf(`  <Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, i+1) + "\n")

}

b.WriteString(`</Types>` + "\n")

return b.Bytes()
}

func buildRootRels() []byte {

var b bytes.Buffer

b.WriteString(xmlHeader())

b.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + "\n")

b.WriteString(`  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` + "\n")

b.WriteString(`  <Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>` + "\n")

b.WriteString(`  <Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/>` + "\n")

b.WriteString(`</Relationships>` + "\n")

return b.Bytes()
}

func (wb *workbook) buildWorkbookXML() []byte {

var b bytes.Buffer

b.WriteString(xmlHeader())

b.WriteString(`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">` + "\n")

b.WriteString("  <sheets>\n")

for i := range wb.sheets {


name := xmlEscape(wb.sheets[i].name)


b.WriteString(fmt.Sprintf(`    <sheet name="%s" sheetId="%d" r:id="rId%d"/>`, name, i+1, i+1) + "\n")

}

b.WriteString("  </sheets>\n")

b.WriteString(`</workbook>` + "\n")

return b.Bytes()
}

func (wb *workbook) buildWorkbookRelsXML() []byte {

var b bytes.Buffer

b.WriteString(xmlHeader())

b.WriteString(`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` + "\n")

for i := range wb.sheets {


b.WriteString(fmt.Sprintf(`  <Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, i+1, i+1) + "\n")

}

// Styles relationship at the end (deterministic)

b.WriteString(fmt.Sprintf(`  <Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>`, len(wb.sheets)+1) + "\n")

b.WriteString(`</Relationships>` + "\n")

return b.Bytes()
}

func buildStylesXML() []byte {

// Minimal styles: style 0 default, style 1 bold

var b bytes.Buffer

b.WriteString(xmlHeader())

b.WriteString(`<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` + "\n")

b.WriteString(`  <fonts count="2">` + "\n")

b.WriteString(`    <font><sz val="11"/><name val="Calibri"/></font>` + "\n")

b.WriteString(`    <font><b/><sz val="11"/><name val="Calibri"/></font>` + "\n")

b.WriteString(`  </fonts>` + "\n")

b.WriteString(`  <fills count="1"><fill><patternFill patternType="none"/></fill></fills>` + "\n")

b.WriteString(`  <borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders>` + "\n")

b.WriteString(`  <cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs>` + "\n")

b.WriteString(`  <cellXfs count="2">` + "\n")

b.WriteString(`    <xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0" applyFont="1"/>` + "\n")

b.WriteString(`    <xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/>` + "\n")

b.WriteString(`  </cellXfs>` + "\n")

b.WriteString(`  <cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles>` + "\n")

b.WriteString(`</styleSheet>` + "\n")

return b.Bytes()
}

func (wb *workbook) buildSheetXML(s sheet) []byte {

var b bytes.Buffer

b.WriteString(xmlHeader())

b.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` + "\n")

b.WriteString(`  <sheetData>` + "\n")


for r := 0; r < len(s.rows); r++ {


row := s.rows[r]


rowNum := r + 1



if row == nil {



continue


}


b.WriteString(fmt.Sprintf(`    <row r="%d">`, rowNum) + "\n")


for c := 0; c < len(row); c++ {



colNum := c + 1



ref := cellRef(colNum, rowNum)



cellXML := buildCellXML(ref, row[c], wb.opts.MaxCellChars)



b.WriteString("      ")



b.WriteString(cellXML)



b.WriteString("\n")


}


b.WriteString("    </row>\n")

}


b.WriteString(`  </sheetData>` + "\n")

b.WriteString(`</worksheet>` + "\n")

return b.Bytes()
}

func buildCellXML(ref string, c cell, maxChars int) string {

styleAttr := ""

if c.style > 0 {


styleAttr = fmt.Sprintf(` s="%d"`, c.style)

}


switch c.typ {

case "n":


v := strings.TrimSpace(c.val)


if v == "" {





txt := xmlEscape("")



return fmt.Sprintf(`<c r="%s" t="inlineStr"%s><is><t xml:space="preserve">%s</t></is></c>`, ref, styleAttr, txt)


}


return fmt.Sprintf(`<c r="%s"%s><v>%s</v></c>`, ref, styleAttr, xmlEscape(v))




default:



txt := capString(c.val, maxChars)


txt = xlsxSanitizeText(txt)


txt = strings.ReplaceAll(txt, "\n", " ")


txt = strings.ReplaceAll(txt, "\r", " ")


return fmt.Sprintf(`<c r="%s" t="inlineStr"%s><is><t xml:space="preserve">%s</t></is></c>`, ref, styleAttr, xmlEscape(txt))

}
}

func capString(s string, maxChars int) string {

s = strings.TrimRight(s, " ")

if maxChars <= 0 {


return s

}

if xlsxRuneLen(s) <= maxChars {


return s

}

return truncateRunes(s, maxChars)
}

////////////////////////////////////////////////////////////////////////////////
// docProps (deterministic)
////////////////////////////////////////////////////////////////////////////////

func buildCorePropsXML(title string, t time.Time) []byte {


ts := t.UTC().Format(time.RFC3339)


title = xlsxSanitizeText(title)


var b bytes.Buffer

b.WriteString(xmlHeader())

b.WriteString(`<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:dcmitype="http://purl.org/dc/dcmitype/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">` + "\n")

b.WriteString("  <dc:title>" + xmlEscape(title) + "</dc:title>\n")

b.WriteString("  <dc:creator>Chartly 2.0</dc:creator>\n")

b.WriteString("  <cp:lastModifiedBy>Chartly 2.0</cp:lastModifiedBy>\n")

b.WriteString(`  <dcterms:created xsi:type="dcterms:W3CDTF">` + xmlEscape(ts) + `</dcterms:created>` + "\n")

b.WriteString(`  <dcterms:modified xsi:type="dcterms:W3CDTF">` + xmlEscape(ts) + `</dcterms:modified>` + "\n")

b.WriteString(`</cp:coreProperties>` + "\n")

return b.Bytes()
}

func (wb *workbook) buildAppPropsXML() []byte {

var b bytes.Buffer

b.WriteString(xmlHeader())

b.WriteString(`<Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties" xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes">` + "\n")

b.WriteString("  <Application>Chartly 2.0</Application>\n")

b.WriteString("  <DocSecurity>0</DocSecurity>\n")

b.WriteString("  <ScaleCrop>false</ScaleCrop>\n")


// HeadingPairs: Worksheets + count

b.WriteString("  <HeadingPairs>\n")

b.WriteString(`    <vt:vector size="2" baseType="variant">` + "\n")

b.WriteString("      <vt:variant><vt:lpstr>Worksheets</vt:lpstr></vt:variant>\n")

b.WriteString("      <vt:variant><vt:i4>" + strconv.Itoa(len(wb.sheets)) + "</vt:i4></vt:variant>\n")

b.WriteString("    </vt:vector>\n")

b.WriteString("  </HeadingPairs>\n")


// TitlesOfParts: list of sheet names

b.WriteString("  <TitlesOfParts>\n")

b.WriteString(`    <vt:vector size="` + strconv.Itoa(len(wb.sheets)) + `" baseType="lpstr">` + "\n")

for i := range wb.sheets {


b.WriteString("      <vt:lpstr>" + xmlEscape(wb.sheets[i].name) + "</vt:lpstr>\n")

}

b.WriteString("    </vt:vector>\n")

b.WriteString("  </TitlesOfParts>\n")


b.WriteString(`</Properties>` + "\n")

return b.Bytes()
}

func parseGeneratedAt(s string) (time.Time, bool) {

	s = strings.TrimSpace(s)

if s == "" {


return time.Time{}, false

}

if t, err := time.Parse(time.RFC3339Nano, s); err == nil {


return t.UTC(), true

}

if t, err := time.Parse(time.RFC3339, s); err == nil {


return t.UTC(), true

}

return time.Time{}, false
}

////////////////////////////////////////////////////////////////////////////////
// Utilities
////////////////////////////////////////////////////////////////////////////////

func buildMetaMap(r reports.Report) map[string]string {

meta := make(map[string]string)


meta["report_id"] = strings.TrimSpace(r.ID)

if strings.TrimSpace(r.TenantID) != "" {


meta["tenant_id"] = strings.TrimSpace(r.TenantID)

}

if strings.TrimSpace(r.RequestID) != "" {


meta["request_id"] = strings.TrimSpace(r.RequestID)

}

if strings.TrimSpace(r.GeneratedAt) != "" {


meta["generated_at"] = strings.TrimSpace(r.GeneratedAt)

}

if r.Meta != nil {


for k, v := range r.Meta {



k = strings.TrimSpace(k)



if k == "" {





continue



}



meta[k] = strings.TrimSpace(v)


}

}


return meta
}

func xmlHeader() string {

return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"
}

func xmlEscape(s string) string {

var b bytes.Buffer

_ = xml.EscapeText(&b, []byte(s))

return b.String()
}

func cellRef(col, row int) string {

return colName(col) + strconv.Itoa(row)
}

func colName(n int) string {

if n <= 0 {


return "A"

}

var out []byte

for n > 0 {


n--


out = append([]byte{byte('A' + (n % 26))}, out...)


n /= 26

}

return string(out)
}

func sanitizeSheetName(name string) string {

name = strings.TrimSpace(name)

if name == "" {


return ""

}

// Invalid in Excel sheet names: : \ / ? * [ ]

repl := func(r rune) rune {


switch r {


case ':', '\\', '/', '?', '*', '[', ']', '\n', '\r', '\t':



return ' '



default:



return r


}

}

name = strings.Map(repl, name)

name = strings.Join(strings.Fields(name), " ")

name = truncateRunes(name, 31)

return strings.TrimSpace(name)
}

func sortedKeys(m map[string]string) []string {

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

func truncateRunes(s string, max int) string {

if max <= 0 {


return ""

}

rs := []rune(s)

if len(rs) <= max {


return s

}

return string(rs[:max])
}

func lenRunes(s string) int {

return utf8.RuneCountInString(s)
}

func xlsxRuneLen(s string) int {

return utf8.RuneCountInString(s)
}

// sanitizeText removes XML-invalid control characters deterministically and normalizes whitespace.
func xlsxSanitizeText(s string) string {

s = strings.ReplaceAll(s, "\u00a0", " ")

s = strings.ReplaceAll(s, "\t", " ")

s = strings.ReplaceAll(s, "\r\n", "\n")

s = strings.ReplaceAll(s, "\r", "\n")


var b strings.Builder

b.Grow(len(s))

for _, r := range s {


// Remove control chars except newline


if r == '\n' {



b.WriteRune('\n')



continue


}


if r < 0x20 {



b.WriteRune(' ')



continue


}


// XML 1.0 disallows some ranges; keep it simple and safe:


if r == 0xFFFE || r == 0xFFFF {



continue


}


b.WriteRune(r)

}

out := b.String()

out = strings.TrimRight(out, " ")

return out
}

////////////////////////////////////////////////////////////////////////////////
// Interface guard
////////////////////////////////////////////////////////////////////////////////

var _ reports.Renderer = ExcelRenderer{}

