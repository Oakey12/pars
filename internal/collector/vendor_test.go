package collector

import "testing"

func TestApplyTSCMetrics(t *testing.T) {
	level := int64(40)
	capacity := int64(300_000_000)
	result := Result{
		Name:           "TTP2410MT",
		SystemObjectID: ".1.3.6.1.4.1.43564.2.6",
		Counters:       []Counter{{Index: "1.1", Unit: "dotRow", Value: 400_000_000}},
		Supplies: []Supply{{
			Type: "ribbonWax", Unit: "micrometers", Level: &level, MaxCapacity: &capacity,
		}},
	}

	applyVendorMetrics(&result)
	if result.PrintedLengthKM == nil || *result.PrintedLengthKM != 50 {
		t.Fatalf("printed length = %v, want 50 km", result.PrintedLengthKM)
	}
	if result.Supplies[0].RemainingPercent == nil || *result.Supplies[0].RemainingPercent != 40 {
		t.Fatalf("ribbon percentage = %v, want 40", result.Supplies[0].RemainingPercent)
	}
	if result.ConsumablePercent == nil || *result.ConsumablePercent != 40 {
		t.Fatalf("consumable percentage = %v, want 40", result.ConsumablePercent)
	}
}

func TestUnknownTSCModelDoesNotInventDistance(t *testing.T) {
	result := Result{SystemObjectID: ".1.3.6.1.4.1.43564.9", Counters: []Counter{{Unit: "dotRow", Value: 10}}}
	applyVendorMetrics(&result)
	if result.PrintedLengthKM != nil {
		t.Fatalf("unexpected distance: %v", *result.PrintedLengthKM)
	}
}

func TestDiagnosticRootFromSystemObjectID(t *testing.T) {
	got := diagnosticRoot("auto", ".1.3.6.1.4.1.1347.41")
	if got != ".1.3.6.1.4.1.1347" {
		t.Fatalf("diagnostic root = %q", got)
	}
	if got := diagnosticRoot(".1.3.6.1.4.1.43564", ""); got != ".1.3.6.1.4.1.43564" {
		t.Fatalf("explicit diagnostic root = %q", got)
	}
}
