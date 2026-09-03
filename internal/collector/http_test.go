package collector

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"printerstats/internal/config"
)

func TestWebCollectsKyoceraCounterAndTonerPages(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<html><head><title>Command Center RX</title></head><body>
		<a href="/counter">Счетчик</a><a href="/status">Состояние задания</a></body></html>`)
	})
	mux.HandleFunc("/counter", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<html><body><h2>Напечатанные страницы</h2><table>
		<tr><th>Функция</th><th>Черно-белый</th><th>Общий</th></tr>
		<tr><td>Копирование</td><td>12604</td><td>12604</td></tr>
		<tr><td>Принтер</td><td>190990</td><td>190990</td></tr>
		<tr><td>Общий</td><td>203594</td><td>203594</td></tr></table>
		<h2>Отсканированные страницы</h2><table>
		<tr><th>Функция</th><th>Страницы оригинала</th></tr>
		<tr><td>Копирование</td><td>7110</td></tr>
		<tr><td>Другое</td><td>116358</td></tr>
		<tr><td>Общий</td><td>123468</td></tr></table></body></html>`)
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<html><body><table><tr><td>Черный тонер</td><td><span title="Осталось 73%">73%</span></td></tr></table></body></html>`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	host, _, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	result := (Web{Timeout: time.Second}).Collect(config.Printer{Address: host, HTTPURL: server.URL + "/"})
	if result.Status != "ok" {
		t.Fatalf("status = %q, error = %q, warnings = %v", result.Status, result.Error, result.Warnings)
	}
	if result.TotalPages == nil || *result.TotalPages != 203594 {
		t.Fatalf("total pages = %v", result.TotalPages)
	}
	if result.PageMetrics == nil || result.PageMetrics.PrintedCopy == nil || *result.PageMetrics.PrintedCopy != 12604 ||
		result.PageMetrics.PrintedPrinter == nil || *result.PageMetrics.PrintedPrinter != 190990 ||
		result.PageMetrics.ScannedTotal == nil || *result.PageMetrics.ScannedTotal != 123468 {
		t.Fatalf("unexpected page metrics: %+v", result.PageMetrics)
	}
	if result.ConsumablePercent == nil || *result.ConsumablePercent != 73 {
		t.Fatalf("toner = %v, supplies = %+v", result.ConsumablePercent, result.Supplies)
	}
}

func TestWebDoesNotFollowAnotherHost(t *testing.T) {
	foreignHits := 0
	foreign := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { foreignHits++ }))
	defer foreign.Close()
	foreignURL := strings.Replace(foreign.URL, "127.0.0.1", "localhost", 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `<html><body>Kyocera printer<a href="%s/counter">Counter</a></body></html>`, foreignURL)
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	host, _, _ := net.SplitHostPort(u.Host)
	(Web{Timeout: time.Second}).Collect(config.Printer{Address: host, HTTPURL: server.URL})
	if foreignHits != 0 {
		t.Fatalf("followed a link to another host %d times", foreignHits)
	}
}

func TestWebCollectsKyoceraJavaScriptEndpoints(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `<html><frameset><frame src="/startwlm/Start_Wlm.htm"></frameset></html>`)
	})
	mux.HandleFunc("/startwlm/Start_Wlm.htm", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `<html><body><script>document.write("Command Center RX")</script></body></html>`)
	})
	mux.HandleFunc("/dvcinfo/dvccounter/DvcInfo_Counter_PrnCounter.htm", func(w http.ResponseWriter, r *http.Request) {
		if cookie, err := r.Cookie("rtl"); err != nil || cookie.Value != "0" {
			http.Error(w, "cookie required", http.StatusForbidden)
			return
		}
		fmt.Fprint(w, `counterBlackWhite[0] = 12604; counterBlackWhite[1] = 190990;`)
	})
	mux.HandleFunc("/dvcinfo/dvccounter/DvcInfo_Counter_ScanCounter.htm", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `counterBlackWhite[0] = 7110; counterBlackWhite[1] = 116358;`)
	})
	mux.HandleFunc("/startwlm/Hme_Toner.htm", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `tonerRemain[0] = 61;`)
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	u, _ := url.Parse(server.URL)
	host, _, _ := net.SplitHostPort(u.Host)

	result := (Web{Timeout: time.Second}).Collect(config.Printer{Address: host, HTTPURL: server.URL + "/"})
	if result.Status != "ok" {
		t.Fatalf("status = %q, error = %q, warnings = %v", result.Status, result.Error, result.Warnings)
	}
	if result.TotalPages == nil || *result.TotalPages != 203594 {
		t.Fatalf("printed total = %v", result.TotalPages)
	}
	if result.PageMetrics == nil || result.PageMetrics.ScannedTotal == nil || *result.PageMetrics.ScannedTotal != 123468 {
		t.Fatalf("page metrics = %+v", result.PageMetrics)
	}
	if result.ConsumablePercent == nil || *result.ConsumablePercent != 61 {
		t.Fatalf("toner = %v", result.ConsumablePercent)
	}
}
