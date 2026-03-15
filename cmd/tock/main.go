package main

import (
	_ "github.com/doug-martin/goqu/v9/dialect/sqlite3" // register the goqu sqlite3 dialect
	_ "github.com/mattn/go-sqlite3"                    // register the sqlite3 database driver

	"github.com/kriuchkov/tock/internal/adapters/cli"
)

func main() {
	cli.Execute()
}
