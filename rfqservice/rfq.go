package rfqservice

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/fn"
	"github.com/lightninglabs/taproot-assets/rfqmsg"
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
}

// Manager is a struct that handles the request for quote (RFQ) system.
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
		CleanupInterval: CacheCleanupInterval,
		HtlcInterceptor: m.cfg.HtlcInterceptor,
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

// handleIncomingMessage handles an incoming message.
func (m *Manager) handleIncomingMessage(incomingMsg rfqmsg.IncomingMsg) error {
	// Perform type specific handling of the incoming message.
	//
	// TODO(ffranr): handle incoming accept and reject messages.
	switch msg := incomingMsg.(type) {
	case *rfqmsg.Request:
		err := m.negotiator.HandleIncomingQuoteRequest(*msg)
		if err != nil {
			return fmt.Errorf("error handling incoming quote "+
				"request: %w", err)
		}
	}

	return nil
}

// handleOutgoingMessage handles an outgoing message.
func (m *Manager) handleOutgoingMessage(outgoingMsg rfqmsg.OutgoingMsg) error {
	// Perform type specific handling of the outgoing message.
	switch msg := outgoingMsg.(type) {
	case *rfqmsg.Accept:
		// Inform the HTLC order handler that we've accepted the quote
		// request.
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
			log.Debugf("RFQ manager has received an incoming " +
				"message")

			err := m.handleIncomingMessage(incomingMsg)
			if err != nil {
				log.Warnf("Error handling incoming message: %v",
					err)
			}

		// Handle outgoing message.
		case outgoingMsg := <-m.outgoingMessages:
			log.Debugf("RFQ manager has received an outgoing " +
				"message.")

			err := m.handleOutgoingMessage(outgoingMsg)
			if err != nil {
				log.Warnf("Error handling outgoing message: %v",
					err)
			}

		case <-m.Quit:
			log.Debug("Manager main event loop has received the " +
				"shutdown signal")
			return
		}
	}
}

// UpsertAssetSellOffer upserts an asset sell offer with the RFQ manager.
func (m *Manager) UpsertAssetSellOffer(offer AssetSellOffer) error {
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
