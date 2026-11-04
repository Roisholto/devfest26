package payment

import (
	"fmt"
	"strings"
	"syscall"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestConnectionErrorsDoNotExposeCredentials(t *testing.T) {
	for _, err := range []error{
		&pgconn.PgError{Code: "28P01", Message: "secret-password"},
		&pgconn.PgError{Code: "3D000", Message: "secret-password"},
		fmt.Errorf("secret-password: %w", syscall.ECONNREFUSED),
		fmt.Errorf("cannot parse postgres://user:secret-password@host/db"),
	} {
		if strings.Contains(ConnectionError(err).Error(), "secret-password") {
			t.Fatal("connection diagnostic exposed upstream credentials")
		}
	}
}
