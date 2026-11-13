package main

import (
	"bytes"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if got, want := strings.Join(columns, ","), "id,page_transition,title,url,client_id,browser,time_usec,favicon_url"; got != want {
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
	if err := interactive(strings.NewReader("\n\""+dbPath+"\"\n2\n1\n'"+inputPath+"'\n"), &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Please enter a path.") || !strings.Contains(output.String(), "Please enter 1 for Chrome.") || !strings.Contains(output.String(), "1 new records") {
		t.Fatalf("unexpected output: %s", output.String())
	}
	if err := interactive(strings.NewReader(""), io.Discard); err == nil {
		t.Fatal("expected error on end of input")
	}
}
