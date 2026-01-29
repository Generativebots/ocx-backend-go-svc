package ledger

import (
	"context"
	"log"

	"github.com/ocx/backend/pb"

	"google.golang.org/protobuf/types/known/timestamppb"
)

type AuditLogger struct {
	// We use the interface so we can plug in the real gRPC client or a mock
	client pb.LedgerServiceClient
}

// NewAuditLogger handles DI
func NewAuditLogger(c pb.LedgerServiceClient) *AuditLogger {
	return &AuditLogger{client: c}
}

type TurnData struct {
	TurnID     string
	AgentID    string
	BinaryHash string
	Status     pb.LedgerEntry_TurnStatus
	IntentHash string
	ActualHash string
}

func (al *AuditLogger) LogTurn(ctx context.Context, data *TurnData) {
	// Use a background context or a worker pool to keep this non-blocking
	go func() {
		entry := &pb.LedgerEntry{
			TurnId:     data.TurnID,
			AgentId:    data.AgentID,
			BinaryHash: data.BinaryHash,
			Status:     data.Status,
			IntentHash: data.IntentHash,
			ActualHash: data.ActualHash,
			Timestamp:  timestamppb.Now(),
		}

		_, err := al.client.RecordEntry(context.Background(), entry)
		if err != nil {
			// Fallback: log to local disk if the Ledger service is down
			log.Printf("CRITICAL: Ledger unreachable: %v", err)
		}
	}()
}
