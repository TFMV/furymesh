// Code generated by the FlatBuffers compiler. DO NOT EDIT.

package fury

import (
	flatbuffers "github.com/google/flatbuffers/go"
)

type ErrorMessage struct {
	_tab flatbuffers.Table
}

func GetRootAsErrorMessage(buf []byte, offset flatbuffers.UOffsetT) *ErrorMessage {
	n := flatbuffers.GetUOffsetT(buf[offset:])
	x := &ErrorMessage{}
	x.Init(buf, n+offset)
	return x
}

func FinishErrorMessageBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.Finish(offset)
}

func GetSizePrefixedRootAsErrorMessage(buf []byte, offset flatbuffers.UOffsetT) *ErrorMessage {
	n := flatbuffers.GetUOffsetT(buf[offset+flatbuffers.SizeUint32:])
	x := &ErrorMessage{}
	x.Init(buf, n+offset+flatbuffers.SizeUint32)
	return x
}

func FinishSizePrefixedErrorMessageBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.FinishSizePrefixed(offset)
}

func (rcv *ErrorMessage) Init(buf []byte, i flatbuffers.UOffsetT) {
	rcv._tab.Bytes = buf
	rcv._tab.Pos = i
}

func (rcv *ErrorMessage) Table() flatbuffers.Table {
	return rcv._tab
}

func (rcv *ErrorMessage) Code() int32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(4))
	if o != 0 {
		return rcv._tab.GetInt32(o + rcv._tab.Pos)
	}
	return 0
}

func (rcv *ErrorMessage) MutateCode(n int32) bool {
	return rcv._tab.MutateInt32Slot(4, n)
}

func (rcv *ErrorMessage) Message() []byte {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(6))
	if o != 0 {
		return rcv._tab.ByteVector(o + rcv._tab.Pos)
	}
	return nil
}

func (rcv *ErrorMessage) Context() []byte {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(8))
	if o != 0 {
		return rcv._tab.ByteVector(o + rcv._tab.Pos)
	}
	return nil
}

func ErrorMessageStart(builder *flatbuffers.Builder) {
	builder.StartObject(3)
}
func ErrorMessageAddCode(builder *flatbuffers.Builder, code int32) {
	builder.PrependInt32Slot(0, code, 0)
}
func ErrorMessageAddMessage(builder *flatbuffers.Builder, message flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(1, flatbuffers.UOffsetT(message), 0)
}
func ErrorMessageAddContext(builder *flatbuffers.Builder, context flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(2, flatbuffers.UOffsetT(context), 0)
}
func ErrorMessageEnd(builder *flatbuffers.Builder) flatbuffers.UOffsetT {
	return builder.EndObject()
}
