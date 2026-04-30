#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

export PATH="/usr/local/go/bin:${PATH}"
OUT_DIR="${EVIDENCE_FIRST_SMOKE_OUT:-/tmp/sports-matcher-evidence-first-smoke}"
mkdir -p "$OUT_DIR"

printf '[1/5] go test ./internal/matcher -run TestEvidenceFirstP5\n'
go test ./internal/matcher -run 'TestEvidenceFirstP5' -v

printf '[2/5] build sports-matcher CLI\n'
go build -o "$OUT_DIR/sports-matcher" ./cmd/server/main.go

printf '[3/5] single-league read-only Evidence-First smoke\n'
"$OUT_DIR/sports-matcher" match-evidence "${EVIDENCE_FIRST_SMOKE_TOURNAMENT:-sr:tournament:17}" \
  --sport "${EVIDENCE_FIRST_SMOKE_SPORT:-football}" \
  --tier "${EVIDENCE_FIRST_SMOKE_TIER:-hot}" \
  --ts-id "${EVIDENCE_FIRST_SMOKE_TS_ID:-jednm9whz0ryox8}" \
  --candidate-limit "${EVIDENCE_FIRST_SMOKE_CANDIDATE_LIMIT:-4}" \
  --review-out "$OUT_DIR/single_review.json" \
  --json > "$OUT_DIR/single_result.json"

test -s "$OUT_DIR/single_review.json"
test -s "$OUT_DIR/single_result.json"

printf '[4/5] batch read-only Evidence-First smoke with four representative samples\n'
cat > "$OUT_DIR/evidence_samples.json" <<'JSON'
[
  {"tournament_id":"sr:tournament:17","sport":"football","tier":"hot","ts_competition_id":"jednm9whz0ryox8"},
  {"tournament_id":"sr:tournament:18","sport":"football","tier":"regular","ts_competition_id":"l965mkyh32r1ge4"},
  {"tournament_id":"sr:tournament:955","sport":"football","tier":"regular","ts_competition_id":"z318q66hl1qo9jd"},
  {"tournament_id":"sr:tournament:138","sport":"basketball","tier":"hot","ts_competition_id":"jednm9ktd5ryox8"}
]
JSON
"$OUT_DIR/sports-matcher" batch-evidence \
  --config "$OUT_DIR/evidence_samples.json" \
  --candidate-limit "${EVIDENCE_FIRST_SMOKE_CANDIDATE_LIMIT:-4}" \
  --review-dir "$OUT_DIR/reviews" \
  --json > "$OUT_DIR/batch_result.json"

test -s "$OUT_DIR/batch_result.json"
find "$OUT_DIR/reviews" -type f -name '*_review.json' -print -quit | grep -q .

printf '[5/5] completed. Artifacts: %s\n' "$OUT_DIR"
