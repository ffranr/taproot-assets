package rfqmessages

import (
	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/lnwire"
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
	// MsgTypeQuoteRequest is the message type identifier for a quote
	// request message.
	MsgTypeQuoteRequest = TapMessageTypeBaseOffset + 1

	// MsgTypeQuoteAccept is the message type identifier for a quote
	// accept message.
	MsgTypeQuoteAccept = TapMessageTypeBaseOffset + 2

	// MsgTypeQuoteReject is the message type identifier for a quote
	// reject message.
	MsgTypeQuoteReject = TapMessageTypeBaseOffset + 3
)

// OutgoingMessage is an interface that represents an outbound message that can
// be sent to a peer.
type OutgoingMessage interface {
	// LndClientCustomMsg returns a custom message that can be sent to a
	// peer using the lndclient.
	LndClientCustomMsg() (lndclient.CustomMessage, error)
}
