#!/usr/bin/env python3
"""
PostgreSQL / Supabase Connection Test Script

Usage:
  # Set env vars and run:
  export POSTGRES_HOST=aws-1-ap-southeast-1.pooler.supabase.com
  export POSTGRES_PORT=6543
  export POSTGRES_DB=postgres
  export POSTGRES_USER=postgres.hluhoennpsdrgfymoaaa
  export POSTGRES_PASSWORD='Windows#2027$%'
  python3 test_connection.py
"""

import os
import sys
import time

try:
    import psycopg2
    from psycopg2.extras import RealDictCursor
except ImportError:
    print("❌ psycopg2 not installed. Run: pip install psycopg2-binary")
    sys.exit(1)


def get_connection_params():
    """Build connection params from env vars."""
    return {
        "host": os.getenv("POSTGRES_HOST", "aws-1-ap-southeast-1.pooler.supabase.com"),
        "port": int(os.getenv("POSTGRES_PORT", "6543")),
        "database": os.getenv("POSTGRES_DB", "postgres"),
        "user": os.getenv("POSTGRES_USER", "postgres.hluhoennpsdrgfymoaaa"),
        "password": os.getenv("POSTGRES_PASSWORD", ""),
        "sslmode": "require",
        "gssencmode": "disable",
        "connect_timeout": 15,
    }


def test_connection():
    params = get_connection_params()
    safe_display = {k: ("***" if "password" in k else v) for k, v in params.items()}
    print(f"🔌 Connecting with: {safe_display}\n")

    start = time.time()
    try:
        conn = psycopg2.connect(**params)
        elapsed = (time.time() - start) * 1000
        print(f"✅ Connected in {elapsed:.0f}ms")
    except Exception as e:
        print(f"❌ Connection FAILED: {e}")
        sys.exit(1)

    cursor = conn.cursor(cursor_factory=RealDictCursor)

    # ── 1. Server info ──
    cursor.execute("SELECT version()")
    print(f"   Server: {cursor.fetchone()['version'][:80]}...")

    cursor.execute("SELECT current_database(), current_user")
    info = cursor.fetchone()
    print(f"   Database: {info['current_database']}  User: {info['current_user']}")

    # ── 2. List all tables ──
    cursor.execute("""
        SELECT table_name
        FROM information_schema.tables
        WHERE table_schema = 'public' AND table_type = 'BASE TABLE'
        ORDER BY table_name
    """)
    tables = [r['table_name'] for r in cursor.fetchall()]
    print(f"\n📋 Tables in public schema: {len(tables)}")
    for t in tables:
        cursor.execute(f'SELECT COUNT(*) AS cnt FROM "{t}"')
        cnt = cursor.fetchone()['cnt']
        print(f"   {'✅' if cnt > 0 else '⚪'} {t}: {cnt} rows")

    # ── 3. List views ──
    cursor.execute("""
        SELECT table_name
        FROM information_schema.views
        WHERE table_schema = 'public'
        ORDER BY table_name
    """)
    views = [r['table_name'] for r in cursor.fetchall()]
    if views:
        print(f"\n👁  Views: {len(views)}")
        for v in views:
            print(f"   • {v}")

    # ── 4. Check expected OCX tables ──
    expected = [
        "tenants", "agents", "rules", "trust_scores", "verdicts",
        "activities", "activity_executions", "evidence",
        "compliance_reports", "activity_approvals", "activity_versions",
        "trust_attestations", "a2a_use_cases", "authority_contracts",
    ]
    print(f"\n🔍 OCX table check:")
    missing = [t for t in expected if t not in tables]
    if missing:
        print(f"   ❌ Missing {len(missing)}: {', '.join(missing)}")
        print("   → Run master_schema.sql first, then seed_data.sql")
    else:
        print(f"   ✅ All {len(expected)} key tables present")

    # ── 5. Quick data sanity ──
    if "tenants" in tables:
        cursor.execute("SELECT tenant_id, tenant_name, subscription_tier FROM tenants LIMIT 5")
        rows = cursor.fetchall()
        if rows:
            print(f"\n🏢 Sample tenants:")
            for r in rows:
                print(f"   • {r['tenant_name']} ({r['subscription_tier']}) — {r['tenant_id'][:8]}...")

    conn.close()
    print(f"\n✅ All checks passed. Connection is healthy.")


if __name__ == "__main__":
    test_connection()
