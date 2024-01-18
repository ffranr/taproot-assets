package rfq

import (
	"context"
	"fmt"

	"github.com/lightninglabs/lndclient"
	"github.com/lightninglabs/taproot-assets/fn"
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

	incomingQuoteRequests := fn.NewEventReceiver[QuoteRequest](
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
func (h *StreamHandler) handleQuoteRequestMsg(msg lndclient.CustomMessage) error {
	// Attempt to decode the message as a request for quote (RFQ) message.
	var quoteRequest QuoteRequest
	err := quoteRequest.Decode(msg.Data)
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
	msg lndclient.CustomMessage) error {

	switch msg.MsgType {
	case MsgTypeQuoteRequest:
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
	log.Info("Starting RFQ stream handler main event loop")

	for {
		select {
		case msg := <-h.recvRawMessages:
			log.Infof("Received raw custom message: %v", msg)

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
	log.Info("Stopping RFQ stream handler")

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
