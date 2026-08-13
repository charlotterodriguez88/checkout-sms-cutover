#!/bin/sh
set -eu

curl --fail-with-body \
  --request POST http://localhost:8080/order-updates \
  --header 'Content-Type: application/json' \
  --data '{"event":"receipt_issued","order_id":"A-42","phone":"+15551234567","amount_cents":2599,"currency":"USD"}'
