package collector

import (
	"math"
	"strings"
)

const tscEnterpriseOID = "1.3.6.1.4.1.43564"

func applyVendorMetrics(result *Result) {
	if isTSC(*result) {
		applyTSCMetrics(result)
	}
	result.ConsumablePercent = lowestConsumablePercent(result.Supplies)
}

func isTSC(result Result) bool {
	oid := strings.TrimPrefix(result.SystemObjectID, ".")
	return strings.HasPrefix(oid, tscEnterpriseOID+".") || oid == tscEnterpriseOID ||
		strings.Contains(strings.ToLower(result.DeviceDescription+" "+result.SystemName), "tsc")
}

func applyTSCMetrics(result *Result) {
	dotsPerMM := tscDotsPerMM(result.Name + " " + result.SystemName + " " + result.DeviceDescription)
	if dotsPerMM > 0 {
		for i := range result.Counters {
			counter := &result.Counters[i]
			if counter.Unit != "dotRow" || counter.Value < 0 {
				continue
			}
			km := float64(counter.Value) / dotsPerMM / 1_000_000
			km = math.Round(km*1_000_000) / 1_000_000
			counter.DistanceKM = &km
			if result.PrintedLengthKM == nil || km > *result.PrintedLengthKM {
				value := km
				result.PrintedLengthKM = &value
			}
		}
	}

	// Some TSC firmware reports ribbon capacity in micrometers, but returns the
	// current level as a whole percentage (0..100). Restrict this workaround to
	// TSC ribbon supplies with a large physical capacity so it cannot affect
	// standards-compliant toner counters.
	for i := range result.Supplies {
		supply := &result.Supplies[i]
		isRibbon := supply.Type == "ribbonWax" || supply.Type == "inkRibbon"
		if !isRibbon || supply.Unit != "micrometers" || supply.Level == nil || supply.MaxCapacity == nil {
			continue
		}
		if *supply.Level < 0 || *supply.Level > 100 || *supply.MaxCapacity < 1_000_000 {
			continue
		}
		percent := float64(*supply.Level)
		supply.RemainingPercent = &percent
		supply.PercentSource = "tscFirmwareLevelAsPercent"
	}
}

func tscDotsPerMM(model string) float64 {
	normalized := strings.ToLower(strings.ReplaceAll(model, "-", ""))
	switch {
	case strings.Contains(normalized, "ttp2410mt"), strings.Contains(normalized, "mh240"):
		return 8 // 203 DPI
	case strings.Contains(normalized, "mh341"):
		return 12 // 300 DPI
	default:
		return 0
	}
}

func lowestConsumablePercent(supplies []Supply) *float64 {
	var min float64
	found := false
	for _, supply := range supplies {
		if supply.RemainingPercent == nil || !isConsumableType(supply.Type) {
			continue
		}
		if !found || *supply.RemainingPercent < min {
			min, found = *supply.RemainingPercent, true
		}
	}
	if !found {
		return nil
	}
	return &min
}

func isConsumableType(value string) bool {
	switch value {
	case "toner", "tonerCartridge", "ink", "inkCartridge", "inkRibbon", "ribbonWax", "solidWax":
		return true
	default:
		return false
	}
}
