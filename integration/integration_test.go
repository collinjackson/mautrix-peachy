//go:build integration && testissuer

// Package integration runs the bridge end to end with no real accounts: a
// local Synapse, the bridge's binary, and peachy-go's in-memory fake Peachy
// server (peachytest). The bridge logs in to a fake account over the
// provisioning API; the test plays the other people with peachy-go Devices
// and a Matrix client, and checks that each kind of event crosses both ways.
//
// scripts/integration.sh sets up Synapse and the bridge's config and runs
// it; it needs these, which the script sets:
//
//	INTEGRATION_HOMESERVER     Synapse's client API (open registration)
//	INTEGRATION_BRIDGE         the bridge binary, built against the
//	                           test-issuer engine
//	INTEGRATION_BRIDGE_CONFIG  its config, registered with Synapse
//	INTEGRATION_BRIDGE_URL     where it listens (appservice.address)
//	INTEGRATION_LOG_DIR        where to leave bridge.log (optional)
package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.mau.fi/util/variationselector"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"github.com/collinjackson/peachy-go"
	"github.com/collinjackson/peachy-go/peachytest"
)

// timeout bounds every wait for something to cross the bridge.
const timeout = 30 * time.Second

const (
	alicePhone = "+15555550100" // the bridge's Peachy account
	bobPhone   = "+15555550101"
	carolPhone = "+15555550102"
)

func TestBridge(t *testing.T) {
	env := func(k string) string {
		v := os.Getenv(k)
		if v == "" {
			t.Skipf("%s isn't set; run scripts/integration.sh", k)
		}
		return v
	}
	hs := env("INTEGRATION_HOMESERVER")
	bin := env("INTEGRATION_BRIDGE")
	config := env("INTEGRATION_BRIDGE_CONFIG")
	bridgeURL := env("INTEGRATION_BRIDGE_URL")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv := peachytest.Start(t)
	aliceActor := srv.AddAccount(alicePhone, "Alice", "alice")

	b := startBridge(t, bin, config, srv.URL)
	mx := newMatrixUser(t, ctx, hs)
	b.login(t, ctx, bridgeURL, mx.userID, alicePhone)

	bob := signIn(t, ctx, srv, bobPhone, "Bob", "bob")
	carol := signIn(t, ctx, srv, carolPhone, "Carol", "carol")
	bobGhost := ghost(bob.actor)

	// A DM, which Bob starts. The bridge's device has published key
	// packages once it's connected, which the login's end doesn't wait for.
	var dm string
	retry(t, "Bob to start a DM with the bridge's account", func() error {
		var err error
		dm, err = bob.FoundGroupForTest(ctx, "", true, []string{aliceActor})
		return err
	})
	room := mx.portal(t, dm)

	t.Run("dm", func(t *testing.T) {
		// Text, both ways.
		bobHi, err := bob.SendText(ctx, dm, "hi alice", "")
		if err != nil {
			t.Fatal(err)
		}
		evBobHi := mx.waitMessage(t, room, bobGhost, "Bob's text", func(c *event.MessageEventContent) bool {
			return c.Body == "hi alice"
		}).ID
		evAliceHi := mx.sendText(t, ctx, room, "hi bob", "")
		aliceHi := bob.waitEvent(t, "Alice's text", func(e peachy.Event) bool {
			return e.Type == peachy.EventMessage && e.Conversation == dm && e.Sender == aliceActor && e.Text == "hi bob"
		}).ID

		// Replies, both ways.
		if _, err := bob.SendText(ctx, dm, "a reply", aliceHi); err != nil {
			t.Fatal(err)
		}
		mx.waitMessage(t, room, bobGhost, "Bob's reply", func(c *event.MessageEventContent) bool {
			return c.Body == "a reply" && c.RelatesTo.GetReplyTo() == evAliceHi
		})
		mx.sendText(t, ctx, room, "a reply back", evBobHi)
		bob.waitEvent(t, "Alice's reply", func(e peachy.Event) bool {
			return e.Type == peachy.EventMessage && e.Text == "a reply back" && e.ReplyTo == bobHi
		})

		// Reactions, added and taken back, both ways.
		if err := bob.React(ctx, dm, aliceHi, "👍", false); err != nil {
			t.Fatal(err)
		}
		evReaction := mx.wait(t, "Bob's reaction", func(e *event.Event) bool {
			r := e.Content.AsReaction()
			return e.RoomID == room && e.Sender == bobGhost && e.Type == event.EventReaction &&
				r.RelatesTo.EventID == evAliceHi && variationselector.Remove(r.RelatesTo.Key) == "👍"
		}).ID
		if err := bob.React(ctx, dm, aliceHi, "👍", true); err != nil {
			t.Fatal(err)
		}
		mx.waitRedaction(t, room, evReaction, "Bob's reaction taken back")

		resp, err := mx.client.SendReaction(ctx, room, evBobHi, "🎉")
		if err != nil {
			t.Fatal(err)
		}
		bob.waitEvent(t, "Alice's reaction", func(e peachy.Event) bool {
			return e.Type == peachy.EventReaction && e.Target == bobHi && e.Emoji == "🎉" && !e.Remove && e.Sender == aliceActor
		})
		if _, err := mx.client.RedactEvent(ctx, room, resp.EventID); err != nil {
			t.Fatal(err)
		}
		bob.waitEvent(t, "Alice's reaction taken back", func(e peachy.Event) bool {
			return e.Type == peachy.EventReaction && e.Target == bobHi && e.Remove && e.Sender == aliceActor
		})

		// Edits, both ways.
		if err := bob.Edit(ctx, dm, bobHi, 1, "hi Alice"); err != nil {
			t.Fatal(err)
		}
		mx.waitMessage(t, room, bobGhost, "Bob's edit", func(c *event.MessageEventContent) bool {
			return c.RelatesTo.GetReplaceID() == evBobHi && c.NewContent != nil && c.NewContent.Body == "hi Alice"
		})
		edit := &event.MessageEventContent{MsgType: event.MsgText, Body: "hi Bob"}
		edit.SetEdit(evAliceHi)
		mx.send(t, ctx, room, edit)
		bob.waitEvent(t, "Alice's edit", func(e peachy.Event) bool {
			return e.Type == peachy.EventEdit && e.Target == aliceHi && e.Text == "hi Bob" && e.Revision == 1
		})

		// Deletes, both ways.
		if err := bob.Delete(ctx, dm, bobHi); err != nil {
			t.Fatal(err)
		}
		mx.waitRedaction(t, room, evBobHi, "Bob's delete")
		if _, err := mx.client.RedactEvent(ctx, room, evAliceHi); err != nil {
			t.Fatal(err)
		}
		bob.waitEvent(t, "Alice's delete", func(e peachy.Event) bool {
			return e.Type == peachy.EventDelete && e.Target == aliceHi && e.Sender == aliceActor
		})
	})

	t.Run("media", func(t *testing.T) {
		// A photo and a file from Peachy.
		photo := pngOf(t, 3, 2)
		mid, _ := peachy.NewMessageID()
		if err := bob.SendMedia(ctx, mid, dm, "", "", []peachy.Media{{Data: photo, Mime: "image/png", FileName: "dot.png", Width: 3, Height: 2}}); err != nil {
			t.Fatal(err)
		}
		c := mx.waitMessage(t, room, bobGhost, "Bob's photo", func(c *event.MessageEventContent) bool {
			return c.MsgType == event.MsgImage
		}).Content.AsMessage()
		if got := mx.download(t, ctx, c); !bytes.Equal(got, photo) {
			t.Fatalf("Bob's photo arrived as %d bytes, not %d", len(got), len(photo))
		}
		if c.Info == nil || c.Info.MimeType != "image/png" || c.Info.Width != 3 || c.Info.Height != 2 {
			t.Fatalf("Bob's photo's info %+v", c.Info)
		}

		notes := []byte("bring the rope\n")
		mid, _ = peachy.NewMessageID()
		if err := bob.SendMedia(ctx, mid, dm, "the list", "", []peachy.Media{{Data: notes, Mime: "text/plain", FileName: "notes.txt"}}); err != nil {
			t.Fatal(err)
		}
		c = mx.waitMessage(t, room, bobGhost, "Bob's file", func(c *event.MessageEventContent) bool {
			return c.MsgType == event.MsgFile
		}).Content.AsMessage()
		if c.FileName != "notes.txt" || c.Body != "the list" {
			t.Fatalf("Bob's file is named %q with caption %q", c.FileName, c.Body)
		}
		if got := mx.download(t, ctx, c); !bytes.Equal(got, notes) {
			t.Fatalf("Bob's file arrived as %q", got)
		}

		// A photo from Matrix.
		photo = pngOf(t, 4, 5)
		up, err := mx.client.UploadBytesWithName(ctx, photo, "image/png", "square.png")
		if err != nil {
			t.Fatal(err)
		}
		mx.send(t, ctx, room, &event.MessageEventContent{MsgType: event.MsgImage, Body: "square.png", URL: up.ContentURI.CUString(),
			Info: &event.FileInfo{MimeType: "image/png", Size: len(photo), Width: 4, Height: 5}})
		m := bob.waitEvent(t, "Alice's photo", func(e peachy.Event) bool {
			return e.Type == peachy.EventMessage && e.Sender == aliceActor && len(e.Attachments) == 1
		})
		a := m.Attachments[0]
		if a.Mime != "image/png" || a.Width != 4 || a.Height != 5 {
			t.Fatalf("Alice's photo %+v", a)
		}
		got, err := bob.Download(ctx, a)
		if err != nil || !bytes.Equal(got, photo) {
			t.Fatalf("Alice's photo: %d bytes, %v", len(got), err)
		}
	})

	t.Run("group", func(t *testing.T) {
		g, err := bob.FoundGroupForTest(ctx, "Climbers", false, []string{aliceActor})
		if err != nil {
			t.Fatal(err)
		}
		groom := mx.portal(t, g)
		mx.wait(t, "the group's name", func(e *event.Event) bool {
			return e.RoomID == groom && e.Type == event.StateRoomName && e.Content.AsRoomName().Name == "Climbers"
		})

		if _, err := bob.SendText(ctx, g, "who's in", ""); err != nil {
			t.Fatal(err)
		}
		mx.waitMessage(t, groom, bobGhost, "Bob's group text", func(c *event.MessageEventContent) bool {
			return c.Body == "who's in"
		})
		mx.sendText(t, ctx, groom, "me", "")
		bob.waitEvent(t, "Alice's group text", func(e peachy.Event) bool {
			return e.Type == peachy.EventMessage && e.Conversation == g && e.Sender == aliceActor && e.Text == "me"
		})

		// Carol joins, then leaves.
		carolGhost := ghost(carol.actor)
		if err := bob.AddMemberForTest(ctx, g, carol.actor); err != nil {
			t.Fatal(err)
		}
		mx.waitMember(t, groom, carolGhost, event.MembershipJoin, "Carol joining")
		carol.waitEvent(t, "Carol's welcome", func(e peachy.Event) bool {
			return e.Type == peachy.EventJoined && e.Conversation == g
		})
		mx.sendText(t, ctx, groom, "hi carol", "")
		carol.waitEvent(t, "Alice's text to Carol", func(e peachy.Event) bool {
			return e.Type == peachy.EventMessage && e.Conversation == g && e.Text == "hi carol"
		})
		if err := bob.RemoveMemberForTest(ctx, g, carol.actor); err != nil {
			t.Fatal(err)
		}
		mx.waitMember(t, groom, carolGhost, event.MembershipLeave, "Carol leaving")
	})
}

// ghost is a Peachy account's Matrix user (appservice.username_template).
func ghost(actor string) id.UserID {
	return id.NewUserID("peachy_"+actor, "localhost")
}

// retry runs f until it succeeds, for up to timeout.
func retry(t *testing.T, what string, f func() error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		err := f()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("waiting for %s: %v", what, err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func pngOf(t *testing.T, w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := range w {
		for y := range h {
			img.Set(x, y, color.RGBA{R: uint8(40 * x), G: uint8(40 * y), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// inbox keeps everything one side has received, so a wait finds an event
// that came before it started; each event satisfies at most one wait.
type inbox[T any] struct {
	mu    sync.Mutex
	items []T
	used  []bool
	wake  chan struct{}
}

func newInbox[T any]() *inbox[T] { return &inbox[T]{wake: make(chan struct{})} }

func (b *inbox[T]) add(v T) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.items = append(b.items, v)
	b.used = append(b.used, false)
	close(b.wake)
	b.wake = make(chan struct{})
}

func (b *inbox[T]) wait(t *testing.T, what string, match func(T) bool) T {
	t.Helper()
	deadline := time.After(timeout)
	for {
		b.mu.Lock()
		for i, v := range b.items {
			if !b.used[i] && match(v) {
				b.used[i] = true
				b.mu.Unlock()
				return v
			}
		}
		wake := b.wake
		b.mu.Unlock()
		select {
		case <-wake:
		case <-deadline:
			var zero T
			b.mu.Lock()
			for i, v := range b.items {
				if !b.used[i] {
					t.Logf("unmatched: %s", describe(v))
				}
			}
			b.mu.Unlock()
			t.Fatalf("timed out after %s waiting for %s", timeout, what)
			return zero
		}
	}
}

func describe(v any) string {
	if e, ok := v.(*event.Event); ok {
		j, _ := json.Marshal(e.Content.Raw)
		return fmt.Sprintf("%s %s in %s from %s: %s", e.Type.Type, e.ID, e.RoomID, e.Sender, j)
	}
	return fmt.Sprintf("%+v", v)
}

// The bridge

type bridge struct {
	secret string
	log    string
}

// startBridge runs the bridge's binary against the fake server, with its
// own database and data folder, until the test ends.
func startBridge(t *testing.T, bin, config, peachyURL string) *bridge {
	dir := t.TempDir()
	logDir := os.Getenv("INTEGRATION_LOG_DIR")
	if logDir == "" {
		logDir = dir
	}
	// A copy of the config: the bridge rewrites the one it runs with.
	cfg, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	b := &bridge{secret: randomHex(16), log: filepath.Join(logDir, "bridge.log")}
	logFile, err := os.Create(b.log)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "-c", cfgPath, "-n")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"MAUTRIX_PEACHY_NETWORK__SERVER="+peachyURL,
		"MAUTRIX_PEACHY_NETWORK__DATA_DIR="+filepath.Join(dir, "peachy-data"),
		"MAUTRIX_PEACHY_DATABASE__URI=file:"+filepath.Join(dir, "bridge.db")+"?_txlock=immediate",
		"MAUTRIX_PEACHY_PROVISIONING__SHARED_SECRET="+b.secret,
	)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		logFile.Close()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-exited:
		case <-time.After(15 * time.Second):
			_ = cmd.Process.Kill()
			<-exited
		}
		if t.Failed() {
			t.Logf("the bridge's log is %s", b.log)
		}
	})
	return b
}

// provision calls the bridge's provisioning API as user.
func (b *bridge) provision(ctx context.Context, base string, user id.UserID, method, path string, body, out any) error {
	var r io.Reader
	if body != nil {
		j, _ := json.Marshal(body)
		r = bytes.NewReader(j)
	}
	req, err := http.NewRequestWithContext(ctx, method, base+"/_matrix/provision"+path+"?user_id="+string(user), r)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+b.secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, data)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

type loginStep struct {
	LoginID string `json:"login_id"`
	Type    string `json:"type"`
	StepID  string `json:"step_id"`
}

// login signs the bridge in to Peachy as phone through the provisioning
// API, as Beeper does: phone number, texted code, then the link, which the
// fake's approver grants at once.
func (b *bridge) login(t *testing.T, ctx context.Context, base string, user id.UserID, phone string) {
	t.Helper()
	retry(t, "the bridge's provisioning API", func() error {
		return b.provision(ctx, base, user, "GET", "/v3/whoami", nil, nil)
	})
	var step loginStep
	if err := b.provision(ctx, base, user, "POST", "/v3/login/start/phone", nil, &step); err != nil {
		t.Fatal(err)
	}
	submit := func(input map[string]string) {
		t.Helper()
		path := fmt.Sprintf("/v3/login/step/%s/%s/%s", step.LoginID, step.StepID, step.Type)
		var body any
		if input != nil {
			body = input
		}
		if err := b.provision(ctx, base, user, "POST", path, body, &step); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; step.Type != "complete"; i++ {
		if i == 10 {
			t.Fatalf("login stuck at %s", step.StepID)
		}
		switch step.StepID {
		case "app.pchy.login.phone":
			submit(map[string]string{"phone": phone})
		case "app.pchy.login.code":
			submit(map[string]string{"code": peachytest.Code})
		case "app.pchy.login.link":
			submit(nil) // display_and_wait: returns when linking moves
		default:
			t.Fatalf("unexpected login step %s (%s)", step.StepID, step.Type)
		}
	}
}

// Peachy, the other side

type person struct {
	*peachy.Device
	actor  string
	events *inbox[peachy.Event]
}

// signIn makes an account on the fake and signs a Device in to it, which
// then reads its mailbox whenever the server says it moved.
func signIn(t *testing.T, ctx context.Context, srv *peachytest.Server, phone, name, handle string) person {
	t.Helper()
	actor := srv.AddAccount(phone, name, handle)
	dev, err := peachy.Open(srv.URL, t.TempDir(), name+"'s phone")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(dev.Close)
	if err := dev.StartLogin(ctx, phone); err != nil {
		t.Fatal(err)
	}
	if _, err := dev.FinishLogin(ctx, phone, peachytest.Code); err != nil {
		t.Fatal(err)
	}
	for i := 0; ; i++ {
		st, err := dev.LinkStep(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if st.State == peachy.LinkApproved {
			break
		}
		if i == 3 {
			t.Fatalf("%s still %s", name, st.State)
		}
	}
	if err := dev.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	p := person{Device: dev, actor: actor, events: newInbox[peachy.Event]()}
	lctx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go p.read(lctx, done)
	t.Cleanup(func() {
		stop()
		<-done
	})
	return p
}

// read polls the mailbox on each change the live stream reports, and every
// few seconds in case a signal is missed.
func (p person) read(ctx context.Context, done chan struct{}) {
	defer close(done)
	changes := p.Listen(ctx)
	tick := time.NewTicker(3 * time.Second)
	defer tick.Stop()
	for {
		for {
			events, more, err := p.Poll(ctx)
			if err != nil {
				break
			}
			for _, e := range events {
				p.events.add(e)
			}
			if !more {
				break
			}
		}
		select {
		case <-ctx.Done():
			return
		case _, ok := <-changes:
			if !ok {
				return
			}
		case <-tick.C:
		}
	}
}

func (p person) waitEvent(t *testing.T, what string, match func(peachy.Event) bool) peachy.Event {
	t.Helper()
	return p.events.wait(t, what, match)
}

// Matrix, the bridge's user

type matrixUser struct {
	client *mautrix.Client
	userID id.UserID
	events *inbox[*event.Event]
}

// newMatrixUser registers a new user on Synapse and syncs it until the test
// ends, joining every room it's invited to.
func newMatrixUser(t *testing.T, ctx context.Context, hs string) *matrixUser {
	t.Helper()
	client, err := mautrix.NewClient(hs, "", "")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.RegisterDummy(ctx, &mautrix.ReqRegister[any]{
		Username: "alice-" + randomHex(4),
		Password: randomHex(16),
	})
	if err != nil {
		t.Fatal(err)
	}
	client.SetCredentials(resp.UserID, resp.AccessToken)
	u := &matrixUser{client: client, userID: resp.UserID, events: newInbox[*event.Event]()}
	syncer := client.Syncer.(*mautrix.DefaultSyncer)
	syncer.OnEvent(func(ctx context.Context, e *event.Event) {
		if e.Type == event.StateMember && e.GetStateKey() == string(u.userID) &&
			e.Content.AsMember().Membership == event.MembershipInvite {
			go func() { _, _ = client.JoinRoomByID(ctx, e.RoomID) }()
		}
		u.events.add(e)
	})
	sctx, stop := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = client.SyncWithContext(sctx)
	}()
	t.Cleanup(func() {
		stop()
		<-done
	})
	return u
}

func (u *matrixUser) wait(t *testing.T, what string, match func(*event.Event) bool) *event.Event {
	t.Helper()
	return u.events.wait(t, what, match)
}

// portal is the room the bridge made for a Peachy conversation, found by
// its bridge info, once this user has joined it.
func (u *matrixUser) portal(t *testing.T, conversation string) id.RoomID {
	t.Helper()
	info := u.wait(t, "a room for "+conversation, func(e *event.Event) bool {
		if e.Type != event.StateBridge && e.Type != event.StateHalfShotBridge {
			return false
		}
		b, ok := e.Content.Parsed.(*event.BridgeEventContent)
		return ok && b.Channel.ID == conversation
	})
	u.waitMember(t, info.RoomID, u.userID, event.MembershipJoin, "joining the room for "+conversation)
	return info.RoomID
}

func (u *matrixUser) waitMember(t *testing.T, room id.RoomID, user id.UserID, m event.Membership, what string) {
	t.Helper()
	u.wait(t, what, func(e *event.Event) bool {
		return e.RoomID == room && e.Type == event.StateMember && e.GetStateKey() == string(user) &&
			e.Content.AsMember().Membership == m
	})
}

func (u *matrixUser) waitMessage(t *testing.T, room id.RoomID, sender id.UserID, what string, match func(*event.MessageEventContent) bool) *event.Event {
	t.Helper()
	return u.wait(t, what, func(e *event.Event) bool {
		return e.RoomID == room && e.Sender == sender && e.Type == event.EventMessage && match(e.Content.AsMessage())
	})
}

func (u *matrixUser) waitRedaction(t *testing.T, room id.RoomID, target id.EventID, what string) {
	t.Helper()
	u.wait(t, what, func(e *event.Event) bool {
		if e.RoomID != room || e.Type != event.EventRedaction {
			return false
		}
		return e.Redacts == target || e.Content.AsRedaction().Redacts == target
	})
}

func (u *matrixUser) send(t *testing.T, ctx context.Context, room id.RoomID, content *event.MessageEventContent) id.EventID {
	t.Helper()
	resp, err := u.client.SendMessageEvent(ctx, room, event.EventMessage, content)
	if err != nil {
		t.Fatal(err)
	}
	return resp.EventID
}

func (u *matrixUser) sendText(t *testing.T, ctx context.Context, room id.RoomID, text string, replyTo id.EventID) id.EventID {
	t.Helper()
	content := &event.MessageEventContent{MsgType: event.MsgText, Body: text}
	if replyTo != "" {
		content.RelatesTo = (&event.RelatesTo{}).SetReplyTo(replyTo)
	}
	return u.send(t, ctx, room, content)
}

func (u *matrixUser) download(t *testing.T, ctx context.Context, c *event.MessageEventContent) []byte {
	t.Helper()
	uri := c.URL
	if c.File != nil {
		t.Fatal("an encrypted file in an unencrypted room")
	}
	mxc, err := uri.Parse()
	if err != nil {
		t.Fatal(err)
	}
	data, err := u.client.DownloadBytes(ctx, mxc)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
