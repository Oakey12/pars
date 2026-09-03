package collector

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/gosnmp/gosnmp"
)

const (
	OIDSysDescr          = ".1.3.6.1.2.1.1.1.0"
	OIDSysObjectID       = ".1.3.6.1.2.1.1.2.0"
	OIDSysName           = ".1.3.6.1.2.1.1.5.0"
	OIDSysLocation       = ".1.3.6.1.2.1.1.6.0"
	OIDHRDeviceType      = ".1.3.6.1.2.1.25.3.2.1.2"
	OIDHRDevicePrinter   = ".1.3.6.1.2.1.25.3.1.5"
	OIDMarkerEntry       = ".1.3.6.1.2.1.43.10.2.1"
	OIDMarkerCounterUnit = ".1.3.6.1.2.1.43.10.2.1.3"
	OIDMarkerLifeCount   = ".1.3.6.1.2.1.43.10.2.1.4"
	OIDSupplyEntry       = ".1.3.6.1.2.1.43.11.1.1"
	OIDSupplyClass       = ".1.3.6.1.2.1.43.11.1.1.4"
	OIDSupplyType        = ".1.3.6.1.2.1.43.11.1.1.5"
	OIDSupplyDescription = ".1.3.6.1.2.1.43.11.1.1.6"
	OIDSupplyUnit        = ".1.3.6.1.2.1.43.11.1.1.7"
	OIDSupplyMaxCapacity = ".1.3.6.1.2.1.43.11.1.1.8"
	OIDSupplyLevel       = ".1.3.6.1.2.1.43.11.1.1.9"
)

var printerDescriptionKeywords = []string{
	"printer", "print server", "mfp", "laserjet", "deskjet", "officejet",
	"kyocera", "ecosys", "taskalfa", "canon", "samsung", "xerox", "brother",
	"ricoh", "epson", "lexmark", "pantum", "oki", "konica", "bizhub",
	"zebra", "tsc", "godex",
}

func looksLikePrinter(values ...string) bool {
	text := strings.ToLower(strings.Join(values, " "))
	for _, keyword := range printerDescriptionKeywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func hasPrinterDeviceType(pdus []gosnmp.SnmpPDU) bool {
	want := strings.TrimPrefix(OIDHRDevicePrinter, ".")
	for _, pdu := range pdus {
		got := strings.TrimPrefix(sanitizeText(pduString(pdu)), ".")
		if got == want {
			return true
		}
	}
	return false
}

func pduByOID(pdus []gosnmp.SnmpPDU, oid string) (gosnmp.SnmpPDU, bool) {
	want := strings.TrimPrefix(oid, ".")
	for _, pdu := range pdus {
		if strings.TrimPrefix(pdu.Name, ".") != want || isExceptionPDU(pdu) {
			continue
		}
		return pdu, true
	}
	return gosnmp.SnmpPDU{}, false
}

func isExceptionPDU(pdu gosnmp.SnmpPDU) bool {
	return pdu.Type == gosnmp.NoSuchObject || pdu.Type == gosnmp.NoSuchInstance || pdu.Type == gosnmp.EndOfMibView
}

func pdusForColumn(pdus []gosnmp.SnmpPDU, oid string) []gosnmp.SnmpPDU {
	prefix := strings.TrimPrefix(oid, ".") + "."
	selected := make([]gosnmp.SnmpPDU, 0)
	for _, pdu := range pdus {
		name := strings.TrimPrefix(pdu.Name, ".")
		if strings.HasPrefix(name, prefix) {
			selected = append(selected, pdu)
		}
	}
	return selected
}

type rawSupply struct {
	class       int64
	typeID      int64
	description string
	unit        int64
	max         int64
	level       int64
	hasMax      bool
	hasLevel    bool
}

func parseCounterPDUs(units, counts []gosnmp.SnmpPDU) []Counter {
	unitByIndex := make(map[string]int64, len(units))
	for _, pdu := range units {
		unitByIndex[oidIndex(pdu.Name, OIDMarkerCounterUnit)] = pduInt(pdu)
	}
	counters := make([]Counter, 0, len(counts))
	for _, pdu := range counts {
		idx := oidIndex(pdu.Name, OIDMarkerLifeCount)
		counters = append(counters, Counter{Index: idx, Unit: counterUnitName(unitByIndex[idx]), Value: pduInt(pdu)})
	}
	sort.Slice(counters, func(i, j int) bool { return counters[i].Index < counters[j].Index })
	return counters
}

func parseSupplyPDUs(columns map[string][]gosnmp.SnmpPDU) []Supply {
	raw := make(map[string]*rawSupply)
	get := func(idx string) *rawSupply {
		if raw[idx] == nil {
			raw[idx] = &rawSupply{}
		}
		return raw[idx]
	}
	for oid, pdus := range columns {
		for _, pdu := range pdus {
			idx := oidIndex(pdu.Name, oid)
			r := get(idx)
			switch oid {
			case OIDSupplyClass:
				r.class = pduInt(pdu)
			case OIDSupplyType:
				r.typeID = pduInt(pdu)
			case OIDSupplyDescription:
				r.description = pduString(pdu)
			case OIDSupplyUnit:
				r.unit = pduInt(pdu)
			case OIDSupplyMaxCapacity:
				r.max, r.hasMax = pduInt(pdu), true
			case OIDSupplyLevel:
				r.level, r.hasLevel = pduInt(pdu), true
			}
		}
	}

	indexes := make([]string, 0, len(raw))
	for idx := range raw {
		indexes = append(indexes, idx)
	}
	sort.Strings(indexes)

	result := make([]Supply, 0, len(indexes))
	for _, idx := range indexes {
		r := raw[idx]
		s := Supply{Index: idx, Description: sanitizeText(r.description), Class: supplyClassName(r.class), Type: supplyTypeName(r.typeID), Unit: supplyUnitName(r.unit)}
		if r.hasMax && r.max >= 0 {
			value := r.max
			s.MaxCapacity = &value
		}
		if r.hasLevel && r.level >= 0 {
			value := r.level
			s.Level = &value
		}
		s.LevelState = specialLevelState(r.level, r.hasLevel)
		if r.hasLevel && r.level >= 0 {
			var percent float64
			valid := false
			switch {
			case r.unit == 19:
				percent, valid = float64(r.level), true
			case r.hasMax && r.max > 0:
				percent, valid = 100*float64(r.level)/float64(r.max), true
			}
			if valid {
				percent = math.Max(0, math.Min(100, percent))
				percent = math.Round(percent*10) / 10
				s.RemainingPercent = &percent
				if r.unit == 19 {
					s.PercentSource = "reportedPercent"
				} else {
					s.PercentSource = "level/maxCapacity"
				}
			}
		}
		result = append(result, s)
	}
	return result
}

func primaryPageCount(counters []Counter) *int64 {
	var max int64
	found := false
	for _, counter := range counters {
		if counter.Value < 0 || (counter.Unit != "impressions" && counter.Unit != "sheets") {
			continue
		}
		if !found || counter.Value > max {
			max, found = counter.Value, true
		}
	}
	if !found {
		return nil
	}
	return &max
}

func lowestTonerPercent(supplies []Supply) *float64 {
	var min float64
	found := false
	for _, supply := range supplies {
		if supply.RemainingPercent == nil || (supply.Type != "toner" && supply.Type != "tonerCartridge") {
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

func oidIndex(name, base string) string {
	name = strings.TrimPrefix(name, ".")
	base = strings.TrimPrefix(base, ".")
	suffix := strings.TrimPrefix(name, base)
	return strings.TrimPrefix(suffix, ".")
}

func pduInt(pdu gosnmp.SnmpPDU) int64 {
	if value := gosnmp.ToBigInt(pdu.Value); value != nil {
		return value.Int64()
	}
	return 0
}

func pduString(pdu gosnmp.SnmpPDU) string {
	switch value := pdu.Value.(type) {
	case []byte:
		return string(value)
	case string:
		return value
	default:
		return fmt.Sprint(value)
	}
}

func sanitizeText(value string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if r == '\x00' {
			return -1
		}
		return r
	}, value))
}

func specialLevelState(level int64, present bool) string {
	if !present {
		return "notReported"
	}
	switch level {
	case -1:
		return "other"
	case -2:
		return "unknown"
	case -3:
		return "someRemaining"
	default:
		if level >= 0 {
			return "value"
		}
		return "vendorSpecific(" + strconv.FormatInt(level, 10) + ")"
	}
}

func counterUnitName(v int64) string {
	return enumName(v, map[int64]string{3: "tenThousandthsOfInches", 4: "micrometers", 5: "characters", 6: "lines", 7: "impressions", 8: "sheets", 9: "dotRow", 11: "hours", 16: "feet", 17: "meters"})
}

func supplyClassName(v int64) string {
	return enumName(v, map[int64]string{1: "other", 3: "consumed", 4: "receptacle"})
}

func supplyTypeName(v int64) string {
	return enumName(v, map[int64]string{1: "other", 2: "unknown", 3: "toner", 4: "wasteToner", 5: "ink", 6: "inkCartridge", 7: "inkRibbon", 8: "wasteInk", 9: "opc", 10: "developer", 11: "fuserOil", 12: "solidWax", 13: "ribbonWax", 14: "wasteWax", 15: "fuser", 16: "coronaWire", 17: "fuserOilWick", 18: "cleanerUnit", 19: "fuserCleaningPad", 20: "transferUnit", 21: "tonerCartridge", 22: "fuserOiler", 26: "wastePaper", 32: "staples"})
}

func supplyUnitName(v int64) string {
	return enumName(v, map[int64]string{1: "other", 2: "unknown", 3: "tenThousandthsOfInches", 4: "micrometers", 7: "impressions", 8: "sheets", 11: "hours", 12: "thousandthsOfOunces", 13: "tenthsOfGrams", 14: "hundredthsOfFluidOunces", 15: "tenthsOfMilliliters", 16: "feet", 17: "meters", 18: "items", 19: "percent"})
}

func enumName(v int64, names map[int64]string) string {
	if name, ok := names[v]; ok {
		return name
	}
	return "value(" + strconv.FormatInt(v, 10) + ")"
}
