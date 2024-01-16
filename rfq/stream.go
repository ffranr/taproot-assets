package rfq

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/lightninglabs/lndclient"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/fn"
)

// TapMessageTypeBaseOffset is the taproot-assets specific message type
// identifier base offset. All tap messages will have a type identifier that is
// greater than this value.
//
// This offset was chosen as the concatenation of the alphabetical index
// positions of the letters "t" (20), "a" (1), and "p" (16).
const TapMessageTypeBaseOffset uint32 = 20116

var (
	// QuoteRequestMsgType is the message type identifier for a quote
	// request message.
	QuoteRequestMsgType = TapMessageTypeBaseOffset + 1
)

// QuoteRequest is a struct that represents a request for a quote (RFQ) from a
// peer.
type QuoteRequest struct {
	// ID is the unique identifier of the request for quote (RFQ).
	ID [32]byte

	// // AssetID represents the identifier of the asset for which the peer
	// is requesting a quote.
	AssetID asset.ID

	// AssetCompressedPubGroupKey is the compressed public group key of the
	// asset for which the peer is requesting a quote. The compressed public
	// key is 33-byte long.
	AssetCompressedPubGroupKey *btcec.PublicKey

	// AssetAmount is the amount of the asset for which the peer is
	// requesting a quote.
	AssetAmount uint64

	SuggestedRateTick uint64
}

// Encode serializes the QuoteRequest struct into a byte slice.
func (q *QuoteRequest) Encode() ([]byte, error) {
	buf := new(bytes.Buffer)

	_, err := buf.Write(q.ID[:])
	if err != nil {
		return nil, err
	}

	_, err = buf.Write(q.AssetID[:])
	if err != nil {
		return nil, err
	}

	groupKeyBytes := q.AssetCompressedPubGroupKey.SerializeCompressed()
	_, err = buf.Write(groupKeyBytes)
	if err != nil {
		return nil, err
	}

	err = binary.Write(buf, binary.BigEndian, q.AssetAmount)
	if err != nil {
		return nil, err
	}

	err = binary.Write(buf, binary.BigEndian, q.SuggestedRateTick)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// Decode populates a QuoteRequest instance from a byte slice
func (q *QuoteRequest) Decode(data []byte) error {
	if len(data) != 113 {
		return fmt.Errorf("invalid data length")
	}

	var err error

	// Parse the request's ID.
	copy(q.ID[:], data[:32])

	// Parse the asset's ID.
	copy(q.AssetID[:], data[32:64])

	// Parse the asset's compressed public group key.
	var compressedPubGroupKeyBytes []byte
	copy(compressedPubGroupKeyBytes[:], data[64:97])
	q.AssetCompressedPubGroupKey, err = btcec.ParsePubKey(
		compressedPubGroupKeyBytes,
	)
	if err != nil {
		return fmt.Errorf("unable to parse compressed public group "+
			"key: %w", err)
	}

	q.AssetAmount = binary.BigEndian.Uint64(data[97:105])
	q.SuggestedRateTick = binary.BigEndian.Uint64(data[105:])

	return nil
}

// StreamHandler is a struct that handles incoming and outgoing peer RFQ stream
// messages.
//
// This component subscribes to incoming raw peer messages (custom messages). It
// processes those messages with the aim of extracting relevant request for
// quotes (RFQs).
type StreamHandler struct {
	// recvRawMessages is a channel that receives incoming raw peer
	// messages.
	recvRawMessages <-chan lndclient.CustomMessage

	// errRecvRawMessages is a channel that receives errors emanating from
	// the peer raw messages subscription.
	errRecvRawMessages <-chan error

	// IncomingQuoteRequests is a channel which is populated with incoming
	// (received) and processed requests for quote (RFQ) messages.
	IncomingQuoteRequests *fn.EventReceiver[QuoteRequest]

	// ErrChan is the handle's error reporting channel.
	ErrChan <-chan error

	// ContextGuard provides a wait group and main quit channel that can be
	// used to create guarded contexts.
	*fn.ContextGuard
}

// NewStreamHandler creates a new RFQ stream handler.
func NewStreamHandler(ctx context.Context,
	peerMessagePorter PeerMessagePorter) (*StreamHandler, error) {

	msgChan, errChan, err := peerMessagePorter.SubscribeCustomMessages(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to "+
			"custom messages via message transfer handle: %w", err)
	}

	return &StreamHandler{
		recvRawMessages:    msgChan,
		errRecvRawMessages: errChan,

		ErrChan: make(<-chan error),
	}, nil
}

// handleRecvRawMessage handles an incoming raw peer message.
func (h *StreamHandler) handleQuoteRequestMsg(msg lndclient.CustomMessage) error {
	// Attempt to decode the message as a request for quote (RFQ) message.
	var quoteRequest QuoteRequest
	err := quoteRequest.Decode(msg.Data)
	if err != nil {
		return fmt.Errorf("unable to decode incoming RFQ message: %w",
			err)
	}

	// TODO(ffranr): Determine whether to keep or discard the RFQ message.

	// Send the quote request to the RFQ manager.
	sendSuccess := fn.SendOrQuit(
		h.IncomingQuoteRequests.NewItemCreated.ChanIn(), quoteRequest,
		h.Quit,
	)
	if !sendSuccess {
		return fmt.Errorf("RFQ stream handler shutting down")
	}

	return nil
}

// handleRecvRawMessage handles an incoming raw peer message.
func (h *StreamHandler) handleRecvRawMessage(
	msg lndclient.CustomMessage) error {

	switch msg.MsgType {
	case QuoteRequestMsgType:
		err := h.handleQuoteRequestMsg(msg)
		if err != nil {
			return fmt.Errorf("unable to handle incoming quote "+
				"request message: %w", err)
		}

	default:
		// Silently disregard irrelevant messages based on message type.
		return nil
	}

	return nil
}

// Start starts the RFQ stream handler.
func (h *StreamHandler) Start() error {
	for {
		select {
		case msg := <-h.recvRawMessages:
			err := h.handleRecvRawMessage(msg)
			if err != nil {
				log.Warnf("Error handling raw custom "+
					"message recieve event: %v", err)
			}

		case errSubCustomMessages := <-h.errRecvRawMessages:
			// If we receive an error from the peer message
			// subscription, we'll terminate the stream handler.
			return fmt.Errorf("error received from RFQ stream "+
				"handler: %w", errSubCustomMessages)

		case <-h.Quit:
			return nil
		}
	}
}

// Stop stops the RFQ stream handler.
func (h *StreamHandler) Stop() error {
	close(h.Quit)
	return nil
}

// PeerMessagePorter is an interface that abstracts the peer message transport
// layer.
type PeerMessagePorter interface {
	SubscribeCustomMessages(
		ctx context.Context) (<-chan lndclient.CustomMessage,
		<-chan error, error)
}
