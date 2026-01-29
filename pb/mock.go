package pb

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Ledger Types
type LedgerEntry_Status int32
type LedgerEntry_TurnStatus int32

const (
	LedgerEntry_COMMITTED   LedgerEntry_TurnStatus = 0
	LedgerEntry_COMPENSATED LedgerEntry_TurnStatus = 1
)

type TurnData struct {
	TurnID     string
	AgentID    string
	Status     LedgerEntry_TurnStatus
	IntentHash string
	ActualHash string
}

type LedgerEntry struct {
	TurnId     string
	AgentId    string
	BinaryHash string
	Status     LedgerEntry_TurnStatus
	IntentHash string
	ActualHash string
	Timestamp  *timestamppb.Timestamp
}

type LedgerServiceClient interface {
	RecordTurn(ctx context.Context, in *TurnData, opts ...grpc.CallOption) (*TurnData, error)
	RecordEntry(ctx context.Context, in *LedgerEntry, opts ...grpc.CallOption) (*LedgerEntry, error)
}

type MockLedgerClient struct{}

func (m *MockLedgerClient) RecordTurn(ctx context.Context, in *TurnData, opts ...grpc.CallOption) (*TurnData, error) {
	return in, nil
}

func (m *MockLedgerClient) RecordEntry(ctx context.Context, in *LedgerEntry, opts ...grpc.CallOption) (*LedgerEntry, error) {
	return in, nil
}

// Plan/Arbitrator Types
type NegotiationTurn struct {
	AgentId   string
	Action    string
	RiskScore float64
	Payload   []byte // Added Payload field
}

type ExecutionPlan struct {
	PlanId               string
	AgentId              string
	Steps                []*PlanStep
	RiskLevel            string
	AllowedCalls         []string
	ExpectedOutcomeHash  string
	ManualReviewRequired bool
}

type PlanStep struct {
	Action string
}

type ActionResponse struct {
	Success bool
	Message string
}

// Service Interfaces
type PlanServiceServer interface {
	SubmitPlan(context.Context, *ExecutionPlan) (*ActionResponse, error)
	Negotiate(context.Context, *NegotiationTurn) (*NegotiationTurn, error)
}

type UnimplementedPlanServiceServer struct{}

func (u *UnimplementedPlanServiceServer) SubmitPlan(context.Context, *ExecutionPlan) (*ActionResponse, error) {
	return nil, nil
}

func (u *UnimplementedPlanServiceServer) Negotiate(context.Context, *NegotiationTurn) (*NegotiationTurn, error) {
	return nil, nil
}

type NegotiationArbitrator_NegotiateServer interface {
	Send(*NegotiationTurn) error
	Recv() (*NegotiationTurn, error)
	grpc.ServerStream
}
