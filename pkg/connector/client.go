package connector

import (
	"context"
	"fmt"
	gomime "mime"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"go.mau.fi/util/jsontime"
	"go.mau.fi/util/ptr"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/bridgev2/simplevent"
	"maunium.net/go/mautrix/bridgev2/status"
	"maunium.net/go/mautrix/event"

	"github.com/collinjackson/peachy-go"
)

// Peachy's live stream says when to fetch; this is the backstop, in case a
// signal is lost while the stream looks alive.
const fallbackInterval = time.Minute

// PeachyClient is one login's connection: a linked Peachy device, fetching
// its mailbox when Peachy's live stream says to.
type PeachyClient struct {
	pc     *PeachyConnector
	login  *bridgev2.UserLogin
	device *peachy.Device
	me     string
	albums albums
	stop   context.CancelFunc

	deliveredMu sync.Mutex
	delivered   map[string]*delivered

	store atomic.Pointer[peachy.Store]
}

var _ bridgev2.NetworkAPI = (*PeachyClient)(nil)

func newClient(pc *PeachyConnector, login *bridgev2.UserLogin, device *peachy.Device) *PeachyClient {
	return &PeachyClient{pc: pc, login: login, device: device}
}

func (c *PeachyClient) Connect(ctx context.Context) {
	if c.stop != nil {
		return
	}
	if err := c.device.Connect(ctx); err != nil {
		c.login.BridgeState.Send(status.BridgeState{
			StateEvent: status.StateUnknownError,
			Error:      "peachy-connect-failed",
			Message:    err.Error(),
		})
		return
	}
	me, err := c.device.Me()
	if err != nil {
		c.login.Log.Err(err).Msg("Peachy: no account id")
		return
	}
	c.me = me
	c.login.BridgeState.Send(status.BridgeState{StateEvent: status.StateConnected})

	// Settings first: they name people and mute rooms.
	c.syncStore(ctx)

	// Make sure every conversation the device already holds has a room.
	if convs, err := c.device.Conversations(); err == nil {
		for _, conv := range convs {
			c.queueResync(conv.ID)
		}
		go c.refreshProfiles(c.login.Log.WithContext(context.Background()), convs)
	}

	// The poll loop outlives Connect's context; it keeps the login's logger.
	ctx, c.stop = context.WithCancel(c.login.Log.WithContext(context.Background()))
	go c.run(ctx)
}

// run fetches when Peachy's live stream says something moved, as Peachy's
// apps do, and every fallbackInterval regardless.
func (c *PeachyClient) run(ctx context.Context) {
	changes := c.device.Listen(ctx)
	tick := time.NewTicker(fallbackInterval)
	defer tick.Stop()
	for {
		var ch peachy.Change
		select {
		case <-ctx.Done():
			return
		case got, ok := <-changes:
			if !ok {
				return
			}
			ch = got
		case <-tick.C:
			ch = peachy.Change{Mailbox: true, Settings: true}
		}
		if ch.Mailbox {
			c.drain(ctx)
		}
		if ch.Settings && c.syncStore(ctx) || ch.People {
			if convs, err := c.device.Conversations(); err == nil {
				c.refreshProfiles(ctx, convs)
			}
		}
	}
}

// drain fetches the mailbox until it's empty, handling what it holds.
func (c *PeachyClient) drain(ctx context.Context) {
	for {
		events, more, err := c.device.Poll(ctx)
		if ctx.Err() != nil {
			return // disconnecting: the fetch was cut short, not failed
		}
		if err != nil {
			c.login.Log.Err(err).Msg("Peachy: mailbox fetch failed")
			c.login.BridgeState.Send(status.BridgeState{
				StateEvent: status.StateTransientDisconnect,
				Error:      "peachy-poll-failed",
				Message:    err.Error(),
			})
			return
		}
		for _, e := range events {
			c.handle(e)
		}
		if !more {
			return
		}
	}
}

func (c *PeachyClient) portalKey(conversation string) networkid.PortalKey {
	return networkid.PortalKey{ID: networkid.PortalID(conversation), Receiver: c.login.ID}
}

func (c *PeachyClient) queueResync(conversation string) {
	c.login.QueueRemoteEvent(&simplevent.ChatResync{
		EventMeta: simplevent.EventMeta{
			Type:         bridgev2.RemoteEventChatResync,
			PortalKey:    c.portalKey(conversation),
			CreatePortal: true,
		},
		GetChatInfoFunc: c.GetChatInfo,
	})
}

func (c *PeachyClient) handle(e peachy.Event) {
	if c.blocked(e) {
		return
	}
	switch e.Type {
	case peachy.EventJoined, peachy.EventChanged:
		c.queueResync(e.Conversation)
	case peachy.EventMessage:
		c.login.QueueRemoteEvent(&simplevent.Message[peachy.Event]{
			EventMeta: simplevent.EventMeta{
				Type:         bridgev2.RemoteEventMessage,
				PortalKey:    c.portalKey(e.Conversation),
				CreatePortal: true,
				Sender:       c.sender(e.Sender),
				Timestamp:    time.UnixMilli(e.AtMS),
			},
			ID:                 networkid.MessageID(e.ID),
			Data:               e,
			ConvertMessageFunc: c.convertMessage,
		})
		c.noteDelivered(e)
	case peachy.EventReaction:
		c.queueReaction(e)
	case peachy.EventDelete:
		c.queueDelete(e)
	case peachy.EventEdit:
		c.queueEdit(e)
	case peachy.EventReceipt:
		// Your own read note, from another of your devices, marks the chat
		// read in Matrix too, and starts disappearing messages' countdown
		// there, as Peachy's apps do.
		if e.Sender != c.me || e.Receipt == peachy.ReceiptRead {
			c.queueReceipt(e)
		}
	case peachy.EventLost:
		c.login.Log.Warn().Msg("Peachy: some messages expired on the server before the bridge fetched them")
	}
}

func (c *PeachyClient) sender(actor string) bridgev2.EventSender {
	return bridgev2.EventSender{
		IsFromMe:    actor == c.me,
		SenderLogin: networkid.UserLoginID(actor),
		Sender:      networkid.UserID(actor),
	}
}

// convertMessage turns a Peachy message into Matrix parts: one per
// attachment, each fetched, opened and re-uploaded to Matrix, with the words
// as the caption of a single attachment and as their own part otherwise. An
// attachment that can't be fetched becomes a notice, not a lost message.
func (c *PeachyClient) convertMessage(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, e peachy.Event) (*bridgev2.ConvertedMessage, error) {
	var parts []*bridgev2.ConvertedMessagePart
	for i, a := range e.Attachments {
		part, err := c.convertAttachment(ctx, portal, intent, a)
		if err != nil {
			zerolog.Ctx(ctx).Warn().Err(err).Int("attachment", i).Msg("Peachy: couldn't bridge an attachment")
			part = &bridgev2.ConvertedMessagePart{
				Type:    event.EventMessage,
				Content: &event.MessageEventContent{MsgType: event.MsgNotice, Body: "An attachment couldn't be loaded. Open Peachy to see it."},
			}
		}
		part.ID = networkid.PartID(strconv.Itoa(i))
		parts = append(parts, part)
	}
	if e.Text != "" {
		if len(parts) == 1 && (parts[0].Content.URL != "" || parts[0].Content.File != nil) {
			// Matrix's caption: the words in body, the name in filename. The
			// file's content is kept so an edit can rewrite the caption.
			content := parts[0].Content
			media := *content
			parts[0].DBMetadata = &MessageMetadata{Media: &media}
			words := c.textContent(e.Text, e.Mentions, e.Everyone)
			content.FileName, content.Body = content.Body, words.Body
			content.Format, content.FormattedBody, content.Mentions = words.Format, words.FormattedBody, words.Mentions
		} else {
			parts = append([]*bridgev2.ConvertedMessagePart{{
				Type:    event.EventMessage,
				Content: c.textContent(e.Text, e.Mentions, e.Everyone),
			}}, parts...)
		}
	}
	converted := &bridgev2.ConvertedMessage{Parts: parts}
	if e.Timer > 0 {
		// Peachy's countdown for someone else's message starts when it's
		// first seen; the Matrix read receipt starts it here.
		converted.Disappear = database.DisappearingSetting{Type: event.DisappearingTypeAfterRead, Timer: e.Timer}
	}
	if e.ReplyTo != "" {
		converted.ReplyTo = &networkid.MessageOptionalPartID{MessageID: networkid.MessageID(e.ReplyTo)}
	}
	return converted, nil
}

func (c *PeachyClient) convertAttachment(ctx context.Context, portal *bridgev2.Portal, intent bridgev2.MatrixAPI, a peachy.Attachment) (*bridgev2.ConvertedMessagePart, error) {
	data, err := c.device.Download(ctx, a)
	if err != nil {
		return nil, err
	}
	mime := a.Mime
	if mime == "" {
		mime = "application/octet-stream"
	}
	name := a.FileName
	if name == "" {
		name = defaultFileName(mime)
	}
	url, file, err := intent.UploadMedia(ctx, portal.MXID, data, name, mime)
	if err != nil {
		return nil, err
	}
	content := &event.MessageEventContent{
		MsgType: msgTypeFor(mime),
		Body:    name,
		URL:     url,
		File:    file,
		Info: &event.FileInfo{
			MimeType: mime,
			Size:     len(data),
			Width:    a.Width,
			Height:   a.Height,
			Duration: int(a.DurationMS),
		},
	}
	return &bridgev2.ConvertedMessagePart{Type: event.EventMessage, Content: content}, nil
}

func msgTypeFor(mime string) event.MessageType {
	switch {
	case strings.HasPrefix(mime, "image/"):
		return event.MsgImage
	case strings.HasPrefix(mime, "video/"):
		return event.MsgVideo
	case strings.HasPrefix(mime, "audio/"):
		return event.MsgAudio
	}
	return event.MsgFile
}

// commonExtensions are the names people expect, where Go's MIME table would
// pick the first of several alphabetically (".m4v" for an mp4, ".jfif" for a
// jpeg).
var commonExtensions = map[string]string{
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"image/gif":       ".gif",
	"image/webp":      ".webp",
	"image/heic":      ".heic",
	"video/mp4":       ".mp4",
	"video/quicktime": ".mov",
	"audio/mp4":       ".m4a",
	"audio/mpeg":      ".mp3",
	"text/plain":      ".txt",
	"application/pdf": ".pdf",
}

// defaultFileName names a file Peachy sent without one (photos usually are).
func defaultFileName(mime string) string {
	ext, ok := commonExtensions[mime]
	if !ok {
		if exts, _ := gomime.ExtensionsByType(mime); len(exts) > 0 {
			ext = exts[0]
		}
	}
	switch msgTypeFor(mime) {
	case event.MsgImage:
		return "photo" + ext
	case event.MsgVideo:
		return "video" + ext
	case event.MsgAudio:
		return "audio" + ext
	}
	return "file" + ext
}

func (c *PeachyClient) Disconnect() {
	c.flushAllAlbums()
	c.deliveredMu.Lock()
	for conv, d := range c.delivered {
		d.timer.Stop() // a lost delivered mark is repaired by the next one
		delete(c.delivered, conv)
	}
	c.deliveredMu.Unlock()
	if c.stop != nil {
		c.stop()
		c.stop = nil
	}
}

func (c *PeachyClient) IsLoggedIn() bool {
	return c.device.Status().Linked
}

// LogoutRemote stops the bridge for this login. The device stays on the
// account until it's removed in Peachy's Devices list: the bridge can't
// remove itself through the calls it has.
func (c *PeachyClient) LogoutRemote(ctx context.Context) {
	c.Disconnect()
}

func (c *PeachyClient) IsThisUser(ctx context.Context, userID networkid.UserID) bool {
	return string(userID) == c.me
}

func (c *PeachyClient) GetChatInfo(ctx context.Context, portal *bridgev2.Portal) (*bridgev2.ChatInfo, error) {
	convs, err := c.device.Conversations()
	if err != nil {
		return nil, err
	}
	var conv *peachy.Conversation
	for i := range convs {
		if convs[i].ID == string(portal.ID) {
			conv = &convs[i]
		}
	}
	if conv == nil {
		return nil, fmt.Errorf("not in conversation %s", portal.ID)
	}

	members := &bridgev2.ChatMemberList{IsFull: true}
	var others []string
	for _, m := range conv.Members {
		members.Members = append(members.Members, bridgev2.ChatMember{
			EventSender: c.sender(m),
			Membership:  event.MembershipJoin,
		})
		if m != c.me {
			others = append(others, m)
		}
	}
	info := &bridgev2.ChatInfo{Members: members, Disappear: &database.DisappearingSetting{}}
	if conv.Timer > 0 {
		// Messages sent from Matrix carry it too (peachy-go adds it), and
		// bridgev2 counts them down from sending, as Peachy does.
		info.Disappear = &database.DisappearingSetting{Type: event.DisappearingTypeAfterRead, Timer: conv.Timer}
	}
	if conv.Photo != nil {
		photo := *conv.Photo
		info.Avatar = &bridgev2.Avatar{
			ID:  networkid.AvatarID(photo.ObjectKey),
			Get: func(ctx context.Context) ([]byte, error) { return c.device.Download(ctx, photo) },
		}
	} else if conv.Name != "" || len(others) != 1 {
		info.Avatar = &bridgev2.Avatar{Remove: true}
	}

	// A conversation with no name of its own and one other person is a DM,
	// named after them by the bridge's DM room logic.
	if conv.Name == "" && len(others) == 1 {
		dm := database.RoomTypeDM
		info.Type = &dm
		members.OtherUserID = networkid.UserID(others[0])
	} else {
		name := conv.Name
		if name == "" {
			name = c.namesOf(ctx, others)
		}
		info.Name = &name
		group := database.RoomTypeDefault
		info.Type = &group
	}
	return info, nil
}

func (c *PeachyClient) namesOf(ctx context.Context, ids []string) string {
	people, err := c.device.People(ctx, ids)
	if err != nil {
		return "Peachy group"
	}
	var names []string
	for _, p := range people {
		names = append(names, p.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

func (c *PeachyClient) GetUserInfo(ctx context.Context, ghost *bridgev2.Ghost) (*bridgev2.UserInfo, error) {
	people, err := c.device.People(ctx, []string{string(ghost.ID)})
	if err != nil {
		return nil, err
	}
	if len(people) != 1 {
		return nil, fmt.Errorf("no Peachy user %s", ghost.ID)
	}
	return c.userInfo(people[0]), nil
}

func (c *PeachyClient) userInfo(p peachy.Person) *bridgev2.UserInfo {
	name := c.settings().NameFor(p.ID, p.Name)
	info := &bridgev2.UserInfo{Name: &name, Avatar: &bridgev2.Avatar{Remove: true}, Identifiers: []string{}}
	if p.Handle != "" {
		// Beeper shows this under the person's name.
		info.Identifiers = []string{"peachy:@" + p.Handle}
	}
	if p.AvatarURL != "" {
		info.Avatar = &bridgev2.Avatar{
			ID:  networkid.AvatarID(p.AvatarURL),
			Get: func(ctx context.Context) ([]byte, error) { return c.device.Avatar(ctx, p.AvatarURL) },
		}
	}
	return info
}

// refreshProfiles brings everyone in the device's conversations up to date:
// bridgev2 asks for a person's profile only once, so a new name or photo
// would otherwise wait for a new login.
func (c *PeachyClient) refreshProfiles(ctx context.Context, convs []peachy.Conversation) {
	var ids []string
	for _, conv := range convs {
		for _, m := range conv.Members {
			if !slices.Contains(ids, m) {
				ids = append(ids, m)
			}
		}
	}
	if len(ids) == 0 {
		return
	}
	people, err := c.device.People(ctx, ids)
	if err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Msg("Peachy: couldn't refresh profiles")
		return
	}
	for _, p := range people {
		ghost, err := c.pc.Bridge.GetGhostByID(ctx, networkid.UserID(p.ID))
		if err != nil || ghost == nil {
			continue
		}
		ghost.UpdateInfo(ctx, c.userInfo(p))
	}
}

func (c *PeachyClient) GetCapabilities(ctx context.Context, portal *bridgev2.Portal) *event.RoomFeatures {
	return roomFeatures
}

// What a Peachy chat takes from Matrix: words, and files up to Peachy's
// sealed attachment cap, with a caption.
var roomFeatures = func() *event.RoomFeatures {
	file := &event.FileFeatures{
		MimeTypes: map[string]event.CapabilitySupportLevel{"*/*": event.CapLevelFullySupported},
		Caption:   event.CapLevelFullySupported,
		MaxSize:   peachy.MaxAttachmentBytes,
	}
	return &event.RoomFeatures{
		ID: "com.github.collinjackson.peachy.capabilities.2026_10_28.2",
		File: event.FileFeatureMap{
			event.MsgImage:  file,
			event.MsgVideo:  file,
			event.MsgAudio:  file,
			event.MsgFile:   file,
			event.CapMsgGIF: file,
		},
		Reply:         event.CapLevelFullySupported,
		Reaction:      event.CapLevelFullySupported,
		ReactionCount: 1,
		Edit:          event.CapLevelFullySupported,
		EditMaxCount:  peachy.MaxEditRevision,
		EditMaxAge:    ptr.Ptr(jsontime.S(editSendWindow)),
		Delete:        event.CapLevelFullySupported,
		ReadReceipts:  true,
	}
}()

func (c *PeachyClient) HandleMatrixMessage(ctx context.Context, msg *bridgev2.MatrixMessage) (*bridgev2.MatrixMessageResponse, error) {
	content := msg.Content
	var id string
	var err error
	switch content.MsgType {
	case event.MsgText, event.MsgNotice, event.MsgEmote:
		text := c.matrixText(ctx, content)
		if content.MsgType == event.MsgEmote {
			text = "/me " + text
		}
		c.inOrder(msg.Portal.ID, func() { // after photos sent just before
			id, err = c.device.SendText(ctx, string(msg.Portal.ID), text, replyTo(msg))
		})
	case event.MsgImage, event.MsgVideo, event.MsgAudio, event.MsgFile:
		return c.queueMatrixMedia(ctx, msg)
	default:
		return nil, fmt.Errorf("%w: %s isn't bridged to Peachy", bridgev2.ErrUnsupportedMessageType, content.MsgType)
	}
	if err != nil {
		return nil, err
	}
	c.acceptOnAnswer(ctx, msg.Portal)
	return &bridgev2.MatrixMessageResponse{
		DB: &database.Message{
			ID:        networkid.MessageID(id),
			SenderID:  networkid.UserID(c.me),
			Timestamp: time.Now(),
			Metadata:  &MessageMetadata{},
		},
	}, nil
}

// acceptOnAnswer lets the other person in when you answer their DM from
// Matrix, as answering a message request in Peachy does; otherwise the chat
// would stay a request on your phone.
func (c *PeachyClient) acceptOnAnswer(ctx context.Context, portal *bridgev2.Portal) {
	other := string(portal.OtherUserID)
	if other == "" {
		return
	}
	if s := c.settings(); s != nil && s.Contacts[other].Accepted {
		return
	}
	if err := c.device.Accept(ctx, other); err != nil {
		zerolog.Ctx(ctx).Warn().Err(err).Msg("Peachy: couldn't accept the message request")
	} else {
		c.syncStore(ctx)
	}
}

// replyTo is the Peachy message a Matrix message answers, or "".
func replyTo(msg *bridgev2.MatrixMessage) string {
	if msg.ReplyTo != nil {
		return string(msg.ReplyTo.ID)
	}
	return ""
}

// queueMatrixMedia fetches a file from Matrix and adds it to the album
// going to Peachy (album.go).
func (c *PeachyClient) queueMatrixMedia(ctx context.Context, msg *bridgev2.MatrixMessage) (*bridgev2.MatrixMessageResponse, error) {
	content := msg.Content
	data, err := c.pc.Bridge.Bot.DownloadMedia(ctx, content.URL, content.File)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", bridgev2.ErrMediaDownloadFailed, err)
	}
	if len(data) > peachy.MaxAttachmentBytes {
		return nil, bridgev2.ErrMediaTooLarge
	}
	m := peachy.Media{
		Data:     data,
		FileName: content.GetFileName(),
	}
	if info := content.Info; info != nil {
		m.Mime, m.Width, m.Height, m.DurationMS = info.MimeType, info.Width, info.Height, int64(info.Duration)
	}
	if m.Mime == "" {
		m.Mime = http.DetectContentType(data)
	}
	if content.MsgType == event.MsgImage {
		// Measured rather than trusted: Peachy lays the bubble out from these
		// before the photo arrives.
		if w, h, color := describeImage(data); w > 0 {
			m.Width, m.Height, m.Color = w, h, color
		}
	}
	zerolog.Ctx(ctx).Debug().Str("msgtype", string(content.MsgType)).Str("mime", m.Mime).Int("bytes", len(data)).
		Msg("Peachy: queued media for an album")
	return c.queueMedia(msg.Portal, m, content.GetCaption(), replyTo(msg))
}
