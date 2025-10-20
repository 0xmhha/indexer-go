#!/bin/bash
set -euo pipefail

# Indexer-Go Health Check Script
# Usage: ./health-check.sh [host:port]

HOST_PORT="${1:-localhost:8080}"
TIMEOUT=5

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo "=== Indexer-Go Health Check ==="
echo "Target: ${HOST_PORT}"
echo ""

# Function to check HTTP endpoint
check_endpoint() {
    local endpoint=$1
    local name=$2

    echo -n "Checking ${name}... "

    if response=$(curl -s -f --max-time ${TIMEOUT} "http://${HOST_PORT}${endpoint}"); then
        echo -e "${GREEN}✓${NC}"
        return 0
    else
        echo -e "${RED}✗${NC}"
        return 1
    fi
}

# Function to check JSON response
check_json_endpoint() {
    local endpoint=$1
    local name=$2
    local expected_field=$3

    echo -n "Checking ${name}... "

    if response=$(curl -s -f --max-time ${TIMEOUT} "http://${HOST_PORT}${endpoint}"); then
        if echo "${response}" | jq -e ".${expected_field}" >/dev/null 2>&1; then
            echo -e "${GREEN}✓${NC}"
            echo "${response}" | jq .
            return 0
        else
            echo -e "${YELLOW}⚠${NC} (unexpected response)"
            echo "${response}"
            return 1
        fi
    else
        echo -e "${RED}✗${NC}"
        return 1
    fi
}

# Initialize counters
PASSED=0
FAILED=0

# Check health endpoint
echo "[1/5] Health Endpoint"
if check_json_endpoint "/health" "Health" "status"; then
    ((PASSED++))
else
    ((FAILED++))
fi
echo ""

# Check version endpoint
echo "[2/5] Version Endpoint"
if check_json_endpoint "/version" "Version" "version"; then
    ((PASSED++))
else
    ((FAILED++))
fi
echo ""

# Check metrics endpoint
echo "[3/5] Metrics Endpoint"
if check_endpoint "/metrics" "Prometheus Metrics"; then
    ((PASSED++))
    echo "  Sample metrics:"
    curl -s "http://${HOST_PORT}/metrics" | grep -E "^indexer_" | head -5
else
    ((FAILED++))
fi
echo ""

# Check subscribers endpoint
echo "[4/5] Subscribers Endpoint"
if check_json_endpoint "/subscribers" "Subscribers" "total_count"; then
    ((PASSED++))
else
    ((FAILED++))
fi
echo ""

# Check GraphQL playground (if enabled)
echo "[5/5] GraphQL Playground"
if check_endpoint "/playground" "GraphQL Playground"; then
    ((PASSED++))
else
    echo -e "${YELLOW}  Note: GraphQL may not be enabled${NC}"
    ((PASSED++))  # Don't fail on this
fi
echo ""

# Check systemd service status (if running as root/with sudo)
if command -v systemctl >/dev/null 2>&1; then
    echo "[Bonus] Systemd Service Status"
    if systemctl is-active --quiet indexer-go 2>/dev/null; then
        echo -e "${GREEN}✓${NC} Service is running"
        systemctl status indexer-go --no-pager | head -10
    else
        echo -e "${YELLOW}⚠${NC} Service is not running (or not installed)"
    fi
    echo ""
fi

# Summary
echo "=== Summary ==="
echo "Passed: ${PASSED}"
echo "Failed: ${FAILED}"
echo ""

if [ ${FAILED} -eq 0 ]; then
    echo -e "${GREEN}All checks passed!${NC}"
    exit 0
else
    echo -e "${RED}Some checks failed${NC}"
    exit 1
fi
