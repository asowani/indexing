// Code generated by protoc-gen-go.
// source: query.proto
// DO NOT EDIT!

/*
Package protobuf is a generated protocol buffer package.

It is generated from these files:
	query.proto

It has these top-level messages:
	Error
	TsConsistency
	QueryPayload
	HeloRequest
	HeloResponse
	StatisticsRequest
	StatisticsResponse
	ScanRequest
	ScanAllRequest
	EndStreamRequest
	ResponseStream
	StreamEndResponse
	CountRequest
	CountResponse
	Span
	Range
	CompositeElementFilter
	Scan
	IndexProjection
	IndexEntry
	IndexStatistics
*/
package protobuf

import proto "github.com/golang/protobuf/proto"
import math "math"

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = math.Inf

// Error message can be sent back as response or
// encapsulated in response packets.
type Error struct {
	Error            *string `protobuf:"bytes,1,req,name=error" json:"error,omitempty"`
	XXX_unrecognized []byte  `json:"-"`
}

func (m *Error) Reset()         { *m = Error{} }
func (m *Error) String() string { return proto.CompactTextString(m) }
func (*Error) ProtoMessage()    {}

func (m *Error) GetError() string {
	if m != nil && m.Error != nil {
		return *m.Error
	}
	return ""
}

// consistency timestamp specifying a subset of vbucket.
// AnyConsistency, this message is typically ignored.
// SessionConsistency, {vbnos, seqnos, crc64} are to be considered.
// QueryConsistency, {vbnos, seqnos, vbuuids} are to be considered.
type TsConsistency struct {
	Vbnos            []uint32 `protobuf:"varint,1,rep,name=vbnos" json:"vbnos,omitempty"`
	Seqnos           []uint64 `protobuf:"varint,2,rep,name=seqnos" json:"seqnos,omitempty"`
	Vbuuids          []uint64 `protobuf:"varint,3,rep,name=vbuuids" json:"vbuuids,omitempty"`
	Crc64            *uint64  `protobuf:"varint,4,opt,name=crc64" json:"crc64,omitempty"`
	XXX_unrecognized []byte   `json:"-"`
}

func (m *TsConsistency) Reset()         { *m = TsConsistency{} }
func (m *TsConsistency) String() string { return proto.CompactTextString(m) }
func (*TsConsistency) ProtoMessage()    {}

func (m *TsConsistency) GetVbnos() []uint32 {
	if m != nil {
		return m.Vbnos
	}
	return nil
}

func (m *TsConsistency) GetSeqnos() []uint64 {
	if m != nil {
		return m.Seqnos
	}
	return nil
}

func (m *TsConsistency) GetVbuuids() []uint64 {
	if m != nil {
		return m.Vbuuids
	}
	return nil
}

func (m *TsConsistency) GetCrc64() uint64 {
	if m != nil && m.Crc64 != nil {
		return *m.Crc64
	}
	return 0
}

// Request can be one of the optional field.
type QueryPayload struct {
	Version           *uint32             `protobuf:"varint,1,req,name=version" json:"version,omitempty"`
	StatisticsRequest *StatisticsRequest  `protobuf:"bytes,2,opt,name=statisticsRequest" json:"statisticsRequest,omitempty"`
	Statistics        *StatisticsResponse `protobuf:"bytes,3,opt,name=statistics" json:"statistics,omitempty"`
	ScanRequest       *ScanRequest        `protobuf:"bytes,4,opt,name=scanRequest" json:"scanRequest,omitempty"`
	ScanAllRequest    *ScanAllRequest     `protobuf:"bytes,5,opt,name=scanAllRequest" json:"scanAllRequest,omitempty"`
	Stream            *ResponseStream     `protobuf:"bytes,6,opt,name=stream" json:"stream,omitempty"`
	CountRequest      *CountRequest       `protobuf:"bytes,7,opt,name=countRequest" json:"countRequest,omitempty"`
	CountResponse     *CountResponse      `protobuf:"bytes,8,opt,name=countResponse" json:"countResponse,omitempty"`
	EndStream         *EndStreamRequest   `protobuf:"bytes,9,opt,name=endStream" json:"endStream,omitempty"`
	StreamEnd         *StreamEndResponse  `protobuf:"bytes,10,opt,name=streamEnd" json:"streamEnd,omitempty"`
	HeloRequest       *HeloRequest        `protobuf:"bytes,11,opt,name=heloRequest" json:"heloRequest,omitempty"`
	HeloResponse      *HeloResponse       `protobuf:"bytes,12,opt,name=heloResponse" json:"heloResponse,omitempty"`
	XXX_unrecognized  []byte              `json:"-"`
}

func (m *QueryPayload) Reset()         { *m = QueryPayload{} }
func (m *QueryPayload) String() string { return proto.CompactTextString(m) }
func (*QueryPayload) ProtoMessage()    {}

func (m *QueryPayload) GetVersion() uint32 {
	if m != nil && m.Version != nil {
		return *m.Version
	}
	return 0
}

func (m *QueryPayload) GetStatisticsRequest() *StatisticsRequest {
	if m != nil {
		return m.StatisticsRequest
	}
	return nil
}

func (m *QueryPayload) GetStatistics() *StatisticsResponse {
	if m != nil {
		return m.Statistics
	}
	return nil
}

func (m *QueryPayload) GetScanRequest() *ScanRequest {
	if m != nil {
		return m.ScanRequest
	}
	return nil
}

func (m *QueryPayload) GetScanAllRequest() *ScanAllRequest {
	if m != nil {
		return m.ScanAllRequest
	}
	return nil
}

func (m *QueryPayload) GetStream() *ResponseStream {
	if m != nil {
		return m.Stream
	}
	return nil
}

func (m *QueryPayload) GetCountRequest() *CountRequest {
	if m != nil {
		return m.CountRequest
	}
	return nil
}

func (m *QueryPayload) GetCountResponse() *CountResponse {
	if m != nil {
		return m.CountResponse
	}
	return nil
}

func (m *QueryPayload) GetEndStream() *EndStreamRequest {
	if m != nil {
		return m.EndStream
	}
	return nil
}

func (m *QueryPayload) GetStreamEnd() *StreamEndResponse {
	if m != nil {
		return m.StreamEnd
	}
	return nil
}

func (m *QueryPayload) GetHeloRequest() *HeloRequest {
	if m != nil {
		return m.HeloRequest
	}
	return nil
}

func (m *QueryPayload) GetHeloResponse() *HeloResponse {
	if m != nil {
		return m.HeloResponse
	}
	return nil
}

// Get current server version/capabilities
type HeloRequest struct {
	Version          *uint32 `protobuf:"varint,1,req,name=version" json:"version,omitempty"`
	XXX_unrecognized []byte  `json:"-"`
}

func (m *HeloRequest) Reset()         { *m = HeloRequest{} }
func (m *HeloRequest) String() string { return proto.CompactTextString(m) }
func (*HeloRequest) ProtoMessage()    {}

func (m *HeloRequest) GetVersion() uint32 {
	if m != nil && m.Version != nil {
		return *m.Version
	}
	return 0
}

type HeloResponse struct {
	Version          *uint32 `protobuf:"varint,1,req,name=version" json:"version,omitempty"`
	XXX_unrecognized []byte  `json:"-"`
}

func (m *HeloResponse) Reset()         { *m = HeloResponse{} }
func (m *HeloResponse) String() string { return proto.CompactTextString(m) }
func (*HeloResponse) ProtoMessage()    {}

func (m *HeloResponse) GetVersion() uint32 {
	if m != nil && m.Version != nil {
		return *m.Version
	}
	return 0
}

// Get Index statistics. StatisticsResponse is returned back from indexer.
type StatisticsRequest struct {
	DefnID           *uint64 `protobuf:"varint,1,req,name=defnID" json:"defnID,omitempty"`
	Span             *Span   `protobuf:"bytes,2,req,name=span" json:"span,omitempty"`
	RequestId        *string `protobuf:"bytes,3,opt,name=requestId" json:"requestId,omitempty"`
	XXX_unrecognized []byte  `json:"-"`
}

func (m *StatisticsRequest) Reset()         { *m = StatisticsRequest{} }
func (m *StatisticsRequest) String() string { return proto.CompactTextString(m) }
func (*StatisticsRequest) ProtoMessage()    {}

func (m *StatisticsRequest) GetDefnID() uint64 {
	if m != nil && m.DefnID != nil {
		return *m.DefnID
	}
	return 0
}

func (m *StatisticsRequest) GetSpan() *Span {
	if m != nil {
		return m.Span
	}
	return nil
}

func (m *StatisticsRequest) GetRequestId() string {
	if m != nil && m.RequestId != nil {
		return *m.RequestId
	}
	return ""
}

type StatisticsResponse struct {
	Stats            *IndexStatistics `protobuf:"bytes,1,req,name=stats" json:"stats,omitempty"`
	Err              *Error           `protobuf:"bytes,2,opt,name=err" json:"err,omitempty"`
	XXX_unrecognized []byte           `json:"-"`
}

func (m *StatisticsResponse) Reset()         { *m = StatisticsResponse{} }
func (m *StatisticsResponse) String() string { return proto.CompactTextString(m) }
func (*StatisticsResponse) ProtoMessage()    {}

func (m *StatisticsResponse) GetStats() *IndexStatistics {
	if m != nil {
		return m.Stats
	}
	return nil
}

func (m *StatisticsResponse) GetErr() *Error {
	if m != nil {
		return m.Err
	}
	return nil
}

// Scan request to indexer.
type ScanRequest struct {
	DefnID           *uint64          `protobuf:"varint,1,req,name=defnID" json:"defnID,omitempty"`
	Span             *Span            `protobuf:"bytes,2,req,name=span" json:"span,omitempty"`
	Distinct         *bool            `protobuf:"varint,3,req,name=distinct" json:"distinct,omitempty"`
	Limit            *int64           `protobuf:"varint,4,req,name=limit" json:"limit,omitempty"`
	Cons             *uint32          `protobuf:"varint,5,req,name=cons" json:"cons,omitempty"`
	Vector           *TsConsistency   `protobuf:"bytes,6,opt,name=vector" json:"vector,omitempty"`
	RequestId        *string          `protobuf:"bytes,7,opt,name=requestId" json:"requestId,omitempty"`
	Scans            []*Scan          `protobuf:"bytes,8,rep,name=scans" json:"scans,omitempty"`
	Indexprojection  *IndexProjection `protobuf:"bytes,9,opt,name=indexprojection" json:"indexprojection,omitempty"`
	Reverse          *bool            `protobuf:"varint,10,opt,name=reverse" json:"reverse,omitempty"`
	Offset           *int64           `protobuf:"varint,11,opt,name=offset" json:"offset,omitempty"`
	RollbackTime     *int64           `protobuf:"varint,12,opt,name=rollbackTime" json:"rollbackTime,omitempty"`
	XXX_unrecognized []byte           `json:"-"`
}

func (m *ScanRequest) Reset()         { *m = ScanRequest{} }
func (m *ScanRequest) String() string { return proto.CompactTextString(m) }
func (*ScanRequest) ProtoMessage()    {}

func (m *ScanRequest) GetDefnID() uint64 {
	if m != nil && m.DefnID != nil {
		return *m.DefnID
	}
	return 0
}

func (m *ScanRequest) GetSpan() *Span {
	if m != nil {
		return m.Span
	}
	return nil
}

func (m *ScanRequest) GetDistinct() bool {
	if m != nil && m.Distinct != nil {
		return *m.Distinct
	}
	return false
}

func (m *ScanRequest) GetLimit() int64 {
	if m != nil && m.Limit != nil {
		return *m.Limit
	}
	return 0
}

func (m *ScanRequest) GetCons() uint32 {
	if m != nil && m.Cons != nil {
		return *m.Cons
	}
	return 0
}

func (m *ScanRequest) GetVector() *TsConsistency {
	if m != nil {
		return m.Vector
	}
	return nil
}

func (m *ScanRequest) GetRequestId() string {
	if m != nil && m.RequestId != nil {
		return *m.RequestId
	}
	return ""
}

func (m *ScanRequest) GetScans() []*Scan {
	if m != nil {
		return m.Scans
	}
	return nil
}

func (m *ScanRequest) GetIndexprojection() *IndexProjection {
	if m != nil {
		return m.Indexprojection
	}
	return nil
}

func (m *ScanRequest) GetReverse() bool {
	if m != nil && m.Reverse != nil {
		return *m.Reverse
	}
	return false
}

func (m *ScanRequest) GetOffset() int64 {
	if m != nil && m.Offset != nil {
		return *m.Offset
	}
	return 0
}

func (m *ScanRequest) GetRollbackTime() int64 {
	if m != nil && m.RollbackTime != nil {
		return *m.RollbackTime
	}
	return 0
}

// Full table scan request from indexer.
type ScanAllRequest struct {
	DefnID           *uint64        `protobuf:"varint,1,req,name=defnID" json:"defnID,omitempty"`
	Limit            *int64         `protobuf:"varint,2,req,name=limit" json:"limit,omitempty"`
	Cons             *uint32        `protobuf:"varint,3,req,name=cons" json:"cons,omitempty"`
	Vector           *TsConsistency `protobuf:"bytes,4,opt,name=vector" json:"vector,omitempty"`
	RequestId        *string        `protobuf:"bytes,5,opt,name=requestId" json:"requestId,omitempty"`
	RollbackTime     *int64         `protobuf:"varint,6,opt,name=rollbackTime" json:"rollbackTime,omitempty"`
	XXX_unrecognized []byte         `json:"-"`
}

func (m *ScanAllRequest) Reset()         { *m = ScanAllRequest{} }
func (m *ScanAllRequest) String() string { return proto.CompactTextString(m) }
func (*ScanAllRequest) ProtoMessage()    {}

func (m *ScanAllRequest) GetDefnID() uint64 {
	if m != nil && m.DefnID != nil {
		return *m.DefnID
	}
	return 0
}

func (m *ScanAllRequest) GetLimit() int64 {
	if m != nil && m.Limit != nil {
		return *m.Limit
	}
	return 0
}

func (m *ScanAllRequest) GetCons() uint32 {
	if m != nil && m.Cons != nil {
		return *m.Cons
	}
	return 0
}

func (m *ScanAllRequest) GetVector() *TsConsistency {
	if m != nil {
		return m.Vector
	}
	return nil
}

func (m *ScanAllRequest) GetRequestId() string {
	if m != nil && m.RequestId != nil {
		return *m.RequestId
	}
	return ""
}

func (m *ScanAllRequest) GetRollbackTime() int64 {
	if m != nil && m.RollbackTime != nil {
		return *m.RollbackTime
	}
	return 0
}

// Request by client to stop streaming the query results.
type EndStreamRequest struct {
	XXX_unrecognized []byte `json:"-"`
}

func (m *EndStreamRequest) Reset()         { *m = EndStreamRequest{} }
func (m *EndStreamRequest) String() string { return proto.CompactTextString(m) }
func (*EndStreamRequest) ProtoMessage()    {}

type ResponseStream struct {
	IndexEntries     []*IndexEntry `protobuf:"bytes,1,rep,name=indexEntries" json:"indexEntries,omitempty"`
	Err              *Error        `protobuf:"bytes,2,opt,name=err" json:"err,omitempty"`
	XXX_unrecognized []byte        `json:"-"`
}

func (m *ResponseStream) Reset()         { *m = ResponseStream{} }
func (m *ResponseStream) String() string { return proto.CompactTextString(m) }
func (*ResponseStream) ProtoMessage()    {}

func (m *ResponseStream) GetIndexEntries() []*IndexEntry {
	if m != nil {
		return m.IndexEntries
	}
	return nil
}

func (m *ResponseStream) GetErr() *Error {
	if m != nil {
		return m.Err
	}
	return nil
}

// Last response packet sent by server to end query results.
type StreamEndResponse struct {
	Err              *Error `protobuf:"bytes,1,opt,name=err" json:"err,omitempty"`
	XXX_unrecognized []byte `json:"-"`
}

func (m *StreamEndResponse) Reset()         { *m = StreamEndResponse{} }
func (m *StreamEndResponse) String() string { return proto.CompactTextString(m) }
func (*StreamEndResponse) ProtoMessage()    {}

func (m *StreamEndResponse) GetErr() *Error {
	if m != nil {
		return m.Err
	}
	return nil
}

// Count request to indexer.
type CountRequest struct {
	DefnID           *uint64        `protobuf:"varint,1,req,name=defnID" json:"defnID,omitempty"`
	Span             *Span          `protobuf:"bytes,2,req,name=span" json:"span,omitempty"`
	Cons             *uint32        `protobuf:"varint,3,req,name=cons" json:"cons,omitempty"`
	Vector           *TsConsistency `protobuf:"bytes,4,opt,name=vector" json:"vector,omitempty"`
	RequestId        *string        `protobuf:"bytes,5,opt,name=requestId" json:"requestId,omitempty"`
	Distinct         *bool          `protobuf:"varint,6,opt,name=distinct" json:"distinct,omitempty"`
	Scans            []*Scan        `protobuf:"bytes,7,rep,name=scans" json:"scans,omitempty"`
	RollbackTime     *int64         `protobuf:"varint,8,opt,name=rollbackTime" json:"rollbackTime,omitempty"`
	XXX_unrecognized []byte         `json:"-"`
}

func (m *CountRequest) Reset()         { *m = CountRequest{} }
func (m *CountRequest) String() string { return proto.CompactTextString(m) }
func (*CountRequest) ProtoMessage()    {}

func (m *CountRequest) GetDefnID() uint64 {
	if m != nil && m.DefnID != nil {
		return *m.DefnID
	}
	return 0
}

func (m *CountRequest) GetSpan() *Span {
	if m != nil {
		return m.Span
	}
	return nil
}

func (m *CountRequest) GetCons() uint32 {
	if m != nil && m.Cons != nil {
		return *m.Cons
	}
	return 0
}

func (m *CountRequest) GetVector() *TsConsistency {
	if m != nil {
		return m.Vector
	}
	return nil
}

func (m *CountRequest) GetRequestId() string {
	if m != nil && m.RequestId != nil {
		return *m.RequestId
	}
	return ""
}

func (m *CountRequest) GetDistinct() bool {
	if m != nil && m.Distinct != nil {
		return *m.Distinct
	}
	return false
}

func (m *CountRequest) GetScans() []*Scan {
	if m != nil {
		return m.Scans
	}
	return nil
}

func (m *CountRequest) GetRollbackTime() int64 {
	if m != nil && m.RollbackTime != nil {
		return *m.RollbackTime
	}
	return 0
}

// total number of entries in index.
type CountResponse struct {
	Count            *int64 `protobuf:"varint,1,req,name=count" json:"count,omitempty"`
	Err              *Error `protobuf:"bytes,2,opt,name=err" json:"err,omitempty"`
	XXX_unrecognized []byte `json:"-"`
}

func (m *CountResponse) Reset()         { *m = CountResponse{} }
func (m *CountResponse) String() string { return proto.CompactTextString(m) }
func (*CountResponse) ProtoMessage()    {}

func (m *CountResponse) GetCount() int64 {
	if m != nil && m.Count != nil {
		return *m.Count
	}
	return 0
}

func (m *CountResponse) GetErr() *Error {
	if m != nil {
		return m.Err
	}
	return nil
}

type Span struct {
	Range            *Range   `protobuf:"bytes,1,opt,name=range" json:"range,omitempty"`
	Equals           [][]byte `protobuf:"bytes,2,rep,name=equals" json:"equals,omitempty"`
	XXX_unrecognized []byte   `json:"-"`
}

func (m *Span) Reset()         { *m = Span{} }
func (m *Span) String() string { return proto.CompactTextString(m) }
func (*Span) ProtoMessage()    {}

func (m *Span) GetRange() *Range {
	if m != nil {
		return m.Range
	}
	return nil
}

func (m *Span) GetEquals() [][]byte {
	if m != nil {
		return m.Equals
	}
	return nil
}

type Range struct {
	Low              []byte  `protobuf:"bytes,1,opt,name=low" json:"low,omitempty"`
	High             []byte  `protobuf:"bytes,2,opt,name=high" json:"high,omitempty"`
	Inclusion        *uint32 `protobuf:"varint,3,req,name=inclusion" json:"inclusion,omitempty"`
	XXX_unrecognized []byte  `json:"-"`
}

func (m *Range) Reset()         { *m = Range{} }
func (m *Range) String() string { return proto.CompactTextString(m) }
func (*Range) ProtoMessage()    {}

func (m *Range) GetLow() []byte {
	if m != nil {
		return m.Low
	}
	return nil
}

func (m *Range) GetHigh() []byte {
	if m != nil {
		return m.High
	}
	return nil
}

func (m *Range) GetInclusion() uint32 {
	if m != nil && m.Inclusion != nil {
		return *m.Inclusion
	}
	return 0
}

type CompositeElementFilter struct {
	Low              []byte  `protobuf:"bytes,1,opt,name=low" json:"low,omitempty"`
	High             []byte  `protobuf:"bytes,2,opt,name=high" json:"high,omitempty"`
	Inclusion        *uint32 `protobuf:"varint,3,req,name=inclusion" json:"inclusion,omitempty"`
	XXX_unrecognized []byte  `json:"-"`
}

func (m *CompositeElementFilter) Reset()         { *m = CompositeElementFilter{} }
func (m *CompositeElementFilter) String() string { return proto.CompactTextString(m) }
func (*CompositeElementFilter) ProtoMessage()    {}

func (m *CompositeElementFilter) GetLow() []byte {
	if m != nil {
		return m.Low
	}
	return nil
}

func (m *CompositeElementFilter) GetHigh() []byte {
	if m != nil {
		return m.High
	}
	return nil
}

func (m *CompositeElementFilter) GetInclusion() uint32 {
	if m != nil && m.Inclusion != nil {
		return *m.Inclusion
	}
	return 0
}

type Scan struct {
	Filters          []*CompositeElementFilter `protobuf:"bytes,1,rep,name=filters" json:"filters,omitempty"`
	Equals           [][]byte                  `protobuf:"bytes,2,rep,name=equals" json:"equals,omitempty"`
	XXX_unrecognized []byte                    `json:"-"`
}

func (m *Scan) Reset()         { *m = Scan{} }
func (m *Scan) String() string { return proto.CompactTextString(m) }
func (*Scan) ProtoMessage()    {}

func (m *Scan) GetFilters() []*CompositeElementFilter {
	if m != nil {
		return m.Filters
	}
	return nil
}

func (m *Scan) GetEquals() [][]byte {
	if m != nil {
		return m.Equals
	}
	return nil
}

type IndexProjection struct {
	EntryKeys        []int64 `protobuf:"varint,1,rep" json:"EntryKeys,omitempty"`
	PrimaryKey       *bool   `protobuf:"varint,2,opt" json:"PrimaryKey,omitempty"`
	XXX_unrecognized []byte  `json:"-"`
}

func (m *IndexProjection) Reset()         { *m = IndexProjection{} }
func (m *IndexProjection) String() string { return proto.CompactTextString(m) }
func (*IndexProjection) ProtoMessage()    {}

func (m *IndexProjection) GetEntryKeys() []int64 {
	if m != nil {
		return m.EntryKeys
	}
	return nil
}

func (m *IndexProjection) GetPrimaryKey() bool {
	if m != nil && m.PrimaryKey != nil {
		return *m.PrimaryKey
	}
	return false
}

type IndexEntry struct {
	EntryKey         []byte `protobuf:"bytes,1,opt,name=entryKey" json:"entryKey,omitempty"`
	PrimaryKey       []byte `protobuf:"bytes,2,req,name=primaryKey" json:"primaryKey,omitempty"`
	XXX_unrecognized []byte `json:"-"`
}

func (m *IndexEntry) Reset()         { *m = IndexEntry{} }
func (m *IndexEntry) String() string { return proto.CompactTextString(m) }
func (*IndexEntry) ProtoMessage()    {}

func (m *IndexEntry) GetEntryKey() []byte {
	if m != nil {
		return m.EntryKey
	}
	return nil
}

func (m *IndexEntry) GetPrimaryKey() []byte {
	if m != nil {
		return m.PrimaryKey
	}
	return nil
}

// Statistics of a given index.
type IndexStatistics struct {
	KeysCount        *uint64 `protobuf:"varint,1,req,name=keysCount" json:"keysCount,omitempty"`
	UniqueKeysCount  *uint64 `protobuf:"varint,2,req,name=uniqueKeysCount" json:"uniqueKeysCount,omitempty"`
	KeyMin           []byte  `protobuf:"bytes,3,req,name=keyMin" json:"keyMin,omitempty"`
	KeyMax           []byte  `protobuf:"bytes,4,req,name=keyMax" json:"keyMax,omitempty"`
	XXX_unrecognized []byte  `json:"-"`
}

func (m *IndexStatistics) Reset()         { *m = IndexStatistics{} }
func (m *IndexStatistics) String() string { return proto.CompactTextString(m) }
func (*IndexStatistics) ProtoMessage()    {}

func (m *IndexStatistics) GetKeysCount() uint64 {
	if m != nil && m.KeysCount != nil {
		return *m.KeysCount
	}
	return 0
}

func (m *IndexStatistics) GetUniqueKeysCount() uint64 {
	if m != nil && m.UniqueKeysCount != nil {
		return *m.UniqueKeysCount
	}
	return 0
}

func (m *IndexStatistics) GetKeyMin() []byte {
	if m != nil {
		return m.KeyMin
	}
	return nil
}

func (m *IndexStatistics) GetKeyMax() []byte {
	if m != nil {
		return m.KeyMax
	}
	return nil
}

func init() {
}
