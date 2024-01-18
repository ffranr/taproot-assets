package rfq

import (
	"bytes"
	"encoding/binary"
	"fmt"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightningnetwork/lnd/lnwire"
)

// TapMessageTypeBaseOffset is the taproot-assets specific message type
// identifier base offset. All tap messages will have a type identifier that is
// greater than this value.
//
// This offset was chosen as the concatenation of the alphabetical index
// positions of the letters "t" (20), "a" (1), and "p" (16).
const TapMessageTypeBaseOffset = uint32(lnwire.CustomTypeStart) + 20116

var (
	// MsgTypeQuoteRequest is the message type identifier for a quote
	// request message.
	MsgTypeQuoteRequest = TapMessageTypeBaseOffset + 1
)

// QuoteRequest is a struct that represents a request for a quote (RFQ) from a
// peer.
type QuoteRequest struct {
	// ID is the unique identifier of the request for quote (RFQ).
	ID [32]byte

	// // AssetID represents the identifier of the asset for which the peer
	// is requesting a quote.
	AssetID asset.ID

	// AssetCompressedPubGroupKey is the compressed public group key of the
	// asset for which the peer is requesting a quote. The compressed public
	// key is 33-byte long.
	AssetCompressedPubGroupKey *btcec.PublicKey

	// AssetAmount is the amount of the asset for which the peer is
	// requesting a quote.
	AssetAmount uint64

	SuggestedRateTick uint64
}

// Encode serializes the QuoteRequest struct into a byte slice.
func (q *QuoteRequest) Encode() ([]byte, error) {
	buf := new(bytes.Buffer)

	_, err := buf.Write(q.ID[:])
	if err != nil {
		return nil, err
	}

	_, err = buf.Write(q.AssetID[:])
	if err != nil {
		return nil, err
	}

	var groupKeyBytes [33]byte
	if q.AssetCompressedPubGroupKey != nil {
		k := q.AssetCompressedPubGroupKey.SerializeCompressed()
		copy(groupKeyBytes[:], k)
	}
	_, err = buf.Write(groupKeyBytes[:])
	if err != nil {
		return nil, err
	}

	err = binary.Write(buf, binary.BigEndian, q.AssetAmount)
	if err != nil {
		return nil, err
	}

	err = binary.Write(buf, binary.BigEndian, q.SuggestedRateTick)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// Decode populates a QuoteRequest instance from a byte slice
func (q *QuoteRequest) Decode(data []byte) error {
	if len(data) != 113 {
		return fmt.Errorf("invalid data length")
	}

	var err error

	// Parse the request's ID.
	copy(q.ID[:], data[:32])

	// Parse the asset's ID.
	copy(q.AssetID[:], data[32:64])

	// Parse the asset's compressed public group key.
	var compressedPubGroupKeyBytes []byte
	copy(compressedPubGroupKeyBytes[:], data[64:97])
	q.AssetCompressedPubGroupKey, err = btcec.ParsePubKey(
		compressedPubGroupKeyBytes,
	)
	if err != nil {
		return fmt.Errorf("unable to parse compressed public group "+
			"key: %w", err)
	}

	q.AssetAmount = binary.BigEndian.Uint64(data[97:105])
	q.SuggestedRateTick = binary.BigEndian.Uint64(data[105:])

	return nil
}
