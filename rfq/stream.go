package rfq

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
	// PeerMessenger is the peer messenger. This component provides the RFQ
	// manager with the ability to send and receive raw peer messages.
	PeerMessenger PeerMessenger

	// IncomingMessages is a channel which is populated with incoming
	// (received) RFQ messages. These messages have been extracted from the
	// raw peer wire messages by the stream handler service.
	IncomingMessages chan<- rfqmsg.IncomingMsg
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

	// ContextGuard provides a wait group and main quit channel that can be
	// used to create guarded contexts.
	*fn.ContextGuard
}

// NewStreamHandler creates and starts a new RFQ stream handler.
//
// TODO(ffranr): Pass in a signer so that we can create a signature over output
// message fields.
func NewStreamHandler(ctx context.Context,
	cfg StreamHandlerCfg) (*StreamHandler, error) {

	pPorter := cfg.PeerMessenger
	msgChan, peerMsgErrChan, err := pPorter.SubscribeCustomMessages(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to subscribe to wire "+
			"messages via peer message porter: %w", err)
	}

	return &StreamHandler{
		cfg: cfg,

		recvRawMessages:    msgChan,
		errRecvRawMessages: peerMsgErrChan,

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

	log.Debugf("Stream handling incoming message (msg_type=%T, "+
		"msg_id=%s, origin_peer=%s)", quoteRequest,
		quoteRequest.ID.String(), quoteRequest.MsgPeer())

	// Send the quote request to the RFQ manager.
	var msg rfqmsg.IncomingMsg = quoteRequest
	sendSuccess := fn.SendOrQuit(h.cfg.IncomingMessages, msg, h.Quit)
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

	log.Debugf("Stream handling incoming message (msg_type=%T, "+
		"origin_peer=%s)", quoteAccept, quoteAccept.MsgPeer())

	// TODO(ffranr): At this point, we need to validate that the quote
	//  accept message is valid. We need to check that the quote accept
	//  message is associated with a valid quote request.

	// Send the message to the RFQ manager.
	var msg rfqmsg.IncomingMsg = quoteAccept
	sendSuccess := fn.SendOrQuit(h.cfg.IncomingMessages, msg, h.Quit)
	if !sendSuccess {
		return fmt.Errorf("RFQ stream handler shutting down")
	}

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

	log.Debugf("Stream handling incoming message (msg_type=%T, "+
		"origin_peer=%s)", quoteReject, quoteReject.MsgPeer())

	// TODO(ffranr): At this point, we need to validate that the quote
	//  reject message is valid. We need to check that the quote reject
	//  message is associated with a valid quote request.

	// Send the message to the RFQ manager.
	var msg rfqmsg.IncomingMsg = quoteReject
	sendSuccess := fn.SendOrQuit(h.cfg.IncomingMessages, msg, h.Quit)
	if !sendSuccess {
		return fmt.Errorf("RFQ stream handler shutting down")
	}

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
		log.Tracef("Silently disregarding incoming message of "+
			"unknown type (msg_type=%d)", wireMsg.MsgType)
	}

	return nil
}

// HandleOutgoingMessage handles an outgoing RFQ message.
func (h *StreamHandler) HandleOutgoingMessage(
	outgoingMsg rfqmsg.OutgoingMsg) error {

	log.Debugf("Stream handling outgoing message (msg_type=%T, "+
		"dest_peer=%s)", outgoingMsg, outgoingMsg.MsgPeer())

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

	err = h.cfg.PeerMessenger.SendCustomMessage(ctx, lndClientCustomMsg)
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
				log.Warnf("Raw peer messages channel closed " +
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
				log.Warnf("Error handling incoming wire "+
					"message: %v", err)
			}

		case errSubCustomMessages := <-h.errRecvRawMessages:
			// If we receive an error from the peer message
			// subscription, we'll terminate the stream handler.
			log.Warnf("Error received from stream handler wire "+
				"message channel: %v", errSubCustomMessages)

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

// PeerMessenger is an interface that abstracts the peer message transport
// layer.
type PeerMessenger interface {
	// SubscribeCustomMessages creates a subscription to raw messages
	// received from our peers.
	SubscribeCustomMessages(
		ctx context.Context) (<-chan lndclient.CustomMessage,
		<-chan error, error)

	// SendCustomMessage sends a raw message to a peer.
	SendCustomMessage(context.Context, lndclient.CustomMessage) error
}
