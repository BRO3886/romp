# Event transport: poll the trigger label in v0; webhooks are a deferred follow-up

Status: accepted

## Context

watch learns about work by asking GitHub which open issues carry the trigger label, on an interval. The natural alternative is a webhook: GitHub pushes each label event to a URL romp exposes, so work starts the moment the label lands. The push model looks strictly better — instant, no polling API cost — but it only wins if romp can actually receive pushes. This shapes whether watch is a long-running poll loop or a small HTTP receiver, so it is a decision, not an implementation detail.

## Decision

v0 polls. watch lists trigger-labelled issues on an interval, and the gap between a label landing and the next poll is the accepted cost of not operating a receiver. Webhooks are recorded here as a deferred follow-up, with the specific obstacle that forced the deferral.

The obstacle is reachability. GitHub's servers deliver webhooks by POSTing to the hook's config.url, so that URL must be reachable from the public internet — a registered hook pointed at localhost is dead on arrival. Creating the hook is easy (`gh api repos/{owner}/{repo}/hooks -X POST`); making the delivery address reachable is the work. Two viable paths exist, neither free:

- A public tunnel (ngrok, cloudflared) or a deployed server gives a real endpoint, but adds a long-running service that must stay up and be reachable.
- The gh CLI's webhook-forwarding extension (`gh webhook forward --repo R --events issues --url http://localhost:PORT`) works without a public IP: gh holds an outbound connection and GitHub relays deliveries down it. It is a testing tool, not infrastructure — the relay is a third-party dependency that can drop, the process must be running before events arrive, and deliveries missed while it is down are lost.

Webhooks are at-least-once and unordered, so they would not remove the existing dedupe: the claim label and job row from ADR 0005 stay. They also cannot be the only transport — an endpoint that is down during a delivery misses that event forever, so a reconciliation poll backstop would remain anyway. Webhooks therefore buy lower latency and fewer list calls at the cost of a reachable receiver, a relay or tunnel dependency, and a second delivery code path. Polling buys nothing to operate — no receiver, no tunnel, no relay — at the cost of latency and constant API usage.

## Consequences

- watch stays one loop: list, claim, run. No HTTP receiver, no tunnel, no relay dependency, nothing new to keep alive.
- Latency to a claim is bounded by the poll interval plus one poll; a label can sit untouched for almost a full interval.
- Every poll spends rate limit even when nothing is labelled. The gh client's rate-limit retry (3 attempts, 5s then 15s backoff) absorbs transient 429s and secondary rate limits so a single poll hitting the limit does not kill a job.
- The webhook paths above are the recorded starting points for the deferred follow-up; the gh CLI has no built-in webhook subcommand, so the work is a receiver plus either a tunnel or the forwarding extension, and the poll loop survives as the backstop.