#!/bin/bash
set -euo pipefail

LOG_GROUP_NAME="my-app-logs"
LOG_STREAM_NAME="my-app-instance-1"
LOG_FILE="${LOG_FILE:-$(dirname "$0")/../logs/app.log}"
AWS_ENDPOINT_URL="${AWS_ENDPOINT_URL:-http://localhost:4566}"

export AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-test}"
export AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-test}"
export AWS_DEFAULT_REGION="${AWS_DEFAULT_REGION:-us-east-1}"

echo "Creating CloudWatch log group..."
aws --endpoint-url="${AWS_ENDPOINT_URL}" logs create-log-group --log-group-name "${LOG_GROUP_NAME}" 2>/dev/null || echo "Log group already exists."

echo "Creating CloudWatch log stream..."
aws --endpoint-url="${AWS_ENDPOINT_URL}" logs create-log-stream --log-group-name "${LOG_GROUP_NAME}" --log-stream-name "${LOG_STREAM_NAME}" 2>/dev/null || echo "Log stream already exists."

echo "Sending logs to CloudWatch..."

events_file=$(mktemp)
trap 'rm -f "$events_file"' EXIT

python3 - "$LOG_FILE" "$events_file" <<'PY'
import sys, json, time

log_file, out_file = sys.argv[1], sys.argv[2]
events = []
with open(log_file) as f:
    for line in f:
        line = line.rstrip('\n')
        if line:
            events.append({"timestamp": int(time.time() * 1000), "message": line})

with open(out_file, 'w') as f:
    json.dump(events, f)
PY

LOG_EVENTS=$(cat "$events_file")

put_logs() {
  if [[ -n "$1" ]]; then
    aws --endpoint-url="${AWS_ENDPOINT_URL}" logs put-log-events \
      --log-group-name "${LOG_GROUP_NAME}" \
      --log-stream-name "${LOG_STREAM_NAME}" \
      --sequence-token "$1" \
      --log-events "$LOG_EVENTS"
  else
    aws --endpoint-url="${AWS_ENDPOINT_URL}" logs put-log-events \
      --log-group-name "${LOG_GROUP_NAME}" \
      --log-stream-name "${LOG_STREAM_NAME}" \
      --log-events "$LOG_EVENTS"
  fi
}

if ! out=$(put_logs "" 2>&1); then
  if echo "$out" | grep -q 'The next expected sequenceToken is'; then
    token=$(echo "$out" | sed -n 's/.*The next expected sequenceToken is: \([A-Za-z0-9]\+\).*/\1/p')
    if [[ -n "$token" ]]; then
      echo "Retrying with sequence token $token" >&2
      put_logs "$token" || { echo "$out" >&2; exit 1; }
    else
      echo "$out" >&2; exit 1
    fi
  else
    echo "$out" >&2; exit 1
  fi
fi

echo "Log sending complete."
