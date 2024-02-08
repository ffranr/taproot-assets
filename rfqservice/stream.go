package rfqservice

import (
	"context"
	"fmt"
	"sync"

	"github.com/lightninglabs/lndclient"
	"github.com/lightninglabs/taproot-assets/fn"
	"github.com/lightninglabs/taproot-assets/rfqmsg"
)

// StreamHandlerCfg is a struct that holds the configuration parameters for the
// RFQ peer message stream handler.
type StreamHandlerCfg struct {
	// PeerMessagePorter is the peer message porter. This component
	// provides the RFQ manager with the ability to send and receive raw
	// peer messages.
	PeerMessagePorter PeerMessagePorter
}

// StreamHandler is a struct that handles incoming and outgoing peer RFQ stream
// messages.
//
// This component subscribes to incoming raw peer messages (custom messages). It
// processes those messages with the aim of extracting relevant request for
// quotes (RFQs).
type StreamHandler struct {
	startOnce sync.Once
	stopOnce  sync.Once

	// cfg holds the configuration parameters for the RFQ peer message
	// stream handler.
	cfg StreamHandlerCfg

	// recvRawMessages is a channel that receives incoming raw peer
	// messages.
	recvRawMessages <-chan lndclient.CustomMessage

	// errRecvRawMessages is a channel that receives errors emanating from
	// the peer raw messages subscription.
	errRecvRawMessages <-chan error

	// incomingMessages is a channel which is populated with incoming
	// (received) RFQ messages.
	incomingMessages chan<- rfqmsg.IncomingMsg

	// ContextGuard provides a wait group and main quit channel that can be
	// used to create guarded contexts.
	*fn.ContextGuard
}

// NewStreamHandler creates and starts a new RFQ stream handler.
//
// TODO(ffranr): Pass in a signer so that we can create a signature over output
// message fields.
func NewStreamHandler(ctx context.Context, cfg StreamHandlerCfg,
	incomingMessages chan<- rfqmsg.IncomingMsg) (*StreamHandler, error) {

	pPorter := cfg.PeerMessagePorter
	msgChan, peerMsgErrChan, err := pPorter.SubscribeCustomMessages(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to custom "+
			"messages via message transfer handle: %w", err)
	}

	return &StreamHandler{
		cfg: cfg,

		recvRawMessages:    msgChan,
		errRecvRawMessages: peerMsgErrChan,

		incomingMessages: incomingMessages,

		ContextGuard: &fn.ContextGuard{
			DefaultTimeout: DefaultTimeout,
			Quit:           make(chan struct{}),
		},
	}, nil
}

// handleIncomingQuoteRequest handles an incoming quote request peer message.
func (h *StreamHandler) handleIncomingQuoteRequest(
	wireMsg rfqmsg.WireMessage) error {

	quoteRequest, err := rfqmsg.NewRequestMsgFromWire(wireMsg)
	if err != nil {
		return fmt.Errorf("unable to create a quote request message "+
			"from a wire message: %w", err)
	}

	// Send the quote request to the RFQ manager.
	var msg rfqmsg.IncomingMsg = quoteRequest
	sendSuccess := fn.SendOrQuit(h.incomingMessages, msg, h.Quit)
	if !sendSuccess {
		return fmt.Errorf("RFQ stream handler shutting down")
	}

	return nil
}

// handleIncomingQuoteAccept handles an incoming quote accept peer message.
func (h *StreamHandler) handleIncomingQuoteAccept(
	wireMsg rfqmsg.WireMessage) error {

	quoteAccept, err := rfqmsg.NewAcceptFromWireMsg(wireMsg)
	if err != nil {
		return fmt.Errorf("unable to create a quote accept message "+
			"from a wire message: %w", err)
	}
	quoteAccept = quoteAccept

	// TODO(ffranr): At this point, we need to validate that the quote
	//  accept message is valid. We need to check that the quote accept
	//  message is associated with a valid quote request. Finally, we need
	//  to inform the RFQ manager that the quote request has been accepted.

	return nil
}

// handleIncomingQuoteReject handles an incoming quote reject peer message.
func (h *StreamHandler) handleIncomingQuoteReject(
	wireMsg rfqmsg.WireMessage) error {

	quoteReject, err := rfqmsg.NewQuoteRejectFromWireMsg(wireMsg)
	if err != nil {
		return fmt.Errorf("unable to create a quote reject message "+
			"from a wire message: %w", err)
	}
	quoteReject = quoteReject

	// TODO(ffranr): At this point, we need to validate that the quote
	//  reject message is valid. We need to check that the quote reject
	//  message is associated with a valid quote request. Finally, we need
	//  to inform the RFQ manager that the quote request has been rejected.

	return nil
}

// handleIncomingWireMessage handles an incoming wire message.
func (h *StreamHandler) handleIncomingWireMessage(
	wireMsg rfqmsg.WireMessage) error {

	switch wireMsg.MsgType {
	case rfqmsg.MsgTypeRequest:
		err := h.handleIncomingQuoteRequest(wireMsg)
		if err != nil {
			return fmt.Errorf("unable to handle incoming quote "+
				"request message: %w", err)
		}

	case rfqmsg.MsgTypeAccept:
		err := h.handleIncomingQuoteAccept(wireMsg)
		if err != nil {
			return fmt.Errorf("unable to handle incoming quote "+
				"accept message: %w", err)
		}

	case rfqmsg.MsgTypeReject:
		err := h.handleIncomingQuoteReject(wireMsg)
		if err != nil {
			return fmt.Errorf("unable to handle incoming quote "+
				"reject message: %w", err)
		}

	default:
		// Silently disregard the message if we don't recognise the
		// message type.
		return nil
	}

	return nil
}

// HandleOutgoingMessage handles an outgoing RFQ message.
func (h *StreamHandler) HandleOutgoingMessage(
	outgoingMsg rfqmsg.OutgoingMsg) error {

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

	err = h.cfg.PeerMessagePorter.SendCustomMessage(ctx, lndClientCustomMsg)
	if err != nil {
		return fmt.Errorf("unable to send message to peer: %w",
			err)
	}

	return nil
}

// mainEventLoop executes the main event handling loop.
func (h *StreamHandler) mainEventLoop() {
	log.Debug("Starting stream handler event loop")

	for {
		select {
		case rawMsg, ok := <-h.recvRawMessages:
			if !ok {
				log.Warnf("raw peer messages channel closed " +
					"unexpectedly")
				return
			}

			// Convert the raw peer message into a wire message.
			// Wire message is a RFQ package type that is used by
			// interfaces throughout the package.
			wireMsg := rfqmsg.WireMessage{
				Peer:    rawMsg.Peer,
				MsgType: rawMsg.MsgType,
				Data:    rawMsg.Data,
			}

			err := h.handleIncomingWireMessage(wireMsg)
			if err != nil {
				log.Warnf("Error handling raw custom "+
					"message recieve event: %v", err)
			}

		case errSubCustomMessages := <-h.errRecvRawMessages:
			// If we receive an error from the peer message
			// subscription, we'll terminate the stream handler.
			log.Warnf("Error received from RFQ stream handler: %v",
				errSubCustomMessages)

		case <-h.Quit:
			log.Debug("Received quit signal. Stopping stream " +
				"handler event loop")
			return
		}
	}
}

// Start starts the service.
func (h *StreamHandler) Start() error {
	var startErr error
	h.startOnce.Do(func() {
		log.Info("Starting subsystem: peer message stream handler")

		// Start the main event loop in a separate goroutine.
		h.Wg.Add(1)
		go func() {
			defer h.Wg.Done()
			h.mainEventLoop()
		}()
	})
	return startErr
}

// Stop stops the handler.
func (h *StreamHandler) Stop() error {
	h.stopOnce.Do(func() {
		log.Info("Stopping subsystem: stream handler")

		// Stop the main event loop.
		close(h.Quit)
	})
	return nil
}

// PeerMessagePorter is an interface that abstracts the peer message transport
// layer.
type PeerMessagePorter interface {
	// SubscribeCustomMessages creates a subscription to raw messages
	// received from our peers.
	SubscribeCustomMessages(
		ctx context.Context) (<-chan lndclient.CustomMessage,
		<-chan error, error)

	// SendCustomMessage sends a raw message to a peer.
	SendCustomMessage(context.Context, lndclient.CustomMessage) error
}
