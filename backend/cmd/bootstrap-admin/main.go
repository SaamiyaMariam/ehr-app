// Command bootstrap-admin grants the Practice Administrator role to an
// existing user. It is the only way to create the first administrator: role
// assignment through the API is limited to administrators, so a fresh
// installation needs an operator with database access to start the chain.
//
// It is a local operator tool, not an endpoint. It never creates users or
// passwords and never prints secrets.
//
//	cd backend
//	set -a; source .env; set +a
//	go run ./cmd/bootstrap-admin --email person@example.com
//	go run ./cmd/bootstrap-admin --list
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func main() {
	email := flag.String("email", "", "email of an existing user to make a practice administrator")
	list := flag.Bool("list", false, "list the current practice administrators and exit")
	flag.Parse()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		fatal("DATABASE_URL is required (load backend/.env first)")
	}

	if !*list && strings.TrimSpace(*email) == "" {
		fatal("provide --email <user email> or --list")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		fatal("could not connect to the database")
	}
	defer conn.Close(ctx)

	if *list {
		printAdmins(ctx, conn)
		return
	}

	address := strings.ToLower(strings.TrimSpace(*email))

	var userID string
	var active bool

	err = conn.QueryRow(ctx, `SELECT id::text, is_active FROM users WHERE LOWER(email) = $1`, address).Scan(&userID, &active)
	if err == pgx.ErrNoRows {
		fatal("no user with that email exists; the person must sign up first")
	}
	if err != nil {
		fatal("could not look the user up")
	}

	if !active {
		fatal("that account is inactive")
	}

	tag, err := conn.Exec(
		ctx,
		`
		INSERT INTO user_roles (user_id, role_id)
		SELECT $1::uuid, id FROM roles WHERE key = 'practice_administrator'
		ON CONFLICT DO NOTHING
		`,
		userID,
	)
	if err != nil {
		fatal("could not assign the role")
	}

	if tag.RowsAffected() == 0 {
		fmt.Println("That user is already a practice administrator.")
	} else {
		fmt.Println("Practice Administrator role granted.")
	}

	printAdmins(ctx, conn)
}

func printAdmins(ctx context.Context, conn *pgx.Conn) {
	rows, err := conn.Query(
		ctx,
		`
		SELECT u.email, u.first_name || ' ' || u.last_name, u.is_active
		FROM user_roles ur
		JOIN roles ro ON ro.id = ur.role_id
		JOIN users u ON u.id = ur.user_id
		WHERE ro.key = 'practice_administrator'
		ORDER BY u.email
		`,
	)
	if err != nil {
		fatal("could not list administrators")
	}
	defer rows.Close()

	fmt.Println("Practice administrators:")

	count := 0

	for rows.Next() {
		var email, name string
		var active bool
		if err := rows.Scan(&email, &name, &active); err != nil {
			fatal("could not read administrators")
		}

		state := ""
		if !active {
			state = " (inactive)"
		}

		fmt.Printf("  %s  %s%s\n", email, name, state)
		count++
	}

	if count == 0 {
		fmt.Println("  (none)")
	}
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, "bootstrap-admin:", message)
	os.Exit(1)
}
