# Browser history to SQLite

Import Google Takeout's `BrowserHistory.json` into SQLite. The JSON is read one visit at a time, so large exports do not need to fit in memory.

## Build and run

Install Go and a C compiler (the SQLite driver uses CGO), then:

```sh
go build -o browser-history-to-sqlite .
./browser-history-to-sqlite -db history.sqlite /path/to/BrowserHistory.json
```

The `-db` option defaults to `history.sqlite` in the current directory. The tool reports progress every 10,000 records and prints the final counts. Run it again on the same file to skip previously imported visits.

The `browser_history` table stores each visit's fields. A visit is considered a duplicate when both its URL and `time_usec` match an existing record. The `log` table records the UTC import time, source path, and new and duplicate counts for each successful import. Each import is a transaction: an error rolls back its visits and log entry.

Run `go test ./...` to check the importer.
