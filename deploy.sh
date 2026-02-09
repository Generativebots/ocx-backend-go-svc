#!/bin/bash
# =============================================================================
# OCX Cloud Run Deployment Script
# =============================================================================
# Usage: ./deploy.sh [service] [region]
#   service: api | trust-registry | all
#   region: us-central1 (default)
# 
# Required environment variables:
#   PROJECT_ID          - GCP project ID
#   SUPABASE_URL        - Supabase project URL
#   SUPABASE_SERVICE_KEY - Supabase service role key
# =============================================================================

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Defaults
SERVICE=${1:-api}
REGION=${2:-us-central1}

# Validate environment
if [ -z "$PROJECT_ID" ]; then
    echo -e "${RED}Error: PROJECT_ID not set${NC}"
    echo "Export your GCP project ID: export PROJECT_ID=your-project-id"
    exit 1
fi

if [ -z "$SUPABASE_URL" ]; then
    echo -e "${RED}Error: SUPABASE_URL not set${NC}"
    exit 1
fi

if [ -z "$SUPABASE_SERVICE_KEY" ]; then
    echo -e "${RED}Error: SUPABASE_SERVICE_KEY not set${NC}"
    exit 1
fi

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}OCX Cloud Run Deployment${NC}"
echo -e "${GREEN}========================================${NC}"
echo "Project: $PROJECT_ID"
echo "Region: $REGION"
echo "Service: $SERVICE"
echo ""

# Enable required APIs
echo -e "${YELLOW}Enabling Cloud APIs...${NC}"
gcloud services enable run.googleapis.com --project=$PROJECT_ID
gcloud services enable cloudbuild.googleapis.com --project=$PROJECT_ID
gcloud services enable artifactregistry.googleapis.com --project=$PROJECT_ID

# Deploy Go API
deploy_api() {
    echo -e "${YELLOW}Deploying ocx-api (Go backend)...${NC}"
    
    cd "$(dirname "$0")"
    
    gcloud builds submit \
        --config=cloudbuild.yaml \
        --project=$PROJECT_ID \
        --substitutions=_REGION=$REGION,_SUPABASE_URL=$SUPABASE_URL,_SUPABASE_SERVICE_KEY=$SUPABASE_SERVICE_KEY
    
    # Get the service URL
    API_URL=$(gcloud run services describe ocx-api --region=$REGION --project=$PROJECT_ID --format='value(status.url)')
    echo -e "${GREEN}✅ Go API deployed: $API_URL${NC}"
    echo ""
    echo "API Endpoints:"
    echo "  - Health: $API_URL/health"
    echo "  - Pool Stats: $API_URL/api/pool/stats"
    echo "  - Escrow: $API_URL/api/escrow/items"
    echo "  - Reputation: $API_URL/api/reputation/{agent_id}"
}

# Deploy Python service
deploy_python_service() {
    SERVICE_NAME=$1
    SERVICE_DIR=$2
    PORT=${3:-8001}
    
    echo -e "${YELLOW}Deploying $SERVICE_NAME...${NC}"
    
    cd "$SERVICE_DIR"
    
    gcloud run deploy "$SERVICE_NAME" \
        --source . \
        --region=$REGION \
        --project=$PROJECT_ID \
        --platform=managed \
        --allow-unauthenticated \
        --set-env-vars="SUPABASE_URL=$SUPABASE_URL,SUPABASE_SERVICE_KEY=$SUPABASE_SERVICE_KEY,PORT=$PORT" \
        --memory=512Mi \
        --cpu=1 \
        --min-instances=0 \
        --max-instances=5
    
    SERVICE_URL=$(gcloud run services describe "$SERVICE_NAME" --region=$REGION --project=$PROJECT_ID --format='value(status.url)')
    echo -e "${GREEN}✅ $SERVICE_NAME deployed: $SERVICE_URL${NC}"
}

# Main deployment logic
case $SERVICE in
    api)
        deploy_api
        ;;
    trust-registry)
        deploy_python_service "ocx-trust-registry" "../ocx-services-py-svc/ocx-services-py-svc/trust-registry" 8001
        ;;
    activity-registry)
        deploy_python_service "ocx-activity-registry" "../ocx-services-py-svc/ocx-services-py-svc/activity-registry" 8002
        ;;
    process-mining)
        deploy_python_service "ocx-process-mining" "../ocx-services-py-svc/ocx-services-py-svc/process-mining" 8003
        ;;
    all)
        deploy_api
        deploy_python_service "ocx-trust-registry" "../ocx-services-py-svc/ocx-services-py-svc/trust-registry" 8001
        deploy_python_service "ocx-activity-registry" "../ocx-services-py-svc/ocx-services-py-svc/activity-registry" 8002
        ;;
    *)
        echo -e "${RED}Unknown service: $SERVICE${NC}"
        echo "Available services: api, trust-registry, activity-registry, process-mining, all"
        exit 1
        ;;
esac

echo ""
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}Deployment Complete!${NC}"
echo -e "${GREEN}========================================${NC}"
