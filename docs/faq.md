---
title: FAQ
---

# Frequently asked questions

[← Peachy for Beeper](index.md)

### Why isn't Peachy in Beeper's list of chat networks?

Beeper's built-in networks are ones Beeper runs itself. Peachy connects
through a bridge you run, which Beeper supports for any chat app. It's an
independent project, not made by Beeper or listed by them.

### Do I need to keep Peachy on my phone?

Yes. You need it to approve the bridge, and Peachy keeps your account safe
by only letting a device in when one of your existing devices approves it.
You can turn off Peachy's notifications so you don't get each message twice.

### Can Peachy's servers read my messages because of the bridge?

No. Messages travel between Peachy devices end-to-end encrypted, as always.
But the bridge itself can read them, and so can Beeper's side of things once
the bridge hands them over. See [Privacy](index.md#privacy-read-this-first).

### Will I see my old messages?

*No, on purpose.* Beeper shows the messages that arrive after you connect.
When you approve the bridge, your phone offers it your recent history the
way it does for a new phone, but the bridge doesn't copy that history into
Beeper: your old messages stay end-to-end encrypted on Peachy instead of
landing on another server.

### Does it work if my Peachy account has a PIN?

Yes. A PIN on its own doesn't get in the way.

If you've also turned on **Registration lock**, the bridge can't connect
yet. Turn Registration lock off in Peachy (Settings → PIN), connect the
bridge, then turn it back on. The bridge keeps working: the lock only
applies when someone signs in to your number again.

### What about events, communities and the Feed?

Not planned. The bridge covers direct messages and private groups.

### What about chats with Advanced Chat Privacy turned on?

*To be decided.* Whether the bridge skips those chats is still being
discussed with the Peachy team.

### How do I disconnect?

In the Peachy app, open **Devices** and remove **Beeper**. The bridge stops
receiving messages right away. To clean up the bridge's computer too, see
[Removing the bridge](getting-started/run-the-bridge.md#removing-the-bridge).

### Who do I ask for help?

Not Beeper Support: they don't support self-hosted bridges. Open an issue on
the [project's GitHub](https://github.com/collinjackson/mautrix-peachy/issues).

## Do disappearing messages disappear in Beeper?

Yes. A message with a timer counts down in Beeper as it does in Peachy:
yours from when you sent it, anyone else's from when you read it, in Beeper
or on another of your devices. When the time is up the bridge deletes it from
Beeper. Messages you send from Beeper carry the chat's timer.

It's best effort, as in Peachy itself and as with Beeper's other bridges: the
bridge deletes the message from your Beeper account, and Beeper's apps remove
it, but a modified app, a screenshot or a copy already saved elsewhere can
keep it. The timer is set in the Peachy app; changing it from Beeper isn't
supported yet.

## Does the bridge use my Peachy settings?

Yes, the ones that matter in Beeper. It reads them from Peachy's encrypted
account store, which only your own devices can open:

- **Names you gave people** in Peachy.
- **Blocked people:** nothing they send reaches Beeper, and the bridge sends
  no receipts in chats they're in.
- **Read receipts:** on or off as in Peachy's Settings → Privacy.

Changes you make in Peachy reach Beeper right away, as they reach your other
devices. The bridge only reads these settings; it never changes them.

**Muted, pinned and archived chats** aren't synced, not even when a chat
first comes to Beeper. Mute and pin mean different things in Peachy and
Beeper, with different options, so you set them on each side.

If several people share one bridge, everyone sees one set of names for a
person, so names from your contacts are best on a bridge you run for yourself.
