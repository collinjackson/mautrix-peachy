package connector

// The account store, read: contact names, blocks and the read-receipts
// setting (peachy.Store). Read on connect, and again when Peachy's live
// stream says it changed.
//
// Mute, pin and archive stay out of Beeper on purpose: their options and
// meaning differ between Peachy and Beeper, so each side keeps its own.

import (
	"context"
	"slices"

	"github.com/rs/zerolog"

	"github.com/collinjackson/peachy-go"
)

func (c *PeachyClient) settings() *peachy.Store { return c.store.Load() }

// syncStore reads the store and reports whether it changed.
func (c *PeachyClient) syncStore(ctx context.Context) bool {
	s, err := c.device.ReadStore(ctx)
	if err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Msg("Peachy: couldn't read the account store")
		return false
	}
	old := c.store.Swap(s)
	return old == nil || old.Version != s.Version
}

// blocked reports whether this person blocked the sender of e. Peachy's apps
// show nothing from someone you've blocked, so neither does the bridge.
func (c *PeachyClient) blocked(e peachy.Event) bool {
	return e.Sender != c.me && c.settings().Blocked(e.Sender)
}

// blockedMember reports whether someone this person blocked is in the
// conversation. Peachy sends no receipt that would reach them, and a wide one
// can't go while they hold a seat, so the bridge sends none there.
func (c *PeachyClient) blockedMember(conversation string) bool {
	s := c.settings()
	if s == nil {
		return false
	}
	convs, err := c.device.Conversations()
	if err != nil {
		return true // can't tell: say less
	}
	for _, conv := range convs {
		if conv.ID == conversation {
			return slices.ContainsFunc(conv.Members, s.Blocked)
		}
	}
	return false
}

// sendReadReceipts follows the person's Peachy setting unless the bridge's
// config overrides it.
func (c *PeachyClient) sendReadReceipts() bool {
	if c.pc.Config.ReadReceipts != nil {
		return *c.pc.Config.ReadReceipts
	}
	s := c.settings()
	return s != nil && s.ReadReceipts
}
