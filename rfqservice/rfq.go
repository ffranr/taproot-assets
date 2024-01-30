package rfqservice

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lightninglabs/taproot-assets/fn"
	msg "github.com/lightninglabs/taproot-assets/rfqmessages"
)

const (
	// DefaultTimeout is the default timeout used for context operations.
	DefaultTimeout = 30 * time.Second
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

	// ContextGuard provides a wait group and main quit channel that can be
	// used to create guarded contexts.
	*fn.ContextGuard
}

// NewManager creates a new RFQ manager.
func NewManager(cfg ManagerCfg) (Manager, error) {
	return Manager{
		cfg: cfg,
		ContextGuard: &fn.ContextGuard{
			DefaultTimeout: DefaultTimeout,
			Quit:           make(chan struct{}),
		},
	}, nil
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
	})

	return stopErr
}

// startSubsystems starts the RFQ subsystems.
func (m *Manager) startSubsystems(ctx context.Context) error {
	var err error

	// Initialise and start the order handler.
	m.orderHandler, err = NewOrderHandler(m.cfg.HtlcInterceptor)
	if err != nil {
		return fmt.Errorf("error initializing RFQ order handler: %w",
			err)
	}

	if err := m.orderHandler.Start(); err != nil {
		return fmt.Errorf("unable to start RFQ order handler: %w", err)
	}

	// Initialise and start the peer message stream handler.
	m.streamHandler, err = NewStreamHandler(
		ctx, m.cfg.PeerMessagePorter,
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
	m.negotiator, err = NewNegotiator()
	if err != nil {
		return fmt.Errorf("error initializing RFQ negotiator: %w",
			err)
	}

	if err := m.negotiator.Start(); err != nil {
		return fmt.Errorf("unable to start RFQ negotiator: %w", err)
	}

	return err
}

// stopSubsystems stops the RFQ subsystems.
func (m *Manager) stopSubsystems() error {
	// Stop the RFQ stream handler.
	err := m.streamHandler.Stop()
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

// handleIncomingQuoteRequest handles an incoming quote request.
func (m *Manager) handleIncomingQuoteRequest(quoteReq msg.QuoteRequest) error {
	// Forward the incoming quote request to the quote negotiator so that
	// it may determine whether the quote should be accepted or rejected.
	err := m.negotiator.HandleIncomingQuoteRequest(quoteReq)
	if err != nil {
		return fmt.Errorf("error handling incoming quote request: %w",
			err)
	}

	return nil
}

// handleOutgoingQuoteAccept handles an outgoing quote accept message.
func (m *Manager) handleOutgoingQuoteAccept(quoteAccept msg.QuoteAccept) error {
	// Inform the HTLC order handler that we've accepted the quote request.
	m.orderHandler.RegisterChannelRemit(quoteAccept)

	// Send the quote accept message to the peer.
	err := m.streamHandler.HandleOutgoingMessage(&quoteAccept)
	if err != nil {
		return fmt.Errorf("error handling outgoing quote accept: %w",
			err)
	}

	return nil
}

// mainEventLoop is the main event loop of the RFQ manager.
func (m *Manager) mainEventLoop() {
	// Incoming message channels.
	incomingQuoteRequests := m.streamHandler.IncomingQuoteRequests.NewItemCreated
	//incomingQuoteAccept := m.streamHandler.IncomingQuoteRequests.NewItemCreated
	//incomingQuoteReject := m.streamHandler.IncomingQuoteRequests.NewItemCreated

	// Outgoing message channels.
	//outgoingQuoteRequest := m.negotiator.AcceptedQuotes.NewItemCreated
	outgoingQuoteAccept := m.negotiator.AcceptedQuotes.NewItemCreated
	//outgoingQuoteReject := m.negotiator.AcceptedQuotes.NewItemCreated

	for {
		select {
		// Handle incoming (received) quote requests from the peer
		// message stream handler.
		case quoteReq := <-incomingQuoteRequests.ChanOut():
			log.Debugf("RFQ manager has received an incoming " +
				"quote request")

			err := m.handleIncomingQuoteRequest(quoteReq)
			if err != nil {
				log.Warnf("Error handling incoming quote "+
					"request: %v", err)
			}

		// Handle accepted quotes.
		case acceptedQuote := <-outgoingQuoteAccept.ChanOut():
			log.Debugf("RFQ manager has received an outgoing " +
				"quote accept message.")

			err := m.handleOutgoingQuoteAccept(acceptedQuote)
			if err != nil {
				log.Warnf("Error handling outgoing quote "+
					"accept: %v", err)
			}

		case <-m.Quit:
			log.Debug("RFQ manager main event loop has received " +
				"the shutdown signal")
			return
		}
	}
}
