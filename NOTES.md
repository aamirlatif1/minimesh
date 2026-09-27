# NOTES — M0: using NetBird itself

Working notes from self-hosting NetBird. Anything confusing or broken in the
docs goes under "Docs issues" — that's the lead for a docs contribution.

## Setup log

- Date:
- Where: Multipass VM `mesh` / cloud VM:
- Quickstart: <https://docs.netbird.io/selfhosted/selfhosted-quickstart>
- Domain / IdP used:
- Time to a working dashboard:

## Two devices connected

`netbird status -d` (paste the relevant part):

```text
```

- Connection type (P2P or Relayed):
- ICE candidate types used (host / srflx / relay):

`sudo wg show` on one peer (the interface is usually `wt0`):

```text
```

- Endpoint:
- Latest handshake:
- Allowed IPs:
- Why the endpoint is (or isn't) the other device's public address:

## The three services in two sentences each

- **Management:**
- **Signal:**
- **Relay:**

## Code skim (`netbirdio/netbird`)

- `signal/`:
- `relay/`:
- `client/internal/peer/`:
- `management/` (how the network map is built and pushed):

## Docs issues

| Page | What was confusing or broken | Suggested fix |
| --- | --- | --- |
| | | |

## Questions for the interview

-
