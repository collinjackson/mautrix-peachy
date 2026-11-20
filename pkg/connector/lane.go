package connector

// Reactions, deletes and edits, both ways. On Peachy each is a small frame on
// the conversation's lane naming a message by its id, and the MLS sender is
// who did it. Peachy's rules, which the bridge holds to on the way in and
// honors on the way out:
//
//   - One reaction per person per message; a new one replaces it.
//   - Only the author may delete or edit a message.
//   - Edits: offered for 24 hours and ten times; a receiver takes one up to
//     48 hours after the message, and only above the revision it has shown.
//
// A delete or edit that arrives before its message is dropped here, where
// Peachy's clients keep it waiting: the bridge only sees messages sent after
// it linked, so the race is rare.

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/event"

	"github.com/collinjackson/peachy-go"
)

const (
	editSendWindow    = 24 * time.Hour
	editReceiveWindow = 48 * time.Hour
)

// MessageMetadata is kept with each bridged message part.
type MessageMetadata struct {
	// The highest edit revision applied, 0 for none.
	Revision int `json:"revision,omitempty"`
	// For a file that carries the message's words as its caption: the
	// Matrix content, so an edit can rewrite the caption around it.
	Media *event.MessageEventContent `json:"media,omitempty"`
}

func metadataOf(m *database.Message) *MessageMetadata {
	if meta, ok := m.Metadata.(*MessageMetadata); ok && meta != nil {
		return meta
	}
	meta := &MessageMetadata{}
	m.Metadata = meta
	return meta
}

func revisionOf(parts []*database.Message) int {
	rev := 0
	for _, p := range parts {
		rev = max(rev, metadataOf(p).Revision)
	}
	return rev
}

// Peachy → Matrix

func (c *PeachyClient) queueReaction(e peachy.Event) {
	meta := simplevent.EventMeta{
		Type:      bridgev2.RemoteEventReaction,
		PortalKey: c.portalKey(e.Conversation),
		Sender:    c.sender(e.Sender),
		Timestamp: time.UnixMilli(e.AtMS),
	}
	if e.Remove {
		meta.Type = bridgev2.RemoteEventReactionRemove
	}
	// No emoji id: one reaction per person, so a new one replaces the last.
	c.login.QueueRemoteEvent(&simplevent.Reaction{
		EventMeta:     meta,
		TargetMessage: networkid.MessageID(e.Target),
		Emoji:         e.Emoji,
	})
}

// remoteDelete is a Peachy delete, checked against the message's author
// once the portal's earlier events (the message itself, perhaps) are in.
type remoteDelete struct {
	simplevent.MessageRemove
}

var _ bridgev2.RemotePreHandlerWithResult = (*remoteDelete)(nil)

func (d *remoteDelete) PreHandleWithResult(ctx context.Context, portal *bridgev2.Portal) *bridgev2.EventHandlingResult {
	target, err := portal.Bridge.DB.Message.GetFirstPartByID(ctx, portal.Receiver, d.TargetMessage)
	if err != nil {
		res := bridgev2.EventHandlingResultFailed.WithError(err)
		return &res
	}
	if target == nil || target.SenderID != d.Sender.Sender {
		zerolog.Ctx(ctx).Debug().Bool("found", target != nil).Msg("Peachy: ignoring a delete that isn't the author's")
		res := bridgev2.EventHandlingResultIgnored
		return &res
	}
	return nil
}

func (c *PeachyClient) queueDelete(e peachy.Event) {
	c.login.QueueRemoteEvent(&remoteDelete{simplevent.MessageRemove{
		EventMeta: simplevent.EventMeta{
			Type:      bridgev2.RemoteEventMessageRemove,
			PortalKey: c.portalKey(e.Conversation),
			Sender:    c.sender(e.Sender),
			Timestamp: time.UnixMilli(e.AtMS),
		},
		TargetMessage: networkid.MessageID(e.Target),
	}})
}

func (c *PeachyClient) queueEdit(e peachy.Event) {
	c.login.QueueRemoteEvent(&simplevent.Message[peachy.Event]{
		EventMeta: simplevent.EventMeta{
			Type:      bridgev2.RemoteEventEdit,
			PortalKey: c.portalKey(e.Conversation),
			Sender:    c.sender(e.Sender),
			Timestamp: time.UnixMilli(e.AtMS),
		},
		TargetMessage:   networkid.MessageID(e.Target),
		Data:            e,
		ConvertEditFunc: c.convertEdit,
	})
}

func (c *PeachyClient) convertEdit(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, existing []*database.Message, e peachy.Event) (*bridgev2.ConvertedEdit, error) {
	first := existing[0]
	switch {
	case first.SenderID != networkid.UserID(e.Sender):
		return nil, fmt.Errorf("%w: an edit that isn't the author's", bridgev2.ErrIgnoringRemoteEvent)
	case time.UnixMilli(e.AtMS).Sub(first.Timestamp) > editReceiveWindow:
		return nil, fmt.Errorf("%w: an edit more than 48 hours after its message", bridgev2.ErrIgnoringRemoteEvent)
	case e.Revision <= revisionOf(existing):
		return nil, fmt.Errorf("%w: edit %d is already shown or stale", bridgev2.ErrIgnoringRemoteEvent, e.Revision)
	}
	// The words live in the text part (id ""), or in the caption of a lone
	// file; a message of files alone gets a text part.
	for _, part := range existing {
		meta := metadataOf(part)
		var content *event.MessageEventContent
		switch {
		case part.PartID == "":
			content = &event.MessageEventContent{MsgType: event.MsgText, Body: e.Text}
		case meta.Media != nil:
			caption := *meta.Media
			if caption.FileName == "" {
				caption.FileName = caption.Body
			}
			caption.Body = e.Text
			content = &caption
		default:
			continue
		}
		for _, p := range existing {
			metadataOf(p).Revision = e.Revision
		}
		return &bridgev2.ConvertedEdit{ModifiedParts: []*bridgev2.ConvertedEditPart{{
			Part: part, Type: event.EventMessage, Content: content,
		}}}, nil
	}
	for _, p := range existing {
		metadataOf(p).Revision = e.Revision
	}
	return &bridgev2.ConvertedEdit{AddedParts: &bridgev2.ConvertedMessage{Parts: []*bridgev2.ConvertedMessagePart{{
		Type:       event.EventMessage,
		Content:    &event.MessageEventContent{MsgType: event.MsgText, Body: e.Text},
		DBMetadata: &MessageMetadata{Revision: e.Revision},
	}}}}, nil
}

// Matrix → Peachy

var (
	_ bridgev2.ReactionHandlingNetworkAPI  = (*PeachyClient)(nil)
	_ bridgev2.RedactionHandlingNetworkAPI = (*PeachyClient)(nil)
	_ bridgev2.EditHandlingNetworkAPI      = (*PeachyClient)(nil)
)

// refusal is an error Beeper shows on the event, certain (no retrying).
func refusal(msg string) error {
	return bridgev2.WrapErrorInStatus(errors.New(msg)).WithIsCertain(true).WithErrorAsMessage().
		WithErrorReason(event.MessageStatusUnsupported)
}

var (
	errNotYours = refusal("Peachy only lets people change their own messages")
	errNotEmoji = refusal("Peachy reactions are a single emoji")
)

func (c *PeachyClient) PreHandleMatrixReaction(ctx context.Context, msg *bridgev2.MatrixReaction) (bridgev2.MatrixReactionPreResponse, error) {
	emoji := msg.Content.RelatesTo.Key
	if !peachy.ValidEmoji(emoji) {
		return bridgev2.MatrixReactionPreResponse{}, errNotEmoji
	}
	return bridgev2.MatrixReactionPreResponse{SenderID: networkid.UserID(c.me), Emoji: emoji}, nil
}

func (c *PeachyClient) HandleMatrixReaction(ctx context.Context, msg *bridgev2.MatrixReaction) (*database.Reaction, error) {
	var err error
	c.inOrder(msg.Portal.ID, func() {
		err = c.device.React(ctx, string(msg.Portal.ID), string(msg.TargetMessage.ID), msg.PreHandleResp.Emoji, false)
	})
	return nil, err
}

func (c *PeachyClient) HandleMatrixReactionRemove(ctx context.Context, msg *bridgev2.MatrixReactionRemove) error {
	var err error
	c.inOrder(msg.Portal.ID, func() {
		err = c.device.React(ctx, string(msg.Portal.ID), string(msg.TargetReaction.MessageID), "", true)
	})
	return err
}

func (c *PeachyClient) HandleMatrixMessageRemove(ctx context.Context, msg *bridgev2.MatrixMessageRemove) error {
	if msg.TargetMessage.SenderID != networkid.UserID(c.me) {
		return errNotYours
	}
	var err error
	c.inOrder(msg.Portal.ID, func() {
		err = c.device.Delete(ctx, string(msg.Portal.ID), string(msg.TargetMessage.ID))
	})
	return err
}

func (c *PeachyClient) HandleMatrixEdit(ctx context.Context, msg *bridgev2.MatrixEdit) error {
	target := msg.EditTarget
	meta := metadataOf(target)
	switch {
	case target.SenderID != networkid.UserID(c.me):
		return errNotYours
	case time.Since(target.Timestamp) > editSendWindow:
		return fmt.Errorf("%w: Peachy allows edits for 24 hours", bridgev2.ErrEditTargetTooOld)
	case meta.Revision >= peachy.MaxEditRevision:
		return fmt.Errorf("%w: Peachy allows ten edits per message", bridgev2.ErrEditTargetTooManyEdits)
	}
	text := msg.Content.Body
	if msg.Content.MsgType.IsMedia() {
		text = msg.Content.GetCaption()
	} else if msg.Content.MsgType == event.MsgEmote {
		text = "/me " + text
	}
	var err error
	c.inOrder(msg.Portal.ID, func() {
		err = c.device.Edit(ctx, string(msg.Portal.ID), string(target.ID), meta.Revision+1, text)
	})
	if err == nil {
		meta.Revision++ // saved by the bridge after this returns
	}
	return err
}

// Receipts. Peachy → Matrix: read marks as read receipts, delivered marks
// as delivery receipts. Matrix → Peachy: a delivered mark for what arrived
// (debounced, to its authors), as Peachy's apps send whatever the reader's
// settings; a read mark when the person's Peachy setting allows it (or the
// config's read_receipts says otherwise).

const deliveredDebounce = 2 * time.Second

func (c *PeachyClient) queueReceipt(e peachy.Event) {
	typ := bridgev2.RemoteEventReadReceipt
	if e.Receipt == peachy.ReceiptDelivered {
		typ = bridgev2.RemoteEventDeliveryReceipt
	}
	target := networkid.MessageID(e.Target)
	c.login.QueueRemoteEvent(&simplevent.Receipt{
		EventMeta: simplevent.EventMeta{
			Type:      typ,
			PortalKey: c.portalKey(e.Conversation),
			Sender:    c.sender(e.Sender),
			Timestamp: time.UnixMilli(e.AtMS),
		},
		LastTarget: target,
		Targets:    []networkid.MessageID{target},
	})
}

// delivered is the delivered mark waiting to go to one conversation.
type delivered struct {
	target  string
	authors []string
	timer   *time.Timer
}

// noteDelivered schedules a delivered mark for a message from someone else.
func (c *PeachyClient) noteDelivered(e peachy.Event) {
	if e.Sender == c.me {
		return
	}
	c.deliveredMu.Lock()
	defer c.deliveredMu.Unlock()
	if c.delivered == nil {
		c.delivered = map[string]*delivered{}
	}
	d := c.delivered[e.Conversation]
	if d == nil {
		d = &delivered{}
		c.delivered[e.Conversation] = d
		conv := e.Conversation
		d.timer = time.AfterFunc(deliveredDebounce, func() { c.sendDelivered(conv) })
	}
	d.target = e.ID
	if !slices.Contains(d.authors, e.Sender) {
		d.authors = append(d.authors, e.Sender)
	}
}

func (c *PeachyClient) sendDelivered(conversation string) {
	c.deliveredMu.Lock()
	d := c.delivered[conversation]
	delete(c.delivered, conversation)
	c.deliveredMu.Unlock()
	if d == nil || c.blockedMember(conversation) {
		return
	}
	ctx, cancel := context.WithTimeout(c.login.Log.WithContext(context.Background()), time.Minute)
	defer cancel()
	if err := c.device.Receipt(ctx, conversation, peachy.ReceiptDelivered, d.target, d.authors); err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Msg("Peachy: couldn't send a delivered mark")
	}
}

var _ bridgev2.ReadReceiptHandlingNetworkAPI = (*PeachyClient)(nil)

func (c *PeachyClient) HandleMatrixReadReceipt(ctx context.Context, msg *bridgev2.MatrixReadReceipt) error {
	if !c.sendReadReceipts() || msg.Implicit || msg.ExactMessage == nil || c.blockedMember(string(msg.Portal.ID)) {
		return nil
	}
	// Everyone: who wrote the messages it covers isn't known here.
	return c.device.Receipt(ctx, string(msg.Portal.ID), peachy.ReceiptRead, string(msg.ExactMessage.ID), nil)
}
