package notify

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/jordan-wright/email"
	twilio "github.com/sfreiberg/gotwilio"
	"golang.org/x/oauth2/jwt"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
)

// SMSTunnel is a Tunnel. It can send a Poke.
type SMSTunnel struct {
	c  *twilio.Twilio
	id string
}

// NewSMSTunnel returns a SMSTunnel
func NewSMSTunnel(num string, c *twilio.Twilio) *SMSTunnel {
	if c == nil {
		c = twilio.NewTwilioClient(os.Getenv("TWILIO_SID"), os.Getenv("TWILIO_AUTH_TOKEN"))
	}
	return &SMSTunnel{
		c:  c,
		id: num,
	}
}

// Type is a method of Tunnel interface
func (SMSTunnel) Type() string { return TypeSMS }

// ID is a method of Tunnel interface
func (t SMSTunnel) ID() string { return t.id }

// ID is a method of resource interface
func (t SMSTunnel) describe() string {
	return fmt.Sprintf("service/%s/tunnel/%s/id/%s", "notify", t.Type(), t.ID())
}

// Send sends a poke through twilio sms.
func (t SMSTunnel) Send(p *Poke) (Record, error) {
	rec := new(Record)
	rec.MessageID = p.ID

	// TODO: register callback
	var callbackURL string
	var err error

	// callbackURL = fmt.Sprintf("https://%s/twilioSMSCallback/%s", "sad", p.ID)

	resp, ex, err := t.c.SendSMS(t.ID(), p.To, string(p.Body), callbackURL, t.c.AccountSid)

	if err != nil {
		rec.TimeStamp = time.Now()
		rec.Status = StatusError
		return *rec, err
	}

	if ex != nil {
		rec.TimeStamp = time.Now()
		rec.Status = ex.MoreInfo
		return *rec, fmt.Errorf("twilio exception: %#v", ex)
	}

	// finally, check response
	tm, err := resp.DateUpdateAsTime()
	if err != nil {
		tm = time.Now()
	}
	rec.TimeStamp = tm
	rec.Status = resp.Status
	return *rec, err
}

// GMailTunnel is a Tunnel. It also implements the resource interface
// It should be initialize by NewGmailTunnel()
type GMailTunnel struct {
	email string
	cred  *jwt.Config
	svc   *gmail.Service
}

// NewGMailTunnel returns a G-Suite domain-delegated gmail tunnel.
func NewGMailTunnel(subject string, base *jwt.Config) (GMailTunnel, error) {
	t := GMailTunnel{}

	pkey := make([]byte, len(base.PrivateKey))
	copy(pkey, base.PrivateKey)

	claims := make(map[string]interface{})
	for k, v := range base.PrivateClaims {
		claims[k] = v
	}

	t.cred = &jwt.Config{
		Email:         base.Email,
		PrivateKey:    pkey,
		PrivateKeyID:  base.PrivateKeyID,
		Scopes:        append([]string{}, base.Scopes...),
		TokenURL:      base.TokenURL,
		Expires:       base.Expires,
		Audience:      base.Audience,
		PrivateClaims: claims,
		UseIDToken:    base.UseIDToken,
	}

	t.cred.Subject = subject
	t.email = subject
	ctx := context.TODO()
	ts := t.cred.TokenSource(ctx)
	svc, err := gmail.NewService(ctx, option.WithTokenSource(ts))
	if err != nil {
		return GMailTunnel{}, err
	}
	t.svc = svc
	return t, nil
}

// Type returns its Type
func (GMailTunnel) Type() string { return TypeEmail }

// ID returns its ID, as a identity of Tunnel
func (t GMailTunnel) ID() string { return fmt.Sprintf("%s", t.email) }
func (t GMailTunnel) describe() string {
	return fmt.Sprintf("service/%s/tunnel/%s/id/%s", "notify", t.Type(), t.ID())
}

// Send sends a poke thought GMailTunnel
func (t GMailTunnel) Send(p *Poke) (Record, error) {
	rec := Record{
		MessageID: p.ID,
	}
	// compose email message body
	msg := &email.Email{
		To:      []string{p.To},
		Subject: p.Subject,
		Text:    []byte(p.Body),
	}
	rawBs, err := msg.Bytes()
	if err != nil {
		rec.Status = StatusError
		rec.TimeStamp = time.Now()
		return rec, err
	}

	raw := base64.URLEncoding.EncodeToString(rawBs)
	// use the tunnel.

	if t.svc == nil {
		rec.TimeStamp = time.Now()
		rec.Status = StatusError
		return rec, fmt.Errorf("could not get gmail service: %s ", fmt.Sprint(" got `nil` "))
	}

	_, err = t.svc.Users.Messages.Send(t.email, &gmail.Message{
		Raw: raw,
	}).Do()

	if err != nil {
		rec.TimeStamp = time.Now()
		rec.Status = StatusUndelivered
		if apiErr, ok := err.(*googleapi.Error); ok {
			rec.TimeStamp = time.Now()
			return rec, fmt.Errorf("gmail error: %s", apiErr.Message)
		}
		return rec, err
	}

	rec.TimeStamp = time.Now()
	rec.Status = StatusDelivered

	return rec, nil
}

// LogWrapper is a Tunnel that can save Record during sending a Poke
type LogWrapper struct {
	t Tunnel
	c *firestore.Client
}

// NewLogWrapperTunnel returns a LogWrapper.
func NewLogWrapperTunnel(t Tunnel, c *firestore.Client) *LogWrapper {
	if c == nil {
		panic("initailze LogWrapper with invalid firestore client")
	}
	return &LogWrapper{
		t: t,
		c: c,
	}
}

// Type is a method of Tunnel interface
func (t LogWrapper) Type() string { return t.t.Type() }

// ID is a method of Tunnel interface
func (t LogWrapper) ID() string { return t.t.ID() }

// describe is a method of resource interface
func (t LogWrapper) describe() string { return t.t.describe() }

// Send is a method of Tunnel interface.
// A Logger Send a Poke with proper record storage.
func (t LogWrapper) Send(p *Poke) (Record, error) {
	ctx := context.TODO()
	var rec Record
	var err error
	rec.MessageID = p.ID

	err = t.c.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		var err error // local error
		rec, err = t.t.Send(p)

		ref := t.c.Collection("service/notify/record").NewDoc()
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s", err)
		}
		err = tx.Create(ref, rec)
		return err
	})

	// Record
	return rec, err
}
