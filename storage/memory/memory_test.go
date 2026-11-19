package memory

import (
	"log/slog"
	"testing"

	"github.com/dexidp/dex/v2/storage"
	"github.com/dexidp/dex/v2/storage/conformance"
)

func TestStorage(t *testing.T) {
	newStorage := func(t *testing.T) storage.Storage {
		logger := slog.New(slog.NewTextHandler(t.Output(), &slog.HandlerOptions{Level: slog.LevelDebug}))

		return New(logger)
	}
	conformance.RunTests(t, newStorage)
	conformance.RunConcurrencyTests(t, newStorage)
}
