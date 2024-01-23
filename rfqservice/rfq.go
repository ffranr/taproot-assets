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
}

// Manager is a struct that handles the request for quote (RFQ) system.
type Manager struct {
	startOnce sync.Once
	stopOnce  sync.Once

	// cfg holds the configuration parameters for the RFQ manager.
	cfg ManagerCfg

	// rfqStreamHandle is the RFQ stream handler. This subsystem handles
	// incoming and outgoing peer RFQ stream messages.
	rfqStreamHandle *StreamHandler

	// quoteNegotiator is the RFQ quote negotiator. This subsystem
	// determines whether a quote is accepted or rejected.
	quoteNegotiator *QuoteNegotiator

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
		err := m.initSubsystems(ctx)
		if err != nil {
			startErr = err
			return
		}

		// Start the manager's main event loop in a separate goroutine.
		m.Wg.Add(1)
		go func() {
			defer func() {
				m.Wg.Done()

				// Cancel the context to stop all subsystems
				// if the main event loop exits.
				defer cancel()
			}()

			log.Info("Starting RFQ manager main event loop")
			err = m.mainEventLoop()
			if err != nil {
				startErr = err
				return
			}
		}()
	})
	return startErr
}

// Stop attempts to stop the RFQ manager.
func (m *Manager) Stop() error {
	var stopErr error

	m.stopOnce.Do(func() {
		log.Info("Stopping RFQ manager")

		err := m.rfqStreamHandle.Stop()
		if err != nil {
			stopErr = fmt.Errorf("error stopping RFQ stream "+
				"handler: %w", err)
			return
		}

		err = m.quoteNegotiator.Stop()
		if err != nil {
			stopErr = fmt.Errorf("error stopping RFQ quote "+
				"negotiator: %w", err)
			return
		}
	})

	return stopErr
}

// initSubsystems initializes the RFQ subsystems.
func (m *Manager) initSubsystems(ctx context.Context) error {
	var err error

	// Initialise the RFQ raw message stream handler.
	m.Wg.Add(1)
	go func() {
		defer m.Wg.Done()
		m.rfqStreamHandle, err = NewStreamHandler(
			ctx, m.cfg.PeerMessagePorter,
		)
	}()
	if err != nil {
		return fmt.Errorf("error initializing RFQ stream handler: %w",
			err)
	}

	// Initialise the RFQ quote negotiator.
	m.Wg.Add(1)
	go func() {
		defer m.Wg.Done()
		m.quoteNegotiator, err = NewQuoteNegotiator()
	}()
	if err != nil {
		return fmt.Errorf("error initializing RFQ quote negotiator: %w",
			err)
	}

	return err
}

// handleIncomingQuoteRequest handles an incoming quote request.
func (m *Manager) handleIncomingQuoteRequest(quoteReq msg.QuoteRequest) error {
	err := m.quoteNegotiator.HandleIncomingQuoteRequest(quoteReq)
	if err != nil {
		return fmt.Errorf("error handling incoming quote request: %w",
			err)
	}

	return nil
}

func (m *Manager) handleOutgoingQuoteAccept(quoteAccept msg.QuoteAccept) error {
	err := m.rfqStreamHandle.HandleOutgoingQuoteAccept(quoteAccept)
	if err != nil {
		return fmt.Errorf("error handling outgoing quote accept: %w",
			err)
	}

	return nil
}

// mainEventLoop is the main event loop of the RFQ manager.
func (m *Manager) mainEventLoop() error {
	newIncomingQuotes := m.rfqStreamHandle.IncomingQuoteRequests.NewItemCreated
	acceptedQuotes := m.quoteNegotiator.AcceptedQuotes.NewItemCreated

	for {
		select {
		// Handle new incoming quote requests.
		case quoteReq := <-newIncomingQuotes.ChanOut():
			log.Debugf("RFQ manager has received an incoming " +
				"quote request")

			err := m.handleIncomingQuoteRequest(quoteReq)
			if err != nil {
				log.Warnf("Error handling incoming quote "+
					"request: %v", err)
			}

		// Handle errors from the RFQ stream handler.
		case errStream := <-m.rfqStreamHandle.ErrChan:
			log.Warnf("Error received from RFQ stream handler: %w",
				errStream)

		// Handle accepted quotes.
		case acceptedQuote := <-acceptedQuotes.ChanOut():
			log.Debugf("RFQ manager has received an accepted " +
				"quote.")

			err := m.handleOutgoingQuoteAccept(acceptedQuote)
			if err != nil {
				log.Warnf("Error handling outgoing quote "+
					"accept: %v", err)
			}

		case <-m.Quit:
			log.Debug("RFQ manager main event loop has received " +
				"the shutdown signal")
			return nil
		}
	}
}
