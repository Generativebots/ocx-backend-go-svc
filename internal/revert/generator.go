package revert

import (
	"context"
	"log"
	"os"
)

// Generator builds the logic to undo specific environmental changes
type Generator struct{}

// UndoFileCreation prepares the deletion of a file if the turn is aborted
func (g *Generator) UndoFileCreation(path string) UndoFunc {
	return func(ctx context.Context) error {
		if _, err := os.Stat(path); err == nil {
			log.Printf("Reverting: Deleting speculative file %s", path)
			return os.Remove(path)
		}
		return nil
	}
}

// UndoDatabaseUpdate prepares a row restoration
// Note: This relies on a hypothetical DBLayer interface. Mocked here.
func (g *Generator) UndoDatabaseUpdate(rowID string, preState []byte) UndoFunc {
	return func(ctx context.Context) error {
		log.Printf("Reverting: Restoring row %s to pre-speculation state", rowID)
		// db.UpdateRow(ctx, rowID, preState)
		return nil
	}
}
