package rfqmessages

import (
	"bytes"
	"fmt"
	"io"

	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/lightningnetwork/lnd/tlv"
)

const (
	// TypeAcceptData field TLV types.

	TypeAcceptDataID             tlv.Type = 0
	TypeAcceptDataCharacteristic tlv.Type = 2
	TypeAcceptDataExpiry         tlv.Type = 4
	TypeAcceptDataSignature      tlv.Type = 6
)

func TypeRecordAcceptDataID(id *ID) tlv.Record {
	return tlv.MakePrimitiveRecord(TypeAcceptDataID, id)
}

func TypeRecordAcceptDataCharacteristic(characteristic *uint64) tlv.Record {
	return tlv.MakePrimitiveRecord(
		TypeAcceptDataCharacteristic, characteristic,
	)
}

func TypeRecordAcceptDataExpiry(expirySeconds *uint64) tlv.Record {
	return tlv.MakePrimitiveRecord(
		TypeAcceptDataExpiry, expirySeconds,
	)
}

func TypeRecordAcceptDataSig(sig *[64]byte) tlv.Record {
	return tlv.MakePrimitiveRecord(
		TypeAcceptDataSignature, sig,
	)
}

// QuoteAcceptMsgData is a struct that represents the data field of a quote
// accept message.
type QuoteAcceptMsgData struct {
	// ID is the unique identifier of the request for quote (RFQ).
	ID ID

	// AmtCharacteristic is the characteristic of the asset amount that
	// determines the fee rate.
	AmtCharacteristic uint64

	// ExpirySeconds is the number of seconds until the quote expires.
	ExpirySeconds uint64

	// sig is a signature over the serialized contents of the message.
	sig [64]byte
}

//func NewQuoteAcceptMsgData(q *QuoteAccept) (*QuoteAcceptMsgData, error) {
//	// Hash the fields of the message data so that we can create a signature
//	// over the message.
//	h := sha256.New()
//
//	_, err := h.Write(q.ID[:])
//	if err != nil {
//		return nil, err
//	}
//
//	err = binary.Write(h, binary.BigEndian, q.AmtCharacteristic)
//	if err != nil {
//		return nil, err
//	}
//
//	err = binary.Write(h, binary.BigEndian, q.ExpirySeconds)
//	if err != nil {
//		return nil, err
//	}
//
//	// TODO(ffranr): Sign the hash of the message data.
//	//fieldsHash := h.Sum(nil)
//	//sig
//
//	return &QuoteAcceptMsgData{
//		ID:                q.ID,
//		AmtCharacteristic: q.AmtCharacteristic,
//		ExpirySeconds:     q.ExpirySeconds,
//		//sig:               sig,
//	}, nil
//}

// EncodeRecords determines the non-nil records to include when encoding at
// runtime.
func (q *QuoteAcceptMsgData) encodeRecords() []tlv.Record {
	var records []tlv.Record

	// Add ID record.
	records = append(records, TypeRecordAcceptDataID(&q.ID))

	// Add characteristic record.
	record := TypeRecordAcceptDataCharacteristic(
		&q.AmtCharacteristic,
	)
	records = append(records, record)

	// Add expiry record.
	records = append(
		records, TypeRecordAcceptDataExpiry(&q.ExpirySeconds),
	)

	// Add signature record.
	records = append(
		records, TypeRecordAcceptDataSig(&q.sig),
	)

	return records
}

// Encode encodes the structure into a TLV stream.
func (q *QuoteAcceptMsgData) Encode(writer io.Writer) error {
	stream, err := tlv.NewStream(q.encodeRecords()...)
	if err != nil {
		return err
	}
	return stream.Encode(writer)
}

// DecodeRecords provides all TLV records for decoding.
func (q *QuoteAcceptMsgData) decodeRecords() []tlv.Record {
	return []tlv.Record{
		TypeRecordAcceptDataID(&q.ID),
		TypeRecordAcceptDataCharacteristic(&q.AmtCharacteristic),
		TypeRecordAcceptDataExpiry(&q.ExpirySeconds),
		TypeRecordAcceptDataSig(&q.sig),
	}
}

// Decode decodes the structure from a TLV stream.
func (q *QuoteAcceptMsgData) Decode(r io.Reader) error {
	stream, err := tlv.NewStream(q.decodeRecords()...)
	if err != nil {
		return err
	}
	return stream.Decode(r)
}

// QuoteAccept is a struct that represents an accepted quote message.
type QuoteAccept struct {
	// Peer is the peer that sent the quote request.
	Peer route.Vertex

	// QuoteAcceptMsgData is the message data for the quote accept message.
	QuoteAcceptMsgData
}

// NewQuoteAcceptFromWireMsg instantiates a new instance from a wire message.
func NewQuoteAcceptFromWireMsg(wireMsg WireMessage) (*QuoteAccept, error) {
	// Decode message data component from TLV bytes.
	var msgData QuoteAcceptMsgData
	err := msgData.Decode(bytes.NewReader(wireMsg.Data))
	if err != nil {
		return nil, fmt.Errorf("unable to decode quote accept "+
			"message data: %w", err)
	}

	return &QuoteAccept{
		Peer:               wireMsg.Peer,
		QuoteAcceptMsgData: msgData,
	}, nil
}

// ToWire returns a wire message with a serialized data field.
func (q *QuoteAccept) ToWire() (WireMessage, error) {
	// Encode message data component as TLV bytes.
	var buff *bytes.Buffer
	err := q.QuoteAcceptMsgData.Encode(buff)
	if err != nil {
		return WireMessage{}, fmt.Errorf("unable to encode message "+
			"data: %w", err)
	}
	msgDataBytes := buff.Bytes()

	return WireMessage{
		Peer:    q.Peer,
		MsgType: MsgTypeQuoteAccept,
		Data:    msgDataBytes,
	}, nil
}

// Ensure that the message type implements the OutgoingMessage interface.
var _ OutgoingMessage = (*QuoteAccept)(nil)
