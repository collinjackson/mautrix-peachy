package connector

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/collinjackson/peachy-go"
)

// albumWindow is how long the bridge waits after a photo or file from Matrix
// for another to join it. Matrix clients send several picked photos as
// separate events moments apart; Peachy shows photos as an album only when
// they travel in one message, so the bridge gathers them.
const albumWindow = 1500 * time.Millisecond

// album is the media from Matrix waiting to go to one Peachy conversation as
// one message.
type album struct {
	id       string
	roomID   id.RoomID
	media    []peachy.Media
	captions []string
	// The message the album answers: the first reply among its files.
	replyTo string
	timer   *time.Timer
}

// albums holds each portal's open album, and each portal's send queue: a
// goroutine that runs the portal's sends one at a time, in the order they
// were queued. mu is held only to change pending or to queue a send, never
// across a send, so a slow upload in one chat holds up nothing but later
// sends to that chat.
type albums struct {
	mu      sync.Mutex
	pending map[networkid.PortalID]*album
	queues  map[networkid.PortalID]chan func()
}

// enqueueLocked queues a send for the portal. Called with mu held, which
// is what keeps the queue in the order things happened.
func (as *albums) enqueueLocked(portalID networkid.PortalID, send func()) {
	if as.queues == nil {
		as.queues = map[networkid.PortalID]chan func(){}
	}
	q := as.queues[portalID]
	if q == nil {
		q = make(chan func(), 64)
		as.queues[portalID] = q
		go func() {
			for send := range q {
				send()
			}
		}()
	}
	q <- send
}

// closeLocked takes the portal's open album, if any, out of pending and
// queues its send. Called with mu held.
func (c *PeachyClient) closeLocked(portalID networkid.PortalID) {
	if a := c.albums.pending[portalID]; a != nil {
		a.timer.Stop()
		delete(c.albums.pending, portalID)
		c.albums.enqueueLocked(portalID, func() { c.sendAlbum(portalID, a) })
	}
}

// queueMedia adds one file to the portal's album, starting one if none is
// open (or the open one is full), and answers with the Peachy message and
// part it will be. The album goes out once albumWindow passes with nothing
// new.
func (c *PeachyClient) queueMedia(portal *bridgev2.Portal, m peachy.Media, caption, replyTo string) (*bridgev2.MatrixMessageResponse, error) {
	c.albums.mu.Lock()
	defer c.albums.mu.Unlock()
	if c.albums.pending == nil {
		c.albums.pending = map[networkid.PortalID]*album{}
	}
	a := c.albums.pending[portal.ID]
	if a != nil && len(a.media) >= peachy.MaxAttachments {
		c.closeLocked(portal.ID)
		a = nil
	}
	if a == nil {
		msgID, err := peachy.NewMessageID()
		if err != nil {
			return nil, err
		}
		a = &album{id: msgID, roomID: portal.MXID}
		c.albums.pending[portal.ID] = a
		a.timer = time.AfterFunc(albumWindow, func() { c.albumTimerFired(portal.ID, a) })
	} else {
		a.timer.Reset(albumWindow)
	}
	a.media = append(a.media, m)
	if caption != "" {
		a.captions = append(a.captions, caption)
	}
	if a.replyTo == "" {
		a.replyTo = replyTo
	}
	return &bridgev2.MatrixMessageResponse{
		DB: &database.Message{
			ID:        networkid.MessageID(a.id),
			PartID:    networkid.PartID(strconv.Itoa(len(a.media) - 1)),
			SenderID:  networkid.UserID(c.me),
			Timestamp: time.Now(),
			Metadata:  &MessageMetadata{},
		},
	}, nil
}

// inOrder runs send after the portal's open album and anything else
// already queued for the portal, and waits for it: a text message goes out
// after the photos Matrix sent before it.
func (c *PeachyClient) inOrder(portalID networkid.PortalID, send func()) {
	done := make(chan struct{})
	c.albums.mu.Lock()
	c.closeLocked(portalID)
	c.albums.enqueueLocked(portalID, func() {
		defer close(done)
		send()
	})
	c.albums.mu.Unlock()
	<-done
}

// flushAllAlbums sends every open album and waits for each portal's queue
// to drain, so a disconnect doesn't drop photos already answered as sent.
func (c *PeachyClient) flushAllAlbums() {
	c.albums.mu.Lock()
	for portalID := range c.albums.pending {
		c.closeLocked(portalID)
	}
	var waits []chan struct{}
	for portalID := range c.albums.queues {
		done := make(chan struct{})
		c.albums.enqueueLocked(portalID, func() { close(done) })
		waits = append(waits, done)
	}
	c.albums.mu.Unlock()
	for _, done := range waits {
		<-done
	}
}

// albumTimerFired queues a's send if it is still the portal's open album.
func (c *PeachyClient) albumTimerFired(portalID networkid.PortalID, a *album) {
	c.albums.mu.Lock()
	defer c.albums.mu.Unlock()
	if c.albums.pending[portalID] == a {
		c.closeLocked(portalID)
	}
}

// sendAlbum runs on the portal's send queue. The Matrix events were
// already answered, so a failure is reported as a notice in the room.
func (c *PeachyClient) sendAlbum(portalID networkid.PortalID, a *album) {
	ctx, cancel := context.WithTimeout(c.login.Log.WithContext(context.Background()), 10*time.Minute)
	defer cancel()
	log := zerolog.Ctx(ctx).With().Str("peachy_message", a.id).Int("files", len(a.media)).Int("bytes", a.bytes()).Logger()
	started := time.Now()
	log.Debug().Msg("Peachy: sending media")
	err := c.device.SendMedia(ctx, a.id, string(portalID), strings.Join(a.captions, "\n"), a.replyTo, a.media)
	if err == nil {
		log.Info().Dur("took", time.Since(started)).Msg("Peachy: sent media")
		return
	}
	log.Err(err).Dur("took", time.Since(started)).Msg("Peachy: couldn't send media")
	what := "This file"
	if len(a.media) > 1 {
		what = strconv.Itoa(len(a.media)) + " files"
	}
	_, _ = c.pc.Bridge.Bot.SendMessage(ctx, a.roomID, event.EventMessage, &event.Content{
		Parsed: &event.MessageEventContent{
			MsgType: event.MsgNotice,
			Body:    what + " didn't reach Peachy: " + err.Error(),
		},
	}, nil)
}

func (a *album) bytes() (n int) {
	for _, m := range a.media {
		n += len(m.Data)
	}
	return
}
