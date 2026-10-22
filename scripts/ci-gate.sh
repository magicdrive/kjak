#!/bin/sh
# The CI Gate: succeeds only when every required job succeeded.
#
#   RESULTS        the `needs` context of the gate job, as JSON
#                  ({"job": {"result": "success"}, ...})
#   EXPECTED_JOBS  the number of required jobs
#
# A failed, skipped or cancelled job, a job without a result, a missing or
# an unexpected job, or RESULTS that is not such an object fails the gate.
set -eu

fail() { echo "ci-gate: $*" >&2; exit 1; }
: "${RESULTS:?RESULTS is not set}"
: "${EXPECTED_JOBS:?EXPECTED_JOBS is not set}"

echo "$RESULTS" | jq -e 'type == "object"' >/dev/null 2>&1 || fail "RESULTS is not a JSON object"
echo "$RESULTS" | jq -r 'to_entries[] | "\(.key): \(.value.result // "none")"'
count=$(echo "$RESULTS" | jq 'length')
[ "$count" -eq "$EXPECTED_JOBS" ] || fail "expected $EXPECTED_JOBS required jobs, got $count"
bad=$(echo "$RESULTS" | jq -r '[to_entries[] | select((.value | type) != "object" or .value.result != "success") | .key] | join(", ")')
[ -z "$bad" ] || fail "not successful: $bad"
echo "ci-gate: all $count required jobs succeeded"
