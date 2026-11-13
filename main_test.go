package main

import (
	"bytes"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestImportAndRollback(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "history.json")
	dbPath := filepath.Join(dir, "history.sqlite")
	data := `{"other":{"nested":[1,2]},"Browser History":[{"url":"https://example.com","time_usec":123,"title":"A"},{"url":"https://example.com","time_usec":123},{"url":"https://other.example","time_usec":123}]}`
	if err := os.WriteFile(input, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	for i := 0; i < 2; i++ {
		if err := run(input, dbPath, "Chrome", &out); err != nil {
			t.Fatal(err)
		}
	}
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query("PRAGMA table_info(browser_history)")
	if err != nil {
		t.Fatal(err)
	}
	var columns []string
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if got, want := strings.Join(columns, ","), "id,page_transition,title,url,client_id,browser,time_usec,favicon_url,import_id"; got != want {
		t.Fatalf("column order = %s, want %s", got, want)
	}
	var visits, logs, added, duplicates int
	if err := db.QueryRow("SELECT count(*) FROM browser_history").Scan(&visits); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*), sum(new_records), sum(duplicate_records) FROM log").Scan(&logs, &added, &duplicates); err != nil {
		t.Fatal(err)
	}
	var browser string
	if err := db.QueryRow("SELECT browser FROM browser_history LIMIT 1").Scan(&browser); err != nil || browser != "Chrome" {
		t.Fatalf("browser = %q, error = %v", browser, err)
	}
	if visits != 2 || logs != 2 || added != 2 || duplicates != 4 {
		t.Fatalf("visits=%d logs=%d added=%d duplicates=%d", visits, logs, added, duplicates)
	}
	if err := os.WriteFile(input, []byte(`{"Browser History":[{"url":"https://new.example","time_usec":456},`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(input, dbPath, "Chrome", &out); err == nil {
		t.Fatal("expected malformed input error")
	}
	if err := db.QueryRow("SELECT count(*) FROM browser_history").Scan(&visits); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*) FROM log").Scan(&logs); err != nil {
		t.Fatal(err)
	}
	if visits != 2 || logs != 2 {
		t.Fatalf("failed import changed database: visits=%d logs=%d", visits, logs)
	}
}

func TestInteractive(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "history database.sqlite")
	inputPath := filepath.Join(dir, "browser history.json")
	if err := os.WriteFile(inputPath, []byte(`{"Browser History":[{"url":"https://example.com","time_usec":123}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := interactive(strings.NewReader("\n\""+dbPath+"\"\n3\n1\n'"+inputPath+"'\n"), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Please enter a path.") || !strings.Contains(output.String(), "Please enter 1 or 2.") || !strings.Contains(output.String(), "1 new records") {
		t.Fatalf("unexpected output: %s", output.String())
	}
	if err := interactive(strings.NewReader(""), io.Discard); err == nil {
		t.Fatal("expected error on end of input")
	}
}

func TestImportProvenance(t *testing.T) {
	dir := t.TempDir()
	input, dbPath := filepath.Join(dir, "history.json"), filepath.Join(dir, "history.sqlite")
	for _, data := range []string{
		`{"Browser History":[{"url":"https://first.example","time_usec":1}]}`,
		`{"Browser History":[{"url":"https://first.example","time_usec":1},{"url":"https://second.example","time_usec":2}]}`,
	} {
		if err := os.WriteFile(input, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		if err := run(input, dbPath, "Chrome", io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT h.url, h.import_id, l.new_records, l.duplicate_records FROM browser_history h JOIN log l ON h.import_id = l.id ORDER BY h.url`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for _, want := range []struct {
		url                   string
		id, added, duplicates int
	}{{"https://first.example", 1, 1, 0}, {"https://second.example", 2, 1, 1}} {
		if !rows.Next() {
			t.Fatal("missing history row")
		}
		var url string
		var id, added, duplicates int
		if err := rows.Scan(&url, &id, &added, &duplicates); err != nil {
			t.Fatal(err)
		}
		if url != want.url || id != want.id || added != want.added || duplicates != want.duplicates {
			t.Fatalf("unexpected provenance: %q %d %d %d", url, id, added, duplicates)
		}
	}
	if rows.Next() {
		t.Fatal("extra history row")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestSafariCSV(t *testing.T) {
	dir := t.TempDir()
	input, dbPath := filepath.Join(dir, "safari.csv"), filepath.Join(dir, "history.sqlite")
	data := "\"A, title\",https://example.com,2,\"Apr 23, 2016, 10:20 PM\"\n,,1,\"Apr 22, 2016, 5:58 PM\"\n"
	if err := os.WriteFile(input, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := runWithLocation(input, dbPath, "Safari", loc, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var title, browser, field, date, zone string
	var micros int64
	err = db.QueryRow(`SELECT h.title,h.browser,h.time_usec,d.source_field_3,d.source_date,d.source_timezone FROM browser_history h JOIN safari_csv_details d ON d.history_id=h.id WHERE h.url=?`, "https://example.com").Scan(&title, &browser, &micros, &field, &date, &zone)
	if err != nil {
		t.Fatal(err)
	}
	wantTime := time.Date(2016, 4, 24, 2, 20, 0, 0, time.UTC).UnixMicro()
	if title != "A, title" || browser != "Safari" || micros != wantTime || field != "2" || date != "Apr 23, 2016, 10:20 PM" || zone != "America/New_York" {
		t.Fatalf("incorrect Safari record: %q %q %d %q %q %q", title, browser, micros, field, date, zone)
	}
	var n int
	if err := db.QueryRow("SELECT count(*) FROM browser_history WHERE url='' ").Scan(&n); err != nil || n != 1 {
		t.Fatalf("empty URL rows = %d, error: %v", n, err)
	}
	var dup int
	if err := db.QueryRow("SELECT duplicate_records FROM log WHERE id=2").Scan(&dup); err != nil || dup != 2 {
		t.Fatalf("duplicates = %d, error: %v", dup, err)
	}
	if err := os.WriteFile(input, []byte("title,url,1,bad date\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runWithLocation(input, dbPath, "Safari", loc, io.Discard); err == nil {
		t.Fatal("expected invalid date error")
	}
	if err := db.QueryRow("SELECT count(*) FROM log").Scan(&n); err != nil || n != 2 {
		t.Fatalf("log rows after failed import = %d, error: %v", n, err)
	}
}

func TestSafariLineEndings(t *testing.T) {
	data := "\"title with\rline and \"\"quotes\"\"\",https://a.example,1,\"Apr 23, 2016, 10:20 PM\"\rsecond,https://b.example,2,\"Apr 23, 2016, 10:21 PM\"\r\nthird,https://c.example,3,\"Apr 23, 2016, 10:22 PM\"\n"
	var visits []visit
	if err := readSafariCSV(strings.NewReader(data), time.UTC, func(v visit) error { visits = append(visits, v); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(visits) != 3 || visits[0].Title != "title with\rline and \"quotes\"" || visits[1].URL != "https://b.example" || visits[2].URL != "https://c.example" {
		t.Fatalf("unexpected rows: %+v", visits)
	}
}
