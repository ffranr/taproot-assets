package rfqservice

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lightninglabs/lndclient"
	"github.com/lightninglabs/taproot-assets/fn"
	msg "github.com/lightninglabs/taproot-assets/rfqmessages"
)

// ChannelRemit is a struct that holds the terms of a channel quote remit.
type ChannelRemit struct {
	// AmtCharacteristic is the characteristic of the asset amount that
	// determines the fee rate.
	AmtCharacteristic uint64

	// ExpirySeconds is the number of seconds until the quote expires.
	ExpirySeconds uint64
}

// OrderHandlerCfg is a struct that holds the configuration parameters for the
// order handler service.
type OrderHandlerCfg struct {
	// CleanupInterval is the interval at which the order
	// handler cleans up expired accepted quotes from its local cache.
	CleanupInterval time.Duration

	// HtlcInterceptor is the HTLC interceptor. This component is used to
	// intercept and accept/reject HTLCs.
	HtlcInterceptor HtlcInterceptor
}

// OrderHandler orchestrates management of accepted RFQ (Request For Quote)
// bundles. It monitors HTLCs (Hash Time Locked Contracts), determining
// acceptance or rejection based on compliance with the terms of the associated
// quote.
type OrderHandler struct {
	startOnce sync.Once
	stopOnce  sync.Once

	// cfg holds the configuration parameters for the RFQ order handler.
	cfg OrderHandlerCfg

	// channelRemits is a map of serialised short channel IDs (SCIDs) to
	// associated active channel quote remits.
	channelRemits map[msg.SerialisedScid]ChannelRemit

	// channelRemitsMtx guards the channelRemits map.
	channelRemitsMtx sync.Mutex

	// ContextGuard provides a wait group and main quit channel that can be
	// used to create guarded contexts.
	*fn.ContextGuard
}

// NewOrderHandler creates a new struct instance.
func NewOrderHandler(cfg OrderHandlerCfg) (*OrderHandler, error) {
	return &OrderHandler{
		cfg:           cfg,
		channelRemits: make(map[msg.SerialisedScid]ChannelRemit),
		ContextGuard: &fn.ContextGuard{
			DefaultTimeout: DefaultTimeout,
			Quit:           make(chan struct{}),
		},
	}, nil
}

// handleIncomingHtlc handles an incoming HTLC.
//
// NOTE: This function must be thread safe.
func (h *OrderHandler) handleIncomingHtlc(_ context.Context,
	htlc lndclient.InterceptedHtlc) (*lndclient.InterceptedHtlcResponse,
	error) {

	scid := msg.SerialisedScid(htlc.OutgoingChannelID.ToUint64())
	channelRemit, ok := h.FetchChannelRemit(scid)

	if !ok {
		return &lndclient.InterceptedHtlcResponse{
			Action: lndclient.InterceptorActionFail,
		}, nil
	}

	// At this point, we know that the channel remit exists and has not
	// expired. We can now check that the HTLC amount is within the bounds
	// of the channel remit's characteristic.

	// TODO(ffranr): Check that the HTLC amount is within the bounds of the
	// channel remit's amt characteristic.
	channelRemit = channelRemit

	return nil, nil
}

// setupHtlcIntercept sets up HTLC interception.
func (h *OrderHandler) setupHtlcIntercept(ctx context.Context) error {
	// Intercept incoming HTLCs. This call passes the handleIncomingHtlc
	// function to the interceptor. The interceptor will call this function
	// in a separate goroutine.
	err := h.cfg.HtlcInterceptor.InterceptHtlcs(ctx, h.handleIncomingHtlc)
	if err != nil {
		return fmt.Errorf("unable to setup incoming HTLCs "+
			"interception: %w", err)
	}

	return nil
}

// mainEventLoop executes the main event handling loop.
func (h *OrderHandler) mainEventLoop() {
	log.Debug("Starting main event loop for order handler")

	cleanupTicker := time.NewTicker(h.cfg.CleanupInterval)
	defer cleanupTicker.Stop()

	for {
		select {
		// Periodically clean up expired channel remits from our local
		// cache.
		case <-cleanupTicker.C:
			log.Debug("Cleaning up any stale channel remits from " +
				"the order handler")
			staleCounter := h.cleanupStaleChannelRemits()
			if staleCounter > 0 {
				log.Tracef("Removed %d stale channel remits "+
					"from the order handler", staleCounter)
			}

		// TODO(ffranr): Listen for HTLCs. Determine whether they are
		//  accepted or rejected based on compliance with the terms of
		//  the any applicable quote.

		case <-h.Quit:
			log.Debug("Received quit signal. Stopping negotiator " +
				"event loop")
			return
		}
	}
}

// Start starts the service.
func (h *OrderHandler) Start() error {
	log.Info("Starting RFQ subsystem: order handler")

	var startErr error
	h.startOnce.Do(func() {
		// Start the main event loop in a separate goroutine.
		h.Wg.Add(1)
		go func() {
			ctx, cancel := h.WithCtxQuitNoTimeout()

			defer cancel()
			defer h.Wg.Done()

			startErr = h.setupHtlcIntercept(ctx)
			if startErr != nil {
				log.Errorf("Error setting up HTLC "+
					"interception: %v", startErr)
				return
			}

			h.mainEventLoop()
		}()
	})

	return startErr
}

// RegisterChannelRemit registers a channel management remit. If a remit exists
// for the channel, it is overwritten.
func (h *OrderHandler) RegisterChannelRemit(quoteAccept msg.QuoteAccept) {
	// Add quote accept to the accepted quotes map.
	scid := quoteAccept.ShortChannelId()

	h.channelRemitsMtx.Lock()
	defer h.channelRemitsMtx.Unlock()

	h.channelRemits[scid] = ChannelRemit{
		AmtCharacteristic: quoteAccept.AmtCharacteristic,
		ExpirySeconds:     quoteAccept.ExpirySeconds,
	}
}

// FetchChannelRemit fetches a channel remit given a serialised SCID. If a
// channel remit is not found, false is returned. Expired channel remits are
// also not returned and are removed from the cache.
func (h *OrderHandler) FetchChannelRemit(
	scid msg.SerialisedScid) (*ChannelRemit, bool) {

	h.channelRemitsMtx.Lock()
	defer h.channelRemitsMtx.Unlock()

	remit, ok := h.channelRemits[scid]
	if !ok {
		return nil, false
	}

	// If the remit has expired, return false and clear it from the cache.
	if time.Now().Unix() > int64(remit.ExpirySeconds) {
		delete(h.channelRemits, scid)
		return nil, false
	}

	return &remit, true
}

// cleanupStaleChannelRemits removes any channel remits that have expired.
func (h *OrderHandler) cleanupStaleChannelRemits() int {
	h.channelRemitsMtx.Lock()
	defer h.channelRemitsMtx.Unlock()

	// Iterate over the channel remits and remove any that have expired.
	staleCounter := 0
	for scid, remit := range h.channelRemits {
		if time.Now().Unix() > int64(remit.ExpirySeconds) {
			staleCounter++
			delete(h.channelRemits, scid)
		}
	}

	return staleCounter
}

// Stop stops the handler.
func (h *OrderHandler) Stop() error {
	log.Info("Stopping RFQ subsystem: order handler")

	close(h.Quit)
	return nil
}

// HtlcInterceptor is an interface that abstracts the hash time locked contract
// (HTLC) intercept functionality.
type HtlcInterceptor interface {
	// InterceptHtlcs intercepts HTLCs, using the handling function provided
	// to respond to HTLCs.
	InterceptHtlcs(context.Context, lndclient.HtlcInterceptHandler) error
}
