# Browser history to SQLite

Import Google Takeout's `BrowserHistory.json` into SQLite. The JSON is read one visit at a time, so large exports do not need to fit in memory.

## Build and run

Install Go and a C compiler (the SQLite driver uses CGO), then:

```sh
go build -o browser-history-to-sqlite .
./browser-history-to-sqlite
```

Enter the SQLite database path first (an existing database or a new file), then select the browser export type, and enter the path to the Google Takeout JSON file. Chrome is currently the only supported browser. Paths may contain spaces; pasted paths can also be surrounded by matching single or double quotes. The tool reports progress every 10,000 records and prints the final counts. Run it again on the same file to skip previously imported visits.

The `browser_history` table stores each visit's fields and its source browser. Create a new database if you previously imported with an earlier version that did not include the browser column. A visit is considered a duplicate when both its URL and `time_usec` match an existing record. The `log` table records the UTC import time, source path, and new and duplicate counts for each successful import. Each import is a transaction: an error rolls back its visits and log entry.

Run `go test ./...` to check the importer.
