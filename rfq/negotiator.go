package rfq

import (
	"encoding/hex"
	"fmt"
	"sync"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightninglabs/taproot-assets/fn"
	"github.com/lightninglabs/taproot-assets/rfqmsg"
	"github.com/lightningnetwork/lnd/routing/route"
)

// NegotiatorCfg holds the configuration for the negotiator.
type NegotiatorCfg struct {
	// PriceOracle is the price oracle that the negotiator will use to
	// determine whether a quote is accepted or rejected.
	PriceOracle PriceOracle
}

// Negotiator is a struct that handles the negotiation of quotes. It is a RFQ
// subsystem. It determines whether a quote request is accepted or rejected.
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

	// ErrChan is a channel that is used to communicate goroutine errors
	// to the parent service.
	ErrChan chan error

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
		ErrChan:          make(chan error, 1),
		ContextGuard: &fn.ContextGuard{
			DefaultTimeout: DefaultTimeout,
			Quit:           make(chan struct{}),
		},
	}, nil
}

// queryAskFromPriceOracle queries the price oracle for an asking price. It
// returns an appropriate outgoing response message which should be sent to the
// peer.
func (n *Negotiator) queryAskFromPriceOracle(
	request rfqmsg.Request) (rfqmsg.OutgoingMsg, error) {

	// Query the price oracle for an asking price.
	ctx, cancel := n.WithCtxQuitNoTimeout()
	defer cancel()

	oracleResponse, err := n.cfg.PriceOracle.QueryAskPrice(
		ctx, request.AssetID, request.AssetGroupKey,
		request.AssetAmount, &request.BidPrice,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query price oracle for ask "+
			"price: %w", err)
	}

	// If the asking price is nil, then we will return a quote reject
	// message which contains the error message supplied by the price
	// oracle.
	if oracleResponse.AskPrice == nil {
		rejectMsg := rfqmsg.NewRejectMsg(request, *oracleResponse.Err)
		return &rejectMsg, nil
	}

	// TODO(ffranr): Ensure that the expiryDelay time is valid and
	//  sufficient.

	// If the asking price is not nil, then we can proceed to respond with
	// an accept message.
	acceptMsg := rfqmsg.NewAcceptFromRequest(
		request, *oracleResponse.AskPrice, oracleResponse.Expiry,
	)
	return &acceptMsg, nil
}

// HandleIncomingQuoteRequest handles an incoming quote request.
func (n *Negotiator) HandleIncomingQuoteRequest(request rfqmsg.Request) error {
	// TODO(ffranr): Ensure that we have a sell offer for the asset that is
	//  being requested.

	// If there is no price oracle available, then we cannot proceed with
	// the negotiation. We will reject the quote request with an error.
	if n.cfg.PriceOracle == nil {
		rejectMsg := rfqmsg.NewRejectMsg(
			request, rfqmsg.ErrPriceOracleUnavailable,
		)
		var msg rfqmsg.OutgoingMsg = &rejectMsg

		sendSuccess := fn.SendOrQuit(n.outgoingMessages, msg, n.Quit)
		if !sendSuccess {
			return fmt.Errorf("negotiator failed to send reject " +
				"message")
		}

		return nil
	}

	// Initiate a query to the price oracle asynchronously using a separate
	// goroutine. Since the price oracle might be an external service,
	// responses could be delayed.
	n.Wg.Add(1)
	go func() {
		defer n.Wg.Done()

		// Query the price oracle for an asking price.
		outgoingMsgResponse, err := n.queryAskFromPriceOracle(request)
		if err != nil {
			err = fmt.Errorf("negotiator failed to handle price "+
				"oracle ask price response: %w", err)
			n.ErrChan <- err
		}

		// Send the response message to the outgoing messages channel.
		sendSuccess := fn.SendOrQuit(
			n.outgoingMessages, outgoingMsgResponse, n.Quit,
		)
		if !sendSuccess {
			err = fmt.Errorf("negotiator failed to add message "+
				"to the outgoing messages channel (msg=%v)",
				outgoingMsgResponse)
			n.ErrChan <- err
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

// queryBidFromPriceOracle queries the price oracle for a bid price. It
// returns an appropriate outgoing response message which should be sent to the
// peer.
func (n *Negotiator) queryBidFromPriceOracle(peer route.Vertex,
	assetId *asset.ID, assetGroupKey *btcec.PublicKey,
	assetAmount uint64) (rfqmsg.OutgoingMsg, error) {

	ctx, cancel := n.WithCtxQuitNoTimeout()
	defer cancel()

	oracleResponse, err := n.cfg.PriceOracle.QueryBidPrice(
		ctx, assetId, assetGroupKey, assetAmount,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query price oracle for "+
			"bid: %w", err)
	}

	// If the bid price is nil, then we will return the error message
	// supplied by the price oracle.
	if oracleResponse.BidPrice == nil {
		return nil, fmt.Errorf("price oracle returned error: %v",
			*oracleResponse.Err)
	}

	// TODO(ffranr): Ensure that the expiryDelay time is valid and
	//  sufficient.

	requestMsg, err := rfqmsg.NewRequest(
		peer, assetId, assetGroupKey, assetAmount,
		*oracleResponse.BidPrice,
	)
	if err != nil {
		return nil, fmt.Errorf("unable to create quote request "+
			"message: %w", err)
	}

	return &requestMsg, nil
}

// RequestQuote requests a bid quote (buying an asset) from a peer.
func (n *Negotiator) RequestQuote(buyOrder BuyOrder) error {
	// If there is no price oracle available, then we cannot proceed with
	// the negotiation. We will reject the quote request with an error.
	if n.cfg.PriceOracle == nil {
		return fmt.Errorf("price oracle is unavailable")
	}

	// Query the price oracle for a reasonable bid price. We perform this
	// query and response handling in a separate goroutine in case it is a
	// remote service and takes a long time to respond.
	n.Wg.Add(1)
	go func() {
		defer n.Wg.Done()

		// Query the price oracle for a bid price.
		outgoingMsg, err := n.queryBidFromPriceOracle(
			*buyOrder.Peer, buyOrder.AssetID,
			buyOrder.AssetGroupKey, buyOrder.MinAssetAmount,
		)
		if err != nil {
			err := fmt.Errorf("negotiator failed to handle price "+
				"oracle response: %w", err)
			n.ErrChan <- err
			return
		}

		// Send the response message to the outgoing messages channel.
		sendSuccess := fn.SendOrQuit(
			n.outgoingMessages, outgoingMsg, n.Quit,
		)
		if !sendSuccess {
			err := fmt.Errorf("negotiator failed to add quote " +
				"request message to the outgoing messages " +
				"channel")
			n.ErrChan <- err
			return
		}
	}()

	return nil
}

// Start starts the service.
func (n *Negotiator) Start() error {
	var startErr error
	n.startOnce.Do(func() {
		log.Info("Starting subsystem: negotiator")
	})
	return startErr
}

// Stop stops the handler.
func (n *Negotiator) Stop() error {
	n.stopOnce.Do(func() {
		log.Info("Stopping subsystem: quote negotiator")

		// Stop any active context.
		close(n.Quit)
	})
	return nil
}
