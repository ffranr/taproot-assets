package rfqmessages

import (
	"bytes"
	"fmt"
	"io"

	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/lightningnetwork/lnd/tlv"
)

const (
	// TypeRejectData field TLV types.

	TypeRejectDataID tlv.Type = 0
)

func TypeRecordRejectDataID(id *ID) tlv.Record {
	return tlv.MakePrimitiveRecord(TypeRejectDataID, id)
}

// QuoteRejectMsgData is a struct that represents the data field of a quote
// reject message.
type QuoteRejectMsgData struct {
	// ID is the unique identifier of the request for quote (RFQ).
	ID ID
}

// EncodeRecords determines the non-nil records to include when encoding at
// runtime.
func (q *QuoteRejectMsgData) encodeRecords() []tlv.Record {
	return []tlv.Record{
		TypeRecordRejectDataID(&q.ID),
	}
}

// Encode encodes the structure into a TLV stream.
func (q *QuoteRejectMsgData) Encode(writer io.Writer) error {
	stream, err := tlv.NewStream(q.encodeRecords()...)
	if err != nil {
		return err
	}
	return stream.Encode(writer)
}

// DecodeRecords provides all TLV records for decoding.
func (q *QuoteRejectMsgData) decodeRecords() []tlv.Record {
	return []tlv.Record{
		TypeRecordRejectDataID(&q.ID),
	}
}

// Decode decodes the structure from a TLV stream.
func (q *QuoteRejectMsgData) Decode(r io.Reader) error {
	stream, err := tlv.NewStream(q.decodeRecords()...)
	if err != nil {
		return err
	}
	return stream.Decode(r)
}

// QuoteReject is a struct that represents a quote reject message.
type QuoteReject struct {
	// Peer is the peer that sent the quote request.
	Peer route.Vertex

	// QuoteRejectMsgData is the message data for the quote reject message.
	QuoteRejectMsgData
}

// NewQuoteRejectFromWireMsg instantiates a new instance from a wire message.
func NewQuoteRejectFromWireMsg(wireMsg WireMessage) (*QuoteReject, error) {
	// Decode message data component from TLV bytes.
	var msgData QuoteRejectMsgData
	err := msgData.Decode(bytes.NewReader(wireMsg.Data))
	if err != nil {
		return nil, fmt.Errorf("unable to decode quote reject "+
			"message data: %w", err)
	}

	return &QuoteReject{
		Peer:               wireMsg.Peer,
		QuoteRejectMsgData: msgData,
	}, nil
}

// ToWire returns a wire message with a serialized data field.
func (q *QuoteReject) ToWire() (WireMessage, error) {
	// Encode message data component as TLV bytes.
	var buff *bytes.Buffer
	err := q.QuoteRejectMsgData.Encode(buff)
	if err != nil {
		return WireMessage{}, fmt.Errorf("unable to encode message "+
			"data: %w", err)
	}
	msgDataBytes := buff.Bytes()

	return WireMessage{
		Peer:    q.Peer,
		MsgType: MsgTypeQuoteReject,
		Data:    msgDataBytes,
	}, nil
}

// Ensure that the message type implements the OutgoingMessage interface.
var _ OutgoingMessage = (*QuoteReject)(nil)
