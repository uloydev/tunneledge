#!/usr/bin/env bash
set -euo pipefail

DSN="${TE_DB_DSN:-host=localhost port=5432 user=tunneledge password=tunneledge dbname=tunneledge sslmode=disable}"

echo "Seeding default tokens..."

PGPASSWORD="${PGPASSWORD:-tunneledge}" psql "${DSN}" -c "
INSERT INTO tokens (token, agent_id, created_at, updated_at)
VALUES
    ('dev-token', 'agent-1', NOW(), NOW()),
    ('dev-token-2', 'agent-2', NOW(), NOW())
ON CONFLICT (token) DO NOTHING;
"

echo "Seeding complete."
