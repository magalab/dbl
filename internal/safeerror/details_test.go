package safeerror

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type privateRedisError string

func (privateRedisError) Error() string { return "ERR confidential-value" }
func (privateRedisError) RedisError()   {}

func TestDetailsRedactsServerMessagesAndConnectionSettings(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "PostgreSQL value-bearing constraint error",
			err: &pgconn.PgError{
				Code: "23505", Message: "duplicate key", Detail: "Key (email)=(private@example.test) already exists",
			},
			want: "SQLSTATE 23505",
		},
		{
			name: "PostgreSQL connection identity",
			err:  &pgconn.ConnectError{Config: &pgconn.Config{User: "private-user", Database: "private-db"}},
			want: "PostgreSQL connection failed",
		},
		{
			name: "MySQL server message",
			err:  &mysqldriver.MySQLError{Number: 1062, SQLState: [5]byte{'2', '3', '0', '0', '0'}, Message: "duplicate key private@example.test"},
			want: "error 1062, SQLSTATE 23000",
		},
		{
			name: "MongoDB duplicate value",
			err:  mongo.CommandError{Code: 11000, Message: "duplicate key private@example.test"},
			want: "error codes [11000]",
		},
		{
			name: "Redis response value",
			err:  privateRedisError("private"),
			want: "Redis server response (details hidden)",
		},
		{
			name: "etcd response value",
			err:  status.Error(codes.InvalidArgument, "private-key-value"),
			want: "etcd service response (InvalidArgument)",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.err
			if test.name != "PostgreSQL connection identity" {
				err = fmt.Errorf("adapter context: %w", err)
			}
			got, ok := Details(err)
			if displayed := Display(err); !ok || !strings.Contains(displayed, test.want) {
				t.Fatalf("Display() = %q; want text containing %q", displayed, test.want)
			}
			for _, sensitive := range []string{"private@example.test", "private-user", "private-db", "private-key-value", "duplicate key", "confidential-value"} {
				if strings.Contains(got, sensitive) {
					t.Fatalf("Details() leaked %q: %q", sensitive, got)
				}
			}
		})
	}
}

func TestDetailsLeavesApplicationErrorsForCaller(t *testing.T) {
	err := errors.New("unterminated SQL quoted value starting at byte 4")
	if got, ok := Details(err); ok || got != "" {
		t.Fatalf("Details() = %q, %v; want unrecognized application error", got, ok)
	}
	if got := Display(err); got != err.Error() {
		t.Fatalf("Display() = %q; want application diagnostic %q", got, err.Error())
	}
}

func TestDetailsRedactsSQLiteParserText(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec("SELECT private_value FROM missing_table")
	if err == nil {
		t.Fatal("query unexpectedly succeeded")
	}
	got, ok := Details(err)
	if !ok || !strings.HasPrefix(got, "SQLite response (error code ") {
		t.Fatalf("Details() = %q, %v; want a SQLite error code", got, ok)
	}
	if strings.Contains(got, "private_value") || strings.Contains(got, "missing_table") {
		t.Fatalf("SQLite parser details leaked submitted SQL identifiers: %q", got)
	}
}
