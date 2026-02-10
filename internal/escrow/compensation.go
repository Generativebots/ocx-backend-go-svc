// Package escrow — Compensation Stack (Patent §9)
//
// Implements: "Automatic rollback of side effects when governance rejects
// an action mid-flight."
//
// When an action enters the governance pipeline, side-effect operations
// (micropayment holds, JIT entitlements, etc.) register compensating
// actions on a per-transaction stack. If the final verdict is BLOCK,
// the stack is executed in LIFO order to undo all side effects.
// On ALLOW, the stack is cleared (committed).
package escrow

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// CompensationConfig configures the compensation stack behavior.
// T11 fix: Adds timeout, retry, and dead-letter support.
type CompensationConfig struct {
	Timeout    time.Duration // Max time per undo operation (default: 5s)
	MaxRetries int           // Max retry attempts for failed undos (default: 3)
	RetryDelay time.Duration // Delay between retries (default: 500ms)
}

// CompensationStack manages rollback functions for speculative side-effects.
// Each transaction gets its own LIFO stack of undo operations.
type CompensationStack struct {
	mu         sync.Mutex
	stacks     map[string][]CompensationEntry // txID -> LIFO stack
	deadLetter []DeadLetterEntry              // T11: failed compensations after retries
	logger     *log.Logger
	config     CompensationConfig
}

// CompensationEntry represents a single compensating action.
type CompensationEntry struct {
	ID           string
	TxID         string
	Description  string
	UndoFn       func() error
	RegisteredAt time.Time
}

// CompensationResult captures the outcome of executing a single undo.
type CompensationResult struct {
	EntryID     string `json:"entry_id"`
	Description string `json:"description"`
	Success     bool   `json:"success"`
	Error       string `json:"error,omitempty"`
	ExecutedAt  string `json:"executed_at"`
	Retries     int    `json:"retries,omitempty"`
}

// DeadLetterEntry represents a compensation that failed after all retries.
// T11 fix: Operator can review and manually remediate.
type DeadLetterEntry struct {
	EntryID     string    `json:"entry_id"`
	TxID        string    `json:"tx_id"`
	Description string    `json:"description"`
	LastError   string    `json:"last_error"`
	Attempts    int       `json:"attempts"`
	FailedAt    time.Time `json:"failed_at"`
}

// NewCompensationStack creates a new compensation stack with configurable behavior.
func NewCompensationStack() *CompensationStack {
	return NewCompensationStackWithConfig(CompensationConfig{})
}

// NewCompensationStackWithConfig creates a compensation stack with explicit config.
func NewCompensationStackWithConfig(cfg CompensationConfig) *CompensationStack {
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryDelay == 0 {
		cfg.RetryDelay = 500 * time.Millisecond
	}
	return &CompensationStack{
		stacks:     make(map[string][]CompensationEntry),
		deadLetter: make([]DeadLetterEntry, 0),
		logger:     log.New(log.Writer(), "[CompensationStack] ", log.LstdFlags),
		config:     cfg,
	}
}

// Push registers a compensating action for a transaction.
// Actions are executed in LIFO order when Execute() is called.
func (cs *CompensationStack) Push(txID, description string, undoFn func() error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	entry := CompensationEntry{
		ID:           fmt.Sprintf("comp-%s-%d", txID, len(cs.stacks[txID])),
		TxID:         txID,
		Description:  description,
		UndoFn:       undoFn,
		RegisteredAt: time.Now(),
	}

	cs.stacks[txID] = append(cs.stacks[txID], entry)
	cs.logger.Printf("📌 Pushed compensation for tx=%s: %s (depth=%d)",
		txID, description, len(cs.stacks[txID]))
}

// Execute runs all compensating actions for a transaction in LIFO order.
// T11 fix: Each undo runs with a timeout and is retried up to MaxRetries times.
// Failed-after-retry entries are sent to the dead-letter log.
func (cs *CompensationStack) Execute(txID string) []CompensationResult {
	cs.mu.Lock()
	stack, exists := cs.stacks[txID]
	if !exists || len(stack) == 0 {
		cs.mu.Unlock()
		return nil
	}
	// Take ownership and remove from map
	delete(cs.stacks, txID)
	cs.mu.Unlock()

	cs.logger.Printf("🔄 Executing %d compensations for tx=%s (LIFO order)", len(stack), txID)

	var results []CompensationResult

	// Execute in reverse order (LIFO)
	for i := len(stack) - 1; i >= 0; i-- {
		entry := stack[i]
		result := cs.executeWithRetry(entry)
		results = append(results, result)
	}

	cs.logger.Printf("🏁 Compensation complete for tx=%s: %d/%d succeeded",
		txID, countSuccesses(results), len(results))

	return results
}

// executeWithRetry runs a single compensation entry with timeout and retries.
func (cs *CompensationStack) executeWithRetry(entry CompensationEntry) CompensationResult {
	result := CompensationResult{
		EntryID:     entry.ID,
		Description: entry.Description,
		ExecutedAt:  time.Now().Format(time.RFC3339),
	}

	var lastErr error
	for attempt := 0; attempt <= cs.config.MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(cs.config.RetryDelay)
			cs.logger.Printf("🔁 Retry %d/%d for compensation %s: %s",
				attempt, cs.config.MaxRetries, entry.ID, entry.Description)
		}

		// Run with timeout
		ctx, cancel := context.WithTimeout(context.Background(), cs.config.Timeout)
		errCh := make(chan error, 1)
		go func() {
			errCh <- entry.UndoFn()
		}()

		select {
		case err := <-errCh:
			cancel()
			if err == nil {
				result.Success = true
				result.Retries = attempt
				cs.logger.Printf("✅ Compensation succeeded for tx=%s: %s (attempt %d)",
					entry.TxID, entry.Description, attempt+1)
				return result
			}
			lastErr = err
		case <-ctx.Done():
			cancel()
			lastErr = fmt.Errorf("timeout after %v", cs.config.Timeout)
		}
	}

	// All retries exhausted — dead-letter
	result.Success = false
	result.Error = lastErr.Error()
	result.Retries = cs.config.MaxRetries + 1
	cs.logger.Printf("❌ Compensation DEAD-LETTERED for tx=%s: %s — %v",
		entry.TxID, entry.Description, lastErr)

	cs.mu.Lock()
	cs.deadLetter = append(cs.deadLetter, DeadLetterEntry{
		EntryID:     entry.ID,
		TxID:        entry.TxID,
		Description: entry.Description,
		LastError:   lastErr.Error(),
		Attempts:    cs.config.MaxRetries + 1,
		FailedAt:    time.Now(),
	})
	cs.mu.Unlock()

	return result
}

// Clear removes all compensating actions for a transaction without executing them.
// Called when a verdict is ALLOW to commit the speculative side-effects.
func (cs *CompensationStack) Clear(txID string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	depth := len(cs.stacks[txID])
	delete(cs.stacks, txID)

	if depth > 0 {
		cs.logger.Printf("✅ Cleared %d compensations for tx=%s (committed)", depth, txID)
	}
}

// GetPending returns the number of pending compensations per transaction.
func (cs *CompensationStack) GetPending() map[string]int {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	pending := make(map[string]int, len(cs.stacks))
	for txID, stack := range cs.stacks {
		pending[txID] = len(stack)
	}
	return pending
}

// TotalPending returns the total number of pending compensations across all txs.
func (cs *CompensationStack) TotalPending() int {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	total := 0
	for _, stack := range cs.stacks {
		total += len(stack)
	}
	return total
}

func countSuccesses(results []CompensationResult) int {
	n := 0
	for _, r := range results {
		if r.Success {
			n++
		}
	}
	return n
}
