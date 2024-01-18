package rfqmessages

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/tlv"
)

const (
	// QuoteRequest field TLV types.

	QuoteRequestIDType                tlv.Type = 0
	QuoteRequestAssetIDType           tlv.Type = 1
	QuoteRequestGroupKeyType          tlv.Type = 3
	QuoteRequestAssetAmountType       tlv.Type = 4
	QuoteRequestAmtCharacteristicType tlv.Type = 6
)

func QuoteRequestIDRecord(id *[32]byte) tlv.Record {
	return tlv.MakePrimitiveRecord(QuoteRequestIDType, id)
}

func QuoteRequestAssetIDRecord(assetID *asset.ID) tlv.Record {
	return tlv.MakePrimitiveRecord(QuoteRequestAssetIDType, assetID)
}

func QuoteRequestGroupKeyRecord(
	assetCompressedPubGroupKey *btcec.PublicKey) tlv.Record {

	// Serialize pub key to its compressed form. This allows us to handle
	// the public key as a primitive record.
	pubKeyBytes := assetCompressedPubGroupKey.SerializeCompressed()

	return tlv.MakePrimitiveRecord(
		QuoteRequestGroupKeyType, &pubKeyBytes,
	)
}

func QuoteRequestAssetAmountRecord(assetAmount *uint64) tlv.Record {
	return tlv.MakePrimitiveRecord(QuoteRequestAssetAmountType, assetAmount)
}

func QuoteRequestAmtCharacteristicRecord(amtCharacteristic *uint64) tlv.Record {
	return tlv.MakePrimitiveRecord(
		QuoteRequestAmtCharacteristicType, amtCharacteristic,
	)
}

// TapMessageTypeBaseOffset is the taproot-assets specific message type
// identifier base offset. All tap messages will have a type identifier that is
// greater than this value.
//
// This offset was chosen as the concatenation of the alphabetical index
// positions of the letters "t" (20), "a" (1), and "p" (16).
const TapMessageTypeBaseOffset = 20116 + uint32(lnwire.CustomTypeStart)

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
	AssetID *asset.ID

	// AssetGroupKey is the public group key of the asset for which the peer
	// is requesting a quote.
	AssetGroupKey *btcec.PublicKey

	// AssetAmount is the amount of the asset for which the peer is
	// requesting a quote.
	AssetAmount uint64

	// TODO(ffranr): rename to AmtCharacteristic?
	SuggestedRateTick uint64
}

// EncodeRecords determines the non-nil records to include when encoding an
// asset witness at runtime.
func (q *QuoteRequest) EncodeRecords() []tlv.Record {
	var records []tlv.Record

	records = append(records, QuoteRequestIDRecord(&q.ID))

	if q.AssetID != nil {
		records = append(records, QuoteRequestAssetIDRecord(q.AssetID))
	}

	if q.AssetGroupKey != nil {
		record := QuoteRequestGroupKeyRecord(q.AssetGroupKey)
		records = append(records, record)
	}

	records = append(records, QuoteRequestAssetAmountRecord(&q.AssetAmount))

	record := QuoteRequestAmtCharacteristicRecord(&q.SuggestedRateTick)
	records = append(records, record)

	return records
}

// Encode encodes the structure into a TLV stream.
func (q *QuoteRequest) Encode(writer io.Writer) error {
	stream, err := tlv.NewStream(q.EncodeRecords()...)
	if err != nil {
		return err
	}
	return stream.Encode(writer)
}

// EncodeNonTlv serializes the QuoteRequest struct into a byte slice.
func (q *QuoteRequest) EncodeNonTlv() ([]byte, error) {
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
	if q.AssetGroupKey != nil {
		k := q.AssetGroupKey.SerializeCompressed()
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
	q.AssetGroupKey, err = btcec.ParsePubKey(
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
