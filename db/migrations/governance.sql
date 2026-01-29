-- Governance Schema for OCX Standards Committee

-- 1. COMMITTEE MEMBERS
-- The 22 members of the OCX Standards Committee
CREATE TABLE committee_members (
    member_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id TEXT NOT NULL, -- The organization they represent
    member_name TEXT NOT NULL,
    email TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'MEMBER', -- CHAIR, MEMBER, OBSERVER
    joined_at TIMESTAMP NOT NULL DEFAULT NOW(),
    is_active BOOLEAN DEFAULT TRUE,
    UNIQUE(email)
);

-- 2. GOVERNANCE PROPOSALS
-- Changes to the protocol or standards
CREATE TABLE governance_proposals (
    proposal_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT NOT NULL,
    description TEXT NOT NULL,
    author_id UUID REFERENCES committee_members(member_id),
    
    -- Proposal Lifecycle
    status TEXT NOT NULL DEFAULT 'DRAFT', 
    CHECK (status IN ('DRAFT', 'OPEN', 'PASSED', 'REJECTED', 'IMPLEMENTED')),
    
    -- Versioning
    target_version TEXT, -- e.g. "v2.0"
    backward_compatible BOOLEAN DEFAULT TRUE,
    
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    voting_starts_at TIMESTAMP,
    voting_ends_at TIMESTAMP,
    passed_at TIMESTAMP
);

-- 3. GOVERNANCE VOTES
-- Votes cast by committee members
CREATE TABLE governance_votes (
    vote_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    proposal_id UUID REFERENCES governance_proposals(proposal_id),
    member_id UUID REFERENCES committee_members(member_id),
    
    vote_choice TEXT NOT NULL, -- YES, NO, ABSTAIN
    justification TEXT,
    
    voted_at TIMESTAMP NOT NULL DEFAULT NOW(),
    
    UNIQUE(proposal_id, member_id)
);
