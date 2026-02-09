#!/bin/bash
# =============================================================================
# Test Supabase Connection
# =============================================================================

set -e

echo "========================================="
echo "OCX Supabase Connection Test"
echo "========================================="
echo ""

# Check for required env vars
if [ -z "$SUPABASE_URL" ]; then
    echo "❌ SUPABASE_URL not set"
    echo ""
    echo "Set it with:"
    echo "  export SUPABASE_URL=https://aadfuooiusjogdnndobp.supabase.co"
    exit 1
fi

if [ -z "$SUPABASE_SERVICE_KEY" ]; then
    echo "❌ SUPABASE_SERVICE_KEY not set"
    echo ""
    echo "Get your service key from:"
    echo "  Supabase Dashboard → Settings → API → service_role key"
    echo ""
    echo "Then set it with:"
    echo "  export SUPABASE_SERVICE_KEY=your-key-here"
    exit 1
fi

echo "SUPABASE_URL: $SUPABASE_URL"
echo ""

# Test REST API connection
echo "Testing Supabase REST API..."
RESPONSE=$(curl -s -w "\n%{http_code}" \
    "${SUPABASE_URL}/rest/v1/tenants?select=tenant_id,tenant_name&limit=5" \
    -H "apikey: ${SUPABASE_SERVICE_KEY}" \
    -H "Authorization: Bearer ${SUPABASE_SERVICE_KEY}")

HTTP_CODE=$(echo "$RESPONSE" | tail -n1)
BODY=$(echo "$RESPONSE" | sed '$d')

if [ "$HTTP_CODE" = "200" ]; then
    echo "✅ Connection successful!"
    echo ""
    echo "Sample data from 'tenants' table:"
    echo "$BODY" | python3 -m json.tool 2>/dev/null || echo "$BODY"
else
    echo "❌ Connection failed (HTTP $HTTP_CODE)"
    echo "$BODY"
    exit 1
fi

echo ""
echo "========================================="
echo "All connections verified!"
echo "========================================="
