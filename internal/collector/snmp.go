package collector

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"

	"printerstats/internal/config"
)

type SNMP struct {
	Timeout           time.Duration
	Retries           int
	DiagnosticOID     string
	MaxDiagnosticOIDs int
}

var errDiagnosticLimit = errors.New("diagnostic OID limit reached")

func (s SNMP) Collect(printer config.Printer) Result {
	versions := []gosnmp.SnmpVersion{gosnmp.Version2c}
	if printer.Version == "1" {
		versions = []gosnmp.SnmpVersion{gosnmp.Version1}
	} else if printer.Version == "auto" {
		versions = []gosnmp.SnmpVersion{gosnmp.Version2c, gosnmp.Version1}
	}

	best := Result{}
	tried := make([]string, 0, len(versions))
	for i, version := range versions {
		tried = append(tried, versionName(version))
		result := s.collectVersion(printer, version)
		if i == 0 || resultScore(result) > resultScore(best) {
			best = result
		}
		if result.Status == "ok" || result.TotalPages != nil || len(result.Supplies) > 0 {
			result.TriedVersions = append([]string(nil), tried...)
			return result
		}
	}
	best.TriedVersions = tried
	return best
}

func (s SNMP) collectVersion(printer config.Printer, version gosnmp.SnmpVersion) Result {
	result := Result{
		CollectedAt:     time.Now().UTC(),
		Name:            printer.Name,
		Address:         printer.Address,
		Location:        printer.Location,
		InventoryNumber: printer.InventoryNumber,
		SerialNumber:    printer.SerialNumber,
		SNMPVersion:     versionName(version),
		Status:          "error",
	}
	automaticName := strings.TrimSpace(result.Name) == ""
	if automaticName {
		result.Name = printer.Address
	}

	client := &gosnmp.GoSNMP{
		Target:    printer.Address,
		Port:      printer.Port,
		Community: printer.Community,
		Version:   version,
		Timeout:   s.Timeout,
		Retries:   s.Retries,
		MaxOids:   gosnmp.MaxOids,
	}
	if err := client.Connect(); err != nil {
		result.Error = fmt.Sprintf("connect: %v", err)
		return result
	}
	defer client.Conn.Close()

	metadataOIDs := []string{OIDSysDescr, OIDSysObjectID, OIDSysName, OIDSysLocation}
	metadata, err := getMetadata(client, metadataOIDs)
	if err != nil {
		result.Error = "SNMP request failed: " + err.Error()
		return result
	}
	if pdu, ok := pduByOID(metadata, OIDSysDescr); ok {
		result.DeviceDescription = sanitizeText(pduString(pdu))
	}
	if pdu, ok := pduByOID(metadata, OIDSysObjectID); ok {
		result.SystemObjectID = sanitizeText(pduString(pdu))
	}
	if pdu, ok := pduByOID(metadata, OIDSysName); ok {
		result.SystemName = sanitizeText(pduString(pdu))
	}
	if pdu, ok := pduByOID(metadata, OIDSysLocation); ok && result.Location == "" {
		result.Location = sanitizeText(pduString(pdu))
	}
	if automaticName {
		if result.SystemName != "" {
			result.Name = result.SystemName
		} else if result.DeviceDescription != "" {
			result.Name = shorten(result.DeviceDescription, 80)
		}
	}

	if looksLikePrinter(result.DeviceDescription, result.SystemName) {
		result.DetectedAsPrinter = true
		result.DetectionMethod = "SNMP system description"
	}

	var errs []string
	markerPDUs, err := walk(client, OIDMarkerEntry)
	if err != nil {
		errs = append(errs, "marker table: "+err.Error())
	}
	units := pdusForColumn(markerPDUs, OIDMarkerCounterUnit)
	counts := pdusForColumn(markerPDUs, OIDMarkerLifeCount)
	result.Counters = parseCounterPDUs(units, counts)
	result.TotalPages = primaryPageCount(result.Counters)

	supplyOIDs := []string{OIDSupplyClass, OIDSupplyType, OIDSupplyDescription, OIDSupplyUnit, OIDSupplyMaxCapacity, OIDSupplyLevel}
	columns := make(map[string][]gosnmp.SnmpPDU, len(supplyOIDs))
	supplyPDUs, err := walk(client, OIDSupplyEntry)
	if err != nil {
		errs = append(errs, "supply table: "+err.Error())
	}
	for _, oid := range supplyOIDs {
		columns[oid] = pdusForColumn(supplyPDUs, oid)
	}
	result.Supplies = parseSupplyPDUs(columns)
	result.TonerPercent = lowestTonerPercent(result.Supplies)
	applyVendorMetrics(&result)
	if result.TotalPages != nil || len(result.Supplies) > 0 {
		result.MetricSources = []string{"snmp"}
	}

	if s.DiagnosticOID != "" {
		root := diagnosticRoot(s.DiagnosticOID, result.SystemObjectID)
		if root == "" {
			errs = append(errs, "cannot determine enterprise OID from sysObjectID")
		} else {
			result.DiagnosticRoot = root
			limit := s.MaxDiagnosticOIDs
			if limit <= 0 {
				limit = 500
			}
			pdus, cutOff, walkErr := walkLimited(client, root, limit)
			result.DiagnosticCutOff = cutOff
			result.DiagnosticOIDs = rawOIDs(pdus)
			if walkErr != nil {
				errs = append(errs, "diagnostic walk: "+walkErr.Error())
			}
		}
	}

	if len(result.Counters) > 0 || len(result.Supplies) > 0 {
		result.DetectedAsPrinter = true
		result.DetectionMethod = "Printer-MIB"
	}
	if !result.DetectedAsPrinter && len(errs) == 0 {
		deviceTypes, walkErr := walk(client, OIDHRDeviceType)
		if walkErr != nil {
			errs = append(errs, "device type table: "+walkErr.Error())
		} else if hasPrinterDeviceType(deviceTypes) {
			result.DetectedAsPrinter = true
			result.DetectionMethod = "HOST-RESOURCES-MIB hrDevicePrinter"
		}
	}

	hasMetrics := len(result.Counters) > 0 || len(result.Supplies) > 0
	switch {
	case hasMetrics && len(errs) == 0:
		result.Status = "ok"
	case hasMetrics:
		result.Status = "partial"
		result.Error = strings.Join(errs, "; ")
	case result.DetectedAsPrinter && len(errs) == 0:
		result.Status = "partial"
		result.Error = "printer detected, but standard Printer-MIB counters are unavailable"
	case result.DetectedAsPrinter:
		result.Error = strings.Join(errs, "; ")
	case len(errs) == 0:
		result.Status = "not_printer"
		result.Error = "SNMP device responded, but it was not identified as a printer"
	default:
		result.Error = strings.Join(errs, "; ")
	}
	return result
}

func getMetadata(client *gosnmp.GoSNMP, oids []string) ([]gosnmp.SnmpPDU, error) {
	packet, err := client.Get(oids)
	if err == nil && packet.Error == gosnmp.NoError {
		return packet.Variables, nil
	}
	if err == nil {
		err = fmt.Errorf("%s (index %d)", packet.Error, packet.ErrorIndex)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "invalid packet length") && client.Version != gosnmp.Version1 {
		return nil, err
	}

	// Some older print servers generate malformed replies when several OIDs are
	// requested together. Retrying one OID at a time keeps those devices usable.
	var variables []gosnmp.SnmpPDU
	var lastErr error
	for _, oid := range oids {
		response, getErr := client.Get([]string{oid})
		if getErr != nil {
			lastErr = getErr
			continue
		}
		if response.Error != gosnmp.NoError {
			lastErr = fmt.Errorf("%s (index %d)", response.Error, response.ErrorIndex)
			continue
		}
		variables = append(variables, response.Variables...)
	}
	if len(variables) > 0 {
		return variables, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, err
}

func diagnosticRoot(requested, systemObjectID string) string {
	if requested != "auto" {
		return requested
	}
	parts := strings.Split(strings.Trim(strings.TrimSpace(systemObjectID), "."), ".")
	if len(parts) < 7 || strings.Join(parts[:6], ".") != "1.3.6.1.4.1" {
		return ""
	}
	return ".1.3.6.1.4.1." + parts[6]
}

func walkLimited(client *gosnmp.GoSNMP, oid string, limit int) ([]gosnmp.SnmpPDU, bool, error) {
	pdus := make([]gosnmp.SnmpPDU, 0, min(limit, 64))
	walkFn := func(pdu gosnmp.SnmpPDU) error {
		if len(pdus) >= limit {
			return errDiagnosticLimit
		}
		if !isExceptionPDU(pdu) {
			pdus = append(pdus, pdu)
		}
		return nil
	}
	var err error
	if client.Version == gosnmp.Version1 {
		err = client.Walk(oid, walkFn)
	} else {
		err = client.BulkWalk(oid, walkFn)
	}
	if errors.Is(err, errDiagnosticLimit) {
		return pdus, true, nil
	}
	return pdus, false, err
}

func rawOIDs(pdus []gosnmp.SnmpPDU) []RawOID {
	result := make([]RawOID, 0, len(pdus))
	for _, pdu := range pdus {
		result = append(result, RawOID{OID: pdu.Name, Type: strconv.Itoa(int(pdu.Type)), Value: sanitizeText(pduString(pdu))})
	}
	return result
}

func resultScore(result Result) int {
	score := 0
	if result.SystemName != "" || result.DeviceDescription != "" {
		score += 10
	}
	if result.DetectedAsPrinter {
		score += 20
	}
	if result.TotalPages != nil {
		score += 100
	}
	score += 10 * len(result.Supplies)
	if result.Status == "ok" {
		score += 10
	} else if result.Status == "partial" {
		score += 5
	}
	return score
}

func versionName(version gosnmp.SnmpVersion) string {
	if version == gosnmp.Version1 {
		return "1"
	}
	return "2c"
}

func shorten(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max-1]) + "…"
}

func walk(client *gosnmp.GoSNMP, oid string) ([]gosnmp.SnmpPDU, error) {
	if client.Version == gosnmp.Version1 {
		return client.WalkAll(oid)
	}
	return client.BulkWalkAll(oid)
}
