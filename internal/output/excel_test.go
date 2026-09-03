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
	firstToner := 6.0
	first := collector.Result{
		CollectedAt:       time.Date(2026, 9, 3, 18, 12, 0, 0, time.UTC),
		Status:            "ok",
		Address:           "192.168.50.31",
		Name:              "Kyocera P2335dn",
		DetectedAsPrinter: true,
		MetricSources:     []string{"http"},
		TotalPages:        &firstPages,
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
	firstStored := strings.ReplaceAll(rows[1][11], ",", "")
	secondStored := strings.ReplaceAll(rows[2][11], ",", "")
	if rows[1][2] != "192.168.50.31" || firstStored != "106297" || secondStored != "106350" {
		t.Fatalf("history was not preserved: %#v", rows)
	}
}
