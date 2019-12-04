package notify

import (
	"time"
)

// Type X is the Type of a Tunnel
const (
	TypeSMS   = "sms"
	TypeEmail = "email"
	TypeVoice = "voice"
)

// status code that the Poke is
// Steal from  twilio sms status code. extend the same meaning to other Tunnels
const (
	StatusQueued      = "Queued"
	StatusDelivered   = "Delivered"
	StatusUndelivered = "Undelievered"
	StatusFailed      = "Failed" // message could not be sent. usually because the provider not accept the message.

	// Error is our error during composing
	StatusError = "Error"
)

// Tunnel describe how to send a Poke
type Tunnel interface {
	describe() string
	Type() string
	ID() string
	Send(p *Poke) (Record, error)
}

// Poke is a message to send
type Poke struct {
	ID         string    `firestore:"-" json:"id"`
	Tunnel     string    `firestore:"tunnel" json:"tunnel"`
	To         string    `firestore:"to" json:"to"`
	Subject    string    `firestore:"subject,omitempty" json:"subject,omitempty"` // sms ignores subject, because it does not have one.
	Body       string    `firestore:"body" json:"body"`
	DateToSend time.Time `firestore:"date_to_send" json:"date_to_send"`
	Expiry     time.Time `firestore:"expiry" json:"expiry"`
}

// ArchivedPoke is an archeived or delivered Poke
type ArchivedPoke struct {
	ID      string `firestore:"-" json:"id"`
	Tunnel  string `firestore:"tunnel" json:"tunnel"`
	To      string `firestore:"to" json:"to"`
	Expired bool   `firestore:"expired" json:"expired"` // is it get archived becuase of expired
}

// Record is a delivery record of a Poke. It lists all status change.
type Record struct {
	MessageID string    `firestore:"message_id" json:"message_id"`
	ID        string    `firestore:"-" json:"id"`
	Status    string    `firestore:"status" json:"status"`
	TimeStamp time.Time `firestore:"timestamp" json:"timestamp"`
}
