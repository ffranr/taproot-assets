package rfqmsg

import (
	"bytes"
	"fmt"
	"io"

	"github.com/lightningnetwork/lnd/routing/route"
	"github.com/lightningnetwork/lnd/tlv"
)

const (
	// Reject message type field TLV types.

	TypeRejectID  tlv.Type = 0
	TypeRejectErr tlv.Type = 1
)

func TypeRecordRejectID(id *ID) tlv.Record {
	const recordSize = 32

	return tlv.MakeStaticRecord(
		TypeRejectID, id, recordSize, IdEncoder, IdDecoder,
	)
}

func TypeRecordRejectErr(rejectErr *RejectErr) tlv.Record {
	sizeFunc := func() uint64 {
		return 4 + uint64(len(rejectErr.ErrMsg))
	}
	return tlv.MakeDynamicRecord(
		TypeRejectErr, rejectErr, sizeFunc,
		rejectErrEncoder, rejectErrDecoder,
	)
}

func rejectErrEncoder(w io.Writer, val any, buf *[8]byte) error {
	if typ, ok := val.(**RejectErr); ok {
		v := *typ

		// Encode error code.
		err := tlv.EUint8T(w, v.ErrCode, buf)
		if err != nil {
			return err
		}

		// Encode error message.
		msgBytes := []byte(v.ErrMsg)
		err = tlv.EVarBytes(w, msgBytes, buf)
		if err != nil {
			return err
		}

		return nil
	}

	return tlv.NewTypeForEncodingErr(val, "RejectErr")
}

func rejectErrDecoder(r io.Reader, val any, buf *[8]byte, l uint64) error {
	if typ, ok := val.(**RejectErr); ok {
		var errCode uint8
		err := tlv.DUint8(r, &errCode, buf, 4)
		if err != nil {
			return err
		}

		var errMsgBytes []byte
		err = tlv.DVarBytes(r, &errMsgBytes, buf, l-4)
		if err != nil {
			return err
		}

		rejectErr := &RejectErr{
			ErrCode: errCode,
			ErrMsg:  string(errMsgBytes),
		}

		*typ = rejectErr
		return nil
	}

	return tlv.NewTypeForDecodingErr(val, "RejectErr", l, l)
}

// RejectErr is a struct that represents the error code and message of a quote
// reject message.
type RejectErr struct {
	// ErrCode is the error code that provides the reason for the rejection.
	ErrCode uint8

	// ErrMsg is the error message that provides the reason for the
	// rejection.
	ErrMsg string
}

var (
	// ErrUnknownReject is the error code for when the quote is rejected
	// for an unspecified reason.
	ErrUnknownReject = RejectErr{
		ErrCode: 0,
		ErrMsg:  "unknown reject error",
	}

	// ErrPriceOracleUnavailable is the error code for when the price
	// oracle is unavailable.
	ErrPriceOracleUnavailable = RejectErr{
		ErrCode: 1,
		ErrMsg:  "price oracle service unavailable",
	}
)

// rejectMsgData is a struct that represents the data field of a quote
// reject message.
type rejectMsgData struct {
	// ID is the unique identifier of the request for quote (RFQ).
	ID ID

	// Err is the error code and message that provides the reason for the
	// rejection.
	Err RejectErr
}

// EncodeRecords determines the non-nil records to include when encoding at
// runtime.
func (q *rejectMsgData) encodeRecords() []tlv.Record {
	return []tlv.Record{
		TypeRecordRejectID(&q.ID),
		TypeRecordRejectErr(&q.Err),
	}
}

// Encode encodes the structure into a TLV stream.
func (q *rejectMsgData) Encode(writer io.Writer) error {
	stream, err := tlv.NewStream(q.encodeRecords()...)
	if err != nil {
		return err
	}
	return stream.Encode(writer)
}

// DecodeRecords provides all TLV records for decoding.
func (q *rejectMsgData) decodeRecords() []tlv.Record {
	return []tlv.Record{
		TypeRecordRejectID(&q.ID),
		TypeRecordRejectErr(&q.Err),
	}
}

// Decode decodes the structure from a TLV stream.
func (q *rejectMsgData) Decode(r io.Reader) error {
	stream, err := tlv.NewStream(q.decodeRecords()...)
	if err != nil {
		return err
	}
	return stream.Decode(r)
}

// Bytes encodes the structure into a TLV stream and returns the bytes.
func (q *rejectMsgData) Bytes() ([]byte, error) {
	var b bytes.Buffer
	err := q.Encode(&b)
	if err != nil {
		return nil, err
	}

	return b.Bytes(), nil
}

// Reject is a struct that represents a quote reject message.
type Reject struct {
	// Peer is the peer that sent the quote request.
	Peer route.Vertex

	// rejectMsgData is the message data for the quote reject message.
	rejectMsgData
}

// NewRejectMsg creates a new instance of a quote reject message.
func NewRejectMsg(peer route.Vertex, id ID, rejectErr RejectErr) Reject {
	return Reject{
		Peer: peer,
		rejectMsgData: rejectMsgData{
			ID:  id,
			Err: rejectErr,
		},
	}
}

// NewQuoteRejectFromWireMsg instantiates a new instance from a wire message.
func NewQuoteRejectFromWireMsg(wireMsg WireMessage) (*Reject, error) {
	// Decode message data component from TLV bytes.
	var msgData rejectMsgData
	err := msgData.Decode(bytes.NewReader(wireMsg.Data))
	if err != nil {
		return nil, fmt.Errorf("unable to decode quote reject "+
			"message data: %w", err)
	}

	return &Reject{
		Peer:          wireMsg.Peer,
		rejectMsgData: msgData,
	}, nil
}

// ToWire returns a wire message with a serialized data field.
func (q *Reject) ToWire() (WireMessage, error) {
	// Encode message data component as TLV bytes.
	msgDataBytes, err := q.rejectMsgData.Bytes()
	if err != nil {
		return WireMessage{}, fmt.Errorf("unable to encode message "+
			"data: %w", err)
	}

	return WireMessage{
		Peer:    q.Peer,
		MsgType: MsgTypeReject,
		Data:    msgDataBytes,
	}, nil
}

func (q *Reject) MsgPeer() route.Vertex {
	return q.Peer
}

// Ensure that the message type implements the OutgoingMsg interface.
var _ OutgoingMsg = (*Reject)(nil)

// Ensure that the message type implements the IncomingMsg interface.
var _ IncomingMsg = (*Reject)(nil)
