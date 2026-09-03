package collector

import (
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"

	"printerstats/internal/config"
)

const (
	defaultMaxHTTPPages = 24
	defaultMaxHTTPBody  = int64(2 << 20)
)

var (
	numberPattern         = regexp.MustCompile(`(?:[0-9]{1,3}(?:[\s\x{00a0}][0-9]{3})+|[0-9]+(?:[.,][0-9]+)?)`)
	percentPattern        = regexp.MustCompile(`(?i)([0-9]{1,3}(?:[.,][0-9]+)?)\s*%`)
	rawLinkPattern        = regexp.MustCompile(`(?i)["']([^"'<>]{0,160}(?:counter|count|cnt|status|toner|supply|device|info|consum)[^"'<>]{0,100})["']`)
	jsArrayPattern        = regexp.MustCompile(`(?im)\b([a-z_$][a-z0-9_$]*)\s*\[\s*([0-9]+)\s*\]\s*=\s*(?:parseInt\s*\(\s*)?["']?\s*([0-9][0-9\s.,]*)`)
	jsValuePattern        = regexp.MustCompile(`(?im)\b([a-z_$][a-z0-9_$]*(?:toner|remain|level|percent)[a-z0-9_$]*)\s*=\s*["']?\s*([0-9]{1,3})\s*%?`)
	jsCounterValuePattern = regexp.MustCompile(`(?im)["']?([a-z_$][a-z0-9_$]*(?:counter|count|pages|impressions)[a-z0-9_$]*)["']?\s*(?:=|:)\s*(?:parseInt\s*\(\s*)?["']?\s*([0-9][0-9\s.,]*)`)
	jsCounterPushPattern  = regexp.MustCompile(`(?im)\b([a-z_$][a-z0-9_$]*(?:counter|count|pages|impressions)[a-z0-9_$]*)\.push\s*\(\s*["']?\s*([0-9][0-9\s.,]*)`)
)

var kyoceraPublicPaths = []string{
	"/dvcinfo/dvccounter/DvcInfo_Counter_PrnCounter.htm",
	"/dvcinfo/dvccounter/DvcInfo_Counter_ScanCounter.htm",
	"/js/jssrc/model/dvcinfo/dvccounter/DvcInfo_Counter_PrnCounter.model.htm",
	"/js/jssrc/model/dvcinfo/dvccounter/DvcInfo_Counter_ScanCounter.model.htm",
	"/startwlm/Hme_Toner.htm",
	"/js/jssrc/model/startwlm/Hme_Toner.model.htm",
}

// Web reads public status pages exposed by a printer. It never submits forms
// and follows links only on the configured printer IP.
type Web struct {
	Timeout      time.Duration
	MaxPages     int
	MaxBodyBytes int64
}

type crawlTarget struct {
	URL   *url.URL
	Depth int
	Score int
}

func (w Web) Collect(printer config.Printer) Result {
	result := newWebResult(printer)
	candidates, err := webCandidates(printer)
	if err != nil {
		result.Error = "HTTP configuration: " + err.Error()
		return result
	}

	var attemptErrors []string
	best := result
	for _, candidate := range candidates {
		current, crawlErr := w.crawl(printer, candidate)
		if crawlErr != nil {
			attemptErrors = append(attemptErrors, candidate.Scheme+": "+crawlErr.Error())
		}
		if webResultScore(current) > webResultScore(best) {
			best = current
		}
		if current.TotalPages != nil && current.ConsumablePercent != nil {
			return current
		}
		// If this scheme reached the printer web UI, switching from HTTP to
		// HTTPS (or vice versa) only repeats slow requests against the same UI.
		if current.DetectedAsPrinter && current.HTTPURL != "" {
			return current
		}
	}

	if webResultScore(best) > 0 {
		if len(attemptErrors) > 0 && best.Status != "error" {
			best.Warnings = append(best.Warnings, strings.Join(attemptErrors, "; "))
		}
		return best
	}
	result.Error = "HTTP request failed"
	if len(attemptErrors) > 0 {
		result.Error += ": " + strings.Join(attemptErrors, "; ")
	}
	return result
}

func newWebResult(printer config.Printer) Result {
	name := strings.TrimSpace(printer.Name)
	if name == "" {
		name = printer.Address
	}
	return Result{
		CollectedAt:     time.Now().UTC(),
		Name:            name,
		Address:         printer.Address,
		Location:        printer.Location,
		InventoryNumber: printer.InventoryNumber,
		SerialNumber:    printer.SerialNumber,
		Status:          "error",
	}
}

func webCandidates(printer config.Printer) ([]*url.URL, error) {
	if printer.HTTPURL != "" {
		u, err := url.Parse(printer.HTTPURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() != printer.Address {
			return nil, fmt.Errorf("http_url must point to %s over HTTP or HTTPS", printer.Address)
		}
		return []*url.URL{u}, nil
	}
	return []*url.URL{
		{Scheme: "http", Host: printer.Address, Path: "/"},
		{Scheme: "https", Host: printer.Address, Path: "/"},
	}, nil
}

func (w Web) crawl(printer config.Printer, start *url.URL) (Result, error) {
	result := newWebResult(printer)
	maxPages := w.MaxPages
	if maxPages <= 0 {
		maxPages = defaultMaxHTTPPages
	}
	maxBody := w.MaxBodyBytes
	if maxBody <= 0 {
		maxBody = defaultMaxHTTPBody
	}
	timeout := w.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // Local printers commonly use self-signed certificates.
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Timeout:   timeout,
		Transport: transport,
		Jar:       jar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Hostname() != printer.Address {
				return errors.New("redirect outside printer address refused")
			}
			return nil
		},
	}
	client.Jar.SetCookies(start, []*http.Cookie{
		{Name: "rtl", Value: "0", Path: "/"},
		{Name: "css", Value: "1", Path: "/"},
	})

	queue := []crawlTarget{{URL: cloneURL(start), Score: 1000}}
	visited := make(map[string]bool)
	var fetchErrors []string
	kyoceraPathsAdded := false
	counterPagesFetched := 0
	for len(queue) > 0 && len(visited) < maxPages {
		sort.SliceStable(queue, func(i, j int) bool { return queue[i].Score > queue[j].Score })
		target := queue[0]
		queue = queue[1:]
		key := canonicalURL(target.URL)
		if visited[key] {
			continue
		}
		visited[key] = true

		doc, raw, finalURL, err := fetchHTML(client, target.URL, start.String(), maxBody)
		if err != nil {
			fetchErrors = append(fetchErrors, target.URL.String()+": "+err.Error())
			continue
		}
		result.HTTPURL = finalURL.String()
		parseWebPage(&result, doc)
		parseKyoceraJavaScript(&result, string(raw), finalURL.Path)
		if strings.Contains(strings.ToLower(finalURL.Path), "counter") {
			counterPagesFetched++
		}
		if !kyoceraPathsAdded && isKyoceraWebPage(doc, raw) {
			kyoceraPathsAdded = true
			result.DetectedAsPrinter = true
			result.DetectionMethod = "Kyocera Command Center RX"
			for index, path := range kyoceraPublicPaths {
				known := cloneURL(start)
				known.Path = path
				known.RawQuery = ""
				known.Fragment = ""
				queue = append(queue, crawlTarget{URL: known, Score: 500 - index})
			}
		}
		if result.TotalPages != nil && result.ConsumablePercent != nil {
			break
		}
		if target.Depth >= 3 {
			continue
		}
		for _, link := range discoverLinks(doc, raw, finalURL, printer.Address) {
			if !visited[canonicalURL(link)] {
				queue = append(queue, crawlTarget{URL: link, Depth: target.Depth + 1, Score: linkScore(link.String())})
			}
		}
	}

	finishWebResult(&result)
	if result.DetectedAsPrinter && result.TotalPages == nil && result.ConsumablePercent == nil {
		result.Warnings = nil
		switch {
		case counterPagesFetched > 0:
			result.Warnings = []string{"Kyocera counter page is reachable, but its data format was not recognized"}
		case len(fetchErrors) > 0:
			result.Warnings = []string{"Kyocera metric pages are unavailable: " + summarizeHTTPFailures(fetchErrors)}
		default:
			result.Warnings = []string{"printer web interface is reachable, but counter and toner pages were not found"}
		}
	}
	if len(visited) == 0 || (result.Status == "error" && len(fetchErrors) > 0) {
		return result, errors.New(summarizeHTTPFailures(fetchErrors))
	}
	return result, nil
}

func fetchHTML(client *http.Client, target *url.URL, referer string, maxBody int64) (*html.Node, []byte, *url.URL, error) {
	req, err := http.NewRequest(http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, nil, target, err
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain;q=0.8,*/*;q=0.1")
	req.Header.Set("User-Agent", "printerstats/1.0")
	req.Header.Set("Referer", referer)
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, target, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 400 {
		return nil, nil, resp.Request.URL, fmt.Errorf("HTTP %s", resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, nil, resp.Request.URL, err
	}
	if int64(len(raw)) > maxBody {
		return nil, nil, resp.Request.URL, fmt.Errorf("response exceeds %d bytes", maxBody)
	}
	utf8Reader, err := charset.NewReader(bytes.NewReader(raw), resp.Header.Get("Content-Type"))
	if err != nil {
		return nil, nil, resp.Request.URL, err
	}
	decoded, err := io.ReadAll(io.LimitReader(utf8Reader, maxBody+1))
	if err != nil {
		return nil, nil, resp.Request.URL, err
	}
	if int64(len(decoded)) > maxBody {
		return nil, nil, resp.Request.URL, fmt.Errorf("decoded response exceeds %d bytes", maxBody)
	}
	doc, err := html.Parse(bytes.NewReader(decoded))
	return doc, decoded, resp.Request.URL, err
}

func parseKyoceraJavaScript(result *Result, source, path string) {
	lowerPath := strings.ToLower(path)
	arrays := javascriptArrays(source)
	if strings.Contains(lowerPath, "prncounter") || (strings.Contains(lowerPath, "counter") && !strings.Contains(lowerPath, "scan")) {
		parseKyoceraPrintedArrays(result, arrays)
		if result.TotalPages == nil {
			parseGenericKyoceraCounter(result, source, arrays, false)
		}
	}
	if strings.Contains(lowerPath, "scancounter") {
		parseKyoceraScannedArrays(result, arrays)
		if result.PageMetrics == nil || result.PageMetrics.ScannedTotal == nil {
			parseGenericKyoceraCounter(result, source, arrays, true)
		}
	}
	if strings.Contains(lowerPath, "toner") && result.ConsumablePercent == nil {
		parseKyoceraTonerJavaScript(result, source, arrays)
	}
}

func parseGenericKyoceraCounter(result *Result, source string, arrays map[string]map[int]int64, scanned bool) {
	values := genericCounterValues(source, arrays)
	if len(values) == 0 {
		return
	}
	total := likelyCounterTotal(values)
	metrics := ensurePageMetrics(result)
	if scanned {
		metrics.ScannedTotal = int64Pointer(total)
		if len(values) == 1 {
			metrics.ScannedOther = int64Pointer(total)
		}
	} else {
		metrics.PrintedTotal = int64Pointer(total)
		result.TotalPages = metrics.PrintedTotal
		if len(values) == 1 {
			metrics.PrintedPrinter = int64Pointer(total)
		}
	}
	markWebMetrics(result)
}

func genericCounterValues(source string, arrays map[string]map[int]int64) []int64 {
	var values []int64
	for name, indexed := range arrays {
		if !isGenericCounterName(name) {
			continue
		}
		for _, value := range indexed {
			values = append(values, value)
		}
	}
	for _, pattern := range []*regexp.Regexp{jsCounterValuePattern, jsCounterPushPattern} {
		for _, match := range pattern.FindAllStringSubmatch(source, -1) {
			if !isGenericCounterName(strings.ToLower(match[1])) {
				continue
			}
			value, err := strconv.ParseInt(onlyDigits(match[2]), 10, 64)
			if err == nil {
				values = append(values, value)
			}
		}
	}
	return uniqueCounterValues(values)
}

func isGenericCounterName(name string) bool {
	if !containsAny(name, "counter", "count", "pages", "impressions", "cntr") {
		return false
	}
	if containsAny(name, "support", "enable", "selected", "index", "length", "size", "type", "mode", "common") {
		return false
	}
	return true
}

func uniqueCounterValues(values []int64) []int64 {
	seen := make(map[int64]bool)
	var result []int64
	for _, value := range values {
		if value < 0 || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	if len(result) > 1 {
		var largest int64
		for _, value := range result {
			if value > largest {
				largest = value
			}
		}
		if largest >= 100 {
			filtered := result[:0]
			for _, value := range result {
				if value > 10 || value == 0 {
					filtered = append(filtered, value)
				}
			}
			result = filtered
		}
	}
	return result
}

func likelyCounterTotal(values []int64) int64 {
	var sum, largest int64
	for _, value := range values {
		sum += value
		if value > largest {
			largest = value
		}
	}
	if len(values) > 1 && sum-largest == largest {
		return largest
	}
	return sum
}

func javascriptArrays(source string) map[string]map[int]int64 {
	arrays := make(map[string]map[int]int64)
	for _, match := range jsArrayPattern.FindAllStringSubmatch(source, -1) {
		index, indexErr := strconv.Atoi(match[2])
		value, valueErr := strconv.ParseInt(onlyDigits(match[3]), 10, 64)
		if indexErr != nil || valueErr != nil {
			continue
		}
		name := strings.ToLower(match[1])
		if arrays[name] == nil {
			arrays[name] = make(map[int]int64)
		}
		arrays[name][index] = value
	}
	return arrays
}

func parseKyoceraPrintedArrays(result *Result, arrays map[string]map[int]int64) {
	byFunction := make(map[int]int64)
	matched := false
	for name, values := range arrays {
		if !isKyoceraPrintCounterArray(name) {
			continue
		}
		matched = true
		for index, value := range values {
			byFunction[index] += value
		}
	}
	if !matched {
		return
	}
	metrics := ensurePageMetrics(result)
	if value, ok := byFunction[0]; ok {
		metrics.PrintedCopy = int64Pointer(value)
	}
	if value, ok := byFunction[1]; ok {
		metrics.PrintedPrinter = int64Pointer(value)
	}
	if value, ok := byFunction[2]; ok {
		metrics.PrintedFax = int64Pointer(value)
	}
	var total int64
	for _, value := range byFunction {
		total += value
	}
	metrics.PrintedTotal = int64Pointer(total)
	result.TotalPages = metrics.PrintedTotal
	markWebMetrics(result)
}

func isKyoceraPrintCounterArray(name string) bool {
	if (!strings.HasPrefix(name, "counter") && !strings.HasPrefix(name, "cntr")) || strings.Contains(name, "common") {
		return false
	}
	return containsAny(name, "blackwhite", "fullcolor", "singlecolor", "onecolor", "twocolor", "threecolor")
}

func parseKyoceraScannedArrays(result *Result, arrays map[string]map[int]int64) {
	byFunction := make(map[int]int64)
	for name, values := range arrays {
		if (!strings.Contains(name, "counter") && !strings.Contains(name, "cntr")) || strings.Contains(name, "common") {
			continue
		}
		if !(strings.Contains(name, "scan") || strings.Contains(name, "original") || isKyoceraPrintCounterArray(name)) {
			continue
		}
		for index, value := range values {
			byFunction[index] += value
		}
	}
	if len(byFunction) == 0 {
		return
	}
	metrics := ensurePageMetrics(result)
	if value, ok := byFunction[0]; ok {
		metrics.ScannedCopy = int64Pointer(value)
	}
	if value, ok := byFunction[1]; ok {
		metrics.ScannedOther = int64Pointer(value)
	}
	if value, ok := byFunction[2]; ok {
		metrics.ScannedFax = int64Pointer(value)
	}
	var total int64
	for _, value := range byFunction {
		total += value
	}
	metrics.ScannedTotal = int64Pointer(total)
	markWebMetrics(result)
}

func parseKyoceraTonerJavaScript(result *Result, source string, arrays map[string]map[int]int64) {
	var levels []float64
	for name, values := range arrays {
		if !isTonerLevelName(name) {
			continue
		}
		for _, value := range values {
			if value >= 0 && value <= 100 {
				levels = append(levels, float64(value))
			}
		}
	}
	for _, match := range jsValuePattern.FindAllStringSubmatch(source, -1) {
		value, err := strconv.ParseFloat(match[2], 64)
		if err == nil && value >= 0 && value <= 100 && containsAny(strings.ToLower(match[1]), "remain", "level", "percent") {
			levels = append(levels, value)
		}
	}
	for _, match := range percentPattern.FindAllStringSubmatchIndex(source, -1) {
		start := max(0, match[0]-120)
		end := min(len(source), match[1]+120)
		if !containsAny(strings.ToLower(source[start:end]), "toner", "тонер") {
			continue
		}
		value, err := strconv.ParseFloat(strings.ReplaceAll(source[match[2]:match[3]], ",", "."), 64)
		if err == nil && value >= 0 && value <= 100 {
			levels = append(levels, value)
		}
	}
	if len(levels) == 0 {
		return
	}
	sort.Float64s(levels)
	value := levels[0]
	result.Supplies = append(result.Supplies, Supply{
		Index:            "http.kyocera.toner",
		Description:      "Toner",
		Class:            "consumed",
		Type:             "toner",
		Unit:             "percent",
		RemainingPercent: float64Pointer(value),
		PercentSource:    "Kyocera web JavaScript",
		LevelState:       "value",
	})
	result.TonerPercent = float64Pointer(value)
	result.ConsumablePercent = float64Pointer(value)
	markWebMetrics(result)
}

func isTonerLevelName(name string) bool {
	if !strings.Contains(name, "toner") && !strings.Contains(name, "remain") {
		return false
	}
	if containsAny(name, "type", "count", "number", "capacity", "max", "support", "enable") {
		return false
	}
	return containsAny(name, "remain", "level", "percent", "black", "cyan", "magenta", "yellow") || name == "toner"
}

func ensurePageMetrics(result *Result) *PageMetrics {
	if result.PageMetrics == nil {
		result.PageMetrics = &PageMetrics{}
	}
	return result.PageMetrics
}

func markWebMetrics(result *Result) {
	result.DetectedAsPrinter = true
	result.DetectionMethod = "Kyocera Command Center RX"
	result.MetricSources = uniqueStrings(append(result.MetricSources, "http"))
}

func parseWebPage(result *Result, doc *html.Node) {
	pageText := normalizeText(nodeText(doc))
	lowerPage := strings.ToLower(pageText)
	if containsAny(lowerPage, "command center rx", "kyocera", "printer", "принтер", "печать", "счетчик", "счётчик") {
		result.DetectedAsPrinter = true
		result.DetectionMethod = "printer web interface"
	}
	if title := documentTitle(doc); title != "" && result.DeviceDescription == "" {
		result.DeviceDescription = title
	}

	metrics := result.PageMetrics
	if metrics == nil {
		metrics = &PageMetrics{}
	}
	for _, table := range findElements(doc, "table") {
		parseCounterTable(metrics, table)
	}
	parseFlatCounters(metrics, lowerPage)
	if !pageMetricsEmpty(metrics) {
		result.PageMetrics = metrics
		if metrics.PrintedTotal != nil {
			result.TotalPages = metrics.PrintedTotal
		}
	}

	for _, supply := range parseWebSupplies(doc) {
		if !hasEquivalentSupply(result.Supplies, supply) {
			result.Supplies = append(result.Supplies, supply)
		}
	}
	if result.ConsumablePercent == nil {
		if percent := parseFlatToner(pageText); percent != nil {
			result.Supplies = append(result.Supplies, Supply{
				Index:            "http.page.toner",
				Description:      "Black toner",
				Class:            "consumed",
				Type:             "toner",
				Unit:             "percent",
				RemainingPercent: percent,
				PercentSource:    "web page Toner section",
				LevelState:       "value",
			})
		}
	}
	if percent := lowestTonerPercent(result.Supplies); percent != nil {
		result.TonerPercent = percent
		result.ConsumablePercent = percent
	}
}

func parseCounterTable(metrics *PageMetrics, table *html.Node) {
	rows := tableRows(table)
	if len(rows) == 0 {
		return
	}
	joined := strings.ToLower(strings.Join(flattenRows(rows), " "))
	isScanned := containsAny(joined, "scanned", "отсканирован", "страниц оригинала", "другое", "other") && !containsAny(joined, "printer", "принтер")
	isPrinted := containsAny(joined, "printed", "напечатан", "printer", "принтер")
	if !isPrinted && !isScanned {
		return
	}
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		label := strings.ToLower(row[0])
		value, ok := lastNumber(row[1:])
		if !ok {
			continue
		}
		if isScanned {
			switch {
			case isLabel(label, "копирование", "copy"):
				metrics.ScannedCopy = int64Pointer(value)
			case isLabel(label, "другое", "other"):
				metrics.ScannedOther = int64Pointer(value)
			case isLabel(label, "факс", "fax"):
				metrics.ScannedFax = int64Pointer(value)
			case isLabel(label, "общий", "итого", "total"):
				metrics.ScannedTotal = int64Pointer(value)
			}
		} else {
			switch {
			case isLabel(label, "копирование", "copy"):
				metrics.PrintedCopy = int64Pointer(value)
			case isLabel(label, "принтер", "printer"):
				metrics.PrintedPrinter = int64Pointer(value)
			case isLabel(label, "факс", "fax"):
				metrics.PrintedFax = int64Pointer(value)
			case isLabel(label, "общий", "итого", "total"):
				metrics.PrintedTotal = int64Pointer(value)
			}
		}
	}
}

func parseFlatCounters(metrics *PageMetrics, text string) {
	printedStart := firstKeyword(text, "напечатанные страницы", "printed pages")
	scannedStart := firstKeyword(text, "отсканированные страницы", "scanned pages")
	if printedStart >= 0 && metrics.PrintedTotal == nil {
		end := len(text)
		if scannedStart > printedStart {
			end = scannedStart
		}
		section := text[printedStart:end]
		metrics.PrintedTotal = labelledNumber(section, "общий", "итого", "total")
		if metrics.PrintedTotal != nil && !containsAny(section, "копирование", "copy", "принтер", "printer") {
			metrics.PrintedPrinter = int64Pointer(*metrics.PrintedTotal)
		}
	}
	if scannedStart >= 0 && metrics.ScannedTotal == nil {
		metrics.ScannedTotal = labelledNumber(text[scannedStart:], "общий", "итого", "total")
	}
}

func parseFlatToner(text string) *float64 {
	lower := strings.ToLower(text)
	start := firstKeyword(lower, "тонер", "toner")
	if start < 0 {
		return nil
	}
	end := min(len(text), start+800)
	match := percentPattern.FindStringSubmatch(text[start:end])
	if len(match) != 2 {
		return nil
	}
	value, err := strconv.ParseFloat(strings.ReplaceAll(match[1], ",", "."), 64)
	if err != nil || value < 0 || value > 100 {
		return nil
	}
	return float64Pointer(value)
}

func parseWebSupplies(doc *html.Node) []Supply {
	var supplies []Supply
	elements := append(findElements(doc, "tr"), findElements(doc, "li")...)
	elements = append(elements, findElements(doc, "div")...)
	for _, element := range elements {
		text := normalizeText(nodeTextWithAttributes(element))
		if len([]rune(text)) > 350 {
			continue
		}
		lower := strings.ToLower(text)
		if !containsAny(lower, "toner", "тонер", "cartridge", "картридж") {
			continue
		}
		match := percentPattern.FindStringSubmatch(text)
		if len(match) != 2 {
			continue
		}
		value, err := strconv.ParseFloat(strings.ReplaceAll(match[1], ",", "."), 64)
		if err != nil || value < 0 || value > 100 {
			continue
		}
		supplies = append(supplies, Supply{
			Index:            "http." + strconv.Itoa(len(supplies)+1),
			Description:      shorten(normalizeText(nodeText(element)), 100),
			Class:            "consumed",
			Type:             "toner",
			Unit:             "percent",
			RemainingPercent: float64Pointer(value),
			PercentSource:    "printer web interface",
			LevelState:       "value",
		})
	}
	return supplies
}

func finishWebResult(result *Result) {
	hasPages := result.TotalPages != nil
	hasSupply := result.ConsumablePercent != nil
	if hasPages || hasSupply {
		result.MetricSources = []string{"http"}
		result.DetectedAsPrinter = true
		result.DetectionMethod = "printer web interface"
	}
	switch {
	case hasPages && hasSupply:
		result.Status = "ok"
	case hasPages:
		result.Status = "ok"
		result.Warnings = append(result.Warnings, "toner level was not found on public web pages")
	case hasSupply:
		result.Status = "partial"
		result.Warnings = append(result.Warnings, "printed page counter was not found on public web pages")
	case result.DetectedAsPrinter:
		result.Status = "partial"
		result.Warnings = append(result.Warnings, "printer web interface found, but public counter and toner values were not recognized")
	default:
		result.Status = "error"
		result.Error = "web interface did not expose recognizable printer metrics"
	}
}

func discoverLinks(doc *html.Node, raw []byte, base *url.URL, address string) []*url.URL {
	seen := make(map[string]bool)
	var result []*url.URL
	add := func(rawValue string) {
		rawValue = strings.TrimSpace(rawValue)
		if rawValue == "" || strings.HasPrefix(strings.ToLower(rawValue), "javascript:") || strings.HasPrefix(rawValue, "#") {
			return
		}
		u, err := url.Parse(rawValue)
		if err != nil {
			return
		}
		u = base.ResolveReference(u)
		if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() != address || isStaticAsset(u.Path) || unsafePage(u.String()) {
			return
		}
		u.Fragment = ""
		key := canonicalURL(u)
		if !seen[key] {
			seen[key] = true
			result = append(result, u)
		}
	}
	walkNodes(doc, func(n *html.Node) {
		if n.Type != html.ElementNode || (n.Data != "a" && n.Data != "frame" && n.Data != "iframe" && n.Data != "script") {
			return
		}
		for _, attr := range n.Attr {
			if attr.Key == "href" || attr.Key == "src" {
				add(attr.Val)
			}
		}
	})
	for _, match := range rawLinkPattern.FindAllSubmatch(raw, -1) {
		if len(match) == 2 {
			add(string(match[1]))
		}
	}
	return result
}

func linkScore(value string) int {
	lower := strings.ToLower(value)
	score := 1
	if containsAny(lower, "counter", "count", "cnt", "счетчик", "счётчик") {
		score += 100
	}
	if containsAny(lower, "toner", "supply", "consum", "status", "тонер", "расход") {
		score += 80
	}
	if containsAny(lower, "device", "info", "устройств") {
		score += 30
	}
	return score
}

func unsafePage(value string) bool {
	lower := strings.ToLower(value)
	return containsAny(lower, "logout", "logoff", "restart", "reboot", "shutdown", "delete", "firmware", "upload")
}

func isStaticAsset(path string) bool {
	lower := strings.ToLower(path)
	if strings.HasSuffix(lower, ".js") && containsAny(lower, "counter", "toner", "model", "supply", "consum") {
		return false
	}
	for _, suffix := range []string{".css", ".js", ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".woff", ".ttf", ".pdf", ".zip"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	return false
}

func isKyoceraWebPage(doc *html.Node, raw []byte) bool {
	value := strings.ToLower(nodeText(doc) + " " + string(raw))
	return containsAny(value, "command center rx", "kyocera", "startwlm", "dvccounter")
}

func summarizeHTTPFailures(failures []string) string {
	if len(failures) == 0 {
		return "no HTTP response"
	}
	counts := map[string]int{"timeout": 0, "not found": 0, "unauthorized": 0, "refused": 0, "other": 0}
	for _, failure := range failures {
		lower := strings.ToLower(failure)
		switch {
		case containsAny(lower, "timeout", "deadline exceeded"):
			counts["timeout"]++
		case strings.Contains(lower, "404"):
			counts["not found"]++
		case strings.Contains(lower, "401"):
			counts["unauthorized"]++
		case strings.Contains(lower, "refused"):
			counts["refused"]++
		default:
			counts["other"]++
		}
	}
	var parts []string
	for _, key := range []string{"timeout", "not found", "unauthorized", "refused", "other"} {
		if counts[key] > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
		}
	}
	return strings.Join(parts, ", ")
}

func onlyDigits(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsDigit(r) {
			return r
		}
		return -1
	}, value)
}

func tableRows(table *html.Node) [][]string {
	var rows [][]string
	walkNodes(table, func(n *html.Node) {
		if n.Type != html.ElementNode || n.Data != "tr" {
			return
		}
		var cells []string
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if child.Type == html.ElementNode && (child.Data == "td" || child.Data == "th") {
				cells = append(cells, normalizeText(nodeText(child)))
			}
		}
		if len(cells) > 0 {
			rows = append(rows, cells)
		}
	})
	return rows
}

func flattenRows(rows [][]string) []string {
	var values []string
	for _, row := range rows {
		values = append(values, row...)
	}
	return values
}

func lastNumber(cells []string) (int64, bool) {
	for i := len(cells) - 1; i >= 0; i-- {
		matches := numberPattern.FindAllString(cells[i], -1)
		for j := len(matches) - 1; j >= 0; j-- {
			digits := strings.Map(func(r rune) rune {
				if unicode.IsDigit(r) {
					return r
				}
				return -1
			}, matches[j])
			if digits == "" {
				continue
			}
			value, err := strconv.ParseInt(digits, 10, 64)
			if err == nil {
				return value, true
			}
		}
	}
	return 0, false
}

func labelledNumber(text string, labels ...string) *int64 {
	for _, label := range labels {
		index := strings.LastIndex(text, label)
		if index < 0 {
			continue
		}
		if value, ok := lastNumber([]string{text[index+len(label):]}); ok {
			return int64Pointer(value)
		}
	}
	return nil
}

func firstKeyword(text string, values ...string) int {
	best := -1
	for _, value := range values {
		if index := strings.Index(text, value); index >= 0 && (best < 0 || index < best) {
			best = index
		}
	}
	return best
}

func isLabel(value string, labels ...string) bool {
	value = normalizeText(value)
	for _, label := range labels {
		if value == label || strings.HasPrefix(value, label+" ") || strings.HasPrefix(value, label+":") {
			return true
		}
	}
	return false
}

func findElements(root *html.Node, tag string) []*html.Node {
	var nodes []*html.Node
	walkNodes(root, func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == tag {
			nodes = append(nodes, n)
		}
	})
	return nodes
}

func walkNodes(root *html.Node, visit func(*html.Node)) {
	if root == nil {
		return
	}
	visit(root)
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		walkNodes(child, visit)
	}
}

func nodeText(root *html.Node) string {
	var values []string
	var walk func(*html.Node, bool)
	walk = func(n *html.Node, ignored bool) {
		if n.Type == html.ElementNode && (n.Data == "script" || n.Data == "style" || n.Data == "noscript") {
			ignored = true
		}
		if n.Type == html.TextNode && !ignored {
			values = append(values, n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child, ignored)
		}
	}
	walk(root, false)
	return strings.Join(values, " ")
}

func nodeTextWithAttributes(root *html.Node) string {
	values := []string{nodeText(root)}
	walkNodes(root, func(n *html.Node) {
		for _, attr := range n.Attr {
			if attr.Key == "alt" || attr.Key == "title" || attr.Key == "style" || attr.Key == "value" {
				values = append(values, attr.Val)
			}
		}
	})
	return strings.Join(values, " ")
}

func documentTitle(doc *html.Node) string {
	for _, title := range findElements(doc, "title") {
		if value := normalizeText(nodeText(title)); value != "" {
			return value
		}
	}
	return ""
}

func normalizeText(value string) string {
	return strings.Join(strings.Fields(strings.ReplaceAll(value, "\u00a0", " ")), " ")
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func cloneURL(value *url.URL) *url.URL {
	copy := *value
	return &copy
}

func canonicalURL(value *url.URL) string {
	copy := cloneURL(value)
	copy.Fragment = ""
	return copy.String()
}

func pageMetricsEmpty(metrics *PageMetrics) bool {
	return metrics.PrintedCopy == nil && metrics.PrintedPrinter == nil && metrics.PrintedFax == nil && metrics.PrintedTotal == nil &&
		metrics.ScannedCopy == nil && metrics.ScannedOther == nil && metrics.ScannedFax == nil && metrics.ScannedTotal == nil
}

func hasEquivalentSupply(supplies []Supply, candidate Supply) bool {
	for _, supply := range supplies {
		if supply.Description == candidate.Description && supply.RemainingPercent != nil && candidate.RemainingPercent != nil && *supply.RemainingPercent == *candidate.RemainingPercent {
			return true
		}
	}
	return false
}

func int64Pointer(value int64) *int64       { return &value }
func float64Pointer(value float64) *float64 { return &value }

func webResultScore(result Result) int {
	score := 0
	if result.DetectedAsPrinter {
		score++
	}
	if result.TotalPages != nil {
		score += 10
	}
	if result.ConsumablePercent != nil {
		score += 5
	}
	return score
}
