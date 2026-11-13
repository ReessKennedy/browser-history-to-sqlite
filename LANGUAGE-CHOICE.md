# Language choice

## Go — selected

Go has built-in streaming JSON support, so the importer can process large browser history exports without loading the entire file into memory or adding a JSON parser dependency. A compiled executable can also be distributed without requiring users to install Go. A SQLite driver will still be needed when building the tool.

## PHP — second choice

PHP is easy to install, and the author has plenty of experience with it. Streaming a large JSON export would likely require an additional library, such as [JSON Machine](https://github.com/halaxa/json-machine) or [JSON Streaming Parser](https://github.com/salsify/jsonstreamingparser). PHP would also need its SQLite extension enabled.

## Bash

- Bash has no built-in JSON parser; handling the export would require an external tool such as `jq`.
- SQLite access would require another tool, such as the `sqlite3` command-line client.
- Streaming, error handling, and transactional imports would be more cumbersome to implement and maintain.
