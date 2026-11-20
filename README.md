# mautrix-peachy

A Matrix-Peachy puppeting bridge for [Peachy](https://pchy.app), the app
where friends make plans. Run it with [Beeper](https://www.beeper.com) to read and answer your
Peachy chats next to everything else.

> **Independent project.** mautrix-peachy is an independent open-source
> project. It isn't part of Peachy, and it isn't made or supported by Peachy
> or Beeper. "Peachy" and "Beeper" are their owners' names, used here only to
> say what the bridge connects.

> **Status: early testing.** Text, photos, video and files work both ways
> in DMs and groups. There are no prebuilt downloads yet.

**User guide:** [docs/](docs/index.md): connecting Peachy on Beeper Desktop,
iPhone and Android, plus FAQ and troubleshooting.

## How it works

The bridge works like a linked device. It signs in to your Peachy account as a
new device, and you approve it from your phone the way you add any other
device: tap "add another device", and of the three numbers it offers, pick the
one the bridge shows you in Beeper. From then on it:

- receives your messages, decrypts them, and posts them to Matrix rooms
- sends what you type in Beeper back to Peachy, encrypted

It's built the same way as [mautrix-signal](https://github.com/mautrix/signal):
written in Go on [mautrix-go](https://github.com/mautrix/go)'s `bridgev2`
framework, with the cryptography left to Peachy's own MLS engine, the one
Peachy's apps use, as mautrix-signal leaves it to libsignal.

## Privacy: read this first

Peachy chats are end-to-end encrypted, so Peachy's servers can't read them.
**The bridge can.** It holds your account's keys and has every message in
plaintext, which it hands to your Matrix homeserver. Run it on a machine you
trust. If you use Beeper's servers, your Peachy messages are then as private
as your Beeper messages, not as private as Peachy.

## Running it with Beeper

See [Run the bridge](docs/getting-started/run-the-bridge.md): install bbctl,
build with `./build.sh`, register with
`bbctl config --type bridgev2 -o config.yaml sh-peachy`, add the Peachy
settings and start it. Then send `login` to `@sh-peachybot:beeper.local` and
approve the bridge from Peachy on your phone.

## What's supported

| Feature | Status |
|---|---|
| Direct messages: send and receive | works |
| Private groups | works |
| Photos, video and files, with captions | works (up to 90 MB each) |
| Several photos sent at once | works: they arrive as one album |
| Message history from before you connected | off on purpose (see the [FAQ](docs/faq.md)) |
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

## Without Beeper

It's a standard Matrix bridge, so it also runs on your own Matrix server
(Synapse and others) for use from Element or any Matrix app. See
[docs/getting-started/matrix.md](docs/getting-started/matrix.md).

## Peachy in a terminal, and bots

The bridge is built on [peachy-go](https://github.com/collinjackson/peachy-go),
a Go client for Peachy (MPL-2.0). It also has `peachy`, a command-line tool,
and `peachy daemon`, which runs chat bots written for signal-cli's REST API.

## Building it

Most people don't need to: the
[releases page](https://github.com/collinjackson/mautrix-peachy/releases) has
builds for macOS (Apple silicon) and Linux (x86-64 and ARM). To build one
yourself you need Go and Rust:

```
git clone https://github.com/collinjackson/mautrix-peachy
cd mautrix-peachy && ./build.sh
```

The Peachy client, [peachy-go](https://github.com/collinjackson/peachy-go),
comes in as a Go module. `build.sh` builds Peachy's MLS engine from
[peachy-protocol](https://github.com/peachysocial/peachy-protocol), cloned
into `target/peachy-protocol` at the commit peachy-go names (or
`$PEACHY_PROTOCOL`), and links it into the bridge. To work on peachy-go
alongside, use a `go.work` that points at your checkout of it.

## License

Copyright (C) 2026 Collin Jackson.

mautrix-peachy is free software: you can redistribute it and/or modify it
under the terms of the GNU Affero General Public License, version 3, as
published by the Free Software Foundation; see [LICENSE](LICENSE). Like the
other mautrix bridges, it comes with no warranty.

It's built against Peachy's protocol files and MLS engine from
peachy-protocol, which are Peachy Social's, under the MIT license, and aren't
in this repository.
