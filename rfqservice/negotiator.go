package rfqservice

import (
	"fmt"
	"sync"

	"github.com/lightninglabs/taproot-assets/fn"
	msg "github.com/lightninglabs/taproot-assets/rfqmessages"
)

// NegotiatorCfg holds the configuration for the negotiator.
type NegotiatorCfg struct {
	// PriceOracle is the price oracle that the negotiator will use to
	// determine whether a quote is accepted or rejected.
	PriceOracle PriceOracle
}

// Negotiator is a struct that handles the negotiation of quotes. It is a
// subsystem of the RFQ system. It determines whether a quote is accepted or
// rejected.
type Negotiator struct {
	startOnce sync.Once
	stopOnce  sync.Once

	// cfg holds the configuration parameters for the negotiator.
	cfg NegotiatorCfg

	// AcceptedQuotes is a channel which is populated with accepted quotes.
	AcceptedQuotes *fn.EventReceiver[msg.Accept]

	// RejectedQuotes is a channel which is populated with rejected quotes.
	RejectedQuotes *fn.EventReceiver[msg.Reject]

	// ContextGuard provides a wait group and main quit channel that can be
	// used to create guarded contexts.
	*fn.ContextGuard
}

// NewNegotiator creates a new quote negotiator.
func NewNegotiator(cfg NegotiatorCfg) (*Negotiator, error) {
	acceptedQuotes := fn.NewEventReceiver[msg.Accept](
		fn.DefaultQueueSize,
	)
	rejectedQuotes := fn.NewEventReceiver[msg.Reject](
		fn.DefaultQueueSize,
	)

	return &Negotiator{
		cfg: cfg,

		AcceptedQuotes: acceptedQuotes,
		RejectedQuotes: rejectedQuotes,

		ContextGuard: &fn.ContextGuard{
			DefaultTimeout: DefaultTimeout,
			Quit:           make(chan struct{}),
		},
	}, nil
}

// queryPriceOracle queries the price oracle for the asking price.
//
// NOTE: This method must be thread-safe.
func (n *Negotiator) queryPriceOracle(
	req msg.Request) (*PriceOracleSuggestedRate, error) {

	ctx, cancel := n.WithCtxQuitNoTimeout()
	defer cancel()

	res, err := n.cfg.PriceOracle.QueryAskingPrice(
		ctx, req.AssetID, req.AssetGroupKey, req.AssetAmount,
		&req.SuggestedExchangeRate,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query price oracle: %w", err)
	}

	return res, nil
}

// handlePriceOracleResponse handles the response from the price oracle.
func (n *Negotiator) handlePriceOracleResponse(
	req msg.Request, res *PriceOracleSuggestedRate) error {

	// If the suggested rate is nil, then we will return the error message
	// supplied by the price oracle.
	if res.SuggestedRate == nil {
		rejectMsg := msg.NewQuoteReject(req.Peer, req.ID, res.Err)

		sendSuccess := fn.SendOrQuit(
			n.RejectedQuotes.NewItemCreated.ChanIn(), rejectMsg,
			n.Quit,
		)
		if !sendSuccess {
			return fmt.Errorf("negotiator failed to send reject " +
				"message")
		}
	}

	// If the suggested rate is not nil, then we can proceed to respond
	// with an accept message.
	var sig [64]byte
	acceptMsg := msg.NewQuoteAccept(
		req.Peer, req.ID, res.SuggestedRate.ScaledRate, res.Expiry, sig,
	)

	sendSuccess := fn.SendOrQuit(
		n.AcceptedQuotes.NewItemCreated.ChanIn(), acceptMsg, n.Quit,
	)
	if !sendSuccess {
		return fmt.Errorf("negotiator failed populate accept message " +
			"channel")
	}

	return nil
}

// HandleIncomingQuoteRequest handles an incoming quote request.
func (n *Negotiator) HandleIncomingQuoteRequest(req msg.Request) error {
	// If there is no price oracle, then we cannot proceed with the
	// negotiation. We will reject the quote request with an error.
	if n.cfg.PriceOracle == nil {
		rejectMsg := msg.NewQuoteReject(
			req.Peer, req.ID, msg.ErrPriceOracleUnavailable,
		)

		sendSuccess := fn.SendOrQuit(
			n.RejectedQuotes.NewItemCreated.ChanIn(), rejectMsg,
			n.Quit,
		)
		if !sendSuccess {
			return fmt.Errorf("negotiator failed to send reject " +
				"message")
		}
	}

	// Query the price oracle in a separate goroutine in case it is a
	// remote service and takes a long time to respond.
	n.Wg.Add(1)
	go func() {
		defer n.Wg.Done()

		oracleResp, err := n.queryPriceOracle(req)
		if err != nil {
			log.Errorf("negotiator failed to query price oracle: "+
				"%v", err)
		}

		err = n.handlePriceOracleResponse(req, oracleResp)
		if err != nil {
			log.Errorf("negotiator failed to handle price oracle "+
				"response: %v", err)
		}
	}()

	return nil
}

// mainEventLoop executes the main event handling loop.
func (n *Negotiator) mainEventLoop() {
	log.Debug("Starting negotiator event loop")

	for {
		select {
		case <-n.Quit:
			log.Debug("Received quit signal. Stopping negotiator " +
				"event loop")
			return
		}
	}
}

// Start starts the service.
func (n *Negotiator) Start() error {
	log.Info("Starting RFQ subsystem: negotiator")

	var startErr error
	n.startOnce.Do(func() {
		// Start the main event loop in a separate goroutine.
		n.Wg.Add(1)
		go func() {
			defer n.Wg.Done()
			n.mainEventLoop()
		}()
	})
	return startErr
}

// Stop stops the handler.
func (n *Negotiator) Stop() error {
	log.Info("Stopping RFQ subsystem: quote negotiator")

	close(n.Quit)
	return nil
}
