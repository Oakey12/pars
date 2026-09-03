package collector

import "time"

type Counter struct {
	Index      string   `json:"index"`
	Unit       string   `json:"unit"`
	Value      int64    `json:"value"`
	DistanceKM *float64 `json:"distance_km,omitempty"`
}

type Supply struct {
	Index            string   `json:"index"`
	Description      string   `json:"description,omitempty"`
	Class            string   `json:"class,omitempty"`
	Type             string   `json:"type,omitempty"`
	Unit             string   `json:"unit,omitempty"`
	MaxCapacity      *int64   `json:"max_capacity,omitempty"`
	Level            *int64   `json:"level,omitempty"`
	RemainingPercent *float64 `json:"remaining_percent,omitempty"`
	PercentSource    string   `json:"percent_source,omitempty"`
	LevelState       string   `json:"level_state,omitempty"`
}

type RawOID struct {
	OID   string `json:"oid"`
	Type  string `json:"type"`
	Value string `json:"value"`
}

type PageMetrics struct {
	PrintedCopy    *int64 `json:"printed_copy,omitempty"`
	PrintedPrinter *int64 `json:"printed_printer,omitempty"`
	PrintedFax     *int64 `json:"printed_fax,omitempty"`
	PrintedTotal   *int64 `json:"printed_total,omitempty"`
	ScannedCopy    *int64 `json:"scanned_copy,omitempty"`
	ScannedOther   *int64 `json:"scanned_other,omitempty"`
	ScannedFax     *int64 `json:"scanned_fax,omitempty"`
	ScannedTotal   *int64 `json:"scanned_total,omitempty"`
}

type Result struct {
	CollectedAt       time.Time    `json:"collected_at"`
	Name              string       `json:"name"`
	Address           string       `json:"address"`
	Location          string       `json:"location,omitempty"`
	InventoryNumber   string       `json:"inventory_number,omitempty"`
	SerialNumber      string       `json:"serial_number,omitempty"`
	SNMPVersion       string       `json:"snmp_version,omitempty"`
	TriedVersions     []string     `json:"tried_versions,omitempty"`
	DetectedAsPrinter bool         `json:"detected_as_printer"`
	DetectionMethod   string       `json:"detection_method,omitempty"`
	SystemName        string       `json:"system_name,omitempty"`
	SystemObjectID    string       `json:"system_object_id,omitempty"`
	DeviceDescription string       `json:"device_description,omitempty"`
	Status            string       `json:"status"`
	MetricSources     []string     `json:"metric_sources,omitempty"`
	HTTPURL           string       `json:"http_url,omitempty"`
	TotalPages        *int64       `json:"total_pages,omitempty"`
	PageMetrics       *PageMetrics `json:"page_metrics,omitempty"`
	PrintedLengthKM   *float64     `json:"printed_length_km,omitempty"`
	TonerPercent      *float64     `json:"toner_percent,omitempty"`
	ConsumablePercent *float64     `json:"consumable_percent,omitempty"`
	Counters          []Counter    `json:"counters,omitempty"`
	Supplies          []Supply     `json:"supplies,omitempty"`
	DiagnosticRoot    string       `json:"diagnostic_root,omitempty"`
	DiagnosticOIDs    []RawOID     `json:"diagnostic_oids,omitempty"`
	DiagnosticCutOff  bool         `json:"diagnostic_cut_off,omitempty"`
	Warnings          []string     `json:"warnings,omitempty"`
	Error             string       `json:"error,omitempty"`
}
