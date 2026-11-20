---
title: Connect Peachy on Beeper for Android
---

# Connect Peachy on Beeper for Android

[← Peachy for Beeper](../index.md)

> **Early testing.** These steps work once someone has the bridge running
> (see [Run the bridge](run-the-bridge.md)).

## Before you start

- The bridge is running ([Run the bridge](run-the-bridge.md)). The bridge
  can't run on an Android phone; it runs on a computer or server.
- Peachy is installed and signed in, on this phone or another one.

## Steps

1. In Beeper, start a new chat with **@sh-peachybot:beeper.local**, the
   Peachy bridge bot.
2. Send the message **`login`**.
3. The bot asks for your phone number. Send it with the country code, like
   `+1 555 555 0100`.
4. Peachy texts you a code. Send the code to the bot.
5. The bot says it's waiting for approval. Switch to **Peachy** and tap
   **Add another device**. Peachy shows three numbers.
6. Switch back to Beeper. The bot now shows **one** number.
7. Switch to Peachy and **tap the number that matches the bot's**.
8. Back in Beeper, wait for the bot to say you're connected.

The number only appears after you tap **Add another device**, so the bot
can't show it earlier. Switching between the apps is fine: the bridge waits
while you approve.

## Good to know

- Keep Peachy installed. You can turn off its notifications so you don't get
  each message twice.
- The bridge shows up in Peachy's **Devices** list as **Beeper** (unless whoever runs it named it something else).
- More answers in the [FAQ](../faq.md).
