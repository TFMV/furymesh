// Code generated by the FlatBuffers compiler. DO NOT EDIT.

package fury

import (
	flatbuffers "github.com/google/flatbuffers/go"
)

type FileMetadata struct {
	_tab flatbuffers.Table
}

func GetRootAsFileMetadata(buf []byte, offset flatbuffers.UOffsetT) *FileMetadata {
	n := flatbuffers.GetUOffsetT(buf[offset:])
	x := &FileMetadata{}
	x.Init(buf, n+offset)
	return x
}

func FinishFileMetadataBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.Finish(offset)
}

func GetSizePrefixedRootAsFileMetadata(buf []byte, offset flatbuffers.UOffsetT) *FileMetadata {
	n := flatbuffers.GetUOffsetT(buf[offset+flatbuffers.SizeUint32:])
	x := &FileMetadata{}
	x.Init(buf, n+offset+flatbuffers.SizeUint32)
	return x
}

func FinishSizePrefixedFileMetadataBuffer(builder *flatbuffers.Builder, offset flatbuffers.UOffsetT) {
	builder.FinishSizePrefixed(offset)
}

func (rcv *FileMetadata) Init(buf []byte, i flatbuffers.UOffsetT) {
	rcv._tab.Bytes = buf
	rcv._tab.Pos = i
}

func (rcv *FileMetadata) Table() flatbuffers.Table {
	return rcv._tab
}

func (rcv *FileMetadata) FileId() []byte {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(4))
	if o != 0 {
		return rcv._tab.ByteVector(o + rcv._tab.Pos)
	}
	return nil
}

func (rcv *FileMetadata) FileName() []byte {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(6))
	if o != 0 {
		return rcv._tab.ByteVector(o + rcv._tab.Pos)
	}
	return nil
}

func (rcv *FileMetadata) FileSize() uint64 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(8))
	if o != 0 {
		return rcv._tab.GetUint64(o + rcv._tab.Pos)
	}
	return 0
}

func (rcv *FileMetadata) MutateFileSize(n uint64) bool {
	return rcv._tab.MutateUint64Slot(8, n)
}

func (rcv *FileMetadata) ChunkSize() uint32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(10))
	if o != 0 {
		return rcv._tab.GetUint32(o + rcv._tab.Pos)
	}
	return 0
}

func (rcv *FileMetadata) MutateChunkSize(n uint32) bool {
	return rcv._tab.MutateUint32Slot(10, n)
}

func (rcv *FileMetadata) TotalChunks() uint32 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(12))
	if o != 0 {
		return rcv._tab.GetUint32(o + rcv._tab.Pos)
	}
	return 0
}

func (rcv *FileMetadata) MutateTotalChunks(n uint32) bool {
	return rcv._tab.MutateUint32Slot(12, n)
}

func (rcv *FileMetadata) MimeType() []byte {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(14))
	if o != 0 {
		return rcv._tab.ByteVector(o + rcv._tab.Pos)
	}
	return nil
}

func (rcv *FileMetadata) CreatedAt() int64 {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(16))
	if o != 0 {
		return rcv._tab.GetInt64(o + rcv._tab.Pos)
	}
	return 0
}

func (rcv *FileMetadata) MutateCreatedAt(n int64) bool {
	return rcv._tab.MutateInt64Slot(16, n)
}

func (rcv *FileMetadata) Hash() []byte {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(18))
	if o != 0 {
		return rcv._tab.ByteVector(o + rcv._tab.Pos)
	}
	return nil
}

func (rcv *FileMetadata) ChunkHashes(j int) []byte {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(20))
	if o != 0 {
		a := rcv._tab.Vector(o)
		return rcv._tab.ByteVector(a + flatbuffers.UOffsetT(j*4))
	}
	return nil
}

func (rcv *FileMetadata) ChunkHashesLength() int {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(20))
	if o != 0 {
		return rcv._tab.VectorLen(o)
	}
	return 0
}

func (rcv *FileMetadata) Encrypted() bool {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(22))
	if o != 0 {
		return rcv._tab.GetBool(o + rcv._tab.Pos)
	}
	return false
}

func (rcv *FileMetadata) MutateEncrypted(n bool) bool {
	return rcv._tab.MutateBoolSlot(22, n)
}

func (rcv *FileMetadata) Compression() []byte {
	o := flatbuffers.UOffsetT(rcv._tab.Offset(24))
	if o != 0 {
		return rcv._tab.ByteVector(o + rcv._tab.Pos)
	}
	return nil
}

func FileMetadataStart(builder *flatbuffers.Builder) {
	builder.StartObject(11)
}
func FileMetadataAddFileId(builder *flatbuffers.Builder, fileId flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(0, flatbuffers.UOffsetT(fileId), 0)
}
func FileMetadataAddFileName(builder *flatbuffers.Builder, fileName flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(1, flatbuffers.UOffsetT(fileName), 0)
}
func FileMetadataAddFileSize(builder *flatbuffers.Builder, fileSize uint64) {
	builder.PrependUint64Slot(2, fileSize, 0)
}
func FileMetadataAddChunkSize(builder *flatbuffers.Builder, chunkSize uint32) {
	builder.PrependUint32Slot(3, chunkSize, 0)
}
func FileMetadataAddTotalChunks(builder *flatbuffers.Builder, totalChunks uint32) {
	builder.PrependUint32Slot(4, totalChunks, 0)
}
func FileMetadataAddMimeType(builder *flatbuffers.Builder, mimeType flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(5, flatbuffers.UOffsetT(mimeType), 0)
}
func FileMetadataAddCreatedAt(builder *flatbuffers.Builder, createdAt int64) {
	builder.PrependInt64Slot(6, createdAt, 0)
}
func FileMetadataAddHash(builder *flatbuffers.Builder, hash flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(7, flatbuffers.UOffsetT(hash), 0)
}
func FileMetadataAddChunkHashes(builder *flatbuffers.Builder, chunkHashes flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(8, flatbuffers.UOffsetT(chunkHashes), 0)
}
func FileMetadataStartChunkHashesVector(builder *flatbuffers.Builder, numElems int) flatbuffers.UOffsetT {
	return builder.StartVector(4, numElems, 4)
}
func FileMetadataAddEncrypted(builder *flatbuffers.Builder, encrypted bool) {
	builder.PrependBoolSlot(9, encrypted, false)
}
func FileMetadataAddCompression(builder *flatbuffers.Builder, compression flatbuffers.UOffsetT) {
	builder.PrependUOffsetTSlot(10, flatbuffers.UOffsetT(compression), 0)
}
func FileMetadataEnd(builder *flatbuffers.Builder) flatbuffers.UOffsetT {
	return builder.EndObject()
}
