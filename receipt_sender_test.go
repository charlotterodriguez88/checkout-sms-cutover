package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMessageBody(t *testing.T) {
	tests := []struct {
		name    string
		update  OrderUpdate
		want    string
		wantErr bool
	}{
		{"checkout", OrderUpdate{Event: CheckoutConfirmed, OrderID: "A-42", Phone: "+15551234567"}, "Order A-42 is confirmed. We will text again when it ships.", false},
		{"shipment requires tracking", OrderUpdate{Event: FulfillmentShipped, OrderID: "A-42", Phone: "+15551234567"}, "", true},
		{"shipment", OrderUpdate{Event: FulfillmentShipped, OrderID: "A-42", Phone: "+15551234567", TrackingCode: "TRACK-9"}, "Order A-42 shipped. Tracking: TRACK-9", false},
		{"receipt", OrderUpdate{Event: ReceiptIssued, OrderID: "A-42", Phone: "+15551234567", AmountCents: 2599, Currency: "usd"}, "Receipt for order A-42: USD 25.99 paid.", false},
		{"cancellation", OrderUpdate{Event: OrderCancelled, OrderID: "A-42", Phone: "+15551234567"}, "Order A-42 was cancelled. No further fulfillment updates will be sent.", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := messageBody(test.update)
			if (err != nil) != test.wantErr {
				t.Fatalf("messageBody() error = %v, wantErr %v", err, test.wantErr)
			}
			if got != test.want {
				t.Fatalf("messageBody() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSendRetriesWithStableIdempotencyKey(t *testing.T) {
	requests := 0
	var firstKey string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.URL.Path != "/v1/sms/send" {
			t.Fatalf("unexpected boundary: %s %s", r.Method, r.URL.Path)
		}
		var body smsRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if requests == 1 {
			firstKey = body.IdempotencyKey
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if body.IdempotencyKey != firstKey || r.Header.Get("Idempotency-Key") != firstKey {
			t.Fatal("idempotency key changed between attempts")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"data":{"message_id":"sms_123"},"error":null,"metadata":{}}`))
	}))
	defer server.Close()

	sender := NewReceiptSender(server.URL, "test-key", server.Client())
	sender.sleep = func(context.Context, time.Duration) error { return nil }
	got, err := sender.Send(context.Background(), OrderUpdate{Event: CheckoutConfirmed, OrderID: "A-42", Phone: "+15551234567"})
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || got.MessageID != "sms_123" {
		t.Fatalf("requests = %d, message = %#v", requests, got)
	}
}
