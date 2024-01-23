package rfqmessages

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/lightninglabs/lndclient"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/lightningnetwork/lnd/tlv"
)

const (
	// QuoteRequestMsgData field TLV types.

	QuoteRequestMsgDataIDType                tlv.Type = 0
	QuoteRequestMsgDataAssetIDType           tlv.Type = 1
	QuoteRequestMsgDataGroupKeyType          tlv.Type = 3
	QuoteRequestMsgDataAssetAmountType       tlv.Type = 4
	QuoteRequestMsgDataAmtCharacteristicType tlv.Type = 6
)

func QuoteRequestMsgDataIDRecord(id *ID) tlv.Record {
	idBytes := (*[32]byte)(id)
	return tlv.MakePrimitiveRecord(QuoteRequestMsgDataIDType, idBytes)
}

func QuoteRequestMsgDataAssetIDRecord(assetID **asset.ID) tlv.Record {
	const recordSize = sha256.Size

	return tlv.MakeStaticRecord(
		QuoteRequestMsgDataAssetIDType, assetID, recordSize,
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

func QuoteRequestMsgDataGroupKeyRecord(groupKey **btcec.PublicKey) tlv.Record {
	const recordSize = btcec.PubKeyBytesLenCompressed

	return tlv.MakeStaticRecord(
		QuoteRequestMsgDataGroupKeyType, groupKey, recordSize,
		asset.CompressedPubKeyEncoder, asset.CompressedPubKeyDecoder,
	)
}

func QuoteRequestMsgDataAssetAmountRecord(assetAmount *uint64) tlv.Record {
	return tlv.MakePrimitiveRecord(QuoteRequestMsgDataAssetAmountType, assetAmount)
}

func QuoteRequestMsgDataAmtCharacteristicRecord(amtCharacteristic *uint64) tlv.Record {
	return tlv.MakePrimitiveRecord(
		QuoteRequestMsgDataAmtCharacteristicType, amtCharacteristic,
	)
}

// QuoteRequestMsgData is a struct that represents the message data from a
// custom message request for a quote (RFQ).
type QuoteRequestMsgData struct {
	// ID is the unique identifier of the request for quote (RFQ).
	ID ID

	// // AssetID represents the identifier of the asset for which the peer
	// is requesting a quote.
	AssetID *asset.ID

	// AssetGroupKey is the public group key of the asset for which the peer
	// is requesting a quote.
	AssetGroupKey *btcec.PublicKey

	// AssetAmount is the amount of the asset for which the peer is
	// requesting a quote.
	AssetAmount uint64

	// AmtCharacteristic is the characteristic of the asset amount that
	// determines the conversion rate.
	AmtCharacteristic uint64
}

func NewQuoteRequestMsgDataFromBytes(
	data []byte) (*QuoteRequestMsgData, error) {

	var msgData QuoteRequestMsgData
	err := msgData.Decode(bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("unable to decode incoming quote "+
			"request message data: %w", err)
	}

	err = msgData.Validate()
	if err != nil {
		return nil, fmt.Errorf("unable to validate quote request "+
			"message data: %w", err)
	}

	return &msgData, nil
}

func NewQuoteRequestMsgData(id ID, assetID *asset.ID,
	assetGroupKey *btcec.PublicKey, assetAmount uint64,
	amtCharacteristic uint64) (*QuoteRequestMsgData, error) {

	if assetID == nil && assetGroupKey == nil {
		return nil, fmt.Errorf("asset id and group key cannot both " +
			"be nil")
	}

	if assetID != nil && assetGroupKey != nil {
		return nil, fmt.Errorf("asset id and group key cannot both " +
			"be non-nil")
	}

	return &QuoteRequestMsgData{
		ID:                id,
		AssetID:           assetID,
		AssetGroupKey:     assetGroupKey,
		AssetAmount:       assetAmount,
		AmtCharacteristic: amtCharacteristic,
	}, nil
}

// Validate ensures that the quote request is valid.
func (q *QuoteRequestMsgData) Validate() error {
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
// at runtime.
func (q *QuoteRequestMsgData) encodeRecords() []tlv.Record {
	var records []tlv.Record

	records = append(records, QuoteRequestMsgDataIDRecord(&q.ID))

	if q.AssetID != nil {
		records = append(records, QuoteRequestMsgDataAssetIDRecord(&q.AssetID))
	}

	if q.AssetGroupKey != nil {
		record := QuoteRequestMsgDataGroupKeyRecord(&q.AssetGroupKey)
		records = append(records, record)
	}

	records = append(records, QuoteRequestMsgDataAssetAmountRecord(&q.AssetAmount))

	record := QuoteRequestMsgDataAmtCharacteristicRecord(&q.AmtCharacteristic)
	records = append(records, record)

	return records
}

// Encode encodes the structure into a TLV stream.
func (q *QuoteRequestMsgData) Encode(writer io.Writer) error {
	stream, err := tlv.NewStream(q.encodeRecords()...)
	if err != nil {
		return err
	}
	return stream.Encode(writer)
}

// Bytes encodes the structure into a TLV stream and returns the bytes.
func (q *QuoteRequestMsgData) Bytes() ([]byte, error) {
	var b bytes.Buffer
	err := q.Encode(&b)
	if err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

// DecodeRecords provides all TLV records for decoding.
func (q *QuoteRequestMsgData) decodeRecords() []tlv.Record {
	return []tlv.Record{
		QuoteRequestMsgDataIDRecord(&q.ID),
		QuoteRequestMsgDataAssetIDRecord(&q.AssetID),
		QuoteRequestMsgDataGroupKeyRecord(&q.AssetGroupKey),
		QuoteRequestMsgDataAssetAmountRecord(&q.AssetAmount),
		QuoteRequestMsgDataAmtCharacteristicRecord(&q.AmtCharacteristic),
	}
}

// Decode decodes the structure from a TLV stream.
func (q *QuoteRequestMsgData) Decode(r io.Reader) error {
	stream, err := tlv.NewStream(q.decodeRecords()...)
	if err != nil {
		return err
	}
	return stream.Decode(r)
}

// QuoteRequest is a struct that represents a request for a quote (RFQ).
type QuoteRequest struct {
	// Peer is the peer that sent the quote request.
	Peer route.Vertex

	// QuoteRequestMsgData is the message data for the quote request
	// message.
	QuoteRequestMsgData
}

// Validate ensures that the quote request is valid.
func (q *QuoteRequest) Validate() error {
	return q.QuoteRequestMsgData.Validate()
}

// NewQuoteRequestFromCustomMsg creates a new quote request from a custom
// message.
func NewQuoteRequestFromCustomMsg(
	customMsg lndclient.CustomMessage) (*QuoteRequest, error) {

	msgData, err := NewQuoteRequestMsgDataFromBytes(customMsg.Data)
	if err != nil {
		return nil, fmt.Errorf("unable to decode quote "+
			"request message data: %w", err)
	}

	quoteRequest := QuoteRequest{
		Peer:                customMsg.Peer,
		QuoteRequestMsgData: *msgData,
	}

	// Perform basic sanity checks on the quote request.
	err = quoteRequest.Validate()
	if err != nil {
		return nil, fmt.Errorf("unable to validate quote request: "+
			"%w", err)
	}

	return &quoteRequest, nil
}
