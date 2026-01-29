package gvisor

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver
)

// DatabaseStateManager handles PostgreSQL savepoint operations
type DatabaseStateManager struct {
	db *sql.DB
}

// NewDatabaseStateManager creates a new database state manager
func NewDatabaseStateManager(dbURL string) (*DatabaseStateManager, error) {
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &DatabaseStateManager{
		db: db,
	}, nil
}

// CreateSavepoint creates a PostgreSQL savepoint for state isolation
func (dsm *DatabaseStateManager) CreateSavepoint(ctx context.Context, savepointName string) (*sql.Tx, error) {
	// Begin transaction
	tx, err := dsm.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelSerializable, // Highest isolation level
	})
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}

	// Create savepoint
	query := fmt.Sprintf("SAVEPOINT %s", savepointName)
	if _, err := tx.ExecContext(ctx, query); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create savepoint: %w", err)
	}

	log.Printf("📸 Created database savepoint: %s", savepointName)

	return tx, nil
}

// RollbackToSavepoint rolls back to a specific savepoint
func (dsm *DatabaseStateManager) RollbackToSavepoint(ctx context.Context, tx *sql.Tx, savepointName string) error {
	query := fmt.Sprintf("ROLLBACK TO SAVEPOINT %s", savepointName)
	if _, err := tx.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("failed to rollback to savepoint: %w", err)
	}

	// Release savepoint after rollback
	releaseQuery := fmt.Sprintf("RELEASE SAVEPOINT %s", savepointName)
	if _, err := tx.ExecContext(ctx, releaseQuery); err != nil {
		return fmt.Errorf("failed to release savepoint: %w", err)
	}

	// Rollback transaction
	if err := tx.Rollback(); err != nil {
		return fmt.Errorf("failed to rollback transaction: %w", err)
	}

	log.Printf("⏪ Rolled back to savepoint: %s", savepointName)

	return nil
}

// CommitSavepoint commits changes from a savepoint
func (dsm *DatabaseStateManager) CommitSavepoint(ctx context.Context, tx *sql.Tx, savepointName string) error {
	// Release savepoint
	query := fmt.Sprintf("RELEASE SAVEPOINT %s", savepointName)
	if _, err := tx.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("failed to release savepoint: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Printf("✅ Committed savepoint: %s", savepointName)

	return nil
}

// CloneAgentState creates a snapshot of agent-specific database state
func (dsm *DatabaseStateManager) CloneAgentState(ctx context.Context, tx *sql.Tx, agentID string, savepointName string) (map[string]interface{}, error) {
	state := make(map[string]interface{})

	// Query agent data
	var agentData struct {
		ID         string
		Name       string
		Reputation int64
		Balance    int64
	}

	query := `SELECT id, name, reputation, balance FROM agents WHERE id = $1`
	err := tx.QueryRowContext(ctx, query, agentID).Scan(
		&agentData.ID,
		&agentData.Name,
		&agentData.Reputation,
		&agentData.Balance,
	)

	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to query agent state: %w", err)
	}

	state["agent"] = agentData
	state["savepoint"] = savepointName
	state["timestamp"] = time.Now().Unix()

	return state, nil
}

// Close closes the database connection
func (dsm *DatabaseStateManager) Close() error {
	return dsm.db.Close()
}

// Example usage
func ExampleDatabaseStateManager() {
	dsm, err := NewDatabaseStateManager("postgres://user:pass@localhost/ocx?sslmode=disable")
	if err != nil {
		log.Fatalf("Failed to create database state manager: %v", err)
	}
	defer dsm.Close()

	ctx := context.Background()

	// Create savepoint
	tx, err := dsm.CreateSavepoint(ctx, "sp_tx_12345")
	if err != nil {
		log.Fatalf("Failed to create savepoint: %v", err)
	}

	// Clone agent state
	state, err := dsm.CloneAgentState(ctx, tx, "AGENT_001", "sp_tx_12345")
	if err != nil {
		log.Fatalf("Failed to clone agent state: %v", err)
	}

	fmt.Printf("Cloned state: %+v\n", state)

	// Later: Rollback or Commit
	// dsm.RollbackToSavepoint(ctx, tx, "sp_tx_12345")
	// dsm.CommitSavepoint(ctx, tx, "sp_tx_12345")
}
