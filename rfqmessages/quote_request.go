package rfqmessages

import (
	"crypto/sha256"
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

func QuoteRequestAssetIDRecord(assetID **asset.ID) tlv.Record {
	const recordSize = sha256.Size

	return tlv.MakeStaticRecord(
		QuoteRequestAssetIDType, assetID, recordSize,
		IDEncoder, IDDecoder,
	)
}

func IDEncoder(w io.Writer, val any, buf *[8]byte) error {
	if t, ok := val.(**asset.ID); ok {
		id := [sha256.Size]byte(**t)
		return tlv.EBytes32(w, &id, buf)
	}

	return tlv.NewTypeForEncodingErr(val, "AssetID")
}

func IDDecoder(r io.Reader, val any, buf *[8]byte, l uint64) error {
	const assetIDBytesLen = sha256.Size

	if typ, ok := val.(**asset.ID); ok {
		var idBytes [assetIDBytesLen]byte

		err := tlv.DBytes32(r, &idBytes, buf, assetIDBytesLen)
		if err != nil {
			return err
		}

		id := asset.ID(idBytes)
		assetId := &id

		*typ = assetId
		return nil
	}

	return tlv.NewTypeForDecodingErr(val, "AssetID", l, sha256.Size)
}

func QuoteRequestGroupKeyRecord(groupKey **btcec.PublicKey) tlv.Record {
	const recordSize = btcec.PubKeyBytesLenCompressed

	return tlv.MakeStaticRecord(
		QuoteRequestGroupKeyType, groupKey, recordSize,
		asset.CompressedPubKeyEncoder, asset.CompressedPubKeyDecoder,
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

// Validate ensures that the quote request is valid.
func (q *QuoteRequest) Validate() error {
	if q.AssetID == nil && q.AssetGroupKey == nil {
		return fmt.Errorf("asset id and group key cannot both be nil")
	}

	if q.AssetID != nil && q.AssetGroupKey != nil {
		return fmt.Errorf("asset id and group key cannot both be " +
			"non-nil")
	}

	return nil
}

// EncodeRecords determines the non-nil records to include when encoding an
// asset witness at runtime.
func (q *QuoteRequest) EncodeRecords() []tlv.Record {
	var records []tlv.Record

	records = append(records, QuoteRequestIDRecord(&q.ID))

	if q.AssetID != nil {
		records = append(records, QuoteRequestAssetIDRecord(&q.AssetID))
	}

	if q.AssetGroupKey != nil {
		record := QuoteRequestGroupKeyRecord(&q.AssetGroupKey)
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

// DecodeRecords provides all TLV records for decoding.
func (q *QuoteRequest) DecodeRecords() []tlv.Record {
	return []tlv.Record{
		QuoteRequestIDRecord(&q.ID),
		QuoteRequestAssetIDRecord(&q.AssetID),
		QuoteRequestGroupKeyRecord(&q.AssetGroupKey),
		QuoteRequestAssetAmountRecord(&q.AssetAmount),
		QuoteRequestAmtCharacteristicRecord(&q.SuggestedRateTick),
	}
}

// Decode decodes the structure from a TLV stream.
func (q *QuoteRequest) Decode(r io.Reader) error {
	stream, err := tlv.NewStream(q.DecodeRecords()...)
	if err != nil {
		return err
	}
	return stream.Decode(r)
}
