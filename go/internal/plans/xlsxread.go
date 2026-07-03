package plans

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Минимальный читатель .xlsx на stdlib (zip + encoding/xml) — без внешних
// зависимостей, парная к writer'у в importexport.go. Возвращает лист как
// [][]string (строки × колонки), раскрывая sharedStrings и inline-строки.
// Пустые ячейки заполняются "" по индексу колонки из ref (A1/C5…).

type xlsxWorkbook struct {
	zr  *zip.Reader
	ss  []string
	rel map[string]string // sheet name → target file
}

func openXLSX(data []byte) (*xlsxWorkbook, error) {
	zr, err := zip.NewReader(strings.NewReader(string(data)), int64(len(data)))
	if err != nil {
		return nil, err
	}
	wb := &xlsxWorkbook{zr: zr, rel: map[string]string{}}
	if err := wb.loadSharedStrings(); err != nil {
		return nil, err
	}
	if err := wb.loadSheetMap(); err != nil {
		return nil, err
	}
	return wb, nil
}

func (wb *xlsxWorkbook) read(name string) ([]byte, bool) {
	for _, f := range wb.zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				return nil, false
			}
			defer rc.Close()
			b, _ := io.ReadAll(rc)
			return b, true
		}
	}
	return nil, false
}

func (wb *xlsxWorkbook) loadSharedStrings() error {
	b, ok := wb.read("xl/sharedStrings.xml")
	if !ok {
		return nil
	}
	type si struct {
		T string   `xml:"t"`
		R []string `xml:"r>t"`
	}
	var doc struct {
		SI []si `xml:"si"`
	}
	if err := xml.Unmarshal(b, &doc); err != nil {
		return err
	}
	for _, s := range doc.SI {
		if len(s.R) > 0 {
			wb.ss = append(wb.ss, strings.Join(s.R, ""))
		} else {
			wb.ss = append(wb.ss, s.T)
		}
	}
	return nil
}

func (wb *xlsxWorkbook) loadSheetMap() error {
	b, ok := wb.read("xl/workbook.xml")
	if !ok {
		return fmt.Errorf("xlsx: нет xl/workbook.xml")
	}
	var doc struct {
		Sheets []struct {
			Name string `xml:"name,attr"`
			RID  string `xml:"http://schemas.openxmlformats.org/officeDocument/2006/relationships id,attr"`
		} `xml:"sheets>sheet"`
	}
	if err := xml.Unmarshal(b, &doc); err != nil {
		return err
	}
	rb, _ := wb.read("xl/_rels/workbook.xml.rels")
	var rels struct {
		Rel []struct {
			ID     string `xml:"Id,attr"`
			Target string `xml:"Target,attr"`
		} `xml:"Relationship"`
	}
	_ = xml.Unmarshal(rb, &rels)
	relmap := map[string]string{}
	for _, r := range rels.Rel {
		relmap[r.ID] = r.Target
	}
	for _, s := range doc.Sheets {
		tgt := relmap[s.RID]
		tgt = strings.TrimPrefix(tgt, "/xl/")
		tgt = strings.TrimPrefix(tgt, "xl/")
		wb.rel[s.Name] = "xl/" + tgt
	}
	return nil
}

// colIndex/colRe определены в importexport.go (тот же пакет) — переиспользуем.

// Sheet читает лист по имени в [][]string.
func (wb *xlsxWorkbook) Sheet(name string) ([][]string, error) {
	file, ok := wb.rel[name]
	if !ok {
		return nil, fmt.Errorf("xlsx: лист %q не найден", name)
	}
	b, ok := wb.read(file)
	if !ok {
		return nil, fmt.Errorf("xlsx: не прочитать %s", file)
	}
	type cell struct {
		R  string `xml:"r,attr"`
		T  string `xml:"t,attr"`
		V  string `xml:"v"`
		IS struct {
			T string   `xml:"t"`
			R []string `xml:"r>t"`
		} `xml:"is"`
	}
	type row struct {
		Cells []cell `xml:"c"`
	}
	var doc struct {
		Rows []row `xml:"sheetData>row"`
	}
	if err := xml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	out := make([][]string, 0, len(doc.Rows))
	for _, r := range doc.Rows {
		cells := map[int]string{}
		maxCol := -1
		for _, c := range r.Cells {
			val := c.V
			switch c.T {
			case "s":
				if i, err := strconv.Atoi(c.V); err == nil && i >= 0 && i < len(wb.ss) {
					val = wb.ss[i]
				}
			case "inlineStr":
				if len(c.IS.R) > 0 {
					val = strings.Join(c.IS.R, "")
				} else {
					val = c.IS.T
				}
			}
			ci := colIndex(c.R)
			cells[ci] = strings.TrimSpace(val)
			if ci > maxCol {
				maxCol = ci
			}
		}
		line := make([]string, maxCol+1)
		for i := 0; i <= maxCol; i++ {
			line[i] = cells[i]
		}
		out = append(out, line)
	}
	return out, nil
}
