package sqlretry_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/go-sql-driver/mysql"
	"github.com/lib/pq"

	"github.com/dexidp/dex/storage/sqlretry"
)

func TestIsSerializationFailure(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"pq serialization failure", &pq.Error{Code: "40001"}, true},
		{"pq deadlock detected", &pq.Error{Code: "40P01"}, true},
		{"pq unique violation", &pq.Error{Code: "23505"}, false},
		{"mysql deadlock", &mysql.MySQLError{Number: 1213}, true},
		{"mysql lock wait timeout", &mysql.MySQLError{Number: 1205}, true},
		{"mysql duplicate entry", &mysql.MySQLError{Number: 1062}, false},
		{"wrapped pq serialization failure", fmt.Errorf("update: %w", &pq.Error{Code: "40001"}), true},
		{"plain error", errors.New("connection refused"), false},
		{"nil error", nil, false},
	}

	for _, tt := range tests {
		if got := sqlretry.IsSerializationFailure(tt.err); got != tt.want {
			t.Errorf("%s: IsSerializationFailure() = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestDoRetriesUntilSuccess(t *testing.T) {
	attempts := 0
	err := sqlretry.Do(
		func() error {
			attempts++
			if attempts < 3 {
				return &pq.Error{Code: "40001"}
			}
			return nil
		},
		sqlretry.IsSerializationFailure,
		nil,
	)
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts (2 retries), got %d", attempts)
	}
}

func TestDoGivesUpAfterMaxRetries(t *testing.T) {
	attempts := 0
	sentinel := &pq.Error{Code: "40P01"}
	err := sqlretry.Do(
		func() error {
			attempts++
			return sentinel
		},
		sqlretry.IsSerializationFailure,
		nil,
	)
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		t.Fatalf("expected the serialization failure to be returned, got: %v", err)
	}
	if want := sqlretry.MaxRetries + 1; attempts != want {
		t.Fatalf("expected %d attempts (1 + %d retries), got %d", want, sqlretry.MaxRetries, attempts)
	}
}

func TestDoDoesNotRetryNonRetryableError(t *testing.T) {
	attempts := 0
	sentinel := errors.New("permanent failure")
	err := sqlretry.Do(
		func() error {
			attempts++
			return sentinel
		},
		sqlretry.IsSerializationFailure,
		nil,
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error to be returned, got: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected exactly 1 attempt for non-retryable error, got %d", attempts)
	}
}

func TestDoNilRetryCheckDoesNotRetry(t *testing.T) {
	attempts := 0
	sentinel := &pq.Error{Code: "40001"}
	err := sqlretry.Do(
		func() error {
			attempts++
			return sentinel
		},
		nil,
		nil,
	)
	var pqErr *pq.Error
	if !errors.As(err, &pqErr) {
		t.Fatalf("expected the serialization failure to be returned, got: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected exactly 1 attempt with nil retry check, got %d", attempts)
	}
}

func TestDoCallsOnRetry(t *testing.T) {
	attempts := 0
	var retryErrors []error
	err := sqlretry.Do(
		func() error {
			attempts++
			if attempts < 2 {
				return &pq.Error{Code: "40001"}
			}
			return nil
		},
		sqlretry.IsSerializationFailure,
		func(attempt int, err error) {
			if attempt != 1 {
				t.Errorf("expected onRetry attempt 1, got %d", attempt)
			}
			retryErrors = append(retryErrors, err)
		},
	)
	if err != nil {
		t.Fatalf("expected success after retry, got: %v", err)
	}
	if len(retryErrors) != 1 {
		t.Fatalf("expected 1 onRetry call, got %d", len(retryErrors))
	}
}
