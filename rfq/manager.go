package rfq

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/lightninglabs/lndclient"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/fn"
	"github.com/lightninglabs/taproot-assets/rfqmsg"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/routing/route"
)

const (
	// DefaultTimeout is the default timeout used for context operations.
	DefaultTimeout = 30 * time.Second

	// CacheCleanupInterval is the interval at which local runtime caches
	// are cleaned up.
	CacheCleanupInterval = 30 * time.Second
)

// ManagerCfg is a struct that holds the configuration parameters for the RFQ
// manager.
type ManagerCfg struct {
	// PeerMessagePorter is the peer message porter. This component
	// provides the RFQ manager with the ability to send and receive raw
	// peer messages.
	PeerMessagePorter PeerMessagePorter

	// HtlcInterceptor is the HTLC interceptor. This component is used to
	// intercept and accept/reject HTLCs.
	HtlcInterceptor HtlcInterceptor

	// PriceOracle is the price oracle that the RFQ manager will use to
	// determine whether a quote is accepted or rejected.
	PriceOracle PriceOracle

	// LightningSelfId is the public key of the lightning node that the RFQ
	// manager is associated with.
	//
	// TODO(ffranr): The tapd node was receiving wire messages that it sent.
	//  This is a temporary fix to prevent the node from processing its own
	//  messages.
	LightningSelfId route.Vertex
}

// Manager is a struct that manages the request for quote (RFQ) system.
type Manager struct {
	startOnce sync.Once
	stopOnce  sync.Once

	// cfg holds the configuration parameters for the RFQ manager.
	cfg ManagerCfg

	// orderHandler is the RFQ order handler. This subsystem monitors HTLCs
	// (Hash Time Locked Contracts), determining acceptance or rejection
	// based on compliance with the terms of any associated quote.
	orderHandler *OrderHandler

	// streamHandler is the RFQ stream handler. This subsystem handles
	// incoming and outgoing peer RFQ stream messages.
	streamHandler *StreamHandler

	// negotiator is the RFQ quote negotiator. This subsystem determines
	// whether a quote is accepted or rejected.
	negotiator *Negotiator

	// incomingMessages is a channel which is populated with incoming
	// messages.
	incomingMessages chan rfqmsg.IncomingMsg

	// outgoingMessages is a channel which is populated with outgoing
	// messages.
	outgoingMessages chan rfqmsg.OutgoingMsg

	// acceptHtlcEvents is a channel which is populated with accept HTLCs
	// events.
	acceptHtlcEvents chan AcceptHtlcEvent

	// peerAcceptedQuotes is a map of serialised short channel IDs (SCIDs)
	// to associated accepted quotes. These quotes have been accepted by
	// peer nodes and are therefore available for use in buying assets.
	peerAcceptedQuotes map[SerialisedScid]rfqmsg.Accept

	// peerAcceptedQuotesMtx guards the peerAcceptedQuotes map.
	peerAcceptedQuotesMtx sync.Mutex

	// subscribers is a map of components that want to be notified on new
	// events, keyed by their subscription ID.
	subscribers map[uint64]*fn.EventReceiver[fn.Event]

	// subscriberMtx guards the subscribers map.
	subscriberMtx sync.Mutex

	// ContextGuard provides a wait group and main quit channel that can be
	// used to create guarded contexts.
	*fn.ContextGuard
}

// NewManager creates a new RFQ manager.
func NewManager(cfg ManagerCfg) (Manager, error) {
	return Manager{
		cfg: cfg,

		incomingMessages: make(chan rfqmsg.IncomingMsg),
		outgoingMessages: make(chan rfqmsg.OutgoingMsg),

		acceptHtlcEvents:   make(chan AcceptHtlcEvent),
		peerAcceptedQuotes: make(map[SerialisedScid]rfqmsg.Accept),

		subscribers: make(
			map[uint64]*fn.EventReceiver[fn.Event],
		),

		ContextGuard: &fn.ContextGuard{
			DefaultTimeout: DefaultTimeout,
			Quit:           make(chan struct{}),
		},
	}, nil
}

// startSubsystems starts the RFQ subsystems.
func (m *Manager) startSubsystems(ctx context.Context) error {
	var err error

	// Initialise and start the order handler.
	m.orderHandler, err = NewOrderHandler(OrderHandlerCfg{
		CleanupInterval:  CacheCleanupInterval,
		HtlcInterceptor:  m.cfg.HtlcInterceptor,
		AcceptHtlcEvents: m.acceptHtlcEvents,
	})
	if err != nil {
		return fmt.Errorf("error initializing RFQ order handler: %w",
			err)
	}

	if err := m.orderHandler.Start(); err != nil {
		return fmt.Errorf("unable to start RFQ order handler: %w", err)
	}

	// Initialise and start the peer message stream handler.
	streamHandlerCfg := StreamHandlerCfg{
		PeerMessagePorter: m.cfg.PeerMessagePorter,
		LightningSelfId:   m.cfg.LightningSelfId,
	}
	m.streamHandler, err = NewStreamHandler(
		ctx, streamHandlerCfg, m.incomingMessages,
	)
	if err != nil {
		return fmt.Errorf("error initializing RFQ subsystem service: "+
			"peer message stream handler: %w", err)
	}

	if err := m.streamHandler.Start(); err != nil {
		return fmt.Errorf("unable to start RFQ subsystem service: "+
			"peer message stream handler: %w", err)
	}

	// Initialise and start the quote negotiator.
	negotiatorCfg := NegotiatorCfg{
		PriceOracle: m.cfg.PriceOracle,
	}
	m.negotiator, err = NewNegotiator(negotiatorCfg, m.outgoingMessages)
	if err != nil {
		return fmt.Errorf("error initializing RFQ negotiator: %w",
			err)
	}

	if err := m.negotiator.Start(); err != nil {
		return fmt.Errorf("unable to start RFQ negotiator: %w", err)
	}

	return err
}

// Start attempts to start a new RFQ manager.
func (m *Manager) Start() error {
	var startErr error
	m.startOnce.Do(func() {
		ctx, cancel := m.WithCtxQuitNoTimeout()

		log.Info("Initializing RFQ subsystems")
		err := m.startSubsystems(ctx)
		if err != nil {
			startErr = err
			return
		}

		// Start the manager's main event loop in a separate goroutine.
		m.Wg.Add(1)
		go func() {
			defer func() {
				m.Wg.Done()

				// Attempt to stop all subsystems if the main
				// event loop exits.
				err = m.stopSubsystems()
				if err != nil {
					log.Errorf("Error stopping RFQ "+
						"subsystems: %v", err)
				}

				// The context can now be cancelled as all
				// dependant components have been stopped.
				cancel()
			}()

			log.Info("Starting RFQ manager main event loop")
			m.mainEventLoop()
		}()
	})
	return startErr
}

// Stop attempts to stop the RFQ manager.
func (m *Manager) Stop() error {
	var stopErr error

	m.stopOnce.Do(func() {
		log.Info("Stopping RFQ system")
		stopErr = m.stopSubsystems()

		// Stop the main event loop.
		close(m.Quit)
	})

	return stopErr
}

// stopSubsystems stops the RFQ subsystems.
func (m *Manager) stopSubsystems() error {
	// Stop the RFQ order handler.
	err := m.orderHandler.Stop()
	if err != nil {
		return fmt.Errorf("error stopping RFQ order handler: %w", err)
	}

	// Stop the RFQ stream handler.
	err = m.streamHandler.Stop()
	if err != nil {
		return fmt.Errorf("error stopping RFQ stream handler: %w", err)
	}

	// Stop the RFQ quote negotiator.
	err = m.negotiator.Stop()
	if err != nil {
		return fmt.Errorf("error stopping RFQ quote negotiator: %w",
			err)
	}

	return nil
}

// handleIncomingMessage handles an incoming message. These are messages that
// have been received from a peer.
func (m *Manager) handleIncomingMessage(incomingMsg rfqmsg.IncomingMsg) error {
	// Perform type specific handling of the incoming message.
	//
	// TODO(ffranr): handle incoming reject messages.
	switch msg := incomingMsg.(type) {
	case *rfqmsg.Request:
		err := m.negotiator.HandleIncomingQuoteRequest(*msg)
		if err != nil {
			return fmt.Errorf("error handling incoming quote "+
				"request: %w", err)
		}

	case *rfqmsg.Accept:
		// TODO(ffranr): The stream handler should ensure that the
		//  accept message corresponds to a request.
		//
		// The quote request has been accepted. Store accepted quote
		// so that it can be used to send a payment by our lightning
		// node.
		m.peerAcceptedQuotesMtx.Lock()
		defer m.peerAcceptedQuotesMtx.Unlock()

		scid := SerialisedScid(msg.ShortChannelId())
		m.peerAcceptedQuotes[scid] = *msg

		// Notify subscribers of the incoming quote accept.
		event := NewIncomingAcceptQuoteEvent(msg)
		m.publishSubscriberEvent(event)
	}

	return nil
}

// handleOutgoingMessage handles an outgoing message. Outgoing messages are
// messages that will be sent to a peer.
func (m *Manager) handleOutgoingMessage(outgoingMsg rfqmsg.OutgoingMsg) error {
	// Perform type specific handling of the outgoing message.
	switch msg := outgoingMsg.(type) {
	case *rfqmsg.Accept:
		// Before sending an accept message to a peer, inform the HTLC
		// order handler that we've accepted the quote request.
		err := m.orderHandler.RegisterChannelRemit(*msg)
		if err != nil {
			return fmt.Errorf("error registering channel remit: %w",
				err)
		}
	}

	// Send the outgoing message to the peer.
	err := m.streamHandler.HandleOutgoingMessage(outgoingMsg)
	if err != nil {
		return fmt.Errorf("error sending outgoing message to stream "+
			"handler: %w", err)
	}

	return nil
}

// mainEventLoop is the main event loop of the RFQ manager.
func (m *Manager) mainEventLoop() {
	for {
		select {
		// Handle incoming message.
		case incomingMsg := <-m.incomingMessages:
			peer := incomingMsg.MsgPeer()
			log.Debugf("Manager handling incoming message "+
				"(msg_type=%T, origin_peer=%s)",
				incomingMsg, peer)

			err := m.handleIncomingMessage(incomingMsg)
			if err != nil {
				log.Warnf("Error handling incoming message: %v",
					err)
			}

		// Handle outgoing message.
		case outgoingMsg := <-m.outgoingMessages:
			peer := outgoingMsg.MsgPeer()
			log.Debugf("Manager handling outgoing message "+
				"(msg_type=%T, dest_peer=%s)",
				outgoingMsg, peer.String())

			err := m.handleOutgoingMessage(outgoingMsg)
			if err != nil {
				log.Warnf("Error handling outgoing message: %v",
					err)
			}

		case acceptHtlcEvent := <-m.acceptHtlcEvents:
			// Handle a HTLC accept event. Notify any subscribers.
			m.publishSubscriberEvent(&acceptHtlcEvent)

		// Handle errors from the negotiator.
		case err := <-m.negotiator.ErrChan:
			log.Warnf("Negotiator has encountered an error: %v",
				err)

		case <-m.Quit:
			log.Debug("Manager main event loop has received the " +
				"shutdown signal")
			return
		}
	}
}

// UpsertAssetSellOffer upserts an asset sell offer with the RFQ manager.
func (m *Manager) UpsertAssetSellOffer(offer SellOffer) error {
	// Store the asset sell offer in the negotiator.
	err := m.negotiator.UpsertAssetSellOffer(offer)
	if err != nil {
		return fmt.Errorf("error registering asset sell offer: %w", err)
	}

	return nil
}

// RemoveAssetSellOffer removes an asset sell offer from the RFQ manager.
func (m *Manager) RemoveAssetSellOffer(assetID *asset.ID,
	assetGroupKey *btcec.PublicKey) error {

	// Remove the asset sell offer from the negotiator.
	err := m.negotiator.RemoveAssetSellOffer(assetID, assetGroupKey)
	if err != nil {
		return fmt.Errorf("error removing asset sell offer: %w", err)
	}

	return nil
}

// BuyOrder is a struct that represents a buy order.
type BuyOrder struct {
	// AssetID is the ID of the asset that the buyer is interested in.
	AssetID *asset.ID

	// AssetGroupKey is the public key of the asset group that the buyer is
	// interested in.
	AssetGroupKey *btcec.PublicKey

	// MinAssetAmount is the minimum amount of the asset that the buyer is
	// willing to accept.
	MinAssetAmount uint64

	// MaxBid is the maximum bid price that the buyer is willing to pay.
	MaxBid lnwire.MilliSatoshi

	// Expiry is the unix timestamp at which the buy order expires.
	Expiry uint64

	// Peer is the peer that the buy order is intended for. This field is
	// optional.
	Peer *route.Vertex
}

// UpsertAssetBuyOrder upserts an asset buy order for management.
func (m *Manager) UpsertAssetBuyOrder(order BuyOrder) error {
	// For now, a peer must be specified.
	//
	// TODO(ffranr): Add support for peerless buy orders. The negotiator
	//  should be able to determine the optimal peer.
	if order.Peer == nil {
		return fmt.Errorf("buy order peer must be specified")
	}

	// Request a quote from a peer via the negotiator.
	err := m.negotiator.RequestQuote(order)
	if err != nil {
		return fmt.Errorf("error registering asset buy order: %w", err)
	}

	return nil
}

// QueryAcceptedQuotes returns a map of accepted quotes that have been
// registered with the RFQ manager.
func (m *Manager) QueryAcceptedQuotes() map[SerialisedScid]rfqmsg.Accept {
	m.peerAcceptedQuotesMtx.Lock()
	defer m.peerAcceptedQuotesMtx.Unlock()

	// Iterate over the accepted quotes and remove any that have expired.
	for scid, remit := range m.peerAcceptedQuotes {
		if time.Now().Unix() > int64(remit.Expiry) {
			delete(m.peerAcceptedQuotes, scid)
		}
	}

	return m.peerAcceptedQuotes
}

// RegisterSubscriber adds a new subscriber to the set of subscribers that will
// be notified of any new events that are broadcast.
//
// TODO(ffranr): Add support for delivering existing events to new subscribers.
func (m *Manager) RegisterSubscriber(
	receiver *fn.EventReceiver[fn.Event],
	deliverExisting bool, deliverFrom uint64) error {

	m.subscriberMtx.Lock()
	defer m.subscriberMtx.Unlock()

	m.subscribers[receiver.ID()] = receiver

	return nil
}

// RemoveSubscriber removes a subscriber from the set of subscribers that will
// be notified of any new events that are broadcast.
func (m *Manager) RemoveSubscriber(
	subscriber *fn.EventReceiver[fn.Event]) error {

	m.subscriberMtx.Lock()
	defer m.subscriberMtx.Unlock()

	_, ok := m.subscribers[subscriber.ID()]
	if !ok {
		return fmt.Errorf("subscriber with ID %d not found",
			subscriber.ID())
	}

	subscriber.Stop()
	delete(m.subscribers, subscriber.ID())

	return nil
}

// publishSubscriberEvent publishes an event to all subscribers.
func (m *Manager) publishSubscriberEvent(event fn.Event) {
	// Lock the subscriber mutex to ensure that we don't modify the
	// subscriber map while we're iterating over it.
	m.subscriberMtx.Lock()
	defer m.subscriberMtx.Unlock()

	// Iterate over the subscribers and deliver the event to each one.
	for _, sub := range m.subscribers {
		sub.NewItemCreated.ChanIn() <- event
	}
}

// IncomingAcceptQuoteEvent is an event that is broadcast when the RFQ manager
// receives an accept quote message from a peer.
type IncomingAcceptQuoteEvent struct {
	// timestamp is the event creation UTC timestamp.
	timestamp time.Time

	// Accept is the accepted quote.
	rfqmsg.Accept
}

// NewIncomingAcceptQuoteEvent creates a new IncomingAcceptQuoteEvent.
func NewIncomingAcceptQuoteEvent(
	accept *rfqmsg.Accept) *IncomingAcceptQuoteEvent {

	return &IncomingAcceptQuoteEvent{
		timestamp: time.Now().UTC(),
		Accept:    *accept,
	}
}

// Timestamp returns the event creation UTC timestamp.
func (q *IncomingAcceptQuoteEvent) Timestamp() time.Time {
	return q.timestamp.UTC()
}

// Ensure that the IncomingAcceptQuoteEvent struct implements the Event
// interface.
var _ fn.Event = (*IncomingAcceptQuoteEvent)(nil)

// AcceptHtlcEvent is an event that is sent to the accept HTLCs channel when
// an HTLC is accepted.
type AcceptHtlcEvent struct {
	// Timestamp is the unix timestamp at which the HTLC was accepted.
	timestamp uint64

	// Htlc is the intercepted HTLC.
	Htlc lndclient.InterceptedHtlc

	// ChannelRemit is the channel remit that the HTLC complies with.
	ChannelRemit ChannelRemit
}

// NewAcceptHtlcEvent creates a new AcceptedHtlcEvent.
func NewAcceptHtlcEvent(htlc lndclient.InterceptedHtlc,
	channelRemit ChannelRemit) AcceptHtlcEvent {

	return AcceptHtlcEvent{
		timestamp:    uint64(time.Now().UTC().Unix()),
		Htlc:         htlc,
		ChannelRemit: channelRemit,
	}
}

// Timestamp returns the event creation UTC timestamp.
func (q *AcceptHtlcEvent) Timestamp() time.Time {
	return time.Unix(int64(q.timestamp), 0).UTC()
}

// Ensure that the AcceptedHtlcEvent struct implements the Event interface.
var _ fn.Event = (*AcceptHtlcEvent)(nil)
