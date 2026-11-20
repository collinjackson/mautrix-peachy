---
title: Use it with your own Matrix server
---

# Use it with your own Matrix server

[← Peachy for Beeper](../index.md)

The bridge is a standard Matrix bridge, so it isn't limited to Beeper. If you
run your own Matrix server (Synapse, conduwuit, Dendrite) you can add it there
and talk to your Peachy chats from Element or any other Matrix app.

> **Early testing.** There's no prebuilt download yet: build it from source
> as in [Run the bridge](run-the-bridge.md), step 3.

## What you need

- A Matrix server you administer, and an account on it.
- A machine for the bridge that your server can reach over the network, and
  that can reach your server. The same machine is simplest.
- Peachy on your phone, to approve the bridge.

## Steps

1. **Make a config file:**

   ```
   ./mautrix-peachy -e
   ```

   This writes `config.yaml`. Keep it private: it will hold the bridge's
   secrets.

2. **Edit `config.yaml`:**

   - `homeserver.address`: how the bridge reaches your server, for example
     `http://localhost:8008`.
   - `homeserver.domain`: your server's name, the part after `:` in your
     Matrix ID.
   - `appservice.address`: how your server reaches the bridge, for example
     `http://localhost:29350`.
   - `bridge.permissions`: give yourself access, for example
     `"example.com": user` or `"@you:example.com": admin`.
   - `database`: SQLite is fine for one person
     (`type: sqlite3-fk-wal`, `uri: file:peachy.db?_txlock=immediate`).
   - Under `network`, the Peachy settings:

     ```yaml
     network:
         server: https://api.pchy.app
         data_dir: ./peachy-data
         device_name: Matrix
     ```

3. **Make the registration file** your server needs:

   ```
   ./mautrix-peachy -g
   ```

   This writes `registration.yaml`. For Synapse, add it to
   `homeserver.yaml` and restart Synapse:

   ```yaml
   app_service_config_files:
     - /path/to/registration.yaml
   ```

4. **Start the bridge:**

   ```
   ./mautrix-peachy -c config.yaml
   ```

5. **Connect Peachy.** In your Matrix app, start a chat with
   `@peachybot:example.com` (your server's name in place of `example.com`)
   and send `login`. The bot asks for your phone number and the code Peachy
   texts you. Then open Peachy on your phone, tap **add another device**, and
   pick the number the bot shows.

Your Peachy chats then appear as rooms.

## Good to know

- **Encrypted rooms.** To have the bridge's rooms end-to-end encrypted on the
  Matrix side too, turn on `encryption` in `config.yaml`
  (see the comments there).
- **Mute, pin and archive** reach your Matrix account only with double
  puppeting (`double_puppet` in `config.yaml`). Everything else works
  without it.
- **One person per bridge** is best: names you gave people in Peachy become
  the names of their Matrix accounts on your server, which everyone on that
  server sees.
- The bridge shows up in Peachy's **Devices** list under `device_name`.
