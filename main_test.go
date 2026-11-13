package main

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
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
		if err := run(input, dbPath, &out); err != nil {
			t.Fatal(err)
		}
	}
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var visits, logs, added, duplicates int
	if err := db.QueryRow("SELECT count(*) FROM browser_history").Scan(&visits); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*), sum(new_records), sum(duplicate_records) FROM log").Scan(&logs, &added, &duplicates); err != nil {
		t.Fatal(err)
	}
	if visits != 2 || logs != 2 || added != 2 || duplicates != 4 {
		t.Fatalf("visits=%d logs=%d added=%d duplicates=%d", visits, logs, added, duplicates)
	}
	if err := os.WriteFile(input, []byte(`{"Browser History":[{"url":"https://new.example","time_usec":456},`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(input, dbPath, &out); err == nil {
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
