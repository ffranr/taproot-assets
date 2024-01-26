package rfqservice

import (
	"context"
	"fmt"
	"sync"

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
	startOnce sync.Once
	stopOnce  sync.Once

	// peerMessagePorter is the peer message porter. This component is used
	// to send and receive raw peer messages.
	peerMessagePorter PeerMessagePorter

	// recvRawMessages is a channel that receives incoming raw peer
	// messages.
	recvRawMessages <-chan lndclient.CustomMessage

	// errRecvRawMessages is a channel that receives errors emanating from
	// the peer raw messages subscription.
	errRecvRawMessages <-chan error

	// IncomingQuoteRequests is a channel which is populated with incoming
	// (received) and valid quote request messages.
	IncomingQuoteRequests *fn.EventReceiver[msg.QuoteRequest]

	// errChan is this system's error reporting channel.
	errChan chan error

	// ContextGuard provides a wait group and main quit channel that can be
	// used to create guarded contexts.
	*fn.ContextGuard
}

// NewStreamHandler creates and starts a new RFQ stream handler.
func NewStreamHandler(ctx context.Context,
	peerMsgPorter PeerMessagePorter) (*StreamHandler, error) {

	msgChan, peerMsgErrChan, err := peerMsgPorter.SubscribeCustomMessages(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to custom "+
			"messages via message transfer handle: %w", err)
	}

	incomingQuoteRequests := fn.NewEventReceiver[msg.QuoteRequest](
		fn.DefaultQueueSize,
	)

	return &StreamHandler{
		peerMessagePorter:  peerMsgPorter,
		recvRawMessages:    msgChan,
		errRecvRawMessages: peerMsgErrChan,

		IncomingQuoteRequests: incomingQuoteRequests,

		ContextGuard: &fn.ContextGuard{
			DefaultTimeout: DefaultTimeout,
			Quit:           make(chan struct{}),
		},
	}, nil
}

// handleIncomingQuoteRequest handles an incoming quote request peer message.
func (h *StreamHandler) handleIncomingQuoteRequest(
	wireMsg msg.WireMessage) error {

	quoteRequest, err := msg.NewQuoteRequestFromWireMsg(wireMsg)
	if err != nil {
		return fmt.Errorf("unable to create a quote request message "+
			"from a lndclient custom message: %w", err)
	}

	// TODO(ffranr): Determine whether to keep or discard the RFQ message
	//  based on the peer's ID and the asset's ID.

	// Send the quote request to the RFQ manager.
	sendSuccess := fn.SendOrQuit(
		h.IncomingQuoteRequests.NewItemCreated.ChanIn(), *quoteRequest,
		h.Quit,
	)
	if !sendSuccess {
		return fmt.Errorf("RFQ stream handler shutting down")
	}

	return nil
}

// handleIncomingQuoteAccept handles an incoming quote accept peer message.
func (h *StreamHandler) handleIncomingQuoteAccept(
	wireMsg msg.WireMessage) error {

	quoteAccept, err := msg.NewQuoteAcceptFromWireMsg(wireMsg)
	if err != nil {
		return fmt.Errorf("unable to create a quote accept message "+
			"from a lndclient custom message: %w", err)
	}
	quoteAccept = quoteAccept

	return nil
}

// handleIncomingQuoteReject handles an incoming quote reject peer message.
func (h *StreamHandler) handleIncomingQuoteReject(
	wireMsg msg.WireMessage) error {

	quoteReject, err := msg.NewQuoteRejectFromWireMsg(wireMsg)
	if err != nil {
		return fmt.Errorf("unable to create a quote reject message "+
			"from a lndclient custom message: %w", err)
	}
	quoteReject = quoteReject

	return nil
}

// handleIncomingRawMessage handles an incoming raw peer message.
func (h *StreamHandler) handleIncomingRawMessage(
	rawMsg lndclient.CustomMessage) error {

	// Convert the lndclient custom message into a wire message. Wire
	// message is a RFQ package type that is used by interfaces throughout
	// the package.
	wireMsg := msg.WireMessage{
		Peer:    rawMsg.Peer,
		MsgType: rawMsg.MsgType,
		Data:    rawMsg.Data,
	}

	switch rawMsg.MsgType {
	case msg.MsgTypeQuoteRequest:
		err := h.handleIncomingQuoteRequest(wireMsg)
		if err != nil {
			return fmt.Errorf("unable to handle incoming quote "+
				"request message: %w", err)
		}

	case msg.MsgTypeQuoteAccept:
		err := h.handleIncomingQuoteAccept(wireMsg)
		if err != nil {
			return fmt.Errorf("unable to handle incoming quote "+
				"accept message: %w", err)
		}

	case msg.MsgTypeQuoteReject:
		err := h.handleIncomingQuoteReject(wireMsg)
		if err != nil {
			return fmt.Errorf("unable to handle incoming quote "+
				"reject message: %w", err)
		}

	default:
		// Silently disregard any messages were we don't recognise the
		// message type.
		return nil
	}

	return nil
}

// HandleOutgoingMessage handles an outgoing RFQ message.
func (h *StreamHandler) HandleOutgoingMessage(
	outgoingMsg msg.OutgoingMessage) error {

	// Convert the outgoing message to a lndclient custom message.
	wireMsg, err := outgoingMsg.ToWire()
	if err != nil {
		return fmt.Errorf("unable to create lndclient custom "+
			"message: %w", err)
	}
	lndClientCustomMsg := lndclient.CustomMessage{
		Peer:    wireMsg.Peer,
		MsgType: wireMsg.MsgType,
		Data:    wireMsg.Data,
	}

	// Send the message to the peer.
	ctx, cancel := h.WithCtxQuitNoTimeout()
	defer cancel()

	err = h.peerMessagePorter.SendCustomMessage(ctx, lndClientCustomMsg)
	if err != nil {
		return fmt.Errorf("unable to send message to peer: %w",
			err)
	}

	return nil
}

// Start starts the service.
func (h *StreamHandler) Start() error {
	log.Info("Starting RFQ subsystem: peer message stream handler")

	var startErr error
	h.startOnce.Do(func() {
		// Start the main event loop in a separate goroutine.
		h.Wg.Add(1)
		go func() {
			defer h.Wg.Done()

			log.Debug("Starting main event loop")
			err := h.mainEventLoop()
			if err != nil {
				startErr = err
				return
			}
		}()
	})
	return startErr
}

// mainEventLoop executes the main event handling loop.
func (h *StreamHandler) mainEventLoop() error {
	log.Info("Starting RFQ subsystem: peer message stream handler")

	for {
		select {
		case rawMsg, ok := <-h.recvRawMessages:
			if !ok {
				return fmt.Errorf("raw custom messages " +
					"channel closed unexpectedly")
			}

			err := h.handleIncomingRawMessage(rawMsg)
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

// Stop stops the handler.
func (h *StreamHandler) Stop() error {
	log.Info("Stopping RFQ subsystem: stream handler")

	close(h.Quit)
	return nil
}

// PeerMessagePorter is an interface that abstracts the peer message transport
// layer.
type PeerMessagePorter interface {
	// SubscribeCustomMessages creates a subscription to custom messages
	// received from our peers.
	SubscribeCustomMessages(
		ctx context.Context) (<-chan lndclient.CustomMessage,
		<-chan error, error)

	// SendCustomMessage sends a custom message to a peer.
	SendCustomMessage(context.Context, lndclient.CustomMessage) error
}
