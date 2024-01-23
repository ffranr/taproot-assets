package rfqservice

import (
	"github.com/lightninglabs/taproot-assets/fn"
	msg "github.com/lightninglabs/taproot-assets/rfqmessages"
)

// QuoteNegotiator is a struct that handles the negotiation of quotes. It is a
// subsystem of the RFQ handling system. It determines whether a quote is
// accepted or rejected.
type QuoteNegotiator struct {
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

// NewQuoteNegotiator creates a new RFQ quote negotiator.
func NewQuoteNegotiator() (*QuoteNegotiator, error) {
	acceptedQuotes := fn.NewEventReceiver[msg.QuoteAccept](
		fn.DefaultQueueSize,
	)

	return &QuoteNegotiator{
		AcceptedQuotes: acceptedQuotes,

		ErrChan: make(<-chan error),

		ContextGuard: &fn.ContextGuard{
			DefaultTimeout: DefaultTimeout,
			Quit:           make(chan struct{}),
		},
	}, nil
}

// HandleIncomingQuoteRequest handles an incoming quote request.
func (h *QuoteNegotiator) HandleIncomingQuoteRequest(_ msg.QuoteRequest) error {
	// TODO(ffranr): Push quote request onto queue. We will need to handle
	//  quote requests synchronously, because we may need to contact
	//  an external oracle service for each request.

	return nil
}

// Start starts the event handler.
func (h *QuoteNegotiator) Start() error {
	log.Info("Starting RFQ subsystem: quote negotiation handler")

	for {
		select {
		case quoteRequest := <-h.incomingQuoteRequests:
			quoteRequest = quoteRequest

		// TODO(ffranr): Consume from quote request queue channel here.

		case <-h.Quit:
			return nil
		}
	}
}

// Stop stops the handler.
func (h *QuoteNegotiator) Stop() error {
	log.Info("Stopping RFQ subsystem: quote negotiation handler")

	close(h.Quit)
	return nil
}
