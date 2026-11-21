# Hollow Signal

**A store-and-forward mesh network for people the Directorate can switch off.**

Patriot HackFest Fall 2026, theme "Life underground".

Two billion people live in the Hollow, underground cities run by the Directorate, which also runs the only network and can shut it down. Systems are failing: blackouts, bad water, lung sickness. When the network goes dark, families can't reach each other and warnings don't get through.

Hollow Signal lets every device carry messages for its neighbors. A message hops from device to device, and any device that can't pass it on yet holds it until someone comes into range. One tool covers check-ins, blackout warnings, sickness reports and calls for help, and the Directorate can't forge messages on it.

> Demo video: (https://screenapp.io/app/share/Jimfbi1No1K1yQ9Ky_QjMy2t)
>
> Screenshot: <img width="2880" height="1402" alt="chrome_Y9ib5bKLBF" src="https://github.com/user-attachments/assets/9483b36b-7a24-4321-8666-7c84929f194b" />


## What it does

- **Store and forward.** Every device relays messages to its neighbors and holds them while no path exists. Cut a link or kill a device, and messages wait, then arrive once a path comes back.
- **Emergencies first.** Messages have a type (emergency, sickness, check-in, general), and the most urgent ones always leave a device first.
- **Signed messages.** Every message is signed with ed25519. Devices check the signature *before* accepting a message, so a forged order is rejected and never spreads. Only authorized senders (for example, the Warden) may send emergencies.
- **Replay protection.** Messages expire. An old message is dropped even if its signature is valid.
- **Delivery receipts.** When a message arrives, a signed receipt travels back. Relays stop carrying the message, and the sender sees "confirmed delivered".
- **Path tracking.** Each delivery reports the devices it passed through, which the UI uses to highlight the route.
- **Abuse limits.** Each device rate-limits every sender (10 per minute, emergencies exempt) and caps its queue at 50, dropping the least urgent message first.
- **Live dashboard.** Events stream to the browser over a websocket, with metrics: delivered, duplicates dropped, attacks blocked, expired, rate limited, average delay.

## Run it

Needs Go 1.27.4 or newer (from `go.mod`). With Go 1.21 or newer installed, `go` downloads the right version automatically.

```
go run .
```

Open http://localhost:8080 and play the demo in four parts, one button each: **1 · Regular day**, **2 · Blackout**, **3 · Emergency first**, **4 · Directorate attack**. Each part waits for your click, so you can narrate between them. **▶ Full simulation** plays all four back to back. The demo:

0. A regular morning: Maya tells her dad she is heading to the market.
1. The Directorate network is severed, and a relay on Level 30 loses power.
2. Maya on Level 40 sends "I'm okay" to her dad on Level 12. It finds a detour and she gets a receipt.
3. The pumps on Level 50 are cut off while chatter piles up for them. When a link returns, the Warden's blackout warning reaches them *before* the chatter.
4. The Directorate injects a forged order, "Level 38 is safe, return": **rejected, invalid signature**.
5. With signature checking switched off, the same forged order spreads to everyone. That's why the signatures matter.

Parts run in order, once per server start. **⚠ Attack** injects another forged order at any time. To play it again, restart with `go run .`.

## Tests

```
go vet ./...
go test ./...
```

70 tests cover routing, cut and restored links, receipts, priority, signing and forgery, expiry, paths, metrics, rate limits, the queue cap, the HTTP controls and the websocket hub. One test runs the exact video scenario end to end. The tests wait on events instead of sleeping, and they use a fake clock for expiry.

## How it works

Everything runs in one Go process. Each device is a goroutine with a channel as its inbox.

| File | What's in it |
|---|---|
| `device.go` | A device: verify, drop duplicates, rate-limit, then deliver, broadcast or queue and forward |
| `network.go` | Owns the devices and the topology: links, cuts, dead devices, signing on send, attack injection |
| `security.go` | ed25519 signing and verification, the trusted key list, emergency authorization |
| `message.go` | The message format, types, priorities and limits |
| `metrics.go` | Shared counters |
| `scenario.go` | The scripted demo. Each step waits for real events before moving on |
| `controls.go` | HTTP endpoints for the dashboard buttons (`/api/...`) |
| `server.go` | Websocket hub: streams events as JSON and replays recent events to new browsers |

**Security details.** The signature covers the ID, sender, recipient, type, priority, body, timestamp, expiry and receipt link. It does not cover the hop count or the path, because relays change those at every hop. Devices verify a message before they mark it as seen, so a forgery that copies a real message's ID can't block the real one.

## Honest limits

This is a simulation and a proof of concept, not a deployable system.

- **No real radio.** Devices are simulated in one process. Real Bluetooth or Wi-Fi Direct is out of scope.
- **Demo keys.** Each device's key is derived from a fixed seed, so demo runs are repeatable. A real system would generate keys on each phone and share only the public keys.
- **No encryption.** Messages are signed, not encrypted: anyone relaying a message can read it. Metadata (who talks to whom) is not hidden either.
- **The Directorate network is only a status flag.** The point is that the mesh works without it.
- **Paths are not trusted.** A relay could lie about the path. It's for display only.
- **No accounts, login or mobile app.**
- **Efficiency.** Devices resend their whole queue on every sync, which is why "duplicates dropped" climbs quickly. Fine at demo scale, not at city scale.
- In the demo, the forged order counts as "delivered" while signatures are off, because it really was delivered.

## Built with

Go standard library (including `crypto/ed25519`) and [gorilla/websocket](https://github.com/gorilla/websocket).
