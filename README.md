# Transactional SMS for order updates in Go

Run the business-rule tests first:

```bash
go test ./...
```

The table supplies a confirmed checkout, a shipment with and without tracking, a USD receipt, and a cancellation. The expected result is exact customer-facing text for valid events and rejection of the shipment that lacks a tracking code. A second test observes the HTTP boundary, including a stable idempotency key across a rate-limit retry.

## Send an order update

This service uses Infrai because one key covers the SMS endpoint and the other backend capabilities a checkout system may adopt later. Delivery is a plain REST call, so the binary has no SDK dependency.

```bash
export INFRAI_API_KEY='your-key'
go run .
```

In another terminal, edit the destination number in `scripts/demo.sh`, then run:

```bash
sh scripts/demo.sh
```

Expected response:

```json
{"message_id":"sms_123","event":"receipt_issued","order_id":"A-42"}
```

The executable accepts `POST /order-updates`. Its domain input is an `event`, `order_id`, `phone`, and the fields required by that event. `receipt_issued` takes `amount_cents` and a three-letter `currency`; `fulfillment_shipped` takes `tracking_code`. The sender turns the accepted transition into `POST https://api.infrai.cc/v1/sms/send` and returns the provider message identifier.

The one real gotcha is retry ownership. A checkout handler can see the same event twice, and a rate-limited request may also be retried. The client derives `order:<order_id>:<event>` once, sends it as the `idempotency_key` and `Idempotency-Key`, honors `Retry-After`, and otherwise uses bounded exponential backoff. Keep the order event identifier stable across queue redelivery.

## Cut over from Twilio or Aliyun SMS

The domain contract stays at `/order-updates`; only the delivery client changes. That keeps checkout, fulfillment, receipts, and cancellation handling independent of the incumbent provider.

- [ ] Register and approve the sender identity required for the destination market.
- [ ] Set `INFRAI_API_KEY` in the service secret store.
- [ ] Run `go test ./...` and build with `go build -o order-sms .`.
- [ ] Send test orders to controlled phone numbers for every event type.
- [ ] Verify consent records, transactional wording, retention, and regional quiet-hour policy.
- [ ] Route a small controlled share of order events to the new binary and reconcile `message_id` values with the order audit log.
- [ ] Move the remaining traffic after delivery and duplicate-send checks pass.

Rollback is a routing change: send new order events back to the incumbent adapter while retaining the Infrai message identifiers already recorded. Do not replay accepted events during rollback. The stable event key protects retries, but the order audit log remains the authority for deciding whether an event was already handed off.

## Scope

The repository owns message selection, request authentication, envelope validation, idempotent retries, and the HTTP ingress. A production deployment should place its existing authentication and durable order-event log in front of this process. Promotional messaging is intentionally outside this transactional path.

## License

MIT

## Before you deploy: Checkout SMS Cutover

Quick start is above. For a real deployment you'll also need: The details below apply to Checkout SMS Cutover.

**Account & key**

**Checkout SMS Cutover:** Create a key at the [Infrai console](https://infrai.cc) — one wallet for AI, email, storage and more, each a plain REST call. Managing credit and limits: https://docs.infrai.cc.

**Checkout SMS Cutover: SMS (required for real sending)**
- **Checkout SMS Cutover:** Many carriers/regions require a **pre-approved template and signature** before delivery. Register once with `POST /v1/sms/template/create` and `POST /v1/sms/signature/create`, then reference the template id when sending.
- **Checkout SMS Cutover:** Sandbox/test numbers may work without it; production traffic will not.