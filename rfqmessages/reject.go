package rfqmessages

import (
	"bytes"
	"fmt"
	"io"

	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/lightningnetwork/lnd/tlv"
)

const (
	// Reject message type field TLV types.

	TypeRejectID tlv.Type = 0
)

func TypeRecordRejectID(id *ID) tlv.Record {
	return tlv.MakePrimitiveRecord(TypeRejectID, id)
}

// RejectMsgData is a struct that represents the data field of a quote
// reject message.
type RejectMsgData struct {
	// ID is the unique identifier of the request for quote (RFQ).
	ID ID
}

// EncodeRecords determines the non-nil records to include when encoding at
// runtime.
func (q *RejectMsgData) encodeRecords() []tlv.Record {
	return []tlv.Record{
		TypeRecordRejectID(&q.ID),
	}
}

// Encode encodes the structure into a TLV stream.
func (q *RejectMsgData) Encode(writer io.Writer) error {
	stream, err := tlv.NewStream(q.encodeRecords()...)
	if err != nil {
		return err
	}
	return stream.Encode(writer)
}

// DecodeRecords provides all TLV records for decoding.
func (q *RejectMsgData) decodeRecords() []tlv.Record {
	return []tlv.Record{
		TypeRecordRejectID(&q.ID),
	}
}

// Decode decodes the structure from a TLV stream.
func (q *RejectMsgData) Decode(r io.Reader) error {
	stream, err := tlv.NewStream(q.decodeRecords()...)
	if err != nil {
		return err
	}
	return stream.Decode(r)
}

// Reject is a struct that represents a quote reject message.
type Reject struct {
	// Peer is the peer that sent the quote request.
	Peer route.Vertex

	// RejectMsgData is the message data for the quote reject message.
	RejectMsgData
}

// NewQuoteRejectFromWireMsg instantiates a new instance from a wire message.
func NewQuoteRejectFromWireMsg(wireMsg WireMessage) (*Reject, error) {
	// Decode message data component from TLV bytes.
	var msgData RejectMsgData
	err := msgData.Decode(bytes.NewReader(wireMsg.Data))
	if err != nil {
		return nil, fmt.Errorf("unable to decode quote reject "+
			"message data: %w", err)
	}

	return &Reject{
		Peer:          wireMsg.Peer,
		RejectMsgData: msgData,
	}, nil
}

// ToWire returns a wire message with a serialized data field.
func (q *Reject) ToWire() (WireMessage, error) {
	// Encode message data component as TLV bytes.
	var buff *bytes.Buffer
	err := q.RejectMsgData.Encode(buff)
	if err != nil {
		return WireMessage{}, fmt.Errorf("unable to encode message "+
			"data: %w", err)
	}
	msgDataBytes := buff.Bytes()

	return WireMessage{
		Peer:    q.Peer,
		MsgType: MsgTypeReject,
		Data:    msgDataBytes,
	}, nil
}

// Ensure that the message type implements the OutgoingMessage interface.
var _ OutgoingMessage = (*Reject)(nil)
