---
title: Peachy for Beeper
---

# Peachy for Beeper

Read and answer your [Peachy](https://pchy.app) chats in
[Beeper](https://www.beeper.com), next to everything else.

> **Status: early testing.** DMs and private groups work: text, photos and
> files, replies, reactions, edits and deletes.

## How it works

Beeper doesn't include Peachy as a built-in chat network. Instead, Peachy
connects to Beeper through a **bridge**, a small program you (or someone you
trust) run on a computer or server. Setup has two parts:

1. **Run the bridge**, once, on a computer that stays on.
   [How to run the bridge](getting-started/run-the-bridge.md)
2. **Connect your Peachy account** from any Beeper app:
   - [Beeper Desktop](getting-started/desktop.md) (Mac, Windows, Linux)
   - [Beeper for iPhone](getting-started/ios.md)
   - [Beeper for Android](getting-started/android.md)

Not on Beeper? [Use it with your own Matrix server](getting-started/matrix.md).

Connecting works like adding a new phone or laptop to Peachy: you sign in
with your phone number, then approve the bridge from the Peachy app by
picking the matching number.

## Privacy: read this first

Peachy chats are end-to-end encrypted, so Peachy's servers can't read them.
**The bridge can.** It becomes one of your Peachy devices, holds your
account's keys, and sees every message so it can pass it to Beeper.

- Run the bridge on a machine you trust.
- Your Peachy messages are then as private as your Beeper messages, not as
  private as Peachy alone.
- Anyone who controls the bridge's computer can read your Peachy chats.

You can remove the bridge at any time from **Devices** in the Peachy app.

## What works

| Feature | Status |
|---|---|
| Direct messages: send and receive text | works |
| Private groups: send and receive text | works |
| Photos, video and files, with captions | works (up to 90 MB each) |
| Several photos sent at once | works: they arrive as one album |
| Older messages from before you connected | not shown, on purpose ([why](faq.md#will-i-see-my-old-messages)) |
| Replies, reactions, edits, deletes | works (Peachy allows one emoji per person; edits for 24 hours, up to 10) |
| Message requests | answering a DM from someone new in Beeper accepts it, as answering does in Peachy |
| Mentions | works both ways: a Peachy @mention is a pill in Beeper and notifies the person, and a pill to someone on Peachy becomes their @handle. @all is @room |
| Read receipts | works, following your Peachy setting |
| Disappearing messages | works: deleted from Beeper when their time is up (best effort, see the FAQ); the chat's timer shows in Beeper, and is set in Peachy |
| Profile photos and group photos | works |
| Your Peachy settings: contact names, blocked people, read receipts | works (read from Peachy; changes reach Beeper right away) |
| Muted, pinned and archived chats | not synced: mute, pin and archive in Beeper and Peachy separately |
| Typing indicators | later |
| Events, communities, Feed | not planned |

## More

- [Peachy in a terminal](https://github.com/collinjackson/peachy-go/blob/main/docs/cli.md): read and send from the command line (peachy-go)
- [Bots on Peachy](https://github.com/collinjackson/peachy-go/blob/main/docs/bots.md): run bots written for signal-cli's REST API, Com Tower for one (peachy-go)
- [Frequently asked questions](faq.md)
- [Troubleshooting](troubleshooting.md)
- [Source code](https://github.com/collinjackson/mautrix-peachy)

Peachy for Beeper is an independent open-source project. It isn't part of
Peachy, and it isn't made or supported by Peachy or Beeper, so neither
company's support team can help with it.
