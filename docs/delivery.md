# Delivery, retry and acceptance semantics

## Routing

`kjak send --to A --to B` (or `defaults.targets`) names feathers and groups.

1. Names are resolved in order; a group contributes its members in its own
   order. Each feather appears once, at its first position; its result lists
   every group that named it.
2. A feather **named directly** must be enabled and accept the level of the
   message. Otherwise it is a preflight error (exit 2): you asked for it.
3. A **group member** that is disabled, or whose `levels` exclude the level,
   is **skipped**: it appears in the results with `status: skipped` and the
   reason (`disabled`, `level_filter`) and group.
4. An unknown name is a preflight error.

Before anything is sent, every active feather is built and its credentials
resolved (the preflight). Any failure there sends nothing.

## The engine

- **Best effort, independent**: every feather is delivered on its own; a
  failure (even a crash of an adapter) does not stop the others.
- **Worker pool**: at most `concurrency` (default 5) deliveries run at the
  same time — attempts and waits of one feather count as one slot.
- **One deadline**: `timeout` (default 10s) is the deadline of the **whole
  send**, from the moment its message is read: the credentials (a keychain
  lookup included), every attempt, every wait between attempts, of every
  feather, end by then; the deliveries have what the credentials left. A
  deadline that passes while the credentials are resolved ends the send
  before anything is sent (exit 2, `error.code: deadline`), and no delivery
  starts after it. A feather still queued when it passes fails without an
  attempt (`acceptance: none`, `error.code: deadline`).
- **Outside the deadline**: reading the command line and the configuration
  files (local, at most 1 MiB each), reading the message (standard input or
  a `--json-input` FIFO is paced by its writer, `make test | kjak send
  --stdin`; SIGINT, SIGTERM or SIGHUP ends that wait) and writing the report.
- **Deterministic order**: results follow the order of the targets, whatever
  the order in which deliveries finish.

## Acceptance

A delivery succeeds only when the provider **confirmed** it accepted the
message. Each attempt is classified as:

| acceptance | meaning | retried |
|---|---|---|
| `confirmed` | the provider answered with acceptance | — |
| `rejected` | certainly not accepted: the provider refused it, or the request never fully reached it | if the refusal is transient |
| `unknown` | the whole request reached the provider, but no answer came back | **never** |

`unknown` is the case of duplicates: the provider may have accepted the
message and lost the answer (the connection broke after the request body, the
deadline passed while waiting for the reply). A retry could deliver it twice,
so kjak does not retry it and reports it: `status: failed`, `acceptance:
unknown`, exit 1, counted in `summary.unknown`.

### Webhooks (Slack, Discord)

kjak speaks HTTP/1.1 to webhooks (also where HTTP/2 is offered): how far the
request got is tracked with `net/http/httptrace`, and the request is "sent"
once it was fully written and flushed — the HTTP/1.1 transport's behavior,
which the tests exercise. A request that was not fully written is classified
as not accepted because the server cannot have a complete request; this is an
inference from the protocol, not a reply of the provider. Every request uses
a new connection (no keep-alive): a request is never written to an idle
connection the server already closed (which would make an unread request look
"unknown"), and the transport has nothing to resend by itself. A response
whose status line arrived counts by its status, even if its body is cut.

| outcome | acceptance | retry |
|---|---|---|
| 2xx | confirmed | — |
| 429 Too Many Requests | rejected | yes, after `Retry-After` |
| 408, 500, 503 and other 5xx | rejected | yes (503 honors `Retry-After`) |
| 502 Bad Gateway, 504 Gateway Timeout | **unknown** | no — a gateway may have forwarded the request |
| 3xx | rejected | no (redirects are not followed) |
| other 4xx (400, 401, 403, 404, 410, ...) | rejected | no (permanent) |
| error before the request was fully written (DNS, connect, TLS) | rejected | yes |
| TLS certificate verification failed | rejected | no |
| error after it was written (reset, timeout, EOF) | **unknown** | no |
| deadline or SIGINT before the request was written | rejected | no (no time left) |
| deadline or SIGINT after it was written | **unknown** | no |

Slack: success is HTTP 200. Discord is executed with `?wait=true`, so success
is HTTP 200 with the created message; its 429 body `retry_after` (seconds) is
used when there is no `Retry-After` header.

`Retry-After` is read as delay-seconds or an HTTP date (both capped at 24h). The
wait before a retry is `Retry-After` when given, else the backoff.

### HTTP feathers

An `http` feather ([http.md](http.md)) is classified like the webhooks
(HTTP/1.1, a connection per request, `httptrace` for how far the request
got), with its own success statuses, and an address policy:

| outcome | acceptance | retryable |
|---|---|---|
| status in `success_status` (default 2xx) | confirmed | — |
| 429 | rejected (`rate_limited`) | by the retry policy, after `Retry-After` |
| 408, 500, 503, other 5xx | rejected (`server_error`) | by the retry policy (`Retry-After` honored) |
| 502, 504 | **unknown** | never |
| 3xx not followed, other statuses, a 2xx not in `success_status` | rejected | no |
| address or redirect refused by the policy | rejected (`blocked`) | no |
| error before the request was fully written; the attempt `timeout` before it was | rejected (`network`) | by the retry policy |
| the attempt `timeout`, the deadline or SIGINT after it was written; connection lost | **unknown** | never |

**The retry policy** decides whether a retryable refusal is retried at all:
by default (`safe`) `GET`, `PUT` and `DELETE` are, `POST` and `PATCH` only
with `idempotency_key = true` (the same key on every attempt of a send);
`never` retries nothing; `always` retries every method. `retry.max_attempts`
caps the attempts below `1 + max_retries`. The engine is unchanged: it
retries what an attempt marks retryable, never an unknown acceptance.

### E-mail (SMTP)

kjak speaks SMTP itself to know the stage of an error:

| stage | error or reply | acceptance | retry |
|---|---|---|---|
| connect, greeting, EHLO, STARTTLS, AUTH, MAIL, RCPT, DATA | 4xx reply (incl. 421) | rejected | yes |
| same | 5xx reply | rejected | no |
| same | network error | rejected | yes |
| connect | TLS certificate verification failed | rejected | no |
| AUTH | 535 and other refusals | rejected | no (retrying a wrong password locks accounts); 454 and 421 are retried |
| while sending the content (no terminator yet) | network error | rejected | yes — without the final `.` the server cannot accept it |
| after the terminating `.` | 2xx | confirmed | — |
| after the terminating `.` | 4xx / 5xx | rejected | 4xx yes, 5xx no |
| after the terminating `.` | connection lost, timeout, deadline, SIGINT | **unknown** | no |
| after the terminating `.` | any other reply (a malformed or 1xx/3xx code) | **unknown** (`protocol`) | no |

The content and the terminator are flushed separately, so an error in the
content is distinguishable from an error after the terminator. From the first
byte of the terminator on, an error is ambiguous. Replies are parsed per RFC
5321 (multi-line, a bare code without text); a malformed reply before the data
is a refusal, after the terminator it is ambiguous. A session reads at most 1
MiB of replies.

One refused recipient (RCPT) refuses the whole message: no DATA is sent.

## Retries and backoff

- At most `max_retries` (default 2) retries after the first attempt, only of
  `rejected` attempts marked retryable (an `unknown` is never retried, whatever
  an adapter says).
- Backoff: 0.5s, 1s, 2s, ... capped at 5s, with equal jitter (each wait is
  between half and all of it).
- A wait that does not fit the remaining deadline is not started: the result
  fails at once with the last error and `(no retry: the wait of ... does not
  fit the remaining ... of the deadline)`.
- `history` in the JSON report lists every attempt: its start, duration,
  acceptance, retryability, status or SMTP code, `retry_after_ms` and the
  `wait_ms` that followed.

## Cancellation (SIGINT, SIGTERM, SIGHUP)

Ctrl-C (SIGINT), SIGTERM (a CI job canceled, a service or container
stopped) or SIGHUP (the terminal closed, an SSH session lost; not when it was
ignored as kjak started, as with `nohup`) cancels the send: attempts in progress end as soon as possible (with
the acceptance their stage gives: an HTTP request already written is
`unknown`), queued feathers are not attempted, the report is printed with
`outcome: canceled`, and kjak exits 130. Ctrl-C during the preflight (a
keychain lookup waiting for a dialog, `--stdin` reading a pipe nobody
closes) ends kjak too (130, nothing sent); a second signal kills it at once. A SIGINT that arrives after the last delivery
ended interrupts nothing: the exit status is the deliveries'.

## Aggregation

| outcome | when | exit |
|---|---|---|
| `success` | no failed target (skipped targets allowed; a send whose targets are all skipped succeeds) | 0 |
| `partial` | some delivered, some failed | 1 |
| `failure` | none delivered, at least one failed | 1 |
| `canceled` | SIGINT, SIGTERM or SIGHUP | 130 |

## Testing these rules

Every rule above has a test, most of them critical tests
(`TestCritical_*`): the engine with a fake clock and scripted
adapters, the webhooks against `httptest` servers that refuse, rate-limit or
cut the connection after reading the request, the e-mail feather against a
fake SMTP server that closes or stalls after the terminator. See
[development.md](development.md).
