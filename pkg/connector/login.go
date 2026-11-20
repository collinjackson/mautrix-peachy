package connector

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"github.com/collinjackson/peachy-go"
)

const (
	loginFlowPhone = "phone"
	stepPhone      = "app.pchy.login.phone"
	stepCode       = "app.pchy.login.code"
	stepLink       = "app.pchy.login.link"
	stepApprover   = "app.pchy.login.approver"
	noneOfThese    = "None of these"
	stepComplete   = "app.pchy.login.complete"
	linkPoll       = 2 * time.Second
	linkTimeout    = 10 * time.Minute
)

func (pc *PeachyConnector) GetLoginFlows() []bridgev2.LoginFlow {
	return []bridgev2.LoginFlow{{
		Name:        "Phone number",
		Description: "Sign in with your phone number, then add this bridge as a device from the Peachy app on your phone.",
		ID:          loginFlowPhone,
	}}
}

func (pc *PeachyConnector) CreateLogin(ctx context.Context, user *bridgev2.User, flowID string) (bridgev2.LoginProcess, error) {
	if flowID != loginFlowPhone {
		return nil, fmt.Errorf("unknown login flow %q", flowID)
	}
	return &phoneLogin{pc: pc, user: user}, nil
}

// phoneLogin walks: phone number → texted code → add this device from the
// phone (pick the number shown) → connected.
type phoneLogin struct {
	pc     *PeachyConnector
	user   *bridgev2.User
	dir    string
	device *peachy.Device
	phone  string
	shown  string
}

var (
	_ bridgev2.LoginProcessUserInput      = (*phoneLogin)(nil)
	_ bridgev2.LoginProcessDisplayAndWait = (*phoneLogin)(nil)
)

func (l *phoneLogin) Start(ctx context.Context) (*bridgev2.LoginStep, error) {
	// A pending folder until we know the account's id, then renamed.
	var b [8]byte
	_, _ = rand.Read(b[:])
	l.dir = filepath.Join(l.pc.Config.DataDir, "pending-"+hex.EncodeToString(b[:]))
	device, err := peachy.Open(l.pc.Config.Server, l.dir, l.pc.Config.DeviceName)
	if err != nil {
		return nil, err
	}
	l.device = device
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeUserInput,
		StepID:       stepPhone,
		Instructions: "Enter the phone number you use with Peachy.",
		UserInputParams: &bridgev2.LoginUserInputParams{
			Fields: []bridgev2.LoginInputDataField{{
				Type: bridgev2.LoginInputFieldTypePhoneNumber,
				ID:   "phone",
				Name: "Phone number",
			}},
		},
	}, nil
}

func (l *phoneLogin) SubmitUserInput(ctx context.Context, input map[string]string) (*bridgev2.LoginStep, error) {
	if phone, ok := input["phone"]; ok {
		l.phone = strings.TrimSpace(phone)
		if err := l.device.StartLogin(ctx, l.phone); err != nil {
			return nil, err
		}
		return &bridgev2.LoginStep{
			Type:         bridgev2.LoginStepTypeUserInput,
			StepID:       stepCode,
			Instructions: "Enter the code Peachy texted to " + l.phone + ".",
			UserInputParams: &bridgev2.LoginUserInputParams{
				Fields: []bridgev2.LoginInputDataField{{
					Type: bridgev2.LoginInputFieldType2FACode,
					ID:   "code",
					Name: "Code",
				}},
			},
		}, nil
	}
	if pick, ok := input["approver_number"]; ok {
		if pick == noneOfThese {
			pick = ""
		}
		if err := l.device.ConfirmApprover(pick); err != nil {
			return nil, fmt.Errorf("%w. If the bridge shows on your phone's device list, remove it, then log in again", err)
		}
		return l.linkStep(ctx)
	}
	if code, ok := input["code"]; ok {
		if _, err := l.device.FinishLogin(ctx, l.phone, strings.TrimSpace(code)); err != nil {
			return nil, err
		}
		return l.linkStep(ctx)
	}
	return nil, fmt.Errorf("unexpected input")
}

// linkStep reports where linking stands as a display-and-wait step: what to
// do on the phone, and the number to pick once the phone offers some.
func (l *phoneLogin) linkStep(ctx context.Context) (*bridgev2.LoginStep, error) {
	s, err := l.device.LinkStep(ctx)
	if err != nil {
		return nil, err
	}
	switch s.State {
	case peachy.LinkApproved:
		return l.complete(ctx)
	case peachy.LinkConfirmApprover:
		// Picking the phone's number here is what vouches for the phone: the
		// account key it sealed opens only after it.
		l.shown = "approver:" + strings.Join(s.Choices, ",")
		return &bridgev2.LoginStep{
			Type:         bridgev2.LoginStepTypeUserInput,
			StepID:       stepApprover,
			Instructions: fmt.Sprintf("%s now shows a number. Pick the same one here. If it isn't listed, pick %q.", s.Approver, noneOfThese),
			UserInputParams: &bridgev2.LoginUserInputParams{
				Fields: []bridgev2.LoginInputDataField{{
					Type:    bridgev2.LoginInputFieldTypeSelect,
					ID:      "approver_number",
					Name:    "Number on your phone",
					Options: append(append([]string{}, s.Choices...), noneOfThese),
				}},
			},
		}, nil
	case peachy.LinkPickNumber:
		l.shown = s.Number
		return &bridgev2.LoginStep{
			Type:         bridgev2.LoginStepTypeDisplayAndWait,
			StepID:       stepLink,
			Instructions: fmt.Sprintf("On %s, pick %s.", s.Approver, s.Number),
			DisplayAndWaitParams: &bridgev2.LoginDisplayAndWaitParams{
				Type: bridgev2.LoginDisplayTypeCode,
				Data: s.Number,
			},
		}, nil
	default:
		l.shown = ""
		return &bridgev2.LoginStep{
			Type:         bridgev2.LoginStepTypeDisplayAndWait,
			StepID:       stepLink,
			Instructions: `Open Peachy on your phone and tap "add another device". A number will show up here; pick the same one on your phone.`,
			DisplayAndWaitParams: &bridgev2.LoginDisplayAndWaitParams{
				Type: bridgev2.LoginDisplayTypeNothing,
			},
		}, nil
	}
}

// Wait polls linking until something the person should see changes.
func (l *phoneLogin) Wait(ctx context.Context) (*bridgev2.LoginStep, error) {
	deadline := time.After(linkTimeout)
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline:
			return nil, fmt.Errorf("timed out waiting for the phone to add this device")
		case <-time.After(linkPoll):
		}
		before := l.shown
		step, err := l.linkStep(ctx)
		if err != nil {
			return nil, err
		}
		if step.Type == bridgev2.LoginStepTypeComplete || l.shown != before {
			return step, nil
		}
	}
}

func (l *phoneLogin) complete(ctx context.Context) (*bridgev2.LoginStep, error) {
	if err := l.device.Connect(ctx); err != nil {
		return nil, err
	}
	me, err := l.device.Me()
	if err != nil {
		return nil, err
	}
	name := l.phone
	if people, err := l.device.People(ctx, []string{me}); err == nil && len(people) == 1 && people[0].Name != "" {
		name = people[0].Name
	}
	// Move the device to its permanent folder, named after the account.
	l.device.Close()
	l.device = nil
	id := networkid.UserLoginID(me)
	final := l.pc.loginDir(id)
	_ = os.RemoveAll(final) // a previous login of the same account, replaced
	if err := os.Rename(l.dir, final); err != nil {
		return nil, fmt.Errorf("keep the device: %w", err)
	}
	ul, err := l.user.NewLogin(ctx, &database.UserLogin{
		ID:         id,
		RemoteName: name,
	}, &bridgev2.NewLoginParams{DeleteOnConflict: true})
	if err != nil {
		return nil, err
	}
	go ul.Client.Connect(ul.Log.WithContext(context.Background()))
	return &bridgev2.LoginStep{
		Type:         bridgev2.LoginStepTypeComplete,
		StepID:       stepComplete,
		Instructions: "Connected to Peachy as " + name + ".",
		CompleteParams: &bridgev2.LoginCompleteParams{
			UserLoginID: ul.ID,
			UserLogin:   ul,
		},
	}, nil
}

func (l *phoneLogin) Cancel() {
	if l.device != nil {
		l.device.Close()
		l.device = nil
	}
	if l.dir != "" {
		// A login that never finished leaves a device row on the account
		// (it shows as pending in Peachy's Devices list) but no keys here.
		_ = os.RemoveAll(l.dir)
	}
}
