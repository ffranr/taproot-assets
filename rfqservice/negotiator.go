package rfqservice

import (
	"sync"

	"github.com/lightninglabs/taproot-assets/fn"
	msg "github.com/lightninglabs/taproot-assets/rfqmessages"
)

// Negotiator is a struct that handles the negotiation of quotes. It is a
// subsystem of the RFQ system. It determines whether a quote is accepted or
// rejected.
type Negotiator struct {
	startOnce sync.Once
	stopOnce  sync.Once

	// AcceptedQuotes is a channel which is populated with accepted quotes.
	AcceptedQuotes *fn.EventReceiver[msg.QuoteAccept]

	// incomingQuoteRequests is a channel which is populated with
	// unprocessed incoming (received) quote request messages.
	incomingQuoteRequests <-chan msg.QuoteRequest

	// ErrChan is the handle's error reporting channel.
	ErrChan <-chan error

	// ContextGuard provides a wait group and main quit channel that can be
	// used to create guarded contexts.
	*fn.ContextGuard
}

// NewNegotiator creates a new quote negotiator.
func NewNegotiator() (*Negotiator, error) {
	acceptedQuotes := fn.NewEventReceiver[msg.QuoteAccept](
		fn.DefaultQueueSize,
	)

	return &Negotiator{
		AcceptedQuotes: acceptedQuotes,

		ErrChan: make(<-chan error),

		ContextGuard: &fn.ContextGuard{
			DefaultTimeout: DefaultTimeout,
			Quit:           make(chan struct{}),
		},
	}, nil
}

// HandleIncomingQuoteRequest handles an incoming quote request.
func (h *Negotiator) HandleIncomingQuoteRequest(_ msg.QuoteRequest) error {
	// TODO(ffranr): Push quote request onto queue. We will need to handle
	//  quote requests synchronously, because we may need to contact
	//  an external oracle service for each request.

	return nil
}

// mainEventLoop executes the main event handling loop.
func (h *Negotiator) mainEventLoop() {
	log.Debug("Starting negotiator event loop")

	for {
		select {
		case quoteRequest := <-h.incomingQuoteRequests:
			quoteRequest = quoteRequest

		// TODO(ffranr): Consume from quote request queue channel here.

		case <-h.Quit:
			log.Debug("Received quit signal. Stopping negotiator " +
				"event loop")
			return
		}
	}
}

// Start starts the event handler.
func (h *Negotiator) Start() error {
	log.Info("Starting RFQ subsystem: negotiator")

	var startErr error
	h.startOnce.Do(func() {
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
func (h *Negotiator) Stop() error {
	log.Info("Stopping RFQ subsystem: quote negotiator")

	close(h.Quit)
	return nil
}
