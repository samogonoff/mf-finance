package auth

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestExistingByEmailRowsErrorFailsClosedOnAmbiguity(t *testing.T) {
	if !errors.Is(existingByEmailRowsError(false, false), pgx.ErrNoRows) {
		t.Fatal("missing subject must remain not-found")
	}
	if err := existingByEmailRowsError(true, true); err == nil || err.Error() != "ambiguous user email" {
		t.Fatal("duplicate case-insensitive subjects must be rejected")
	}
	if err := existingByEmailRowsError(true, false); err != nil {
		t.Fatal(err)
	}
}
