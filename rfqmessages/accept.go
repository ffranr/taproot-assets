package rfqmessages

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/lightningnetwork/lnd/lnwire"
	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/lightningnetwork/lnd/tlv"
)

const (
	// Accept message type field TLV types.

	TypeAcceptID          tlv.Type = 0
	TypeAcceptAskingPrice tlv.Type = 2
	TypeAcceptExpiry      tlv.Type = 4
	TypeAcceptSignature   tlv.Type = 6
)

func TypeRecordAcceptID(id *ID) tlv.Record {
	return tlv.MakePrimitiveRecord(TypeAcceptID, id)
}

func TypeRecordAcceptAskingPrice(askingPrice *lnwire.MilliSatoshi) tlv.Record {
	return tlv.MakeStaticRecord(
		TypeAcceptAskingPrice, askingPrice, 8, milliSatoshiEncoder,
		milliSatoshiDecoder,
	)
}

func milliSatoshiEncoder(w io.Writer, val interface{}, buf *[8]byte) error {
	if ms, ok := val.(*lnwire.MilliSatoshi); ok {
		return tlv.EUint64(w, uint64(*ms), buf)
	}

	return tlv.NewTypeForEncodingErr(val, "MilliSatoshi")
}

func milliSatoshiDecoder(r io.Reader, val interface{}, buf *[8]byte,
	l uint64) error {

	if ms, ok := val.(*lnwire.MilliSatoshi); ok {
		var msInt uint64
		err := tlv.DUint64(r, &msInt, buf, l)
		if err != nil {
			return err
		}

		*ms = lnwire.MilliSatoshi(msInt)
		return nil
	}

	return tlv.NewTypeForDecodingErr(val, "MilliSatoshi", l, 8)
}

func TypeRecordAcceptExpiry(expirySeconds *uint64) tlv.Record {
	return tlv.MakePrimitiveRecord(
		TypeAcceptExpiry, expirySeconds,
	)
}

func TypeRecordAcceptSig(sig *[64]byte) tlv.Record {
	return tlv.MakePrimitiveRecord(
		TypeAcceptSignature, sig,
	)
}

// AcceptMsgData is a struct that represents the data field of a quote
// accept message.
type AcceptMsgData struct {
	// ID is the unique identifier of the request for quote (RFQ).
	ID ID

	// AskingPrice is the asking price of the quote.
	AskingPrice lnwire.MilliSatoshi

	// ExpirySeconds is the number of seconds until the quote expires.
	ExpirySeconds uint64

	// sig is a signature over the serialized contents of the message.
	sig [64]byte
}

// EncodeRecords determines the non-nil records to include when encoding at
// runtime.
func (q *AcceptMsgData) encodeRecords() []tlv.Record {
	var records []tlv.Record

	// Add ID record.
	records = append(records, TypeRecordAcceptID(&q.ID))

	// Add asking price record.
	records = append(records, TypeRecordAcceptAskingPrice(&q.AskingPrice))

	// Add expiry record.
	records = append(
		records, TypeRecordAcceptExpiry(&q.ExpirySeconds),
	)

	// Add signature record.
	records = append(
		records, TypeRecordAcceptSig(&q.sig),
	)

	return records
}

// Encode encodes the structure into a TLV stream.
func (q *AcceptMsgData) Encode(writer io.Writer) error {
	stream, err := tlv.NewStream(q.encodeRecords()...)
	if err != nil {
		return err
	}
	return stream.Encode(writer)
}

// DecodeRecords provides all TLV records for decoding.
func (q *AcceptMsgData) decodeRecords() []tlv.Record {
	return []tlv.Record{
		TypeRecordAcceptID(&q.ID),
		TypeRecordAcceptAskingPrice(&q.AskingPrice),
		TypeRecordAcceptExpiry(&q.ExpirySeconds),
		TypeRecordAcceptSig(&q.sig),
	}
}

// Decode decodes the structure from a TLV stream.
func (q *AcceptMsgData) Decode(r io.Reader) error {
	stream, err := tlv.NewStream(q.decodeRecords()...)
	if err != nil {
		return err
	}
	return stream.Decode(r)
}

// Accept is a struct that represents an accepted quote message.
type Accept struct {
	// Peer is the peer that sent the quote request.
	Peer route.Vertex

	// AcceptMsgData is the message data for the quote accept message.
	AcceptMsgData
}

// NewQuoteAccept creates a new instance of a quote accept message.
func NewQuoteAccept(peer route.Vertex, id ID, askingPrice uint64,
	expirySeconds uint64, sig [64]byte) Accept {
	return Accept{
		Peer: peer,
		AcceptMsgData: AcceptMsgData{
			ID:            id,
			AskingPrice:   lnwire.MilliSatoshi(askingPrice),
			ExpirySeconds: expirySeconds,
			sig:           sig,
		},
	}
}

// NewQuoteAcceptFromWireMsg instantiates a new instance from a wire message.
func NewQuoteAcceptFromWireMsg(wireMsg WireMessage) (*Accept, error) {
	// Decode message data component from TLV bytes.
	var msgData AcceptMsgData
	err := msgData.Decode(bytes.NewReader(wireMsg.Data))
	if err != nil {
		return nil, fmt.Errorf("unable to decode quote accept "+
			"message data: %w", err)
	}

	return &Accept{
		Peer:          wireMsg.Peer,
		AcceptMsgData: msgData,
	}, nil
}

// ShortChannelId returns the short channel ID of the quote accept.
func (q *Accept) ShortChannelId() SerialisedScid {
	// Given valid RFQ message ID, we then define a RFQ short chain ID
	// (SCID) by taking the last 8 bytes of the RFQ message ID and
	// interpreting them as a 64-bit integer.
	scidBytes := q.ID[24:]

	// TODO(ffranr): Am I doing this right?
	scidInteger := binary.BigEndian.Uint64(scidBytes)

	return SerialisedScid(scidInteger)
}

// ToWire returns a wire message with a serialized data field.
//
// TODO(ffranr): This method should accept a signer so that we can generate a
// signature over the message data.
func (q *Accept) ToWire() (WireMessage, error) {
	// Encode message data component as TLV bytes.
	var buff *bytes.Buffer
	err := q.AcceptMsgData.Encode(buff)
	if err != nil {
		return WireMessage{}, fmt.Errorf("unable to encode message "+
			"data: %w", err)
	}
	msgDataBytes := buff.Bytes()

	return WireMessage{
		Peer:    q.Peer,
		MsgType: MsgTypeAccept,
		Data:    msgDataBytes,
	}, nil
}

// Ensure that the message type implements the OutgoingMessage interface.
var _ OutgoingMessage = (*Accept)(nil)
