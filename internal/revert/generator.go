package revert

import (
	"context"
	"log/slog"
	"os"
)

// Generator builds the logic to undo specific environmental changes
type Generator struct{}

// UndoFileCreation prepares the deletion of a file if the turn is aborted
func (g *Generator) UndoFileCreation(path string) UndoFunc {
	return func(ctx context.Context) error {
		if _, err := os.Stat(path); err == nil {
			slog.Info("Reverting: Deleting speculative file", "path", path)
			return os.Remove(path)
		}
		return nil
	}
}

// UndoDatabaseUpdate prepares a row restoration
// Note: This relies on a hypothetical DBLayer interface. Mocked here.
func (g *Generator) UndoDatabaseUpdate(rowID string, preState []byte) UndoFunc {
	return func(ctx context.Context) error {
		slog.Info("Reverting: Restoring row to pre-speculation state", "row_i_d", rowID)
		// db.UpdateRow(ctx, rowID, preState)
		return nil
	}
}
