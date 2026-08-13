package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type OrderEvent string

const (
	CheckoutConfirmed  OrderEvent = "checkout_confirmed"
	FulfillmentShipped OrderEvent = "fulfillment_shipped"
	ReceiptIssued      OrderEvent = "receipt_issued"
	OrderCancelled     OrderEvent = "order_cancelled"
)

var ErrInvalidUpdate = errors.New("invalid order update")

type OrderUpdate struct {
	Event        OrderEvent `json:"event"`
	OrderID      string     `json:"order_id"`
	Phone        string     `json:"phone"`
	AmountCents  int64      `json:"amount_cents,omitempty"`
	Currency     string     `json:"currency,omitempty"`
	TrackingCode string     `json:"tracking_code,omitempty"`
}

type SentMessage struct {
	MessageID string `json:"message_id"`
	Event     string `json:"event"`
	OrderID   string `json:"order_id"`
}

type smsRequest struct {
	To             string `json:"to"`
	Body           string `json:"body"`
	IdempotencyKey string `json:"idempotency_key"`
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *apiError       `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint"`
}

type smsData struct {
	MessageID string `json:"message_id"`
}

type ReceiptSender struct {
	baseURL string
	key     string
	http    *http.Client
	sleep   func(context.Context, time.Duration) error
}

func NewReceiptSender(baseURL, key string, client *http.Client) *ReceiptSender {
	if client == nil {
		client = http.DefaultClient
	}
	return &ReceiptSender{
		baseURL: strings.TrimRight(baseURL, "/"),
		key:     key,
		http:    client,
		sleep: func(ctx context.Context, delay time.Duration) error {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				return nil
			}
		},
	}
}

func (s *ReceiptSender) Send(ctx context.Context, update OrderUpdate) (SentMessage, error) {
	body, err := messageBody(update)
	if err != nil {
		return SentMessage{}, err
	}

	payload := smsRequest{
		To:             update.Phone,
		Body:           body,
		IdempotencyKey: "order:" + update.OrderID + ":" + string(update.Event),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return SentMessage{}, fmt.Errorf("encode SMS: %w", err)
	}

	var reply envelope
	for attempt := 0; attempt < 4; attempt++ {
		// infrai.sms.send maps to this explicit POST request.
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/v1/sms/send", bytes.NewReader(encoded))
		if err != nil {
			return SentMessage{}, fmt.Errorf("create SMS request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+s.key)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", payload.IdempotencyKey)

		response, err := s.http.Do(req)
		if err != nil {
			return SentMessage{}, fmt.Errorf("send SMS: %w", err)
		}
		responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		response.Body.Close()
		if readErr != nil {
			return SentMessage{}, fmt.Errorf("read SMS response: %w", readErr)
		}

		if response.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			if err := s.sleep(ctx, retryDelay(response.Header.Get("Retry-After"), attempt)); err != nil {
				return SentMessage{}, err
			}
			continue
		}
		if err := json.Unmarshal(responseBody, &reply); err != nil {
			return SentMessage{}, fmt.Errorf("decode SMS response (HTTP %d): %w", response.StatusCode, err)
		}
		if !reply.OK {
			return SentMessage{}, envelopeError(response.StatusCode, reply.Error)
		}
		var data smsData
		if err := json.Unmarshal(reply.Data, &data); err != nil {
			return SentMessage{}, fmt.Errorf("decode SMS data: %w", err)
		}
		if data.MessageID == "" {
			return SentMessage{}, errors.New("SMS response omitted message_id")
		}
		return SentMessage{MessageID: data.MessageID, Event: string(update.Event), OrderID: update.OrderID}, nil
	}
	return SentMessage{}, errors.New("SMS retry budget exhausted")
}

func messageBody(update OrderUpdate) (string, error) {
	if strings.TrimSpace(update.OrderID) == "" || strings.TrimSpace(update.Phone) == "" {
		return "", ErrInvalidUpdate
	}
	switch update.Event {
	case CheckoutConfirmed:
		return fmt.Sprintf("Order %s is confirmed. We will text again when it ships.", update.OrderID), nil
	case FulfillmentShipped:
		if strings.TrimSpace(update.TrackingCode) == "" {
			return "", ErrInvalidUpdate
		}
		return fmt.Sprintf("Order %s shipped. Tracking: %s", update.OrderID, update.TrackingCode), nil
	case ReceiptIssued:
		if update.AmountCents < 0 || len(update.Currency) != 3 {
			return "", ErrInvalidUpdate
		}
		return fmt.Sprintf("Receipt for order %s: %s %d.%02d paid.", update.OrderID, strings.ToUpper(update.Currency), update.AmountCents/100, update.AmountCents%100), nil
	case OrderCancelled:
		return fmt.Sprintf("Order %s was cancelled. No further fulfillment updates will be sent.", update.OrderID), nil
	default:
		return "", ErrInvalidUpdate
	}
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(header)); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Second * time.Duration(1<<attempt)
}

func envelopeError(status int, failure *apiError) error {
	if failure == nil {
		return fmt.Errorf("SMS request rejected (HTTP %d)", status)
	}
	detail := failure.Hint
	if detail == "" {
		detail = failure.Message
	}
	if detail == "" {
		detail = failure.Code
	}
	return fmt.Errorf("SMS request rejected (HTTP %d): %s", status, detail)
}
