# Demo video script (target 2:40, hard limit 3:00)

**SAY** is the voiceover. **DO** is what you click or show. **SCREEN** is what appears, so you know when to move on.

The demo runs in four parts, one button each at the bottom left. **A part only starts when you click it**, so take your time narrating between parts. While a part plays, its button says **(running)**. When it finishes, it turns green with a ✓ and the next button lights up orange. The map redraws on its own as links are cut and devices go offline.

If the feed runs past the bottom, scroll it up to point at a row. It stops auto-scrolling while you're scrolled up, and starts following again when you scroll back to the bottom.

---

## Before you hit record (2 minutes)

1. Open a terminal in the project folder. If the server is running, stop it (Ctrl+C). **Each part runs once per server start.**
2. Run `go run .` and wait for `running at http://localhost:8080`.
3. Open http://localhost:8080 in Chrome. Press **F11** for full screen. Zoom 100%.
4. Check:
   - The banner says **DIRECTORATE NETWORK: UNKNOWN** (gray).
   - The feed shows "Connected to Hollow Signal".
   - All the Network health numbers are 0.
   - **1 · Regular day** is orange and the other parts are dimmed.
5. Have a second terminal ready in the project folder (for `go test` at the end), but keep it hidden.
6. Close notifications. Start the screen recorder.

---

## 0:00 – 0:20 Hook

**DO:** Recording starts on the idle dashboard. Slowly move the mouse over the map, from the top (Level 10) to the bottom (Level 50).

**SAY:**
> "Two billion people live in the Hollow, cities stacked underground. The Directorate runs the only network, and it can switch it off. When it does, families can't reach each other, and warnings don't get through."

## 0:20 – 0:35 What it is

**DO:** Point at the map nodes: Dad (L12), then Maya (L40), then Pumps (L50).

**SAY:**
> "Hollow Signal is a mesh network. Every device carries messages for its neighbors. If there's no path, a device holds the message until someone comes into range. Every message is signed, so the Directorate can't fake one."

---

## PART 1: Regular day (0:35 – 0:50)

**DO:** Click **1 · Regular day**.

**SCREEN:**
1. The banner turns green: **DIRECTORATE NETWORK: ONLINE**.
2. The feed shows: *"A regular morning: Maya tells her dad she is heading to the market."*
3. A few **FORWARDED** rows.
4. **DELIVERED** at Dad: *Morning, Dad. Heading to the market.*
5. **ACKED** at Maya.
6. The button turns green ✓ after about 8 seconds.

**SAY** (while it plays):
> "A normal day. The Directorate network is up. Maya tells her dad she's heading to the market. The message hops device to device, and a signed receipt comes back."

**DO:** Point at the green DELIVERED and ACKED rows.

---

## PART 2: Blackout (0:50 – 1:15)

**DO:** Click **2 · Blackout**.

**SCREEN:**
1. The banner turns red: **DIRECTORATE NETWORK: SEVERED**.
2. **DEVICE KILLED**: *Device L30-Relay went OFFLINE*. The Relay node on the map turns gray, with a line through its name.
3. *"Maya on Level 40 sends 'I'm okay' to her dad on Level 12."*
4. **FORWARDED** rows, then **DELIVERED** at Dad: *Dad, I'm okay. Staying at the market.*
5. **ACKED**.

**SAY:**
> "Blackout. The Directorate cuts the network, and the relay on Level 30 loses power."

**DO:** Point at the red banner, then at the gray Relay.

**SAY:**
> "Maya sends 'I'm okay'. The relay is dead, so the message goes around it, hop by hop. Dad gets it, and Maya gets a receipt."

**DO:** Trace the detour on the map with the mouse: Maya → Market → Clinic → School → Shaft → Dad. Then point at **DELIVERED** and **ACKED**. Wait for the button to turn green ✓.

---

## PART 3: Emergency first (1:15 – 1:45)

**DO:** Click **3 · Emergency first**.

**SCREEN:**
1. Two **LINK CUT** rows: *Market <-> Pumps* and *Shelter <-> Pumps*. Both links around Pumps turn red and dashed on the map.
2. *"The Warden broadcasts an EMERGENCY blackout warning."*, then amber **BROADCAST** rows: *BLACKOUT on Levels 40-50 in 5 minutes. Move up now.*
3. **LINK RESTORED** *Market <-> Pumps*. That link is solid again.
4. **BROADCAST** at *L50-Pumps* (the warning) **before** the two **DELIVERED** chatter rows: *Water ration moved to 0600*, *Market closes early today*.

**SAY:**
> "Now the pumps on Level 50 are cut off. Market chatter for them piles up. Then the Warden sends an emergency: blackout coming, move up now."

**DO:** When LINK RESTORED appears, point at the restored link on the map.

**SAY:**
> "A runner reaches the pumps. Look at the order: the emergency arrives first, ahead of chatter that was waiting longer. Urgent messages always leave first."

**DO:** Scroll the feed up if needed. Point at the Pumps BROADCAST row, then at the two DELIVERED rows under it.

---

## PART 4: Directorate attack (1:45 – 2:10)

**DO:** Click **4 · Directorate attack**.

**SCREEN:**
1. *"DIRECTORATE ATTACK: a forged order 'Level 38 is safe, return' hits Maya's device."*
2. A red **REJECTED** row: *FORGED MESSAGE REJECTED: invalid signature*. **Attacks blocked** shows **1**.
3. About 6 seconds later: *"Without signatures…"*. **Signatures: OFF** turns red at the top right. A flood of amber **BROADCAST** rows follows: *DIRECTORATE ORDER: Level 38 is safe…* at every device.
4. About 6 seconds later: *"The network is the people. No one can switch it off."* **Signatures: ON** turns green again.

**SAY:**
> "Now the Directorate attacks. It forges an order: Level 38 is safe, go back home. Maya's device checks the signature and rejects it."

**DO:** Point at REJECTED, then at **Attacks blocked**.

**SAY** (as the flood starts):
> "Here's the same attack with signature checking switched off. The fake order reaches every device in the Hollow. That's why every message is signed."

**SAY** (on the last caption):
> "The network is the people. No one can switch it off."

## 2:10 – 2:18 Attack live

**DO:** Click **⚠ Attack** (bottom right).

**SCREEN:** A new red **REJECTED** row appears, and **Attacks blocked** goes up by one.

**SAY:**
> "And that's live. I hit attack again, and it's rejected again."

## 2:18 – 2:32 How it's built

**DO:** Switch to the second terminal. Run `go test ./...` and wait for `ok`.

**SAY:**
> "It's all Go. Every device is a goroutine with its own inbox. Messages are signed with ed25519, they expire so they can't be replayed, and devices rate-limit senders. There are 70 tests, including one that runs this exact demo end to end."

## 2:32 – 2:40 Closing

**DO:** Switch back to the dashboard and hold.

**SAY:**
> "Check-ins, blackout warnings, calls for help, all on one network the Directorate can't forge and can't switch off. This is Hollow Signal."

**DO:** Stop recording at about 2:40.

---

## If something goes wrong

- **Clicked a part too early:** an alert says "wait for the current part to finish" or "run part N first". Dismiss it and click again when the button is orange.
- **Need a second take:** stop the server (Ctrl+C), run `go run .` again, and reload the page.
- **Running long:** cut "How it's built" (saves 14 s), or shorten the pause after Part 1.
- **No time to narrate between parts:** click **▶ Full simulation** instead. It plays all four parts back to back in under a minute. It only works before any part has run.
