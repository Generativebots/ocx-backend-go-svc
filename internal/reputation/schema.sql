-- Cloud Spanner DDL: The Reputation Ledger
-- This schema is designed for high-frequency updates and behavioral auditing.

-- Main table for agent state and reputation scores
CREATE TABLE Agents (
    AgentID STRING(36) NOT NULL,
    TrustScore FLOAT64 NOT NULL,
    BehavioralDrift FLOAT64 NOT NULL,
    GovTaxBalance INT64 NOT NULL,
    IsFrozen BOOL NOT NULL,
    UpdatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp=true),
) PRIMARY KEY (AgentID);

-- Interleaved audit table for behavioral drift tracking
-- Interleaving ensures agent history is physically co-located with the agent record
CREATE TABLE ReputationAudit (
    AgentID STRING(36) NOT NULL,
    AuditID STRING(36) NOT NULL,
    TransactionID STRING(36),
    Verdict STRING(20), -- 'SUCCESS', 'FAILURE', 'DRIFT_DETECTED', 'REWARD', 'RECOVERED'
    EntropyDelta FLOAT64,
    TaxLevied INT64,
    CreatedAt TIMESTAMP NOT NULL OPTIONS (allow_commit_timestamp=true),
) PRIMARY KEY (AgentID, AuditID),
  INTERLEAVE IN PARENT Agents ON DELETE CASCADE;

-- Index for fast trust-based queries (e.g., finding high-trust jurors)
CREATE INDEX IndexAgentsByTrust ON Agents(TrustScore DESC);

-- Index for recent audit lookups
CREATE INDEX IndexAuditByTime ON ReputationAudit(AgentID, CreatedAt DESC);
