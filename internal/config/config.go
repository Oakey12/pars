package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultCommunity   = "public"
	defaultTimeout     = 3 * time.Second
	defaultRetries     = 1
	defaultConcurrency = 8
)

type File struct {
	DefaultCommunity string    `json:"default_community"`
	Timeout          string    `json:"timeout"`
	Retries          *int      `json:"retries"`
	Concurrency      int       `json:"concurrency"`
	Printers         []Printer `json:"printers"`

	ParsedTimeout time.Duration `json:"-"`
}

type Printer struct {
	Name            string `json:"name"`
	Address         string `json:"address"`
	Port            uint16 `json:"port,omitempty"`
	Location        string `json:"location,omitempty"`
	InventoryNumber string `json:"inventory_number,omitempty"`
	SerialNumber    string `json:"serial_number,omitempty"`
	Community       string `json:"community,omitempty"`
	Version         string `json:"version,omitempty"`
	Protocol        string `json:"protocol,omitempty"`
	HTTPURL         string `json:"http_url,omitempty"`
	Enabled         *bool  `json:"enabled,omitempty"`
	Note            string `json:"note,omitempty"`
}

func Load(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, fmt.Errorf("read config: %w", err)
	}

	var cfg File
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		return File{}, fmt.Errorf("decode config: %w", err)
	}
	if err := cfg.applyDefaultsAndValidate(); err != nil {
		return File{}, err
	}
	return cfg, nil
}

func (f *File) applyDefaultsAndValidate() error {
	if strings.TrimSpace(f.DefaultCommunity) == "" {
		f.DefaultCommunity = defaultCommunity
	}

	if strings.TrimSpace(f.Timeout) == "" {
		f.ParsedTimeout = defaultTimeout
	} else {
		d, err := time.ParseDuration(f.Timeout)
		if err != nil || d <= 0 {
			return fmt.Errorf("timeout must be a positive Go duration, got %q", f.Timeout)
		}
		f.ParsedTimeout = d
	}

	if f.Retries == nil {
		retries := defaultRetries
		f.Retries = &retries
	} else if *f.Retries < 0 {
		return errors.New("retries cannot be negative")
	}
	if f.Concurrency == 0 {
		f.Concurrency = defaultConcurrency
	} else if f.Concurrency < 1 || f.Concurrency > 128 {
		return errors.New("concurrency must be between 1 and 128")
	}
	if len(f.Printers) == 0 {
		return errors.New("config has no printers")
	}

	seen := make(map[string]struct{}, len(f.Printers))
	for i := range f.Printers {
		p := &f.Printers[i]
		p.Name = strings.TrimSpace(p.Name)
		p.Address = strings.TrimSpace(p.Address)
		if p.Name == "" {
			return fmt.Errorf("printers[%d].name is required", i)
		}
		if net.ParseIP(p.Address) == nil {
			return fmt.Errorf("printers[%d].address is not an IP address: %q", i, p.Address)
		}
		if p.Port == 0 {
			p.Port = 161
		}
		if p.Community == "" {
			p.Community = f.DefaultCommunity
		}
		if p.Version == "" {
			p.Version = "2c"
		}
		if p.Version != "auto" && p.Version != "1" && p.Version != "2c" {
			return fmt.Errorf("printers[%d].version must be \"auto\", \"1\", or \"2c\"", i)
		}
		if p.Protocol == "" {
			p.Protocol = "auto"
		}
		if p.Protocol != "auto" && p.Protocol != "snmp" && p.Protocol != "http" {
			return fmt.Errorf("printers[%d].protocol must be \"auto\", \"snmp\", or \"http\"", i)
		}
		if p.HTTPURL != "" {
			u, err := url.Parse(p.HTTPURL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() != p.Address {
				return fmt.Errorf("printers[%d].http_url must be an http(s) URL for %s", i, p.Address)
			}
		}
		key := net.JoinHostPort(p.Address, fmt.Sprint(p.Port))
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate printer address: %s", key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func (p Printer) IsEnabled() bool {
	return p.Enabled == nil || *p.Enabled
}
