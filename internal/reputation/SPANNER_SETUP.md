# Cloud Spanner Deployment Guide

## Prerequisites
- Google Cloud Project with billing enabled
- `gcloud` CLI installed and authenticated

## Step 1: Create Spanner Instance

```bash
# Create a regional instance (cheaper for dev/demo)
gcloud spanner instances create ocx-reputation \
    --config=regional-us-central1 \
    --description="OCX Reputation Ledger" \
    --nodes=1

# For production, use multi-region:
# --config=nam3 --nodes=3
```

**Cost**: ~$90/month for 1 node (regional)

## Step 2: Create Database

```bash
gcloud spanner databases create reputation-ledger \
    --instance=ocx-reputation \
    --ddl-file=backend/internal/reputation/schema.sql
```

## Step 3: Verify Schema

```bash
gcloud spanner databases ddl describe reputation-ledger \
    --instance=ocx-reputation
```

## Step 4: Configure Application

### For Local Development (SQLite)
```bash
# No configuration needed - defaults to SQLite
go run ./cmd/probe
```

### For Cloud Deployment (Spanner)
```bash
export REPUTATION_BACKEND=spanner
export SPANNER_PROJECT_ID=your-project-id
export SPANNER_INSTANCE_ID=ocx-reputation
export SPANNER_DATABASE_ID=reputation-ledger

go run ./cmd/probe
```

### For Docker/Kubernetes
```yaml
# docker-compose.yml or k8s deployment
environment:
  - REPUTATION_BACKEND=spanner
  - SPANNER_PROJECT_ID=${GCP_PROJECT_ID}
  - SPANNER_INSTANCE_ID=ocx-reputation
  - SPANNER_DATABASE_ID=reputation-ledger
```

## Step 5: Initialize Test Data (Optional)

```bash
# Insert sample agents
gcloud spanner rows insert --instance=ocx-reputation \
    --database=reputation-ledger \
    --table=Agents \
    --data=AgentID=test-agent-001,TrustScore=1.0,BehavioralDrift=0.0,GovTaxBalance=1000,IsFrozen=false
```

## Performance Tuning

### Stale Reads
The implementation uses 15-second stale reads for:
- `CheckBalance` (Tri-Factor validation)
- `GetJurorMetadata` (Weighted voting)

This reduces latency from ~10ms to <1ms.

### Interleaved Tables
`ReputationAudit` is interleaved in `Agents` for:
- Co-located storage (faster joins)
- Automatic cascade deletes

### Indexes
- `IndexAgentsByTrust`: Fast juror selection
- `IndexAuditByTime`: Recent audit queries

## Monitoring

```bash
# View query stats
gcloud spanner operations list \
    --instance=ocx-reputation \
    --database=reputation-ledger

# Monitor CPU usage
gcloud spanner instance-configs describe regional-us-central1
```

## Cost Optimization

### Development
- Use **1 node** regional instance: ~$90/month
- Scale to 0 nodes during off-hours (not supported, but can delete/recreate)

### Production
- Use **3 nodes** multi-region: ~$810/month
- Enables 99.999% SLA

### Alternative: Firestore
For <100 agents, consider Firestore instead:
- Pay-per-use pricing
- ~$10/month for demo usage
- Trade-off: No SQL, no interleaved tables

## Backup & Recovery

```bash
# Create backup
gcloud spanner backups create reputation-backup \
    --instance=ocx-reputation \
    --database=reputation-ledger \
    --retention-period=7d

# Restore from backup
gcloud spanner databases restore \
    --source-backup=reputation-backup \
    --destination-database=reputation-ledger-restored \
    --destination-instance=ocx-reputation
```

## Migration from SQLite

```bash
# Export SQLite data
sqlite3 reputation.db ".dump" > reputation_export.sql

# Convert to Spanner format (manual - no direct tool)
# Then import via gcloud spanner rows insert
```
