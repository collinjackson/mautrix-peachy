---
title: Run the bridge
---

# Run the bridge

[← Peachy for Beeper](../index.md)

The bridge runs on a computer or server you control, using Beeper's
**bbctl** tool to connect to your Beeper account. You do this once; after
that, you connect Peachy from any Beeper app.

## What you need

- A Beeper account.
- A computer that stays on and online: a Mac, a Linux machine or server, or
  Windows with [WSL](https://learn.microsoft.com/windows/wsl/install). bbctl
  doesn't run on Windows directly.
- Peachy on your phone, to approve the bridge.

## Steps

1. **Install bbctl** from
   [bbctl's releases](https://github.com/beeper/bridge-manager/releases):
   download the newest file for your computer (`bbctl-macos-arm64` for a
   Mac with Apple silicon), rename it `bbctl` and make it runnable
   (`chmod +x bbctl`).

   Don't use Homebrew's `beeper/tap/bbctl` for now: it installs an older
   version (0.14.0) that Beeper refuses with "Your client is out of date".
   Check yours with `bbctl --version`; you need 0.15.0 or newer.

2. **Sign in to Beeper:**

   ```
   bbctl login
   ```

3. **Download the bridge** into a new folder, which will also hold its
   data. Pick the line for your computer:

   ```
   mkdir peachy-bridge && chmod 700 peachy-bridge && cd peachy-bridge

   # Mac with Apple silicon
   curl -fLo mautrix-peachy https://github.com/collinjackson/mautrix-peachy/releases/latest/download/mautrix-peachy-macos-arm64
   # Linux (most PCs and servers)
   curl -fLo mautrix-peachy https://github.com/collinjackson/mautrix-peachy/releases/latest/download/mautrix-peachy-linux-amd64
   # Linux on ARM (Raspberry Pi 4 or 5 with a 64-bit system)
   curl -fLo mautrix-peachy https://github.com/collinjackson/mautrix-peachy/releases/latest/download/mautrix-peachy-linux-arm64

   chmod +x mautrix-peachy
   ```

   Every download is on the
   [releases page](https://github.com/collinjackson/mautrix-peachy/releases),
   with its checksum. To build it yourself instead, see
   [Building it](https://github.com/collinjackson/mautrix-peachy#building-it).

4. **Register it with Beeper**, in the same folder:

   ```
   bbctl config --type bridgev2 -o config.yaml sh-peachy
   chmod 600 config.yaml
   ```

   Flags go before the name `sh-peachy`. This registers the bridge on your
   Beeper account; its bot is `@sh-peachybot:beeper.local`. `config.yaml`
   holds your Beeper tokens, so keep it private.

5. **Add the Peachy settings** at the top of `config.yaml`:

   ```yaml
   network:
       server: https://api.pchy.app
       data_dir: ./peachy-data
       device_name: Beeper
       read_receipts: null
   ```

   `peachy-data` will hold the bridge's Peachy keys. Keep it private and
   backed up. `device_name` is what Peachy's **Devices** list calls the
   bridge; a change applies the next time the bridge starts. Read receipts follow
   your Peachy setting (Settings → Privacy); set `read_receipts` to `true` or
   `false` to override it for the bridge.

6. **Start it:**

   ```
   ./mautrix-peachy -c config.yaml
   ```

   It connects out to Beeper, so your computer doesn't need to be reachable
   from the internet.

7. **Leave it running.** The bridge only passes messages while it's
   running, so keep the terminal open (or run it in `tmux`). If it's off for
   more than 30 days, messages from that time are lost (see
   [Troubleshooting](../troubleshooting.md)).

Then connect your Peachy account from
[Desktop](desktop.md), [iPhone](ios.md) or [Android](android.md).

## Removing the bridge

1. In the Peachy app, open **Devices** and remove **Beeper**.
2. On the bridge's computer, run:

   ```
   bbctl delete sh-peachy
   ```

   This removes the bridge and its rooms from Beeper. It doesn't touch the
   bridge's folder on your computer: delete that yourself, since it holds
   the bridge's copy of your Peachy keys.
