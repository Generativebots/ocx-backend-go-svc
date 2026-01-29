-- Billing Schema

CREATE TABLE billing_transactions (
    transaction_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    
    trust_score FLOAT NOT NULL,
    transaction_value FLOAT DEFAULT 1.0, -- Normalized value, can be passed in request
    trust_tax FLOAT NOT NULL,            -- Calculated: (1.0 - trust_score) * value
    
    timestamp TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_billing_tenant_time ON billing_transactions (tenant_id, timestamp DESC);
