package rfqservice

import (
	"bytes"
	"context"
	"fmt"

	"github.com/lightninglabs/lndclient"
	"github.com/lightninglabs/taproot-assets/fn"
	msg "github.com/lightninglabs/taproot-assets/rfqmessages"
)

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
	IncomingQuoteRequests *fn.EventReceiver[msg.QuoteRequest]

	// ErrChan is the handle's error reporting channel.
	ErrChan <-chan error

	// ContextGuard provides a wait group and main quit channel that can be
	// used to create guarded contexts.
	*fn.ContextGuard
}

// NewStreamHandler creates a new RFQ stream handler.
func NewStreamHandler(ctx context.Context,
	peerMessagePorter PeerMessagePorter) (*StreamHandler, error) {

	res, err := peerMessagePorter.GetInfo(ctx)
	res = res

	log.Infof("Peer message porter info: %v", res)

	msgChan, errChan, err := peerMessagePorter.SubscribeCustomMessages(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to custom "+
			"messages via message transfer handle: %w", err)
	}

	incomingQuoteRequests := fn.NewEventReceiver[msg.QuoteRequest](
		fn.DefaultQueueSize,
	)

	return &StreamHandler{
		recvRawMessages:    msgChan,
		errRecvRawMessages: errChan,

		IncomingQuoteRequests: incomingQuoteRequests,

		ErrChan: make(<-chan error),

		ContextGuard: &fn.ContextGuard{
			DefaultTimeout: DefaultTimeout,
			Quit:           make(chan struct{}),
		},
	}, nil
}

// handleRecvRawMessage handles an incoming raw peer message.
func (h *StreamHandler) handleQuoteRequestMsg(
	rawMsg lndclient.CustomMessage) error {

	// Attempt to decode the message as a request for quote (RFQ) message.
	var quoteRequest msg.QuoteRequest
	err := quoteRequest.Decode(bytes.NewBuffer(rawMsg.Data))
	if err != nil {
		return fmt.Errorf("unable to decode incoming RFQ message: %w",
			err)
	}

	// TODO(ffranr): Determine whether to keep or discard the RFQ message
	//  based on the peer's ID and the asset's ID.

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
	rawMsg lndclient.CustomMessage) error {

	switch rawMsg.MsgType {
	case msg.MsgTypeQuoteRequest:
		err := h.handleQuoteRequestMsg(rawMsg)
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
	log.Info("Starting RFQ subsystem: peer message stream handler")

	for {
		select {
		case rawMsg, ok := <-h.recvRawMessages:
			if !ok {
				return fmt.Errorf("raw custom messages " +
					"channel closed unexpectedly")
			}

			log.Infof("Received raw custom message: %v", rawMsg)
			err := h.handleRecvRawMessage(rawMsg)
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

			//default:
			//	log.Info("No events to handle, waiting for new events")
		}
	}
}

// Stop stops the RFQ stream handler.
func (h *StreamHandler) Stop() error {
	log.Info("Stopping RFQ subsystem: stream handler")

	close(h.Quit)
	return nil
}

// PeerMessagePorter is an interface that abstracts the peer message transport
// layer.
type PeerMessagePorter interface {
	GetInfo(ctx context.Context) (*lndclient.Info, error)

	SubscribeCustomMessages(
		ctx context.Context) (<-chan lndclient.CustomMessage,
		<-chan error, error)
}
