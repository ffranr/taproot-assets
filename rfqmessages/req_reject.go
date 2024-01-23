package rfqmessages

import (
	"bytes"
	"fmt"
	"io"

	"github.com/lightninglabs/lndclient"
	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/lightningnetwork/lnd/tlv"
)

const (
	// QuoteRejectMsgData field TLV types.

	QuoteRejectMsgDataIDType tlv.Type = 0
)

func QuoteRejectMsgDataIDRecord(id *ID) tlv.Record {
	return tlv.MakePrimitiveRecord(QuoteRejectMsgDataIDType, id)
}

// QuoteRejectMsgData is a struct that represents the data field of a quote
// reject custom message.
type QuoteRejectMsgData struct {
	// ID is the unique identifier of the request for quote (RFQ).
	ID ID
}

// EncodeRecords determines the non-nil records to include when encoding at
// runtime.
func (q *QuoteRejectMsgData) encodeRecords() []tlv.Record {
	return []tlv.Record{
		QuoteRejectMsgDataIDRecord(&q.ID),
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
		QuoteRejectMsgDataIDRecord(&q.ID),
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

// NewQuoteRejectFromCustomMsg creates a new quote reject message from a
// lndclient custom message.
func NewQuoteRejectFromCustomMsg(
	customMsg lndclient.CustomMessage) (*QuoteReject, error) {

	// Decode message data component from TLV bytes.
	var msgData QuoteRejectMsgData
	err := msgData.Decode(bytes.NewReader(customMsg.Data))
	if err != nil {
		return nil, fmt.Errorf("unable to decode quote reject "+
			"message data: %w", err)
	}

	return &QuoteReject{
		Peer:               customMsg.Peer,
		QuoteRejectMsgData: msgData,
	}, nil
}
