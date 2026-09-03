package collector

import (
	"strings"

	"printerstats/internal/config"
)

type Hybrid struct {
	SNMP SNMP
	Web  Web
}

func (h Hybrid) Collect(printer config.Printer) Result {
	switch printer.Protocol {
	case "http":
		return h.Web.Collect(printer)
	case "snmp":
		return h.SNMP.Collect(printer)
	}

	snmpResult := h.SNMP.Collect(printer)
	if snmpResult.TotalPages != nil && snmpResult.ConsumablePercent != nil {
		return snmpResult
	}
	webResult := h.Web.Collect(printer)
	return mergeResults(snmpResult, webResult)
}

func mergeResults(snmpResult, webResult Result) Result {
	result := snmpResult
	if result.CollectedAt.IsZero() || (!webResult.CollectedAt.IsZero() && webResult.CollectedAt.Before(result.CollectedAt)) {
		result.CollectedAt = webResult.CollectedAt
	}
	if webResult.HTTPURL != "" {
		result.HTTPURL = webResult.HTTPURL
	}
	if webResult.PageMetrics != nil {
		result.PageMetrics = webResult.PageMetrics
	}
	if result.TotalPages == nil && webResult.TotalPages != nil {
		result.TotalPages = webResult.TotalPages
	}
	if result.TonerPercent == nil && webResult.TonerPercent != nil {
		result.TonerPercent = webResult.TonerPercent
	}
	if result.ConsumablePercent == nil && webResult.ConsumablePercent != nil {
		result.ConsumablePercent = webResult.ConsumablePercent
	}
	for _, supply := range webResult.Supplies {
		if !hasEquivalentSupply(result.Supplies, supply) {
			result.Supplies = append(result.Supplies, supply)
		}
	}
	result.MetricSources = uniqueStrings(append(result.MetricSources, webResult.MetricSources...))

	webHasMetrics := webResult.TotalPages != nil || webResult.ConsumablePercent != nil
	if webHasMetrics {
		result.DetectedAsPrinter = true
		if result.DetectionMethod == "" || strings.HasPrefix(result.DetectionMethod, "SNMP") {
			result.DetectionMethod = "printer web interface"
		}
		if snmpResult.Error != "" {
			result.Warnings = append(result.Warnings, "SNMP: "+snmpResult.Error)
		}
		result.Warnings = append(result.Warnings, webResult.Warnings...)
		result.Error = ""
		switch {
		case result.TotalPages != nil && result.ConsumablePercent != nil:
			result.Status = "ok"
		default:
			result.Status = "partial"
		}
		return result
	}

	if webResult.Error != "" {
		if !hasUsefulMetrics(snmpResult) {
			result.Warnings = append(result.Warnings, "HTTP: "+webResult.Error)
		}
	}
	if !hasUsefulMetrics(snmpResult) {
		result.Warnings = append(result.Warnings, webResult.Warnings...)
	}
	return result
}

func hasUsefulMetrics(result Result) bool {
	return result.TotalPages != nil || result.PrintedLengthKM != nil || result.ConsumablePercent != nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}
