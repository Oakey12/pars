package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"

	"printerstats/internal/collector"
)

func Write(w io.Writer, format string, results []collector.Result) error {
	switch format {
	case "json":
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ")
		return encoder.Encode(results)
	case "csv":
		return writeCSV(w, results)
	case "table":
		return writeTable(w, results)
	default:
		return fmt.Errorf("unknown format %q; use table, json, or csv", format)
	}
}

func writeCSV(w io.Writer, results []collector.Result) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"collected_at", "name", "address", "location", "inventory_number", "serial_number", "status", "detected_as_printer", "detection_method", "snmp_version", "tried_versions", "system_name", "system_object_id", "total_pages", "printed_length_km", "toner_percent", "consumable_percent", "supplies", "error"}); err != nil {
		return err
	}
	for _, result := range results {
		if err := cw.Write([]string{
			result.CollectedAt.Format("2006-01-02T15:04:05Z07:00"), result.Name, result.Address, result.Location,
			result.InventoryNumber, result.SerialNumber, result.Status, strconv.FormatBool(result.DetectedAsPrinter), result.DetectionMethod, result.SNMPVersion, strings.Join(result.TriedVersions, ","),
			result.SystemName, result.SystemObjectID, intText(result.TotalPages), floatText(result.PrintedLengthKM), percentText(result.TonerPercent), percentText(result.ConsumablePercent), supplySummary(result.Supplies), result.Error,
		}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func writeTable(w io.Writer, results []collector.Result) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "STATUS\tADDRESS\tPRINTER\tSNMP\tNAME\tPAGES\tLENGTH_KM\tSUPPLY%\tLOCATION"); err != nil {
		return err
	}
	for _, result := range results {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", result.Status, result.Address, detectionText(result), attemptedVersions(result), result.Name, intText(result.TotalPages), floatText(result.PrintedLengthKM), percentText(result.ConsumablePercent), result.Location); err != nil {
			return err
		}
		if result.Error != "" {
			if _, err := fmt.Fprintf(tw, "\t\tERROR: %s\n", result.Error); err != nil {
				return err
			}
		}
	}
	return tw.Flush()
}

func attemptedVersions(result collector.Result) string {
	if len(result.TriedVersions) > 0 {
		return strings.Join(result.TriedVersions, ",")
	}
	return result.SNMPVersion
}

func detectionText(result collector.Result) string {
	if result.DetectedAsPrinter {
		return "yes"
	}
	if result.Status == "not_printer" {
		return "no"
	}
	return "unknown"
}

func intText(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}

func percentText(value *float64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(*value, 'f', 1, 64)
}

func floatText(value *float64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(*value, 'f', 3, 64)
}

func supplySummary(supplies []collector.Supply) string {
	parts := make([]string, 0, len(supplies))
	for _, supply := range supplies {
		if supply.Type != "toner" && supply.Type != "tonerCartridge" {
			continue
		}
		value := supply.LevelState
		if supply.RemainingPercent != nil {
			value = percentText(supply.RemainingPercent) + "%"
		}
		parts = append(parts, strings.TrimSpace(supply.Description+" "+value))
	}
	return strings.Join(parts, "; ")
}
