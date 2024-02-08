package rfqservice

import (
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/fn"
	"github.com/lightninglabs/taproot-assets/rfqmsg"
)

// NegotiatorCfg holds the configuration for the negotiator.
type NegotiatorCfg struct {
	// PriceOracle is the price oracle that the negotiator will use to
	// determine whether a quote is accepted or rejected.
	PriceOracle PriceOracle
}

// Negotiator is a struct that handles the negotiation of quotes. It is a RFQ
// subsystem. It determines whether a quote is accepted or rejected.
type Negotiator struct {
	startOnce sync.Once
	stopOnce  sync.Once

	// cfg holds the configuration parameters for the negotiator.
	cfg NegotiatorCfg

	// outgoingMessages is a channel which is populated with outgoing peer
	// messages.
	outgoingMessages chan<- rfqmsg.OutgoingMsg

	// assetSellOffers is a map (keyed on asset) that holds the asset sell
	// offers that the negotiator has received.
	assetSellOffers map[string]AssetSellOffer

	assetGroupSellOffers map[string]AssetSellOffer

	// ContextGuard provides a wait group and main quit channel that can be
	// used to create guarded contexts.
	*fn.ContextGuard
}

// NewNegotiator creates a new quote negotiator.
func NewNegotiator(cfg NegotiatorCfg,
	outgoingMessages chan<- rfqmsg.OutgoingMsg) (*Negotiator, error) {

	return &Negotiator{
		cfg:              cfg,
		outgoingMessages: outgoingMessages,
		ContextGuard: &fn.ContextGuard{
			DefaultTimeout: DefaultTimeout,
			Quit:           make(chan struct{}),
		},
	}, nil
}

// queryPriceOracle queries the price oracle for the asking price.
func (n *Negotiator) queryPriceOracle(
	req rfqmsg.Request) (*OracleAskingPriceResponse, error) {

	ctx, cancel := n.WithCtxQuitNoTimeout()
	defer cancel()

	res, err := n.cfg.PriceOracle.QueryAskingPrice(
		ctx, req.AssetID, req.AssetGroupKey, req.AssetAmount,
		&req.BidPrice,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query price oracle: %w", err)
	}

	return res, nil
}

// handlePriceOracleResponse handles a response from the price oracle.
func (n *Negotiator) handlePriceOracleResponse(request rfqmsg.Request,
	oracleResponse *OracleAskingPriceResponse) error {

	// If the asking price is nil, then we will return the error message
	// supplied by the price oracle.
	if oracleResponse.AskingPrice == nil {
		rejectMsg := rfqmsg.NewRejectMsg(
			request.Peer, request.ID, *oracleResponse.Err,
		)
		var msg rfqmsg.OutgoingMsg = &rejectMsg

		sendSuccess := fn.SendOrQuit(n.outgoingMessages, msg, n.Quit)
		if !sendSuccess {
			return fmt.Errorf("negotiator failed to add reject " +
				"message to the outgoing messages channel")
		}
	}

	// TODO(ffranr): Ensure that the expiry time is valid and sufficient.

	// If the asking price is not nil, then we can proceed to respond with
	// an accept message.
	acceptMsg := rfqmsg.NewAcceptFromRequest(
		request, *oracleResponse.AskingPrice, oracleResponse.Expiry,
	)
	var msg rfqmsg.OutgoingMsg = &acceptMsg

	sendSuccess := fn.SendOrQuit(n.outgoingMessages, msg, n.Quit)
	if !sendSuccess {
		return fmt.Errorf("negotiator failed to add accept message " +
			"to the outgoing messages channel")
	}

	return nil
}

// HandleIncomingQuoteRequest handles an incoming quote request.
func (n *Negotiator) HandleIncomingQuoteRequest(req rfqmsg.Request) error {
	// Ensure that we have a sell offer for the asset that is being
	// requested.

	// If there is no price oracle available, then we cannot proceed with
	// the negotiation. We will reject the quote request with an error.
	if n.cfg.PriceOracle == nil {
		rejectMsg := rfqmsg.NewRejectMsg(
			req.Peer, req.ID, rfqmsg.ErrPriceOracleUnavailable,
		)
		var msg rfqmsg.OutgoingMsg = &rejectMsg

		sendSuccess := fn.SendOrQuit(n.outgoingMessages, msg, n.Quit)
		if !sendSuccess {
			return fmt.Errorf("negotiator failed to send reject " +
				"message")
		}
	}

	// Query the price oracle in a separate goroutine in case it is a remote
	// service and takes a long time to respond.
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

// AssetSellOffer is a struct that represents an asset sell offer. This
// data structure describes the maximum amount of an asset that is available
// for sale.
type AssetSellOffer struct {
	// AssetID represents the identifier of the subject asset.
	AssetID *asset.ID

	// AssetGroupKey is the public group key of the subject asset.
	AssetGroupKey *btcec.PublicKey

	// MaxAssetAmount is the maximum amount of the asset under offer.
	MaxAssetAmount uint64
}

// Validate validates the asset sell offer.
func (a *AssetSellOffer) Validate() error {
	if a.AssetID == nil && a.AssetGroupKey == nil {
		return fmt.Errorf("asset ID is nil and asset group key is nil")
	}

	if a.AssetID != nil && a.AssetGroupKey != nil {
		return fmt.Errorf("asset ID and asset group key are both set")
	}

	if a.MaxAssetAmount == 0 {
		return fmt.Errorf("max asset amount is zero")
	}

	return nil
}

// UpsertAssetSellOffer upserts an asset sell offer with the negotiator.
func (n *Negotiator) UpsertAssetSellOffer(offer AssetSellOffer) error {
	// Validate the offer.
	err := offer.Validate()
	if err != nil {
		return fmt.Errorf("invalid asset sell offer: %w", err)
	}

	// Store the offer in the appropriate map.
	//
	// If the asset group key is not nil, then we will use it as the key for
	// the offer. Otherwise, we will use the asset ID as the key.
	switch {
	case offer.AssetGroupKey != nil:
		compressedKey := offer.AssetGroupKey.SerializeCompressed()
		keyStr := hex.EncodeToString(compressedKey)

		n.assetGroupSellOffers[keyStr] = offer

	case offer.AssetID != nil:
		idStr := offer.AssetID.String()
		n.assetSellOffers[idStr] = offer
	}

	return nil
}

// RemoveAssetSellOffer removes an asset sell offer from the negotiator.
func (n *Negotiator) RemoveAssetSellOffer(assetID *asset.ID,
	assetGroupKey *btcec.PublicKey) error {

	// Remove the offer from the appropriate map.
	//
	// If the asset group key is not nil, then we will use it as the key for
	// the offer. Otherwise, we will use the asset ID as the key.
	switch {
	case assetGroupKey != nil:
		compressedKey := assetGroupKey.SerializeCompressed()
		keyStr := hex.EncodeToString(compressedKey)

		delete(n.assetGroupSellOffers, keyStr)

	case assetID != nil:
		idStr := assetID.String()
		delete(n.assetSellOffers, idStr)

	default:
		return fmt.Errorf("asset ID and asset group key are both nil")
	}

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
	var startErr error
	n.startOnce.Do(func() {
		log.Info("Starting subsystem: negotiator")

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
	n.stopOnce.Do(func() {
		log.Info("Stopping subsystem: quote negotiator")

		// Stop the main event loop.
		close(n.Quit)
	})
	return nil
}
