package exporters

import (

"bytes"

"encoding/json"

"errors"

"fmt"

"sort"

"strings"

"unicode/utf8"


"github.com/Ap3pp3rs94/Chartly2.0/services/analytics/internal/reports"
)

const (

pdfPageW  = 612.0

pdfPageH  = 792.0

pdfMargin = 72.0


pdfTopY    = pdfPageH - pdfMargin

pdfBottomY = pdfMargin


pdfLeftX = pdfMargin


// Built-in font resource names.

fontHelvetica = "F1"

fontCourier   = "F2"
)

type PDFRenderer struct {

MaxTableRows        int  // default 50

IncludeCharts       bool // default true

MaxPointsPerSection int  // safety cap on emitted lines per section; default 2000
}

func (PDFRenderer) Name() string        { return "pdf" }
func (PDFRenderer) ContentType() string { return "application/pdf" }

func (pr PDFRenderer) Render(r reports.Report) ([]byte, error) {

// Minimal validation (report_engine.go validation is unexported)

if strings.TrimSpace(r.ID) == "" {


return nil, fmt.Errorf("%w: report id missing", reports.ErrRender)

}

if strings.TrimSpace(r.Title) == "" {


return nil, fmt.Errorf("%w: report title missing", reports.ErrRender)

}


maxRows := pr.MaxTableRows

if maxRows <= 0 {


maxRows = 50

}

includeCharts := pr.IncludeCharts

if !pr.IncludeCharts && pr.IncludeCharts == false {


// explicit false; keep

} else {


// default true


includeCharts = true

}

maxPerSection := pr.MaxPointsPerSection

if maxPerSection <= 0 {


maxPerSection = 2000

}


lines := buildReportLines(r, includeCharts, maxRows, maxPerSection)

pages := paginate(lines)


pdf, err := buildPDF(pages)

if err != nil {


return nil, fmt.Errorf("%w: %v", reports.ErrRender, err)

}


return pdf, nil
}

////////////////////////////////////////////////////////////////////////////////
// Report -> lines
////////////////////////////////////////////////////////////////////////////////

type pdfLine struct {

Text string

Font string // F1/F2

Size int

// hardWrap indicates "code-like" wrapping behavior (no word-boundary required).

HardWrap bool
}

func buildReportLines(r reports.Report, includeCharts bool, maxTableRows int, maxPerSection int) []pdfLine {

out := make([]pdfLine, 0, 2048)


// Title

out = append(out, pdfLine{Text: sanitizeText(r.Title), Font: fontHelvetica, Size: 18})

if strings.TrimSpace(r.Subtitle) != "" {


out = append(out, pdfLine{Text: sanitizeText(r.Subtitle), Font: fontHelvetica, Size: 12})

}

if strings.TrimSpace(r.Summary) != "" {


out = append(out, pdfLine{Text: "", Font: fontHelvetica, Size: 10})


out = append(out, wrapParagraph(sanitizeText(r.Summary), fontHelvetica, 11, false)...)

}

out = append(out, pdfLine{Text: "", Font: fontHelvetica, Size: 10})


// Metadata

metaLines := renderMetadata(r)

if len(metaLines) > 0 {


out = append(out, pdfLine{Text: "Metadata", Font: fontHelvetica, Size: 14})


out = append(out, pdfLine{Text: "", Font: fontHelvetica, Size: 10})


out = append(out, metaLines...)


out = append(out, pdfLine{Text: "", Font: fontHelvetica, Size: 10})

}


// Sections

for si, s := range r.Sections {


secTitle := strings.TrimSpace(s.Title)


if secTitle == "" {



secTitle = fmt.Sprintf("Section %d", si+1)


}


out = append(out, pdfLine{Text: sanitizeText(secTitle), Font: fontHelvetica, Size: 14})


out = append(out, pdfLine{Text: "", Font: fontHelvetica, Size: 10})



// Render body based on kind


kind := strings.ToLower(strings.TrimSpace(s.Kind))


secStart := len(out)



switch kind {


case "text":



if strings.TrimSpace(s.Text) == "" {




out = append(out, pdfLine{Text: "(no text)", Font: fontCourier, Size: 10, HardWrap: true})



} else {




out = append(out, wrapParagraph(sanitizeText(s.Text), fontHelvetica, 11, false)...)



}



case "table":



if s.Table == nil {




out = append(out, pdfLine{Text: "(no table data)", Font: fontCourier, Size: 10, HardWrap: true})



} else {






tableLines := renderTable(*s.Table, maxTableRows)




out = append(out, tableLines...)



}



case "chart":



if !includeCharts {




out = append(out, pdfLine{Text: "(chart spec omitted)", Font: fontCourier, Size: 10, HardWrap: true})



} else if s.Chart == nil {




out = append(out, pdfLine{Text: "(no chart spec)", Font: fontCourier, Size: 10, HardWrap: true})



} else {






j, err := json.MarshalIndent(s.Chart, "", "  ")




if err != nil {






out = append(out, pdfLine{Text: "(invalid chart spec)", Font: fontCourier, Size: 10, HardWrap: true})




} else {






out = append(out, renderCodeBlock(string(j))...)




}



}




case "json":



j, err := json.MarshalIndent(s.JSON, "", "  ")



if err != nil {




out = append(out, pdfLine{Text: "(invalid json)", Font: fontCourier, Size: 10, HardWrap: true})



} else {




out = append(out, renderCodeBlock(string(j))...)



}







default:



out = append(out, pdfLine{Text: "(unsupported section kind)", Font: fontCourier, Size: 10, HardWrap: true})


}



// Section meta


if len(s.Meta) > 0 {



out = append(out, pdfLine{Text: "", Font: fontHelvetica, Size: 10})



out = append(out, pdfLine{Text: "Section meta:", Font: fontHelvetica, Size: 11})



out = append(out, renderStringMapAsCode(s.Meta)...)


}



// Safety cap per section


if maxPerSection > 0 {



secLen := len(out) - secStart



if secLen > maxPerSection {






// truncate deterministically




out = out[:secStart+maxPerSection]




out = append(out, pdfLine{Text: "... (section truncated)", Font: fontCourier, Size: 10, HardWrap: true})



}


}



out = append(out, pdfLine{Text: "", Font: fontHelvetica, Size: 10})

}


return out
}

func renderMetadata(r reports.Report) []pdfLine {

lines := make([]pdfLine, 0, 64)


kv := make(map[string]string)

if strings.TrimSpace(r.TenantID) != "" {


kv["tenant_id"] = strings.TrimSpace(r.TenantID)

}

if strings.TrimSpace(r.RequestID) != "" {


kv["request_id"] = strings.TrimSpace(r.RequestID)

}

if strings.TrimSpace(r.GeneratedAt) != "" {


kv["generated_at"] = strings.TrimSpace(r.GeneratedAt)

}

if r.Meta != nil {


for k, v := range r.Meta {



k = strings.TrimSpace(k)



if k == "" {





continue



}



kv[k] = strings.TrimSpace(v)


}

}


if len(kv) == 0 {


return nil

}


keys := make([]string, 0, len(kv))

for k := range kv {


keys = append(keys, k)

}

sort.Strings(keys)


for _, k := range keys {


lines = append(lines, pdfLine{



Text: fmt.Sprintf("%s: %s", sanitizeText(k), sanitizeText(kv[k])),



Font: fontHelvetica,



Size: 11,


})

}

return lines
}

func renderTable(t reports.Table, maxRows int) []pdfLine {

cols := make([]string, 0, len(t.Columns))

for _, c := range t.Columns {


c = strings.TrimSpace(c)


if c != "" {



cols = append(cols, sanitizeText(c))


}

}

if len(cols) == 0 {


return []pdfLine{{Text: "(empty table)", Font: fontCourier, Size: 10, HardWrap: true}}

}


rows := t.Rows

truncated := false

if maxRows > 0 && len(rows) > maxRows {


rows = rows[:maxRows]


truncated = true

}


// Render as monospace pseudo-table with pipes. Keep deterministic.

lines := make([]pdfLine, 0, 2+len(rows)+4)


header := "| " + strings.Join(cols, " | ") + " |"

sepParts := make([]string, len(cols))

for i := range cols {


sepParts[i] = strings.Repeat("-", maxInt(3, minInt(20, len(cols[i]))))

}

sep := "| " + strings.Join(sepParts, " | ") + " |"


lines = append(lines, pdfLine{Text: header, Font: fontCourier, Size: 9, HardWrap: true})

lines = append(lines, pdfLine{Text: sep, Font: fontCourier, Size: 9, HardWrap: true})


for _, row := range rows {


cells := make([]string, len(cols))


for i := 0; i < len(cols); i++ {



var v any



if i < len(row) {





v = row[i]



}



cells[i] = sanitizeText(stringifyCell(v))


}


line := "| " + strings.Join(cells, " | ") + " |"


lines = append(lines, pdfLine{Text: line, Font: fontCourier, Size: 9, HardWrap: true})

}


if truncated {


lines = append(lines, pdfLine{Text: fmt.Sprintf("... (showing %d of %d rows)", len(rows), len(t.Rows)), Font: fontCourier, Size: 9, HardWrap: true})

}


return lines
}

func renderCodeBlock(s string) []pdfLine {

s = strings.ReplaceAll(s, "\r\n", "\n")

s = strings.ReplaceAll(s, "\r", "\n")

raw := strings.Split(s, "\n")


out := make([]pdfLine, 0, len(raw)+2)

for _, ln := range raw {


out = append(out, pdfLine{Text: sanitizeText(ln), Font: fontCourier, Size: 9, HardWrap: true})

}

return out
}

func renderStringMapAsCode(m map[string]string) []pdfLine {

if m == nil {


return nil

}


keys := make([]string, 0, len(m))

for k := range m {


k = strings.TrimSpace(k)


if k == "" {




continue


}


keys = append(keys, k)

}

sort.Strings(keys)


out := make([]pdfLine, 0, len(keys))

for _, k := range keys {


out = append(out, pdfLine{



Text:     fmt.Sprintf("%s=%s", sanitizeText(k), sanitizeText(strings.TrimSpace(m[k]))),



Font:     fontCourier,



Size:     9,



HardWrap: true,


})

}

return out
}

func stringifyCell(v any) string {

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


return fmt.Sprintf("%.6g", t)

case float32:


return fmt.Sprintf("%.6g", float64(t))

case int:


return fmt.Sprintf("%d", t)

case int64:


return fmt.Sprintf("%d", t)

case int32:


return fmt.Sprintf("%d", t)

case uint:


return fmt.Sprintf("%d", t)

case uint64:


return fmt.Sprintf("%d", t)

case uint32:


return fmt.Sprintf("%d", t)

default:


b, err := json.Marshal(t)


if err != nil {



return ""


}


return string(b)

}
}

////////////////////////////////////////////////////////////////////////////////
// Wrapping + sanitization
////////////////////////////////////////////////////////////////////////////////

func wrapParagraph(s string, font string, size int, hard bool) []pdfLine {

s = strings.ReplaceAll(s, "\r\n", "\n")

s = strings.ReplaceAll(s, "\r", "\n")


paras := strings.Split(s, "\n")

out := make([]pdfLine, 0, len(paras)*2)


for i, p := range paras {


p = strings.TrimRight(p, " \t")


if strings.TrimSpace(p) == "" {




// blank line




out = append(out, pdfLine{Text: "", Font: font, Size: size})




continue


}


limit := maxChars(font, size)


var lines []string


if hard {



lines = wrapHard(p, limit)


} else {



lines = wrapSoft(p, limit)


}


for _, ln := range lines {



out = append(out, pdfLine{Text: sanitizeText(ln), Font: font, Size: size, HardWrap: hard})


}


// preserve explicit paragraph breaks (except after last)


if i != len(paras)-1 && strings.TrimSpace(paras[i+1]) != "" {




// no extra


}


}

return out
}

func maxChars(font string, size int) int {

if size <= 0 {


size = 11

}

width := pdfPageW - 2*pdfMargin // 468

// rough average character width factor

factor := 0.55

if font == fontCourier {


factor = 0.60

}

n := int(width / (float64(size) * factor))

if n < 20 {


n = 20

}

if n > 200 {


n = 200

}

return n
}

func wrapSoft(s string, limit int) []string {

s = strings.TrimSpace(s)

if s == "" {


return []string{""}

}

if limit <= 0 {


return []string{s}

}


words := strings.Fields(s)

if len(words) == 0 {


return []string{""}

}


lines := make([]string, 0)

cur := words[0]

for i := 1; i < len(words); i++ {


w := words[i]


if runeLen(cur)+1+runeLen(w) <= limit {



cur = cur + " " + w


} else {



lines = append(lines, cur)



cur = w


}

}

lines = append(lines, cur)


// Hard wrap any overlong line (single very long token)

out := make([]string, 0, len(lines))

for _, ln := range lines {


if runeLen(ln) <= limit {



out = append(out, ln)



continue


}


out = append(out, wrapHard(ln, limit)...)

}

return out
}

func wrapHard(s string, limit int) []string {

if limit <= 0 {


return []string{s}

}

rs := []rune(s)

if len(rs) <= limit {


return []string{s}

}

out := make([]string, 0, (len(rs)/limit)+1)

for i := 0; i < len(rs); i += limit {


j := i + limit


if j > len(rs) {



j = len(rs)


}


out = append(out, string(rs[i:j]))

}

return out
}

func runeLen(s string) int {

return utf8.RuneCountInString(s)
}

// sanitizeText keeps output deterministic and safe for built-in PDF fonts.
// - Replace non-ASCII runes with '?' (PDF base fonts are limited; avoid encoding issues).
// - Preserve tabs minimally by converting to single spaces.
func sanitizeText(s string) string {

s = strings.ReplaceAll(s, "\t", " ")

s = strings.ReplaceAll(s, "\u00a0", " ")


var b strings.Builder

b.Grow(len(s))

for _, r := range s {


if r == '\n' || r == '\r' {




// should already be split, but keep safe




b.WriteRune(' ')




continue


}


if r < 32 {




// control chars -> space




b.WriteRune(' ')




continue


}


if r > 126 {




b.WriteRune('?')




continue


}


b.WriteRune(r)

}

return strings.TrimRight(b.String(), " ")
}

////////////////////////////////////////////////////////////////////////////////
// Pagination
////////////////////////////////////////////////////////////////////////////////

func paginate(lines []pdfLine) [][]pdfLine {

pages := make([][]pdfLine, 0, 4)

cur := make([]pdfLine, 0, 256)



y := pdfTopY


for _, ln := range lines {


size := ln.Size


if size <= 0 {



size = 11


}


leading := float64(size) + 2.0



// if line would go below bottom margin, new page


if y-leading < pdfBottomY {




// commit current page (even if empty)




pages = append(pages, cur)




cur = make([]pdfLine, 0, 256)




y = pdfTopY


}



// wrap per line based on font/size and hardWrap flag


limit := maxChars(ln.Font, size)


var wrapped []string


if ln.HardWrap {



wrapped = wrapHard(ln.Text, limit)


} else {



wrapped = wrapSoft(ln.Text, limit)


}


for _, w := range wrapped {



ln2 := ln



ln2.Text = w




leading2 := float64(size) + 2.0



if y-leading2 < pdfBottomY {






pages = append(pages, cur)





cur = make([]pdfLine, 0, 256)





y = pdfTopY


}



cur = append(cur, ln2)



y -= leading2


}

}


if len(cur) > 0 || len(pages) == 0 {


pages = append(pages, cur)

}

return pages
}

////////////////////////////////////////////////////////////////////////////////
// Minimal PDF writer (no external libs)
////////////////////////////////////////////////////////////////////////////////

func buildPDF(pages [][]pdfLine) ([]byte, error) {

if len(pages) == 0 {


return nil, errors.New("no pages")

}


// Object IDs:

// 1: Catalog

// 2: Pages

// 3: Helvetica font

// 4: Courier font

// 5..: per-page: Page obj, Content stream obj (two per page)


helvID := 3

courID := 4

firstPageID := 5

objCount := 4 + (len(pages) * 2)


// Precompute page IDs

pageIDs := make([]int, len(pages))

contentIDs := make([]int, len(pages))

for i := range pages {


pageIDs[i] = firstPageID + i*2


contentIDs[i] = pageIDs[i] + 1

}


var buf bytes.Buffer

// Header

buf.WriteString("%PDF-1.4\n")

// Binary comment line (recommended)

buf.Write([]byte{0x25, 0xE2, 0xE3, 0xCF, 0xD3, '\n'})


offsets := make([]int, objCount+1) // xref includes object 0


writeObj := func(id int, body string) {


offsets[id] = buf.Len()


buf.WriteString(fmt.Sprintf("%d 0 obj\n", id))


buf.WriteString(body)


if !strings.HasSuffix(body, "\n") {



buf.WriteString("\n")


}


buf.WriteString("endobj\n")

}


writeStreamObj := func(id int, stream []byte) {


offsets[id] = buf.Len()


buf.WriteString(fmt.Sprintf("%d 0 obj\n", id))


buf.WriteString(fmt.Sprintf("<< /Length %d >>\n", len(stream)))


buf.WriteString("stream\n")


buf.Write(stream)


if len(stream) == 0 || stream[len(stream)-1] != '\n' {



buf.WriteByte('\n')


}


buf.WriteString("endstream\n")


buf.WriteString("endobj\n")

}


// 3: Helvetica

writeObj(helvID, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")

// 4: Courier

writeObj(courID, "<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>")


// Page objects + streams

for i := range pages {


content := buildPageContent(pages[i], i+1, len(pages))


writeStreamObj(contentIDs[i], content)



pageBody := fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.0f %.0f] /Resources << /Font << /%s %d 0 R /%s %d 0 R >> >> /Contents %d 0 R >>",



pdfPageW, pdfPageH,



fontHelvetica, helvID,



fontCourier, courID,



contentIDs[i],


)


writeObj(pageIDs[i], pageBody)

}


// 2: Pages

var kids strings.Builder

kids.WriteString("[")

for i := range pageIDs {


if i > 0 {



kids.WriteString(" ")


}


kids.WriteString(fmt.Sprintf("%d 0 R", pageIDs[i]))

}

kids.WriteString("]")


pagesBody := fmt.Sprintf("<< /Type /Pages /Kids %s /Count %d >>", kids.String(), len(pageIDs))

writeObj(2, pagesBody)


// 1: Catalog

catalogBody := "<< /Type /Catalog /Pages 2 0 R >>"

writeObj(1, catalogBody)


// XRef

startXRef := buf.Len()

buf.WriteString("xref\n")

buf.WriteString(fmt.Sprintf("0 %d\n", objCount+1))

buf.WriteString("0000000000 65535 f \n")

for i := 1; i <= objCount; i++ {


buf.WriteString(fmt.Sprintf("%010d 00000 n \n", offsets[i]))

}


// Trailer

buf.WriteString("trailer\n")

buf.WriteString(fmt.Sprintf("<< /Size %d /Root 1 0 R >>\n", objCount+1))

buf.WriteString("startxref\n")

buf.WriteString(fmt.Sprintf("%d\n", startXRef))

buf.WriteString("%%EOF\n")


return buf.Bytes(), nil
}

func buildPageContent(lines []pdfLine, pageNo int, pageTotal int) []byte {

var b bytes.Buffer


// Main text block

b.WriteString("q\n")

b.WriteString("BT\n")

// set text matrix to start position

b.WriteString(fmt.Sprintf("1 0 0 1 %.0f %.0f Tm\n", pdfLeftX, pdfTopY))


for _, ln := range lines {


font := ln.Font


if font != fontHelvetica && font != fontCourier {



font = fontHelvetica


}


size := ln.Size


if size <= 0 {



size = 11


}


leading := float64(size) + 2.0



b.WriteString(fmt.Sprintf("/%s %d Tf\n", font, size))


b.WriteString("(")


b.WriteString(escapePDFString(ln.Text))


b.WriteString(") Tj\n")


b.WriteString(fmt.Sprintf("0 -%.2f Td\n", leading))

}


b.WriteString("ET\n")


// Footer page numbering

b.WriteString("BT\n")

b.WriteString(fmt.Sprintf("/%s %d Tf\n", fontHelvetica, 9))

b.WriteString(fmt.Sprintf("1 0 0 1 %.0f %.0f Tm\n", pdfLeftX, 40.0))

b.WriteString("(")

b.WriteString(escapePDFString(fmt.Sprintf("Page %d of %d", pageNo, pageTotal)))

b.WriteString(") Tj\n")

b.WriteString("ET\n")


b.WriteString("Q\n")

return b.Bytes()
}

// escapePDFString escapes \ ( ) and normalizes to safe ASCII (already sanitized, but double-safe).
func escapePDFString(s string) string {

s = sanitizeText(s)


var b strings.Builder

b.Grow(len(s))

for _, r := range s {


switch r {


case '\\':



b.WriteString("\\\\")


case '(':



b.WriteString("\\(")


case ')':



b.WriteString("\\)")



default:



if r < 32 || r > 126 {




b.WriteByte('?')



} else {




b.WriteRune(r)



}


}

}

return b.String()
}

func minInt(a, b int) int {

if a < b {


return a

}

return b
}

func maxInt(a, b int) int {

if a > b {


return a

}

return b
}
