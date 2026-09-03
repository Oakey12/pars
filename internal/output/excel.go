package output

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"

	"printerstats/internal/collector"
)

const excelHistorySheet = "История"

var excelHeaders = []string{
	"Дата и время",
	"Статус",
	"IP-адрес",
	"Название",
	"Описание устройства",
	"Расположение",
	"Инвентарный номер",
	"Серийный номер",
	"Принтер определён",
	"Источник",
	"SNMP",
	"Всего напечатано",
	"Печать",
	"Копирование",
	"Факс",
	"Всего отсканировано",
	"Сканирование: копии",
	"Сканирование: другое",
	"Длина печати, км",
	"Остаток, %",
	"Метод определения",
	"HTTP-страница",
	"Ошибка / предупреждения",
}

// AppendExcel adds one history row per result. Existing rows are never
// replaced; a missing workbook or sheet is initialized automatically.
func AppendExcel(path string, results []collector.Result) (int, error) {
	if strings.TrimSpace(path) == "" {
		return 0, errors.New("Excel path is empty")
	}
	if !strings.EqualFold(filepath.Ext(path), ".xlsx") {
		return 0, fmt.Errorf("Excel file must have .xlsx extension: %s", path)
	}
	if len(results) == 0 {
		return 0, nil
	}
	if directory := filepath.Dir(path); directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return 0, fmt.Errorf("create Excel directory: %w", err)
		}
	}

	book, created, err := openExcelHistory(path)
	if err != nil {
		return 0, err
	}
	defer book.Close()

	if err := ensureExcelHistorySheet(book); err != nil {
		return 0, err
	}
	rows, err := book.GetRows(excelHistorySheet)
	if err != nil {
		return 0, fmt.Errorf("read Excel history: %w", err)
	}
	if err := validateExcelHeaders(rows); err != nil {
		return 0, err
	}
	startRow := len(rows) + 1
	if startRow < 2 {
		startRow = 2
	}
	styles, err := createExcelStyles(book)
	if err != nil {
		return 0, err
	}

	for index, result := range results {
		rowNumber := startRow + index
		if err := writeExcelResult(book, rowNumber, result, styles); err != nil {
			return 0, err
		}
	}
	lastRow := startRow + len(results) - 1
	if err := formatExcelHistory(book, lastRow, styles); err != nil {
		return 0, err
	}
	if err := book.SaveAs(path); err != nil {
		if created {
			return 0, fmt.Errorf("create Excel history: %w", err)
		}
		return 0, fmt.Errorf("append Excel history (close the file in Excel and retry): %w", err)
	}
	return len(results), nil
}

func openExcelHistory(path string) (*excelize.File, bool, error) {
	if _, err := os.Stat(path); err == nil {
		book, openErr := excelize.OpenFile(path)
		if openErr != nil {
			return nil, false, fmt.Errorf("open Excel history: %w", openErr)
		}
		return book, false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, false, fmt.Errorf("inspect Excel history: %w", err)
	}
	return excelize.NewFile(), true, nil
}

func ensureExcelHistorySheet(book *excelize.File) error {
	if index, err := book.GetSheetIndex(excelHistorySheet); err == nil && index >= 0 {
		return nil
	}
	sheets := book.GetSheetList()
	if len(sheets) == 1 && sheets[0] == "Sheet1" {
		if err := book.SetSheetName("Sheet1", excelHistorySheet); err != nil {
			return fmt.Errorf("rename Excel sheet: %w", err)
		}
	} else if _, err := book.NewSheet(excelHistorySheet); err != nil {
		return fmt.Errorf("create Excel history sheet: %w", err)
	}
	row := make([]interface{}, len(excelHeaders))
	for index, header := range excelHeaders {
		row[index] = header
	}
	if err := book.SetSheetRow(excelHistorySheet, "A1", &row); err != nil {
		return fmt.Errorf("write Excel headers: %w", err)
	}
	return nil
}

func validateExcelHeaders(rows [][]string) error {
	if len(rows) == 0 {
		return nil
	}
	if len(rows[0]) < len(excelHeaders) {
		return errors.New("Excel history has an incompatible header; use another -excel file name")
	}
	for index, expected := range excelHeaders {
		if rows[0][index] != expected {
			return fmt.Errorf("Excel column %d is %q, expected %q; use another -excel file name", index+1, rows[0][index], expected)
		}
	}
	return nil
}

type excelStyles struct {
	header  int
	row     int
	date    int
	integer int
	decimal int
	percent int
	ok      int
	partial int
	failure int
}

func createExcelStyles(book *excelize.File) (excelStyles, error) {
	border := []excelize.Border{{Type: "bottom", Color: "D9E2F3", Style: 1}}
	header, err := book.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF", Family: "Aptos", Size: 10},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"1F4E78"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center", WrapText: true},
		Border:    []excelize.Border{{Type: "bottom", Color: "17365D", Style: 2}},
	})
	if err != nil {
		return excelStyles{}, fmt.Errorf("create Excel header style: %w", err)
	}
	row, err := book.NewStyle(&excelize.Style{Font: &excelize.Font{Family: "Aptos", Size: 10}, Alignment: &excelize.Alignment{Vertical: "center"}, Border: border})
	if err != nil {
		return excelStyles{}, err
	}
	dateFormat := "yyyy-mm-dd hh:mm:ss"
	date, err := book.NewStyle(&excelize.Style{CustomNumFmt: &dateFormat, Alignment: &excelize.Alignment{Vertical: "center"}, Border: border})
	if err != nil {
		return excelStyles{}, err
	}
	integerFormat := "#,##0"
	integer, err := book.NewStyle(&excelize.Style{CustomNumFmt: &integerFormat, Alignment: &excelize.Alignment{Horizontal: "right", Vertical: "center"}, Border: border})
	if err != nil {
		return excelStyles{}, err
	}
	decimalFormat := "#,##0.000"
	decimal, err := book.NewStyle(&excelize.Style{CustomNumFmt: &decimalFormat, Alignment: &excelize.Alignment{Horizontal: "right", Vertical: "center"}, Border: border})
	if err != nil {
		return excelStyles{}, err
	}
	percentFormat := "0.0%"
	percent, err := book.NewStyle(&excelize.Style{CustomNumFmt: &percentFormat, Alignment: &excelize.Alignment{Horizontal: "right", Vertical: "center"}, Border: border})
	if err != nil {
		return excelStyles{}, err
	}
	statusStyle := func(fill, font string) (int, error) {
		return book.NewStyle(&excelize.Style{
			Font:      &excelize.Font{Bold: true, Color: font, Family: "Aptos", Size: 10},
			Fill:      excelize.Fill{Type: "pattern", Color: []string{fill}, Pattern: 1},
			Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
			Border:    border,
		})
	}
	ok, err := statusStyle("E2F0D9", "375623")
	if err != nil {
		return excelStyles{}, err
	}
	partial, err := statusStyle("FFF2CC", "7F6000")
	if err != nil {
		return excelStyles{}, err
	}
	failure, err := statusStyle("FCE4D6", "9C0006")
	if err != nil {
		return excelStyles{}, err
	}
	return excelStyles{header: header, row: row, date: date, integer: integer, decimal: decimal, percent: percent, ok: ok, partial: partial, failure: failure}, nil
}

func writeExcelResult(book *excelize.File, rowNumber int, result collector.Result, styles excelStyles) error {
	row := []interface{}{
		result.CollectedAt.Local(),
		result.Status,
		result.Address,
		result.Name,
		result.DeviceDescription,
		result.Location,
		result.InventoryNumber,
		result.SerialNumber,
		yesNo(result.DetectedAsPrinter),
		strings.Join(result.MetricSources, "+"),
		attemptedVersions(result),
		pointerValue(result.TotalPages),
		pageMetricValue(result.PageMetrics, func(metrics *collector.PageMetrics) *int64 { return metrics.PrintedPrinter }),
		pageMetricValue(result.PageMetrics, func(metrics *collector.PageMetrics) *int64 { return metrics.PrintedCopy }),
		pageMetricValue(result.PageMetrics, func(metrics *collector.PageMetrics) *int64 { return metrics.PrintedFax }),
		pageMetricValue(result.PageMetrics, func(metrics *collector.PageMetrics) *int64 { return metrics.ScannedTotal }),
		pageMetricValue(result.PageMetrics, func(metrics *collector.PageMetrics) *int64 { return metrics.ScannedCopy }),
		pageMetricValue(result.PageMetrics, func(metrics *collector.PageMetrics) *int64 { return metrics.ScannedOther }),
		floatPointerValue(result.PrintedLengthKM),
		percentValue(result.ConsumablePercent),
		result.DetectionMethod,
		result.HTTPURL,
		resultDetails(result),
	}
	cell, _ := excelize.CoordinatesToCellName(1, rowNumber)
	if err := book.SetSheetRow(excelHistorySheet, cell, &row); err != nil {
		return fmt.Errorf("write Excel row %d: %w", rowNumber, err)
	}
	if err := book.SetCellStyle(excelHistorySheet, cell, fmt.Sprintf("W%d", rowNumber), styles.row); err != nil {
		return err
	}
	if err := book.SetCellStyle(excelHistorySheet, fmt.Sprintf("A%d", rowNumber), fmt.Sprintf("A%d", rowNumber), styles.date); err != nil {
		return err
	}
	if err := book.SetCellStyle(excelHistorySheet, fmt.Sprintf("L%d", rowNumber), fmt.Sprintf("R%d", rowNumber), styles.integer); err != nil {
		return err
	}
	if err := book.SetCellStyle(excelHistorySheet, fmt.Sprintf("S%d", rowNumber), fmt.Sprintf("S%d", rowNumber), styles.decimal); err != nil {
		return err
	}
	if err := book.SetCellStyle(excelHistorySheet, fmt.Sprintf("T%d", rowNumber), fmt.Sprintf("T%d", rowNumber), styles.percent); err != nil {
		return err
	}
	statusStyle := styles.failure
	if result.Status == "ok" {
		statusStyle = styles.ok
	} else if result.Status == "partial" {
		statusStyle = styles.partial
	}
	if err := book.SetCellStyle(excelHistorySheet, fmt.Sprintf("B%d", rowNumber), fmt.Sprintf("B%d", rowNumber), statusStyle); err != nil {
		return err
	}
	return book.SetRowHeight(excelHistorySheet, rowNumber, 21)
}

func formatExcelHistory(book *excelize.File, lastRow int, styles excelStyles) error {
	if err := book.SetCellStyle(excelHistorySheet, "A1", "W1", styles.header); err != nil {
		return err
	}
	if err := book.SetRowHeight(excelHistorySheet, 1, 34); err != nil {
		return err
	}
	widths := map[string]float64{
		"A": 20, "B": 12, "C": 16, "D": 34, "E": 36, "F": 24, "G": 18, "H": 20,
		"I": 17, "J": 12, "K": 10, "L": 18, "M": 15, "N": 15, "O": 12, "P": 20,
		"Q": 22, "R": 22, "S": 18, "T": 14, "U": 28, "V": 45, "W": 55,
	}
	for column, width := range widths {
		if err := book.SetColWidth(excelHistorySheet, column, column, width); err != nil {
			return err
		}
	}
	if err := book.SetPanes(excelHistorySheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return err
	}
	if err := book.AutoFilter(excelHistorySheet, fmt.Sprintf("A1:W%d", max(lastRow, 1)), []excelize.AutoFilterOptions{}); err != nil {
		return err
	}
	book.SetActiveSheet(mustSheetIndex(book, excelHistorySheet))
	return nil
}

func mustSheetIndex(book *excelize.File, sheet string) int {
	index, _ := book.GetSheetIndex(sheet)
	return index
}

func pointerValue(value *int64) interface{} {
	if value == nil {
		return nil
	}
	return *value
}

func floatPointerValue(value *float64) interface{} {
	if value == nil {
		return nil
	}
	return *value
}

func percentValue(value *float64) interface{} {
	if value == nil {
		return nil
	}
	return *value / 100
}

func pageMetricValue(metrics *collector.PageMetrics, selectValue func(*collector.PageMetrics) *int64) interface{} {
	if metrics == nil {
		return nil
	}
	return pointerValue(selectValue(metrics))
}

func resultDetails(result collector.Result) string {
	parts := append([]string(nil), result.Warnings...)
	if result.Error != "" {
		parts = append(parts, result.Error)
	}
	return strings.Join(parts, " | ")
}

func yesNo(value bool) string {
	if value {
		return "Да"
	}
	return "Нет"
}
