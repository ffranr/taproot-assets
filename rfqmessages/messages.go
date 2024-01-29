package rfqmessages

import (
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/routing/route"
)

// ID is the identifier for a RFQ message.
type ID [32]byte

// TapMessageTypeBaseOffset is the taproot-assets specific message type
// identifier base offset. All tap messages will have a type identifier that is
// greater than this value.
//
// This offset was chosen as the concatenation of the alphabetical index
// positions of the letters "t" (20), "a" (1), and "p" (16).
const TapMessageTypeBaseOffset = 20116 + uint32(lnwire.CustomTypeStart)

const (
	// MsgTypeRequest is the message type identifier for a quote request
	// message.
	MsgTypeRequest = TapMessageTypeBaseOffset + 0

	// MsgTypeAccept is the message type identifier for a quote accept
	// message.
	MsgTypeAccept = TapMessageTypeBaseOffset + 1

	// MsgTypeReject is the message type identifier for a quote
	// reject message.
	MsgTypeReject = TapMessageTypeBaseOffset + 2
)

// WireMessage is a struct that represents a general wire message.
type WireMessage struct {
	// Peer is the origin/destination peer for this message.
	Peer route.Vertex

	// MsgType is the protocol message type number.
	MsgType uint32

	// Data is the data exchanged.
	Data []byte
}

// OutgoingMessage is an interface that represents an outbound wire message
// that can be sent to a peer.
type OutgoingMessage interface {
	// ToWire returns a wire message with a serialized data field.
	ToWire() (WireMessage, error)
}
