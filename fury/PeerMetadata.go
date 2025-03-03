// Code generated by the FlatBuffers compiler. DO NOT EDIT.

package fury

import (
	flatbuffers "github.com/google/flatbuffers/go"
)

type PeerMetadata struct {
	_tab flatbuffers.Table
}

func GetRootAsPeerMetadata(buf []byte, offset flatbuffers.UOffsetT) *PeerMetadata {
	n := flatbuffers.GetUOffsetT(buf[offset:])
	x := &PeerMetadata{}
	x.Init(buf, n+offset)
	return x
}

func FinishPeerMetadataBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.Finish(offset)
}

func GetSizePrefixedRootAsPeerMetadata(buf []byte, offset flatbuffers.UOffsetT) *PeerMetadata {
	n := flatbuffers.GetUOffsetT(buf[offset+flatbuffers.SizeUint32:])
	x := &PeerMetadata{}
	x.Init(buf, n+offset+flatbuffers.SizeUint32)
	return x
}

func FinishSizePrefixedPeerMetadataBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.FinishSizePrefixed(offset)
}

func (rcv *PeerMetadata) Init(buf []byte, i flatbuffers.UOffsetT) {
	rcv._tab.Bytes = buf
	rcv._tab.Pos = i
}

func (rcv *PeerMetadata) Table() flatbuffers.Table {
	return rcv._tab
}

func (rcv *PeerMetadata) PeerId() []byte {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(4))
	if o != 0 {
		return rcv._tab.ByteVector(o + rcv._tab.Pos)
	}
	return nil
}

func (rcv *PeerMetadata) AvailableFiles(j int) []byte {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(6))
	if o != 0 {
		a := rcv._tab.Vector(o)
		return rcv._tab.ByteVector(a + flatbuffers.UOffsetT(j*4))
	}
	return nil
}

func (rcv *PeerMetadata) AvailableFilesLength() int {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(6))
	if o != 0 {
		return rcv._tab.VectorLen(o)
	}
	return 0
}

func PeerMetadataStart(builder *flatbuffers.Builder) {
	builder.StartObject(2)
}
func PeerMetadataAddPeerId(builder *flatbuffers.Builder, peerId flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(0, flatbuffers.UOffsetT(peerId), 0)
}
func PeerMetadataAddAvailableFiles(builder *flatbuffers.Builder, availableFiles flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(1, flatbuffers.UOffsetT(availableFiles), 0)
}
func PeerMetadataStartAvailableFilesVector(builder *flatbuffers.Builder, numElems int) flatbuffers.UOffsetT {
	return builder.StartVector(4, numElems, 4)
}
func PeerMetadataEnd(builder *flatbuffers.Builder) flatbuffers.UOffsetT {
	return builder.EndObject()
}
