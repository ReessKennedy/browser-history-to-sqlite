# Browser history to SQLite

Import browser history into SQLite. Supported formats are Google Takeout Chrome JSON and the particular headerless Safari CSV export used for this project. Both formats are streamed one record at a time.

## Build and run

Install Go and a C compiler (the SQLite driver uses CGO), then:

```sh
go build -o browser-history-to-sqlite .
./browser-history-to-sqlite
```

Enter the database path, select the export format, and enter the export file path. Paths may contain spaces or be surrounded by matching quotes. Safari dates have no time zone: enter the IANA time zone used when the history was recorded (for example `America/New_York`), or press Enter to use the computer's local time zone. The tool reports progress every 10,000 records and prints final counts.

## Data and duplicate handling

The `browser_history` table stores normalized visit fields and the source browser. `import_id` references the `log` entry that first added a visit. Imports are transactional: errors roll back both visits and the log entry. A matching URL and `time_usec` is treated as a duplicate across all browsers; duplicates retain their original import ID.

The Safari CSV has four fields: title, URL, a numeric third field whose meaning is not yet confirmed, and a local date and time. The `safari_csv_details` table preserves that third field, the original date string, and the chosen time zone for each newly imported Safari row. The sample includes a row with an empty URL, which is preserved. Safari dates have only minute precision, so their normalized timestamps have zero seconds and microseconds. This can cause distinct visits to the same URL in the same minute to be treated as duplicates, or match a Chrome visit at exactly that minute. Ambiguous local times around daylight-saving changes cannot be uniquely resolved from the export alone.

Use a new database if importing from a version that predates the `browser` and `import_id` columns. Existing databases with those columns can be used for both export formats.

## To do

Browsers can produce multiple export formats. Add support as representative exports become available; eventually consider asking for the browser and detecting the file format automatically. For now, select each supported browser and format explicitly.

Run `go test ./...` to check the importer.
