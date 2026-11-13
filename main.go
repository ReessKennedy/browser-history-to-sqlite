package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type visit struct {
	FaviconURL     string `json:"favicon_url"`
	PageTransition string `json:"page_transition"`
	Title          string `json:"title"`
	URL            string `json:"url"`
	ClientID       string `json:"client_id"`
	TimeUsec       *int64 `json:"time_usec"`
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [-db history.sqlite] BrowserHistory.json\n", filepath.Base(os.Args[0]))
		flag.PrintDefaults()
	}
	dbPath := flag.String("db", "history.sqlite", "SQLite database path")
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(flag.Arg(0), *dbPath, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "Import failed:", err)
		os.Exit(1)
	}
}

func run(inputPath, dbPath string, output io.Writer) error {
	input, err := os.Open(inputPath)
	if err != nil {
		return fmt.Errorf("open input: %w", err)
	}
	defer input.Close()
	inputInfo, err := input.Stat()
	if err != nil {
		return err
	}
	if dbInfo, err := os.Stat(dbPath); err == nil && os.SameFile(inputInfo, dbInfo) {
		return errors.New("input and database must be different files")
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("check database: %w", err)
	}
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin import: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS browser_history (
 id INTEGER PRIMARY KEY, favicon_url TEXT, page_transition TEXT, title TEXT,
 url TEXT NOT NULL, client_id TEXT, time_usec INTEGER NOT NULL,
 UNIQUE(time_usec, url)
 );
 CREATE TABLE IF NOT EXISTS log (
 id INTEGER PRIMARY KEY, imported_at TEXT NOT NULL, source_file TEXT NOT NULL,
 new_records INTEGER NOT NULL, duplicate_records INTEGER NOT NULL
 );`)
	if err != nil {
		return fmt.Errorf("create tables: %w", err)
	}
	stmt, err := tx.Prepare(`INSERT INTO browser_history (favicon_url, page_transition, title, url, client_id, time_usec)
 VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT(time_usec, url) DO NOTHING`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()
	fmt.Fprintf(output, "Importing %s into %s...\n", inputPath, dbPath)
	added, duplicates := 0, 0
	err = readHistory(input, func(v visit) error {
		result, err := stmt.Exec(v.FaviconURL, v.PageTransition, v.Title, v.URL, v.ClientID, *v.TimeUsec)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			duplicates++
		} else {
			added++
		}
		if (added+duplicates)%10000 == 0 {
			fmt.Fprintf(output, "Processed %d records (%d new, %d duplicates)...\n", added+duplicates, added, duplicates)
		}
		return nil
	})
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO log (imported_at, source_file, new_records, duplicate_records) VALUES (?, ?, ?, ?)", time.Now().UTC().Format(time.RFC3339Nano), inputPath, added, duplicates)
	if err != nil {
		return fmt.Errorf("write import log: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit import: %w", err)
	}
	fmt.Fprintf(output, "Done: %d new records, %d duplicates skipped.\n", added, duplicates)
	return nil
}

// readHistory streams the history array, keeping only one visit in memory.
func readHistory(r io.Reader, insert func(visit) error) error {
	dec := json.NewDecoder(r)
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return fmt.Errorf("expected a JSON object at the root")
	}
	found := false
	for dec.More() {
		token, err = dec.Token()
		if err != nil {
			return fmt.Errorf("read JSON key: %w", err)
		}
		key, ok := token.(string)
		if !ok {
			return errors.New("invalid JSON key")
		}
		if key != "Browser History" {
			if err := skipValue(dec); err != nil {
				return err
			}
			continue
		}
		if found {
			return errors.New("duplicate Browser History field")
		}
		found = true
		token, err = dec.Token()
		if err != nil || token != json.Delim('[') {
			return errors.New("Browser History must be an array")
		}
		for i := 1; dec.More(); i++ {
			var v visit
			if err := dec.Decode(&v); err != nil {
				return fmt.Errorf("read record %d: %w", i, err)
			}
			if v.URL == "" || v.TimeUsec == nil {
				return fmt.Errorf("record %d must have a URL and time_usec", i)
			}
			if err := insert(v); err != nil {
				return fmt.Errorf("insert record %d: %w", i, err)
			}
		}
		if _, err := dec.Token(); err != nil {
			return fmt.Errorf("finish history array: %w", err)
		}
	}
	if _, err := dec.Token(); err != nil {
		return fmt.Errorf("finish JSON object: %w", err)
	}
	if !found {
		return errors.New("missing Browser History array")
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			return errors.New("unexpected data after JSON object")
		}
		return fmt.Errorf("read end of JSON: %w", err)
	}
	return nil
}

func skipValue(dec *json.Decoder) error {
	token, err := dec.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	if delim != '{' && delim != '[' {
		return errors.New("invalid JSON value")
	}
	for dec.More() {
		if delim == '{' {
			if _, err := dec.Token(); err != nil {
				return err
			}
		}
		if err := skipValue(dec); err != nil {
			return err
		}
	}
	_, err = dec.Token()
	return err
}
