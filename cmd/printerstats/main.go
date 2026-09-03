package main

import (
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"sync"
	"time"

	"printerstats/internal/collector"
	"printerstats/internal/config"
	"printerstats/internal/output"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("printerstats", flag.ContinueOnError)
	flags.SetOutput(stderr)
	configPath := flags.String("config", "printers.json", "path to JSON configuration")
	directIP := flags.String("ip", "", "query one IP directly without a configuration file")
	community := flags.String("community", "public", "SNMP community for -ip mode")
	port := flags.Uint("port", 161, "SNMP UDP port for -ip mode")
	version := flags.String("version", "auto", "SNMP version for -ip mode: auto, 1, or 2c")
	protocol := flags.String("protocol", "auto", "collection protocol for -ip mode: auto, snmp, or http")
	httpURL := flags.String("http-url", "", "optional counter/status page URL for -ip HTTP mode")
	timeout := flags.Duration("timeout", 3*time.Second, "SNMP timeout for -ip mode")
	retries := flags.Int("retries", 1, "SNMP retries for -ip mode")
	walkOID := flags.String("walk-oid", "", "include a diagnostic OID walk in -ip JSON output; use auto for the vendor enterprise tree")
	format := flags.String("format", "table", "output format: table, json, or csv")
	outputPath := flags.String("out", "", "write output to a file instead of stdout")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *format != "table" && *format != "json" && *format != "csv" {
		fmt.Fprintf(stderr, "output error: unknown format %q; use table, json, or csv\n", *format)
		return 2
	}
	if *walkOID != "" && (*directIP == "" || *format != "json") {
		fmt.Fprintln(stderr, "configuration error: -walk-oid requires -ip and -format json")
		return 2
	}

	var cfg config.File
	var err error
	if *directIP != "" {
		if net.ParseIP(*directIP) == nil {
			fmt.Fprintf(stderr, "configuration error: -ip is not a valid IP address: %q\n", *directIP)
			return 2
		}
		if *port == 0 || *port > 65535 {
			fmt.Fprintln(stderr, "configuration error: -port must be between 1 and 65535")
			return 2
		}
		if *version != "auto" && *version != "1" && *version != "2c" {
			fmt.Fprintln(stderr, "configuration error: -version must be auto, 1, or 2c")
			return 2
		}
		if *protocol != "auto" && *protocol != "snmp" && *protocol != "http" {
			fmt.Fprintln(stderr, "configuration error: -protocol must be auto, snmp, or http")
			return 2
		}
		if *timeout <= 0 || *retries < 0 {
			fmt.Fprintln(stderr, "configuration error: -timeout must be positive and -retries cannot be negative")
			return 2
		}
		cfg = config.File{
			ParsedTimeout: *timeout,
			Retries:       retries,
			Concurrency:   1,
			Printers: []config.Printer{{
				Address:   *directIP,
				Port:      uint16(*port),
				Community: *community,
				Version:   *version,
				Protocol:  *protocol,
				HTTPURL:   *httpURL,
			}},
		}
	} else {
		cfg, err = config.Load(*configPath)
		if err != nil {
			fmt.Fprintln(stderr, "configuration error:", err)
			return 2
		}
	}

	hybridCollector := collector.Hybrid{
		SNMP: collector.SNMP{Timeout: cfg.ParsedTimeout, Retries: *cfg.Retries, DiagnosticOID: *walkOID, MaxDiagnosticOIDs: 500},
		Web:  collector.Web{Timeout: cfg.ParsedTimeout + 2*time.Second, MaxPages: 24, MaxBodyBytes: 2 << 20},
	}
	jobs := make(chan config.Printer)
	results := make(chan collector.Result)
	var workers sync.WaitGroup
	for i := 0; i < cfg.Concurrency; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for printer := range jobs {
				results <- hybridCollector.Collect(printer)
			}
		}()
	}
	go func() {
		for _, printer := range cfg.Printers {
			if printer.IsEnabled() {
				jobs <- printer
			}
		}
		close(jobs)
		workers.Wait()
		close(results)
	}()

	collected := make([]collector.Result, 0, len(cfg.Printers))
	for result := range results {
		collected = append(collected, result)
	}
	sort.SliceStable(collected, func(i, j int) bool {
		if collected[i].Location == collected[j].Location {
			return collected[i].Address < collected[j].Address
		}
		return collected[i].Location < collected[j].Location
	})

	var writer io.Writer = stdout
	var file *os.File
	if *outputPath != "" {
		file, err = os.Create(*outputPath)
		if err != nil {
			fmt.Fprintln(stderr, "cannot create output:", err)
			return 1
		}
		defer file.Close()
		writer = file
	}
	if err := output.Write(writer, *format, collected); err != nil {
		fmt.Fprintln(stderr, "output error:", err)
		return 1
	}

	for _, result := range collected {
		if result.Status == "error" || result.Status == "not_printer" {
			return 1
		}
	}
	return 0
}
