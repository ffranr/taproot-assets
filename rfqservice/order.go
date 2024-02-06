package rfqservice

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lightninglabs/lndclient"
	"github.com/lightninglabs/taproot-assets/fn"
	"github.com/lightninglabs/taproot-assets/rfqmsg"
	"github.com/lightningnetwork/lnd/lnwire"
)

// SerialisedScid is a serialised short channel id (SCID).
type SerialisedScid uint64

// ChannelRemit is a struct that holds the terms which determine whether a
// channel HTLC is accepted or rejected.
type ChannelRemit struct {
	// Scid is the serialised short channel ID (SCID) of the channel for
	// which the remit applies.
	Scid SerialisedScid

	// AssetAmount is the amount of the tap asset that is being requested.
	AssetAmount uint64

	// MinimumChannelPayment is the minimum number of millisatoshis that must be
	// sent in the HTLC.
	MinimumChannelPayment lnwire.MilliSatoshi

	// Expiry is the asking price expiry lifetime unix timestamp.
	Expiry uint64
}

// NewChannelRemit creates a new channel remit.
func NewChannelRemit(assetAmount uint64,
	quoteAccept rfqmsg.Accept) (*ChannelRemit, error) {

	// Compute the serialised short channel ID (SCID) for the channel.
	scid := SerialisedScid(quoteAccept.ShortChannelId())

	return &ChannelRemit{
		Scid:                  scid,
		AssetAmount:           assetAmount,
		MinimumChannelPayment: quoteAccept.AskingPrice,
		Expiry:                quoteAccept.Expiry,
	}, nil
}

// CheckHtlcCompliance returns an error if the given HTLC intercept descriptor
// does not satisfy the subject channel remit.
func (c *ChannelRemit) CheckHtlcCompliance(
	htlc lndclient.InterceptedHtlc) error {

	// Check that the HTLC amount is at least the minimum acceptable amount.
	if htlc.AmountOutMsat <= c.MinimumChannelPayment {
		return fmt.Errorf("htlc out amount is less than the remit's "+
			"minimum (htlc_out_msat=%d, remit_min_msat=%d)",
			htlc.AmountOutMsat, c.MinimumChannelPayment)
	}

	// Check that the channel SCID is as expected.
	htlcScid := SerialisedScid(htlc.OutgoingChannelID.ToUint64())
	if htlcScid != c.Scid {
		return fmt.Errorf("htlc outgoing channel ID does not match "+
			"remit's SCID (htlc_scid=%d, remit_scid=%d)", htlcScid, c.Scid)
	}

	// Lastly, check to ensure that the channel remit has not expired.
	if time.Now().Unix() > int64(c.Expiry) {
		return fmt.Errorf("channel remit has expired (expiry=%d)", c.Expiry)
	}

	return nil
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
	channelRemits map[SerialisedScid]ChannelRemit

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
		channelRemits: make(map[SerialisedScid]ChannelRemit),
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

	// TODO(ffranr): I think we need a way to silently dismiss HTLCs that
	//  are not associated with tap.

	scid := SerialisedScid(htlc.OutgoingChannelID.ToUint64())
	channelRemit, ok := h.FetchChannelRemit(scid)

	// If a channel remit does not exist for the channel SCID, we reject the
	// HTLC.
	if !ok {
		return &lndclient.InterceptedHtlcResponse{
			Action: lndclient.InterceptorActionFail,
		}, nil
	}

	// At this point, we know that the channel remit exists and has not
	// expired whilst sitting in the local cache. We can now check that the
	// HTLC complies with the channel remit.
	err := channelRemit.CheckHtlcCompliance(htlc)
	if err != nil {
		log.Warnf("HTLC does not comply with channel remit: %v "+
			"(htlc=%v, channel_remit=%v)", err, htlc, channelRemit)

		return &lndclient.InterceptedHtlcResponse{
			Action: lndclient.InterceptorActionFail,
		}, nil
	}

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
			h.cleanupStaleChannelRemits()

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
// for the channel SCID, it is overwritten.
func (h *OrderHandler) RegisterChannelRemit(assetAmount uint64,
	quoteAccept rfqmsg.Accept) error {

	channelRemit, err := NewChannelRemit(assetAmount, quoteAccept)
	if err != nil {
		return fmt.Errorf("unable to create channel remit: %w", err)
	}

	h.channelRemitsMtx.Lock()
	defer h.channelRemitsMtx.Unlock()

	h.channelRemits[channelRemit.Scid] = *channelRemit

	return nil
}

// FetchChannelRemit fetches a channel remit given a serialised SCID. If a
// channel remit is not found, false is returned. Expired channel remits are
// also not returned and are removed from the cache.
func (h *OrderHandler) FetchChannelRemit(scid SerialisedScid) (*ChannelRemit,
	bool) {

	h.channelRemitsMtx.Lock()
	defer h.channelRemitsMtx.Unlock()

	remit, ok := h.channelRemits[scid]
	if !ok {
		return nil, false
	}

	// If the remit has expired, return false and clear it from the cache.
	if time.Now().Unix() > int64(remit.Expiry) {
		delete(h.channelRemits, scid)
		return nil, false
	}

	return &remit, true
}

// cleanupStaleChannelRemits removes expired channel remits from the local
// cache.
func (h *OrderHandler) cleanupStaleChannelRemits() {
	h.channelRemitsMtx.Lock()
	defer h.channelRemitsMtx.Unlock()

	// Iterate over the channel remits and remove any that have expired.
	staleCounter := 0
	for scid, remit := range h.channelRemits {
		if time.Now().Unix() > int64(remit.Expiry) {
			staleCounter++
			delete(h.channelRemits, scid)
		}
	}

	if staleCounter > 0 {
		log.Tracef("Removed %d stale channel remits from the order "+
			"handler", staleCounter)
	}
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
