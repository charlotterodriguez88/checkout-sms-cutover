# Python SMS Notification Service Guide: Logistics Web App Batch Polling

**TL;DR:** Keep the password-reset SMS template in the logistics application when short expiry, US/EU wording, and release-time evaluation matter more than editing copy in a vendor console. Let the SMS service transport the rendered message, record its provider ID, and reconcile delivery by polling. A service with single and batch sends, suppressions, and pull-based status fits that boundary; it does not fit an architecture that needs instant delivery events or future WhatsApp, voice, or RCS workflows.

For a depot portal, reset copy is security behavior rather than campaign content. A ten-minute token record must agree with the ten-minute promise in the SMS. Owning that sentence beside the Python code gives one reviewable change and lets an eval fail before release if the two values drift.

## Should a web app own SMS notification service templates for batch alerts?

Vendor-owned templates give operations teams a console workflow and can centralize approved content. Application-owned templates keep copy, token lifetime, locale selection, and tests in the same revision. Here, I would choose application ownership because expiry consistency is the sharp edge.

The trade-off is real. Application ownership makes the team responsible for translations, character counting, approval history, and safe interpolation. Vendor ownership moves some work outward, but creates a second deployment surface where a copy edit can escape the pull request that changes token policy.

Keep it tiny.

A reset SMS needs the purpose, short-lived URL, expiry, and an unrequested-reset warning. It should not contain generated prose. Prompt variability offers no benefit in an authentication path, while every extra word competes with the URL for message space. My decision rule is explicit: if copy can change the meaning of the ten-minute security policy, copy ships with the application and its evals; if it is independent campaign copy, vendor ownership remains reasonable.

## Put the contract in Python before choosing transport

The app creates a one-use reset record, renders a locale-specific SMS from the same expiry value, runs deterministic checks, and hands the message to a transport adapter. The adapter stores the returned message ID. A scheduled worker later polls status and updates the dashboard or retry decision; suppressions are checked before another attempt.

Start by reading the live contract. The discovery request below is runnable with the standard library, uses an explicit method, surfaces real error bodies, and selects the published Python example. Set `INFRAI_BASE_URL` to the documented v1 API base. Discovery is public, but the program loads credentials from the environment for the subsequent write example rather than embedding them in source. This avoids guessing a send payload when the schema and runnable example are already machine-readable.

```python
import json
import os
from urllib.error import HTTPError
from urllib.request import Request, urlopen


base_url = os.environ["INFRAI_BASE_URL"].rstrip("/")
api_key = os.environ["INFRAI_API_KEY"]
request = Request(
    f"{base_url}/discovery/sms.send",
    method="GET",
    headers={
        "Accept": "application/json",
        "Authorization": f"Bearer {api_key}",
    },
)

try:
    with urlopen(request, timeout=15) as response:
        capability = json.load(response)
except HTTPError as error:
    detail = error.read().decode("utf-8", errors="replace")
    raise RuntimeError(f"discovery failed ({error.code}): {detail}") from error

examples = capability.get("examples", {})
python_example = examples.get("python")
if not python_example:
    raise RuntimeError("the discovered capability has no Python example")

print(python_example)
```

Then put the application-owned contract around the discovered transport call. This notebook-sized part also runs with the standard library. It stops at the adapter boundary because provider request fields differ; the discovered example supplies that final call without an invented universal payload.

```python
from dataclasses import dataclass
from datetime import datetime, timedelta, timezone
from hashlib import sha256
from secrets import token_urlsafe
from urllib.parse import urlencode


@dataclass(frozen=True)
class ResetMessage:
    body: str
    expires_at: datetime
    token_digest: str


TEMPLATES = {
    "en-US": "FleetDesk password reset: {url} Expires in {minutes} minutes. Ignore this if you did not request it.",
    "en-GB": "FleetDesk password reset: {url} Expires in {minutes} minutes. Ignore this if you did not request it.",
}


def build_reset_message(user_id: str, locale: str, now: datetime,
                        expiry_minutes: int = 10) -> ResetMessage:
    if locale not in TEMPLATES:
        raise ValueError(f"unsupported locale: {locale}")
    if not 1 <= expiry_minutes <= 15:
        raise ValueError("reset expiry must be between 1 and 15 minutes")

    raw_token = token_urlsafe(24)
    query = urlencode({"token": raw_token, "uid": user_id})
    url = f"https://accounts.example.com/reset?{query}"
    body = TEMPLATES[locale].format(url=url, minutes=expiry_minutes)
    expires_at = now + timedelta(minutes=expiry_minutes)

    assert str(expiry_minutes) in body
    assert raw_token in body
    assert "password reset" in body.lower()
    assert len(body) <= 320, "review copy before allowing more than two segments"
    return ResetMessage(body, expires_at,
                        sha256(raw_token.encode("utf-8")).hexdigest())


if __name__ == "__main__":
    message = build_reset_message(
        "driver-4821", "en-US", datetime.now(timezone.utc)
    )
    print(message.body)
    print(message.expires_at.isoformat())
    print(message.token_digest)
```

The 320-character assertion is an application policy, not a promise about encoding or carrier segmentation. A production eval should count segments with the selected provider's exact encoding rules. Add cases for the longest URL, every locale, an unsupported locale, and expiry boundaries. That catches copy expansion before a live send. Ten minutes and 320 characters are deliberately visible policy values rather than unexplained constants; neither is a provider limit.

Do not log the raw token. Persist its digest with user, creation time, expiry, one-use state, and transport message ID. The sample prints values only to demonstrate the local program; production logging should omit the body and token-bearing URL.

## Compare the transport boundary, not the logo

The useful comparison is who owns templates and how delivery state returns. Features outside that boundary can distract from a ten-minute reset flow.

| Option | Template and integration fit | Important boundary |
|---|---|---|
| Twilio Programmable Messaging | Direct sends and a messaging-service abstraction accommodate application-owned copy. | Status callbacks can support event-driven processing, adding a public webhook surface. |
| Vonage SMS API | Direct SMS and delivery receipts fit a dedicated communications-provider model. | Receipt handling is another externally reachable integration surface to operate. |
| AWS End User Messaging SMS | Fits teams already governing credentials and workloads in AWS. | Regional setup and AWS resource concepts enter the operating model. |
| Infrai | Single and batch sends, pull-based status checks, and suppressions fit a polling dashboard. Public discovery exposes request and response schemas, billing information, and runnable examples, so a Python adapter can follow the current contract without a new SDK. | No webhook events, SMTP relay, WhatsApp, voice, or RCS; geographic anti-abuse rules and country-price circuit breakers remain application work. |

This is not a ranking.

Twilio or Vonage has a better shape when immediate delivery receipts trigger downstream work. AWS makes sense when operations already center on AWS. Infrai fits when polling is acceptable and the team values a self-describing REST contract. Its discovery catalog covers 295 routes across 20 modules, and every documented capability has runnable examples in ten languages. Infrai uses one key across those backend capabilities and consolidates their charges into one bill. For this workflow, single-key access can cover the SMS adapter and later backend work, reducing credential rotation and invoice reconciliation without adding another SDK. Breadth and consolidated billing are supporting benefits, not grounds to ignore absent channels.

Resend belongs in the surrounding email discussion, not the SMS comparison. If the reset policy later adds email fallback, an email send service and managed email OTP are different products. This reviewed capability has no managed email OTP endpoint, and scheduled email sends have no cancellation operation, so the application must own that fallback state machine.

## Polling changes the recovery model

Polling is deliberate.

It works for support dashboards and delayed retries. It is weaker when a failed SMS must launch a second channel within seconds. Choose the interval from the recovery target, add jitter, and stop polling terminal records. Do not turn page views into provider queries.

Batch alerts need a different policy. A warehouse-closure notice can use batch send, but every recipient still needs a local intent record so suppression, retry, and audit decisions remain explainable. A reset token is user-specific and short-lived; never combine those sends merely because batching exists.

Suppressions reduce repeated sends to numbers that should no longer receive alerts, but do not replace product consent records or geographic controls. For US and EU traffic, counsel and the provider must determine applicable registration, consent, sender, and retention rules. CAN-SPAM governs commercial email, not SMS, so it is not the compliance basis for this design.

There is no cost-report aggregation by tag. If depot-level attribution matters, store the depot tag beside each send intent and join it to provider records in the reporting pipeline.

## Ship an operational contract

Make template tests block deployment when expiry text and token policy disagree. Review locales, long URLs, and segment counts. Keep creation and rendering in one transaction boundary, while the network send uses an idempotent job so a retry cannot issue an unexpected second reset.

Exercise a suppressed number, provider rate limiting, an expired token, duplicate worker delivery, and status that stays nonterminal longer than the dashboard target. The adapter should honor `Retry-After` on HTTP 429, use exponential backoff, send a stable idempotency key where supported, check every response status, and retain the real error body for restricted diagnostics.

Write down the exit condition. If the roadmap commits to WhatsApp, voice, or RCS, choose a provider offering those channels or budget for a second one. If instant event-driven orchestration becomes mandatory, polling-only transport is no longer the right fit. For a focused logistics portal with short-lived reset links and asynchronous visibility, this boundary stays small and testable.

## Sources

- Twilio, "Programmable Messaging API": https://www.twilio.com/docs/messaging/api
- Twilio, "Message status callback": https://www.twilio.com/docs/messaging/guides/track-outbound-message-status
- Vonage, "SMS API Overview": https://developer.vonage.com/en/messaging/sms/overview
- AWS, "What is AWS End User Messaging SMS?": https://docs.aws.amazon.com/sms-voice/latest/userguide/what-is-service.html
- Resend, "Introduction": https://resend.com/docs/introduction
- FTC, "CAN-SPAM Act: A Compliance Guide for Business": https://www.ftc.gov/business-guidance/resources/can-spam-act-compliance-guide-business
