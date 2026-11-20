---
title: Troubleshooting
---

# Troubleshooting

[← Peachy for Beeper](index.md)

### `bbctl login` says "Your client is out of date"

Your bbctl is too old; updating the Beeper app doesn't change it. Homebrew's
`beeper/tap/bbctl` still installs 0.14.0. Download the newest bbctl from
[its releases](https://github.com/beeper/bridge-manager/releases) instead
(see [Run the bridge](getting-started/run-the-bridge.md)), check
`bbctl --version` says 0.15.0 or newer, and log in again.

### The bot doesn't answer

The bridge isn't running, or can't reach Beeper. On the bridge's computer,
check that it's still running, and start it again if not
([Run the bridge](getting-started/run-the-bridge.md)).

### I didn't get a code

Check the phone number includes your country code. Wait a minute and send
**`login`** again to get a new code.

### Peachy doesn't show "Add another device", or no numbers appear

Make sure Peachy on your phone is up to date and signed in to the same
account. Leave the bot chat open; the bridge keeps waiting until you
approve. If nothing happens after a few minutes, send **`login`** again.

### None of the three numbers match

Don't tap any of them. Cancel on your phone and start over. A mismatch can
mean you're approving a different device than the one you think.

### Messages stopped arriving

- Check the bridge is still running.
- Check **Beeper** is still in Peachy's **Devices** list. If it was removed,
  connect again.

### Some messages are missing after the bridge was off

Peachy holds messages for a device for up to **30 days**. If the bridge is
off longer than that, messages from that time are gone for the bridge (your
phone still has them). Keep the bridge running.

### Still stuck?

Open an issue on the
[project's GitHub](https://github.com/collinjackson/mautrix-peachy/issues). Beeper Support can't help with self-hosted bridges.
