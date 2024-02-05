package rfqmessages

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/lightninglabs/taproot-assets/asset"
	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/lightningnetwork/lnd/tlv"
)

const (
	// Request message type field TLV types.

	TypeRequestID                          tlv.Type = 0
	TypeRequestAssetID                     tlv.Type = 1
	TypeRequestAssetGroupKey               tlv.Type = 3
	TypeRequestAssetAmount                 tlv.Type = 4
	TypeRequestScaledExchangeRate          tlv.Type = 6
	TypeRequestExchangeRateScalingExponent tlv.Type = 8
)

func TypeRecordRequestID(id *ID) tlv.Record {
	idBytes := (*[32]byte)(id)
	return tlv.MakePrimitiveRecord(TypeRequestID, idBytes)
}

func TypeRecordRequestAssetID(assetID **asset.ID) tlv.Record {
	const recordSize = sha256.Size

	return tlv.MakeStaticRecord(
		TypeRequestAssetID, assetID, recordSize,
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

func TypeRecordRequestAssetGroupKey(groupKey **btcec.PublicKey) tlv.Record {
	const recordSize = btcec.PubKeyBytesLenCompressed

	return tlv.MakeStaticRecord(
		TypeRequestAssetGroupKey, groupKey, recordSize,
		asset.CompressedPubKeyEncoder, asset.CompressedPubKeyDecoder,
	)
}

func TypeRecordRequestAssetAmount(assetAmount *uint64) tlv.Record {
	return tlv.MakePrimitiveRecord(TypeRequestAssetAmount, assetAmount)
}

func TypeRecordRequestScaledExchangeRate(
	scaledExchangeRate *uint64) tlv.Record {

	return tlv.MakePrimitiveRecord(
		TypeRequestScaledExchangeRate, scaledExchangeRate,
	)
}

func TypeRecordRequestExchangeRateScalingExponent(
	scalingExponent *uint8) tlv.Record {

	return tlv.MakePrimitiveRecord(
		TypeRequestExchangeRateScalingExponent, scalingExponent,
	)
}

// RequestMsgData is a struct that represents the message data from a
// quote request message.
type RequestMsgData struct {
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

	// SuggestedExchangeRate is the suggested exchange rate for the asset.
	SuggestedExchangeRate ExchangeRate
}

func NewQuoteRequestMsgDataFromBytes(
	data []byte) (*RequestMsgData, error) {

	var msgData RequestMsgData
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
	scaledExchangeRate uint64,
	exchangeRateScalingExponent uint8) (*RequestMsgData, error) {

	if assetID == nil && assetGroupKey == nil {
		return nil, fmt.Errorf("asset id and group key cannot both " +
			"be nil")
	}

	if assetID != nil && assetGroupKey != nil {
		return nil, fmt.Errorf("asset id and group key cannot both " +
			"be non-nil")
	}

	return &RequestMsgData{
		ID:            id,
		AssetID:       assetID,
		AssetGroupKey: assetGroupKey,
		AssetAmount:   assetAmount,
		SuggestedExchangeRate: ExchangeRate{
			ScaledRate:      scaledExchangeRate,
			ScalingExponent: exchangeRateScalingExponent,
		},
	}, nil
}

// Validate ensures that the quote request is valid.
func (q *RequestMsgData) Validate() error {
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
func (q *RequestMsgData) encodeRecords() []tlv.Record {
	var records []tlv.Record

	records = append(records, TypeRecordRequestID(&q.ID))

	if q.AssetID != nil {
		records = append(records, TypeRecordRequestAssetID(&q.AssetID))
	}

	if q.AssetGroupKey != nil {
		record := TypeRecordRequestAssetGroupKey(&q.AssetGroupKey)
		records = append(records, record)
	}

	records = append(records, TypeRecordRequestAssetAmount(&q.AssetAmount))

	scaledRateRecord := TypeRecordRequestScaledExchangeRate(
		&q.SuggestedExchangeRate.ScaledRate,
	)
	records = append(records, scaledRateRecord)

	scalingExponentRecord := TypeRecordRequestExchangeRateScalingExponent(
		&q.SuggestedExchangeRate.ScalingExponent,
	)
	records = append(records, scalingExponentRecord)

	return records
}

// Encode encodes the structure into a TLV stream.
func (q *RequestMsgData) Encode(writer io.Writer) error {
	stream, err := tlv.NewStream(q.encodeRecords()...)
	if err != nil {
		return err
	}
	return stream.Encode(writer)
}

// Bytes encodes the structure into a TLV stream and returns the bytes.
func (q *RequestMsgData) Bytes() ([]byte, error) {
	var b bytes.Buffer
	err := q.Encode(&b)
	if err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

// DecodeRecords provides all TLV records for decoding.
func (q *RequestMsgData) decodeRecords() []tlv.Record {
	return []tlv.Record{
		TypeRecordRequestID(&q.ID),
		TypeRecordRequestAssetID(&q.AssetID),
		TypeRecordRequestAssetGroupKey(&q.AssetGroupKey),
		TypeRecordRequestAssetAmount(&q.AssetAmount),
		TypeRecordRequestScaledExchangeRate(
			&q.SuggestedExchangeRate.ScaledRate,
		),
		TypeRecordRequestExchangeRateScalingExponent(
			&q.SuggestedExchangeRate.ScalingExponent,
		),
	}
}

// Decode decodes the structure from a TLV stream.
func (q *RequestMsgData) Decode(r io.Reader) error {
	stream, err := tlv.NewStream(q.decodeRecords()...)
	if err != nil {
		return err
	}
	return stream.Decode(r)
}

// Request is a struct that represents a request for a quote (RFQ).
type Request struct {
	// Peer is the peer that sent the quote request.
	Peer route.Vertex

	// RequestMsgData is the message data for the quote request
	// message.
	RequestMsgData
}

// NewRequestMsgFromWire instantiates a new instance from a wire message.
func NewRequestMsgFromWire(wireMsg WireMessage) (*Request, error) {
	msgData, err := NewQuoteRequestMsgDataFromBytes(wireMsg.Data)
	if err != nil {
		return nil, fmt.Errorf("unable to decode quote "+
			"request message data: %w", err)
	}

	quoteRequest := Request{
		Peer:           wireMsg.Peer,
		RequestMsgData: *msgData,
	}

	// Perform basic sanity checks on the quote request.
	err = quoteRequest.Validate()
	if err != nil {
		return nil, fmt.Errorf("unable to validate quote request: "+
			"%w", err)
	}

	return &quoteRequest, nil
}

// Validate ensures that the quote request is valid.
func (q *Request) Validate() error {
	return q.RequestMsgData.Validate()
}

// ToWire returns a wire message with a serialized data field.
func (q *Request) ToWire() (WireMessage, error) {
	// Encode message data component as TLV bytes.
	var buff *bytes.Buffer
	err := q.RequestMsgData.Encode(buff)
	if err != nil {
		return WireMessage{}, fmt.Errorf("unable to encode message "+
			"data: %w", err)
	}
	msgDataBytes := buff.Bytes()

	return WireMessage{
		Peer:    q.Peer,
		MsgType: MsgTypeRequest,
		Data:    msgDataBytes,
	}, nil
}

// Ensure that the message type implements the OutgoingMsg interface.
var _ OutgoingMsg = (*Request)(nil)

// Ensure that the message type implements the IncomingMsg interface.
var _ IncomingMsg = (*Request)(nil)
