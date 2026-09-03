package collector

import "time"

type Counter struct {
	Index string `json:"index"`
	Unit  string `json:"unit"`
	Value int64  `json:"value"`
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
	LevelState       string   `json:"level_state,omitempty"`
}

type Result struct {
	CollectedAt       time.Time `json:"collected_at"`
	Name              string    `json:"name"`
	Address           string    `json:"address"`
	Location          string    `json:"location,omitempty"`
	InventoryNumber   string    `json:"inventory_number,omitempty"`
	SerialNumber      string    `json:"serial_number,omitempty"`
	SNMPVersion       string    `json:"snmp_version,omitempty"`
	TriedVersions     []string  `json:"tried_versions,omitempty"`
	DetectedAsPrinter bool      `json:"detected_as_printer"`
	DetectionMethod   string    `json:"detection_method,omitempty"`
	SystemName        string    `json:"system_name,omitempty"`
	SystemObjectID    string    `json:"system_object_id,omitempty"`
	DeviceDescription string    `json:"device_description,omitempty"`
	Status            string    `json:"status"`
	TotalPages        *int64    `json:"total_pages,omitempty"`
	TonerPercent      *float64  `json:"toner_percent,omitempty"`
	Counters          []Counter `json:"counters,omitempty"`
	Supplies          []Supply  `json:"supplies,omitempty"`
	Error             string    `json:"error,omitempty"`
}
