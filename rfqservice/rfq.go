package rfqservice

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lightninglabs/taproot-assets/fn"
)

const (
	DefaultTimeout = 30 * time.Second
)

type ManagerCfg struct {
	PeerMessagePorter PeerMessagePorter
}

// Manager is a struct that handles RFQ order management.
type Manager struct {
	startOnce sync.Once
	stopOnce  sync.Once

	cfg ManagerCfg

	rfqStreamHandle *StreamHandler

	// ContextGuard provides a wait group and main quit channel that can be
	// used to create guarded contexts.
	*fn.ContextGuard
}

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
		defer cancel()

		log.Info("Initializing RFQ subsystems")
		err := m.initSubsystems(ctx)
		if err != nil {
			startErr = err
			return
		}

		// Start the manager's main event loop in a separate goroutine.
		m.Wg.Add(1)
		go func() {
			defer m.Wg.Done()

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
	})

	return stopErr
}

func (m *Manager) initSubsystems(ctx context.Context) error {
	var err error

	// Initialise the RFQ raw message stream handler and start it in a
	// separate goroutine.
	m.rfqStreamHandle, err = NewStreamHandler(ctx, m.cfg.PeerMessagePorter)
	if err != nil {
		return fmt.Errorf("failed to create RFQ stream handler: %w",
			err)
	}

	m.Wg.Add(1)
	go func() {
		defer m.Wg.Done()

		// Start the RFQ stream handler.
		err = m.rfqStreamHandle.Start()
		if err != nil {
			return
		}
	}()

	return nil
}

func (m *Manager) mainEventLoop() error {
	for {
		select {
		// Handle RFQ message stream events.
		case quoteReq := <-m.rfqStreamHandle.IncomingQuoteRequests.NewItemCreated.ChanOut():
			log.Debugf("Received RFQ quote request from message "+
				"stream handler: %v", quoteReq)
			// TODO(ffranr): send to negotiator (+ price oracle)

		case errStream := <-m.rfqStreamHandle.ErrChan:
			return fmt.Errorf("error received from RFQ stream "+
				"handler: %w", errStream)

		case <-m.Quit:
			return nil
		}
	}
}
