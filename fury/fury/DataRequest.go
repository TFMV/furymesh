// Code generated by the FlatBuffers compiler. DO NOT EDIT.

package fury

import (
	flatbuffers "github.com/google/flatbuffers/go"
)

type DataRequest struct {
	_tab flatbuffers.Table
}

func GetRootAsDataRequest(buf []byte, offset flatbuffers.UOffsetT) *DataRequest {
	n := flatbuffers.GetUOffsetT(buf[offset:])
	x := &DataRequest{}
	x.Init(buf, n+offset)
	return x
}

func FinishDataRequestBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.Finish(offset)
}

func GetSizePrefixedRootAsDataRequest(buf []byte, offset flatbuffers.UOffsetT) *DataRequest {
	n := flatbuffers.GetUOffsetT(buf[offset+flatbuffers.SizeUint32:])
	x := &DataRequest{}
	x.Init(buf, n+offset+flatbuffers.SizeUint32)
	return x
}

func FinishSizePrefixedDataRequestBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.FinishSizePrefixed(offset)
}

func (rcv *DataRequest) Init(buf []byte, i flatbuffers.UOffsetT) {
	rcv._tab.Bytes = buf
	rcv._tab.Pos = i
}

func (rcv *DataRequest) Table() flatbuffers.Table {
	return rcv._tab
}

func (rcv *DataRequest) RequestId() []byte {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(4))
	if o != 0 {
		return rcv._tab.ByteVector(o + rcv._tab.Pos)
	}
	return nil
}

func (rcv *DataRequest) FileId() []byte {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(6))
	if o != 0 {
		return rcv._tab.ByteVector(o + rcv._tab.Pos)
	}
	return nil
}

func DataRequestStart(builder *flatbuffers.Builder) {
	builder.StartObject(2)
}
func DataRequestAddRequestId(builder *flatbuffers.Builder, requestId flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(0, flatbuffers.UOffsetT(requestId), 0)
}
func DataRequestAddFileId(builder *flatbuffers.Builder, fileId flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(1, flatbuffers.UOffsetT(fileId), 0)
}
func DataRequestEnd(builder *flatbuffers.Builder) flatbuffers.UOffsetT {
	return builder.EndObject()
}
