#!/usr/bin/env bash
set -euo pipefail

# Tokens are stored as bcrypt hashes (cost 10).
# Plaintext:    dev-token      → agent-1
# Plaintext:    dev-token-2    → agent-2
#
# To regenerate hashes:
#   go run scripts/hash-token.go <plaintext>
#
# These hashes were generated with bcrypt.DefaultCost (10).

DSN="${TE_DB_DSN:-host=localhost port=5432 user=tunneledge password=tunneledge dbname=tunneledge sslmode=disable}"

echo "Seeding default tokens..."

PGPASSWORD="${PGPASSWORD:-tunneledge}" psql "${DSN}" -c "
INSERT INTO tokens (token, agent_id, created_at, updated_at)
VALUES
    ('\$2a\$10\$t.2xY2ZIpRvPLaNgFfhC4esHrnaNCwR3vBh5eTU67jTM1yabJWSg2', 'agent-1', NOW(), NOW()),
    ('\$2a\$10\$KIAT38diyMoBz3XY24ZAS.NTxOK.r563GDg1DyePa46.tevqsg41u', 'agent-2', NOW(), NOW())
ON CONFLICT (token) DO NOTHING;
"

echo "Seeding complete."
