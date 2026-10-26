// Package safeerror produces technical error details without echoing database
// server messages, which can contain query values or connection identifiers.
package safeerror

import (
	"errors"
	"fmt"

	mysqldriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	redisclient "github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"google.golang.org/grpc/status"
	sqlitedriver "modernc.org/sqlite"
)

// Details returns stable, non-sensitive details for known database errors.
// The boolean is false when err is not a recognized database error; callers
// may then display their own application-generated diagnostic.
func Details(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return "PostgreSQL response (SQLSTATE " + pgErr.Code + ")", true
	}
	var pgConnectErr *pgconn.ConnectError
	if errors.As(err, &pgConnectErr) {
		return "PostgreSQL connection failed", true
	}
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) {
		if mysqlErr.SQLState != [5]byte{} {
			return fmt.Sprintf("MySQL response (error %d, SQLSTATE %s)", mysqlErr.Number, string(mysqlErr.SQLState[:])), true
		}
		return fmt.Sprintf("MySQL response (error %d)", mysqlErr.Number), true
	}
	var mongoErr mongo.ServerError
	if errors.As(err, &mongoErr) {
		codes := mongoErr.ErrorCodes()
		if len(codes) == 0 {
			return "MongoDB server response (details hidden)", true
		}
		return fmt.Sprintf("MongoDB server response (error codes %v)", codes), true
	}
	var redisErr redisclient.Error
	if errors.As(err, &redisErr) {
		return "Redis server response (details hidden)", true
	}
	var sqliteErr *sqlitedriver.Error
	if errors.As(err, &sqliteErr) {
		return fmt.Sprintf("SQLite response (error code %d)", sqliteErr.Code()), true
	}
	if grpcErr, ok := status.FromError(err); ok {
		return "etcd service response (" + grpcErr.Code().String() + ")", true
	}
	return "", false
}

// Display returns sanitized details for recognized database errors and the
// original text for application-generated errors without a sensitive payload.
func Display(err error) string {
	if err == nil {
		return ""
	}
	if details, ok := Details(err); ok {
		return details
	}
	return err.Error()
}
