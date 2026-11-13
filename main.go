package main

import (
	"bufio"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
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
	SafariField3   string `json:"-"`
	SafariDate     string `json:"-"`
}

func main() {
	if err := interactive(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "Import failed:", err)
		os.Exit(1)
	}
}

func interactive(input io.Reader, output io.Writer) error {
	reader := bufio.NewReader(input)
	dbPath, err := promptPath(reader, output, "SQLite database path (create or update)")
	if err != nil {
		return err
	}
	browser, err := promptBrowser(reader, output)
	if err != nil {
		return err
	}
	label := "Chrome JSON export file path"
	if browser == "Safari" {
		label = "Safari CSV export file path"
	}
	inputPath, err := promptPath(reader, output, label)
	if err != nil {
		return err
	}
	location := time.Local
	if browser == "Safari" {
		location, err = promptLocation(reader, output)
		if err != nil {
			return err
		}
	}
	return runWithLocation(inputPath, dbPath, browser, location, output)
}

func promptBrowser(reader *bufio.Reader, output io.Writer) (string, error) {
	fmt.Fprintln(output, "Browser export type:\n  1. Chrome JSON export\n  2. Safari CSV export")
	for {
		fmt.Fprint(output, "Choose an export type (1 or 2): ")
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("read browser: %w", err)
		}
		switch strings.TrimSpace(line) {
		case "1":
			return "Chrome", nil
		case "2":
			return "Safari", nil
		}
		if err == io.EOF {
			return "", errors.New("input ended before a browser was selected")
		}
		fmt.Fprintln(output, "Please enter 1 or 2.")
	}
}

func promptLocation(reader *bufio.Reader, output io.Writer) (*time.Location, error) {
	fmt.Fprintln(output, "Safari dates have no time zone. Enter the time zone they were recorded in (for example, America/New_York).")
	for {
		fmt.Fprint(output, "Time zone [local]: ")
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, fmt.Errorf("read time zone: %w", err)
		}
		name := strings.TrimSpace(line)
		if name == "" {
			return time.Local, nil
		}
		location, loadErr := time.LoadLocation(name)
		if loadErr == nil {
			return location, nil
		}
		if err == io.EOF {
			return nil, fmt.Errorf("unknown time zone %q", name)
		}
		fmt.Fprintln(output, "Unknown time zone; try an IANA name such as America/New_York.")
	}
}

func promptPath(reader *bufio.Reader, output io.Writer, label string) (string, error) {
	for {
		fmt.Fprintf(output, "%s: ", label)
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("read path: %w", err)
		}
		path := strings.TrimSpace(line)
		// Accept paths pasted with matching quotes, as well as paths containing spaces.
		if len(path) >= 2 && ((path[0] == '\'' && path[len(path)-1] == '\'') || (path[0] == '"' && path[len(path)-1] == '"')) {
			path = path[1 : len(path)-1]
		}
		if path != "" {
			return path, nil
		}
		if err == io.EOF {
			return "", errors.New("input ended before a path was provided")
		}
		fmt.Fprintln(output, "Please enter a path.")
	}
}

func run(inputPath, dbPath, browser string, output io.Writer) error {
	return runWithLocation(inputPath, dbPath, browser, time.Local, output)
}

func runWithLocation(inputPath, dbPath, browser string, location *time.Location, output io.Writer) error {
	if browser != "Chrome" && browser != "Safari" {
		return fmt.Errorf("unsupported browser: %s", browser)
	}
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
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return fmt.Errorf("enable foreign keys: %w", err)
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin import: %w", err)
	}
	defer tx.Rollback()
	_, err = tx.Exec(`CREATE TABLE IF NOT EXISTS browser_history (
 id INTEGER PRIMARY KEY, page_transition TEXT, title TEXT, url TEXT NOT NULL,
 client_id TEXT, browser TEXT NOT NULL, time_usec INTEGER NOT NULL, favicon_url TEXT,
 import_id INTEGER NOT NULL REFERENCES log(id),
 UNIQUE(time_usec, url)
 );
 CREATE TABLE IF NOT EXISTS log (
 id INTEGER PRIMARY KEY, imported_at TEXT NOT NULL, source_file TEXT NOT NULL,
 new_records INTEGER NOT NULL, duplicate_records INTEGER NOT NULL
 );
 CREATE TABLE IF NOT EXISTS safari_csv_details (
 history_id INTEGER PRIMARY KEY REFERENCES browser_history(id),
 source_field_3 TEXT NOT NULL, source_date TEXT NOT NULL, source_timezone TEXT NOT NULL
 );`)
	if err != nil {
		return fmt.Errorf("create tables: %w", err)
	}
	logResult, err := tx.Exec("INSERT INTO log (imported_at, source_file, new_records, duplicate_records) VALUES (?, ?, 0, 0)", time.Now().UTC().Format(time.RFC3339Nano), inputPath)
	if err != nil {
		return fmt.Errorf("start import log: %w", err)
	}
	importID, err := logResult.LastInsertId()
	if err != nil {
		return fmt.Errorf("get import ID: %w", err)
	}
	stmt, err := tx.Prepare(`INSERT INTO browser_history (favicon_url, page_transition, title, url, client_id, browser, time_usec, import_id)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(time_usec, url) DO NOTHING`)
	if err != nil {
		return fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()
	fmt.Fprintf(output, "Importing %s into %s...\n", inputPath, dbPath)
	added, duplicates := 0, 0
	insert := func(v visit) error {
		result, err := stmt.Exec(v.FaviconURL, v.PageTransition, v.Title, v.URL, v.ClientID, browser, *v.TimeUsec, importID)
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
			if browser == "Safari" {
				id, err := result.LastInsertId()
				if err != nil {
					return err
				}
				if _, err := tx.Exec("INSERT INTO safari_csv_details (history_id, source_field_3, source_date, source_timezone) VALUES (?, ?, ?, ?)", id, v.SafariField3, v.SafariDate, location.String()); err != nil {
					return err
				}
			}
		}
		if (added+duplicates)%10000 == 0 {
			fmt.Fprintf(output, "Processed %d records (%d new, %d duplicates)...\n", added+duplicates, added, duplicates)
		}
		return nil
	}
	if browser == "Safari" {
		err = readSafariCSV(input, location, insert)
	} else {
		err = readHistory(input, insert)
	}
	if err != nil {
		return err
	}
	_, err = tx.Exec("UPDATE log SET new_records = ?, duplicate_records = ? WHERE id = ?", added, duplicates, importID)
	if err != nil {
		return fmt.Errorf("write import log: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit import: %w", err)
	}
	fmt.Fprintf(output, "Done: %d new records, %d duplicates skipped.\n", added, duplicates)
	return nil
}

// csvLineEndings converts old Mac CR record separators to LF while preserving
// carriage returns and newlines inside quoted fields.
type csvLineEndings struct {
	reader                         *bufio.Reader
	quoted, afterQuote, fieldStart bool
}

func (r *csvLineEndings) Read(p []byte) (int, error) {
	n := 0
	for n < len(p) {
		c, err := r.reader.ReadByte()
		if err != nil {
			if n > 0 {
				return n, nil
			}
			return 0, err
		}
		if r.quoted {
			if r.afterQuote {
				if c == '"' {
					r.afterQuote = false
					p[n] = c
					n++
					continue
				}
				r.quoted = false
				r.afterQuote = false
			} else {
				if c == '"' {
					r.afterQuote = true
				}
				p[n] = c
				n++
				continue
			}
		}
		if r.fieldStart && c == '"' {
			r.quoted = true
			r.fieldStart = false
		} else if c == ',' {
			r.fieldStart = true
		} else if c == '\r' {
			if next, err := r.reader.Peek(1); err == nil && next[0] == '\n' {
				_, _ = r.reader.ReadByte()
			}
			c = '\n'
			r.fieldStart = true
		} else if c == '\n' {
			r.fieldStart = true
		} else {
			r.fieldStart = false
		}
		p[n] = c
		n++
	}
	return n, nil
}

// readSafariCSV reads the observed headerless Safari export one row at a time.
func readSafariCSV(input io.Reader, location *time.Location, insert func(visit) error) error {
	reader := csv.NewReader(&csvLineEndings{reader: bufio.NewReader(input), fieldStart: true})
	reader.FieldsPerRecord = 4
	for row := 1; ; row++ {
		fields, err := reader.Read()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("read Safari CSV row %d: %w", row, err)
		}
		if row == 1 {
			fields[0] = strings.TrimPrefix(fields[0], "\ufeff")
		}
		timestamp, err := time.ParseInLocation("Jan 2, 2006, 3:04 PM", fields[3], location)
		if err != nil {
			return fmt.Errorf("parse Safari CSV row %d date %q: %w", row, fields[3], err)
		}
		micros := timestamp.UnixMicro()
		v := visit{Title: fields[0], URL: fields[1], TimeUsec: &micros, SafariField3: fields[2], SafariDate: fields[3]}
		if err := insert(v); err != nil {
			return fmt.Errorf("insert Safari CSV row %d: %w", row, err)
		}
	}
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
