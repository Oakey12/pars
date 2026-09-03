package collector

import (
	"testing"

	"github.com/gosnmp/gosnmp"
)

func integerPDU(name string, value int) gosnmp.SnmpPDU {
	return gosnmp.SnmpPDU{Name: name, Type: gosnmp.Integer, Value: value}
}

func TestParseCounterAndPrimaryPageCount(t *testing.T) {
	units := []gosnmp.SnmpPDU{integerPDU(OIDMarkerCounterUnit+".1.1", 7)}
	counts := []gosnmp.SnmpPDU{integerPDU(OIDMarkerLifeCount+".1.1", 12345)}
	counters := parseCounterPDUs(units, counts)
	if len(counters) != 1 || counters[0].Unit != "impressions" || counters[0].Value != 12345 {
		t.Fatalf("unexpected counters: %+v", counters)
	}
	if total := primaryPageCount(counters); total == nil || *total != 12345 {
		t.Fatalf("unexpected total: %v", total)
	}
}

func TestPDUsForColumnDoesNotMixOIDPrefixes(t *testing.T) {
	pdus := []gosnmp.SnmpPDU{
		integerPDU(OIDMarkerCounterUnit+".1.1", 7),
		integerPDU(OIDMarkerLifeCount+".1.1", 123),
	}
	selected := pdusForColumn(pdus, OIDMarkerCounterUnit)
	if len(selected) != 1 || selected[0].Name != OIDMarkerCounterUnit+".1.1" {
		t.Fatalf("unexpected selection: %+v", selected)
	}
}

func TestPrinterDetection(t *testing.T) {
	if !looksLikePrinter("KYOCERA ECOSYS M2040dn") {
		t.Fatal("expected known printer description to be detected")
	}
	if looksLikePrinter("Windows workstation") {
		t.Fatal("workstation must not be detected as a printer")
	}
	pdus := []gosnmp.SnmpPDU{{Name: OIDHRDeviceType + ".7", Type: gosnmp.ObjectIdentifier, Value: OIDHRDevicePrinter}}
	if !hasPrinterDeviceType(pdus) {
		t.Fatal("expected hrDevicePrinter to be detected")
	}
}

func TestParseSupplyPercentAndUnknown(t *testing.T) {
	columns := map[string][]gosnmp.SnmpPDU{
		OIDSupplyType: {integerPDU(OIDSupplyType+".1.1", 21), integerPDU(OIDSupplyType+".1.2", 3)},
		OIDSupplyDescription: {
			{Name: OIDSupplyDescription + ".1.1", Type: gosnmp.OctetString, Value: []byte("Black toner\x00")},
			{Name: OIDSupplyDescription + ".1.2", Type: gosnmp.OctetString, Value: []byte("Cyan toner")},
		},
		OIDSupplyUnit:        {integerPDU(OIDSupplyUnit+".1.1", 7), integerPDU(OIDSupplyUnit+".1.2", 19)},
		OIDSupplyMaxCapacity: {integerPDU(OIDSupplyMaxCapacity+".1.1", 10000)},
		OIDSupplyLevel:       {integerPDU(OIDSupplyLevel+".1.1", 2500), integerPDU(OIDSupplyLevel+".1.2", -2)},
	}
	supplies := parseSupplyPDUs(columns)
	if len(supplies) != 2 || supplies[0].RemainingPercent == nil || *supplies[0].RemainingPercent != 25 {
		t.Fatalf("unexpected supplies: %+v", supplies)
	}
	if supplies[1].RemainingPercent != nil || supplies[1].LevelState != "unknown" {
		t.Fatalf("unknown level must not become zero: %+v", supplies[1])
	}
}
