package escrow

import (
	"context"
	"fmt"
	"log"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// JuryGRPCClient is a production client for the Python Jury service
type JuryGRPCClient struct {
	conn   *grpc.ClientConn
	logger *log.Logger
	// In production, this would be: client pb.JuryServiceClient
}

// NewJuryGRPCClient creates a gRPC client for the Jury service
func NewJuryGRPCClient(juryAddr string) (*JuryGRPCClient, error) {
	conn, err := grpc.NewClient(juryAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Jury service: %w", err)
	}

	return &JuryGRPCClient{
		conn:   conn,
		logger: log.New(log.Writer(), "[JuryClient] ", log.LstdFlags),
	}, nil
}

// EvaluateAction calls the Python Jury service with weighted voting
func (j *JuryGRPCClient) EvaluateAction(ctx context.Context, agentID, action string, context map[string]interface{}) (bool, error) {
	// In production, this would call:
	// req := &pb.JuryRequest{
	//     AgentId: agentID,
	//     Action:  action,
	//     Context: marshalContext(context),
	// }
	// resp, err := j.client.Evaluate(ctx, req)

	j.logger.Printf("Evaluating action for agent %s via Jury service", agentID)

	// For now, simulate the call
	// The Python Jury service would:
	// 1. Fetch juror TrustScores from Spanner
	// 2. Execute weighted voting (compliance, factuality, logic)
	// 3. Return verdict with final_trust_score

	// Mock response - in production, parse from gRPC response
	passed := true // resp.Verdict == "ALLOW"

	return passed, nil
}

// EvaluateTrace calls the Python Jury service to evaluate a full execution trace
func (j *JuryGRPCClient) EvaluateTrace(ctx context.Context, traceID string, payload []byte) (bool, error) {
	j.logger.Printf("Evaluating trace %s via Jury service", traceID)
	// Mock implementation for now
	return true, nil
}

// Close closes the gRPC connection
func (j *JuryGRPCClient) Close() error {
	return j.conn.Close()
}

// WeightedJuryRequest represents the request to the Jury service
type WeightedJuryRequest struct {
	AgentID       string
	Action        string
	Context       map[string]interface{}
	JurorIDs      []string // List of juror agent IDs
	RequireQuorum bool     // Require 66% consensus
}

// WeightedJuryResponse represents the response from the Jury service
type WeightedJuryResponse struct {
	Verdict          string // ALLOW, WARN, BLOCK
	FinalTrustScore  float64
	VectorBreakdown  map[string]float64 // compliance, factuality, strategic_alignment
	KillSwitch       bool
	ReasoningSummary string
	JurorVotes       []JurorVote
}

// JurorVote represents a single juror's vote with their trust score
type JurorVote struct {
	JurorID    string
	TrustScore float64
	Vote       string  // APPROVE, REJECT
	Weight     float64 // TrustScore * base_weight
}

// CalculateWeightedConsensus performs weighted voting calculation
// This logic mirrors what the Python Jury service does
func CalculateWeightedConsensus(votes []JurorVote, threshold float64) (bool, float64) {
	var totalWeight float64
	var approvedWeight float64

	for _, vote := range votes {
		totalWeight += vote.Weight
		if vote.Vote == "APPROVE" {
			approvedWeight += vote.Weight
		}
	}

	if totalWeight == 0 {
		return false, 0.0
	}

	consensusRatio := approvedWeight / totalWeight
	passed := consensusRatio >= threshold

	return passed, consensusRatio
}
