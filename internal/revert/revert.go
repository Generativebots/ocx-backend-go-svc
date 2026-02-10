package revert

import (
	"context"
	"fmt"
	"log/slog"
)

// UndoFunc is a closure that reverses a specific action
type UndoFunc func(ctx context.Context) error

type CompensationStack struct {
	TurnID string
	ops    []UndoFunc
}

func NewStack(turnID string) *CompensationStack {
	return &CompensationStack{
		TurnID: turnID,
		ops:    make([]UndoFunc, 0),
	}
}

// Push adds a compensating action to the stack (LIFO)
func (s *CompensationStack) Push(undo UndoFunc) {
	s.ops = append(s.ops, undo)
}

// Compensate executes the undo stack in reverse order (Last-In, First-Out)
func (s *CompensationStack) Compensate(ctx context.Context) error {
	slog.Info("Initiating compensation for Turn", "turn_i_d", s.TurnID)
	for i := len(s.ops) - 1; i >= 0; i-- {
		if err := s.ops[i](ctx); err != nil {
			return fmt.Errorf("compensation failed at step %d: %w", i, err)
		}
	}
	return nil
}
