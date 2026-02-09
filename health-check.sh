#!/bin/bash

echo "=== PicPic Render System Health Check ==="
echo ""

echo "1. Checking Docker containers..."
docker compose ps
echo ""

echo "2. Checking agent connection..."
AGENT_COUNT=$(curl -s http://localhost:8080/api/agents | grep -o '"id"' | wc -l)
echo "   Active agents: $AGENT_COUNT"
curl -s http://localhost:8080/api/agents | python3 -c "import sys, json; agents = json.load(sys.stdin); [print(f'   - Agent {a[\"id\"]}: {a[\"name\"]} ({a[\"status\"]})') for a in agents]" 2>/dev/null || echo "   (JSON parsing failed, but agents endpoint is responding)"
echo ""

echo "3. Checking health endpoints..."
LIVE=$(curl -s http://localhost:8080/health/live)
READY=$(curl -s http://localhost:8080/health/ready)
echo "   /health/live: $LIVE"
echo "   /health/ready: $READY"
echo ""

echo "4. Checking metrics..."
curl -s http://localhost:8080/metrics | grep -E "^(agents_active|jobs_active|queue_depth)" | head -3
echo ""

echo "5. Creating test job..."
JOB_ID=$(curl -s -X POST http://localhost:8080/api/jobs \
  -H "Content-Type: application/json" \
  -d '{"command":"echo Health Check Test && date","image":"alpine","priority":8}' \
  | grep -o '"id":"[^"]*"' | cut -d'"' -f4)
echo "   Created job: $JOB_ID"
echo ""

echo "6. Waiting for job execution..."
sleep 3
echo ""

echo "7. Checking agent logs..."
docker logs agent-render-agent-1-1 2>&1 | tail -10
echo ""

echo "=== Health Check Complete ==="
