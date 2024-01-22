package rfqmessages

import (
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightningnetwork/lnd/tlv"
)

const (
	// QuoteAccept field TLV types.

	QuoteAcceptIDType                tlv.Type = 0
	QuoteAcceptAssetIDType           tlv.Type = 1
	QuoteAcceptGroupKeyType          tlv.Type = 3
	QuoteAcceptAssetAmountType       tlv.Type = 4
	QuoteAcceptAmtCharacteristicType tlv.Type = 6
)

func QuoteAcceptIDRecord(id *[32]byte) tlv.Record {
	return tlv.MakePrimitiveRecord(QuoteAcceptIDType, id)
}

func QuoteAcceptAssetIDRecord(assetID **asset.ID) tlv.Record {
	const recordSize = sha256.Size

	return tlv.MakeStaticRecord(
		QuoteAcceptAssetIDType, assetID, recordSize,
		IDEncoder, IDDecoder,
	)
}

func QuoteAcceptGroupKeyRecord(groupKey **btcec.PublicKey) tlv.Record {
	const recordSize = btcec.PubKeyBytesLenCompressed

	return tlv.MakeStaticRecord(
		QuoteAcceptGroupKeyType, groupKey, recordSize,
		asset.CompressedPubKeyEncoder, asset.CompressedPubKeyDecoder,
	)
}

func QuoteAcceptAssetAmountRecord(assetAmount *uint64) tlv.Record {
	return tlv.MakePrimitiveRecord(QuoteAcceptAssetAmountType, assetAmount)
}

func QuoteAcceptAmtCharacteristicRecord(amtCharacteristic *uint64) tlv.Record {
	return tlv.MakePrimitiveRecord(
		QuoteAcceptAmtCharacteristicType, amtCharacteristic,
	)
}

// QuoteAccept is a struct that represents a request for a quote (RFQ) from a
// peer.
type QuoteAccept struct {
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
func (q *QuoteAccept) Validate() error {
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
func (q *QuoteAccept) EncodeRecords() []tlv.Record {
	var records []tlv.Record

	records = append(records, QuoteAcceptIDRecord(&q.ID))

	if q.AssetID != nil {
		records = append(records, QuoteAcceptAssetIDRecord(&q.AssetID))
	}

	if q.AssetGroupKey != nil {
		record := QuoteAcceptGroupKeyRecord(&q.AssetGroupKey)
		records = append(records, record)
	}

	records = append(records, QuoteAcceptAssetAmountRecord(&q.AssetAmount))

	record := QuoteAcceptAmtCharacteristicRecord(&q.SuggestedRateTick)
	records = append(records, record)

	return records
}

// Encode encodes the structure into a TLV stream.
func (q *QuoteAccept) Encode(writer io.Writer) error {
	stream, err := tlv.NewStream(q.EncodeRecords()...)
	if err != nil {
		return err
	}
	return stream.Encode(writer)
}

// DecodeRecords provides all TLV records for decoding.
func (q *QuoteAccept) DecodeRecords() []tlv.Record {
	return []tlv.Record{
		QuoteAcceptIDRecord(&q.ID),
		QuoteAcceptAssetIDRecord(&q.AssetID),
		QuoteAcceptGroupKeyRecord(&q.AssetGroupKey),
		QuoteAcceptAssetAmountRecord(&q.AssetAmount),
		QuoteAcceptAmtCharacteristicRecord(&q.SuggestedRateTick),
	}
}

// Decode decodes the structure from a TLV stream.
func (q *QuoteAccept) Decode(r io.Reader) error {
	stream, err := tlv.NewStream(q.DecodeRecords()...)
	if err != nil {
		return err
	}
	return stream.Decode(r)
}

//func EncodeAsPeerMsg()
