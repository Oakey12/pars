package output

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"

	"printerstats/internal/collector"
)

func TestAppendExcelPreservesHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.xlsx")
	firstPages := int64(106297)
	firstScanned := int64(123468)
	firstToner := 6.0
	first := collector.Result{
		CollectedAt:       time.Date(2026, 9, 3, 18, 12, 0, 0, time.UTC),
		Status:            "ok",
		Address:           "192.168.50.31",
		Name:              "Kyocera P2335dn",
		DetectedAsPrinter: true,
		MetricSources:     []string{"http"},
		TotalPages:        &firstPages,
		PageMetrics:       &collector.PageMetrics{ScannedTotal: &firstScanned},
		ConsumablePercent: &firstToner,
	}
	if rows, err := AppendExcel(path, []collector.Result{first}); err != nil || rows != 1 {
		t.Fatalf("first append: rows=%d err=%v", rows, err)
	}
	secondPages := int64(106350)
	second := first
	second.TotalPages = &secondPages
	second.CollectedAt = second.CollectedAt.Add(time.Hour)
	if rows, err := AppendExcel(path, []collector.Result{second}); err != nil || rows != 1 {
		t.Fatalf("second append: rows=%d err=%v", rows, err)
	}

	book, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	rows, err := book.GetRows(excelHistorySheet)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("row count = %d, want header + 2 history rows", len(rows))
	}
	firstStored := strings.ReplaceAll(rows[1][4], ",", "")
	secondStored := strings.ReplaceAll(rows[2][4], ",", "")
	scannedStored := strings.ReplaceAll(rows[1][5], ",", "")
	if rows[1][2] != "192.168.50.31" || firstStored != "106297" || secondStored != "106350" || scannedStored != "123468" {
		t.Fatalf("history was not preserved: %#v", rows)
	}
}

func TestAppendExcelMigratesOldHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old-history.xlsx")
	book := excelize.NewFile()
	if err := book.SetSheetName("Sheet1", excelHistorySheet); err != nil {
		t.Fatal(err)
	}
	legacyHeaders := []interface{}{
		"Дата и время", "Статус", "IP-адрес", "Название", "Описание устройства",
		"Расположение", "Инвентарный номер", "Серийный номер", "Принтер определён",
		"Источник", "SNMP", "Всего напечатано", "Печать", "Копирование", "Факс",
		"Всего отсканировано", "Сканирование: копии", "Сканирование: другое",
		"Длина печати, км", "Остаток, %", "Метод определения", "HTTP-страница",
		"Ошибка / предупреждения",
	}
	if err := book.SetSheetRow(excelHistorySheet, "A1", &legacyHeaders); err != nil {
		t.Fatal(err)
	}
	legacyRow := make([]interface{}, len(legacyHeaders))
	legacyRow[0], legacyRow[1], legacyRow[2], legacyRow[3] = "2026-09-03 18:12:00", "ok", "192.168.50.31", "Kyocera P2335dn"
	legacyRow[11], legacyRow[15] = 106297, 123468
	if err := book.SetSheetRow(excelHistorySheet, "A2", &legacyRow); err != nil {
		t.Fatal(err)
	}
	if err := book.SaveAs(path); err != nil {
		t.Fatal(err)
	}
	_ = book.Close()

	pages := int64(106350)
	if _, err := AppendExcel(path, []collector.Result{{
		CollectedAt: time.Date(2026, 9, 3, 19, 12, 0, 0, time.UTC),
		Status:      "ok", Address: "192.168.50.31", Name: "Kyocera P2335dn", TotalPages: &pages,
	}}); err != nil {
		t.Fatal(err)
	}
	book, err := excelize.OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	rows, err := book.GetRows(excelHistorySheet)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || len(rows[0]) != 6 || strings.ReplaceAll(rows[1][4], ",", "") != "106297" || strings.ReplaceAll(rows[1][5], ",", "") != "123468" {
		t.Fatalf("legacy history was not migrated: %#v", rows)
	}
}
