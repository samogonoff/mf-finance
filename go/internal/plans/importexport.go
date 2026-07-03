package plans

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Импорт/экспорт Excel для TPL-MP (TPL-06). Минимальный валидный .xlsx на stdlib
// (archive/zip + encoding/xml) — без внешних зависимостей. Экспорт пишет
// inlineStr; чтение понимает inlineStr и sharedStrings (для файлов, пересохранённых
// Excel). См. docs/reports/plans/SPEC.md §15.

// ImportRow — строка импорта (editable-колонки).
type ImportRow struct {
	CodeCFO   int
	CodePL    int
	BlockType string
	Year      int
	Month     int
	Amount    float64
	Reason    string
}

// exportColumns — порядок колонок экспорта (имена как ключи импорта).
var exportColumns = []string{"code_cfo", "name_cfo", "code_pl", "block_type", "year", "month", "fact", "tactic", "reason"}

// ExportMpForm выгружает текущий снимок формы (ABAC-фильтрованной) в .xlsx.
func (s *Service) ExportMpForm(ctx context.Context, p Principal, year, month int, segment, currency string) ([]byte, error) {
	form, err := s.MpForm(ctx, p, year, month, segment, currency)
	if err != nil {
		return nil, err
	}
	rows := make([][]string, 0)
	for _, b := range form.Blocks {
		for _, r := range b.Rows {
			tactic := ""
			if r.Tactic != nil {
				tactic = strconv.FormatFloat(*r.Tactic, 'f', -1, 64)
			}
			rows = append(rows, []string{
				strconv.Itoa(r.CodeCFO), r.NameCFO, strconv.Itoa(b.CodePL), b.BlockType,
				strconv.Itoa(year), strconv.Itoa(month),
				strconv.FormatFloat(r.Fact, 'f', -1, 64), tactic, "",
			})
		}
	}
	return writeXlsx("TPL-MP", exportColumns, rows)
}

// parseImport читает .xlsx и валидирует editable-строки. Неизвестный
// code_cfo/code_pl или пустая причина → ошибка строки (TPL-06). ABAC-проверка
// (чужой срез → отказ всего файла) выполняется при SaveMpForm.
func parseImport(data []byte, segment string, defYear, defMonth int) ([]ImportRow, error) {
	header, rows, err := readXlsx(data)
	if err != nil {
		return nil, err
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.TrimSpace(strings.ToLower(h))] = i
	}
	for _, req := range []string{"code_cfo", "code_pl", "reason"} {
		if _, ok := col[req]; !ok {
			return nil, fmt.Errorf("в файле нет колонки %q", req)
		}
	}
	// Колонка значения тактики: amount (импорт-шаблон) или tactic (экспорт-снимок).
	valueCol := "amount"
	if _, ok := col["amount"]; !ok {
		if _, ok := col["tactic"]; ok {
			valueCol = "tactic"
		} else {
			return nil, errors.New("в файле нет колонки amount/tactic")
		}
	}
	platforms := segmentPlatforms(segment)
	editable := map[string]bool{}
	plBlock := map[int]string{}
	for _, b := range mpEditableBlocks() {
		editable[b.BlockType] = true
		plBlock[b.CodePL] = b.BlockType
	}

	get := func(r []string, name string) string {
		if i, ok := col[name]; ok && i < len(r) {
			return strings.TrimSpace(r[i])
		}
		return ""
	}

	out := make([]ImportRow, 0, len(rows))
	for n, r := range rows {
		ln := n + 2 // +1 заголовок, +1 в 1-индексацию
		codeCFO, err := strconv.Atoi(get(r, "code_cfo"))
		if err != nil {
			return nil, fmt.Errorf("строка %d: code_cfo не число", ln)
		}
		if _, ok := platforms[codeCFO]; !ok {
			return nil, fmt.Errorf("строка %d: неизвестный code_cfo %d для сегмента %s", ln, codeCFO, segment)
		}
		codePL, err := strconv.Atoi(get(r, "code_pl"))
		if err != nil {
			return nil, fmt.Errorf("строка %d: code_pl не число", ln)
		}
		blockType := get(r, "block_type")
		if blockType == "" {
			blockType = plBlock[codePL]
		}
		if !editable[blockType] || plBlock[codePL] == "" {
			return nil, fmt.Errorf("строка %d: неизвестный/нередактируемый code_pl %d", ln, codePL)
		}
		reason := get(r, "reason")
		if reason == "" {
			return nil, fmt.Errorf("строка %d: причина обязательна (TPL-06/ADJ-02)", ln)
		}
		amount, err := strconv.ParseFloat(get(r, valueCol), 64)
		if err != nil {
			return nil, fmt.Errorf("строка %d: %s не число", ln, valueCol)
		}
		year, month := defYear, defMonth
		if v := get(r, "year"); v != "" {
			if y, e := strconv.Atoi(v); e == nil {
				year = y
			}
		}
		if v := get(r, "month"); v != "" {
			if m, e := strconv.Atoi(v); e == nil {
				month = m
			}
		}
		out = append(out, ImportRow{
			CodeCFO: codeCFO, CodePL: codePL, BlockType: blockType,
			Year: year, Month: month, Amount: amount, Reason: reason,
		})
	}
	return out, nil
}

// ImportMpForm применяет импортированные строки как тактику (is_manual + причина),
// через SaveMpForm — с ABAC и обязательной причиной. Возвращает pl_id.
func (s *Service) ImportMpForm(ctx context.Context, p Principal, segment string, year, month int, data []byte) (int64, error) {
	imported, err := parseImport(data, segment, year, month)
	if err != nil {
		return 0, err
	}
	if len(imported) == 0 {
		return 0, errors.New("файл не содержит строк")
	}
	req := SaveMpFormRequest{
		TemplateCode: TemplateMP,
		Segment:      segment,
		Period:       PeriodRef{Year: year, Month: month},
		Header:       SaveHeader{Currency: "RUB", Scenario: ScenarioTactic},
	}
	for _, r := range imported {
		req.Rows = append(req.Rows, SaveRow{
			CodeCFO: r.CodeCFO, CodePL: r.CodePL, BlockType: r.BlockType,
			Amount: r.Amount, Comment: r.Reason, IsManual: true,
		})
	}
	return s.SaveMpForm(ctx, p, req, data)
}

// ---- минимальный xlsx-кодек (stdlib) ----

func colName(i int) string {
	s := ""
	for i >= 0 {
		s = string(rune('A'+i%26)) + s
		i = i/26 - 1
	}
	return s
}

var colRe = regexp.MustCompile(`^([A-Z]+)`)

func colIndex(ref string) int {
	m := colRe.FindString(strings.ToUpper(ref))
	n := 0
	for _, c := range m {
		n = n*26 + int(c-'A'+1)
	}
	return n - 1
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// writeXlsx собирает минимальный .xlsx с одним листом (header + rows, inlineStr).
func writeXlsx(sheetName string, header []string, rows [][]string) ([]byte, error) {
	var sheet bytes.Buffer
	sheet.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`)
	sheet.WriteString(`<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`)
	writeRow := func(rn int, cells []string) {
		fmt.Fprintf(&sheet, `<row r="%d">`, rn)
		for i, v := range cells {
			ref := colName(i) + strconv.Itoa(rn)
			if v == "" {
				continue
			}
			if isNumeric(v) {
				fmt.Fprintf(&sheet, `<c r="%s"><v>%s</v></c>`, ref, v)
			} else {
				fmt.Fprintf(&sheet, `<c r="%s" t="inlineStr"><is><t>%s</t></is></c>`, ref, xmlEscape(v))
			}
		}
		sheet.WriteString(`</row>`)
	}
	writeRow(1, header)
	for i, r := range rows {
		writeRow(i+2, r)
	}
	sheet.WriteString(`</sheetData></worksheet>`)

	contentTypes := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>` +
		`<Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/>` +
		`<Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>` +
		`</Types>`
	rels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/>` +
		`</Relationships>`
	workbook := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>`+
		`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">`+
		`<sheets><sheet name="%s" sheetId="1" r:id="rId1"/></sheets></workbook>`, xmlEscape(sheetName))
	wbRels := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">` +
		`<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>` +
		`</Relationships>`

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", contentTypes},
		{"_rels/.rels", rels},
		{"xl/workbook.xml", workbook},
		{"xl/_rels/workbook.xml.rels", wbRels},
		{"xl/worksheets/sheet1.xml", sheet.String()},
	}
	for _, p := range parts {
		f, err := zw.Create(p.name)
		if err != nil {
			return nil, err
		}
		if _, err := f.Write([]byte(p.body)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func isNumeric(s string) bool {
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

type xlsxCell struct {
	R  string `xml:"r,attr"`
	T  string `xml:"t,attr"`
	V  string `xml:"v"`
	Is struct {
		T string `xml:"t"`
	} `xml:"is"`
}
type xlsxRow struct {
	Cells []xlsxCell `xml:"c"`
}
type xlsxSheet struct {
	Rows []xlsxRow `xml:"sheetData>row"`
}
type xlsxSST struct {
	SI []struct {
		T string `xml:"t"`
	} `xml:"si"`
}

// readXlsx читает первый лист: возвращает header (строка 1) и data-строки.
func readXlsx(data []byte) ([]string, [][]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, nil, fmt.Errorf("не .xlsx: %w", err)
	}
	var sheetXML, sstXML []byte
	for _, f := range zr.File {
		switch {
		case f.Name == "xl/worksheets/sheet1.xml":
			sheetXML, err = readZipFile(f)
		case f.Name == "xl/sharedStrings.xml":
			sstXML, _ = readZipFile(f)
		}
		if err != nil {
			return nil, nil, err
		}
	}
	if sheetXML == nil {
		return nil, nil, errors.New("в файле нет xl/worksheets/sheet1.xml")
	}
	var sst xlsxSST
	if sstXML != nil {
		_ = xml.Unmarshal(sstXML, &sst)
	}
	var sheet xlsxSheet
	if err := xml.Unmarshal(sheetXML, &sheet); err != nil {
		return nil, nil, fmt.Errorf("парсинг листа: %w", err)
	}

	grid := make([][]string, 0, len(sheet.Rows))
	for _, row := range sheet.Rows {
		maxCol := -1
		vals := map[int]string{}
		for _, c := range row.Cells {
			idx := colIndex(c.R)
			if idx > maxCol {
				maxCol = idx
			}
			vals[idx] = cellValue(c, sst)
		}
		line := make([]string, maxCol+1)
		for i := 0; i <= maxCol; i++ {
			line[i] = vals[i]
		}
		grid = append(grid, line)
	}
	if len(grid) == 0 {
		return nil, nil, errors.New("пустой лист")
	}
	return grid[0], grid[1:], nil
}

func cellValue(c xlsxCell, sst xlsxSST) string {
	switch c.T {
	case "s":
		if i, err := strconv.Atoi(strings.TrimSpace(c.V)); err == nil && i >= 0 && i < len(sst.SI) {
			return sst.SI[i].T
		}
		return ""
	case "inlineStr":
		return c.Is.T
	default:
		return c.V
	}
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var b bytes.Buffer
	if _, err := b.ReadFrom(rc); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
