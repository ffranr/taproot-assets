package rfqservice

import (
	"context"
	"crypto/tls"
	"net/url"
	"time"

	"github.com/lightninglabs/taproot-assets/asset"
	msg "github.com/lightninglabs/taproot-assets/rfqmessages"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// PriceOracleSuggestedRate is a struct that holds the price oracle's suggested
// exchange rate for an asset.
type PriceOracleSuggestedRate struct {
	// AssetID is the asset ID.
	AssetID *asset.ID

	// AssetGroupKey is the asset group key.
	AssetGroupKey *asset.GroupKey

	// AssetAmount is the asset amount.
	AssetAmount uint64

	// SuggestedRate is the suggested exchange rate.
	SuggestedRate *msg.ExchangeRate

	// Expiry is the suggested rate expiry lifetime unix timestamp.
	Expiry uint64
}

// PriceOracle is an interface that provides exchange rate information for
// assets.
type PriceOracle interface {
	// QueryExchangeRate returns the exchange rate for the given asset.
	QueryExchangeRate(ctx context.Context, assetId *asset.ID,
		assetGroupKey *asset.GroupKey, assetAmount uint64,
		suggestedRate *msg.ExchangeRate) (*PriceOracleSuggestedRate,
		error)
}

// RpcPriceOracle is a price oracle that uses an external RPC server to get
// exchange rate information.
type RpcPriceOracle struct {
}

// serverDialOpts returns the set of server options needed to connect to the
// price oracle RPC server using a TLS connection.
func serverDialOpts() ([]grpc.DialOption, error) {
	var opts []grpc.DialOption

	// Skip TLS certificate verification.
	tlsConfig := tls.Config{InsecureSkipVerify: true}
	transportCredentials := credentials.NewTLS(&tlsConfig)
	opts = append(opts, grpc.WithTransportCredentials(transportCredentials))

	return opts, nil
}

// NewRpcPriceOracle creates a new RPC price oracle handle given the address
// of the price oracle RPC server.
func NewRpcPriceOracle(addr url.URL) (*RpcPriceOracle, error) {
	//// Connect to the RPC server.
	//dialOpts, err := serverDialOpts()
	//if err != nil {
	//	return nil, err
	//}
	//
	//serverAddr := fmt.Sprintf("%s:%s", addr.Hostname(), addr.Port())
	//conn, err := grpc.Dial(serverAddr, dialOpts...)
	//if err != nil {
	//	return nil, err
	//}

	return &RpcPriceOracle{}, nil
}

// QueryExchangeRate returns the exchange rate for the given asset.
func (r *RpcPriceOracle) QueryExchangeRate(ctx context.Context,
	assetId *asset.ID, assetGroupKey *asset.GroupKey, assetAmount uint64,
	suggestedRate *msg.ExchangeRate) (*msg.ExchangeRate, error) {

	//// Call the external oracle service to get the exchange rate.
	//conn := getClientConn(ctx, false)

	return nil, nil
}

// MockPriceOracle is a mock implementation of the PriceOracle interface.
// It returns the suggested rate as the exchange rate.
type MockPriceOracle struct {
	rateLifetime uint64
}

// NewMockPriceOracle creates a new mock price oracle.
func NewMockPriceOracle(rateLifetime uint64) *MockPriceOracle {
	return &MockPriceOracle{
		rateLifetime: rateLifetime,
	}
}

// QueryExchangeRate returns the suggested rate as the exchange rate.
func (m *MockPriceOracle) QueryExchangeRate(_ context.Context,
	assetId *asset.ID, assetGroupKey *asset.GroupKey, assetAmt uint64,
	suggestedRate *msg.ExchangeRate) (*PriceOracleSuggestedRate, error) {

	// Calculate the rate expiry lifetime.
	expiry := uint64(time.Now().Unix()) + m.rateLifetime

	return &PriceOracleSuggestedRate{
		AssetID:       assetId,
		AssetGroupKey: assetGroupKey,
		AssetAmount:   assetAmt,
		SuggestedRate: suggestedRate,
		Expiry:        expiry,
	}, nil
}

// Ensure that MockPriceOracle implements the PriceOracle interface.
var _ PriceOracle = (*MockPriceOracle)(nil)
