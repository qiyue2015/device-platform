package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"strings"

	_ "github.com/lib/pq"
	"github.com/qiyue2015/device-platform/internal/storage"
)

func main() {
	direction := flag.String("direction", "up", "migration direction: up or down")
	flag.Parse()
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is required")
		os.Exit(2)
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer db.Close()
	switch *direction {
	case "up":
		err = storage.ApplyMigrations(context.Background(), db)
	case "down":
		err = storage.RollbackLastMigration(context.Background(), db)
	default:
		err = fmt.Errorf("unsupported direction %q", *direction)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
