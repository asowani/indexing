// Code generated by protoc-gen-go.
// source: rollback.proto
// DO NOT EDIT!

package protobuf

import proto "code.google.com/p/goprotobuf/proto"
import math "math"

// Reference imports to suppress errors if they are not otherwise used.
var _ = proto.Marshal
var _ = math.Inf

// Will be set by Index-Coordinator during kv-rollback.
type RollbackState int32

const (
	// Rollback is detected, rollback context created, failover-timestamp and
	// restart-timestamp computed, rollback context replicated with
	// coordinator replicas, RollbackStart request is made to all indexer
	// nodes.
	RollbackState_RollbackStart RollbackState = 1
	// RollbackPrepare received from all indexers, rollback context
	// replicated with coordinator replicas.
	RollbackState_RollbackPrepare RollbackState = 2
	// Projector handshake completed, gets back failover-timestamp and
	// kv-timestamp, rollback context replicated with coordinator replicas,
	// RollbackCommit request is made to all indexer nodes.
	RollbackState_RollbackCommit RollbackState = 3
	// RollbackDone received from all indexers, rollback context replicated
	// with coordinator replicas.
	RollbackState_RollbackDone RollbackState = 4
)

var RollbackState_name = map[int32]string{
	1: "RollbackStart",
	2: "RollbackPrepare",
	3: "RollbackCommit",
	4: "RollbackDone",
}
var RollbackState_value = map[string]int32{
	"RollbackStart":   1,
	"RollbackPrepare": 2,
	"RollbackCommit":  3,
	"RollbackDone":    4,
}

func (x RollbackState) Enum() *RollbackState {
	p := new(RollbackState)
	*p = x
	return p
}
func (x RollbackState) String() string {
	return proto.EnumName(RollbackState_name, int32(x))
}
func (x *RollbackState) UnmarshalJSON(data []byte) error {
	value, err := proto.UnmarshalJSONEnum(RollbackState_value, data, "RollbackState")
	if err != nil {
		return err
	}
	*x = RollbackState(value)
	return nil
}

// Posted by Coordinator to its replica during the process of rollback. Error
// message will be sent back as reponse.
type UpdateRollbackContextRequest struct {
	Bucket            *string        `protobuf:"bytes,1,req,name=bucket" json:"bucket,omitempty"`
	State             *RollbackState `protobuf:"varint,2,req,name=state,enum=protobuf.RollbackState" json:"state,omitempty"`
	FailoverTimestamp *TsVbuuid      `protobuf:"bytes,3,req,name=failoverTimestamp" json:"failoverTimestamp,omitempty"`
	RestartTimestamp  *TsVbuuid      `protobuf:"bytes,4,req,name=restartTimestamp" json:"restartTimestamp,omitempty"`
	KvTimestamp       *TsVbuuid      `protobuf:"bytes,5,req,name=kvTimestamp" json:"kvTimestamp,omitempty"`
	XXX_unrecognized  []byte         `json:"-"`
}

func (m *UpdateRollbackContextRequest) Reset()         { *m = UpdateRollbackContextRequest{} }
func (m *UpdateRollbackContextRequest) String() string { return proto.CompactTextString(m) }
func (*UpdateRollbackContextRequest) ProtoMessage()    {}

func (m *UpdateRollbackContextRequest) GetBucket() string {
	if m != nil && m.Bucket != nil {
		return *m.Bucket
	}
	return ""
}

func (m *UpdateRollbackContextRequest) GetState() RollbackState {
	if m != nil && m.State != nil {
		return *m.State
	}
	return RollbackState_RollbackStart
}

func (m *UpdateRollbackContextRequest) GetFailoverTimestamp() *TsVbuuid {
	if m != nil {
		return m.FailoverTimestamp
	}
	return nil
}

func (m *UpdateRollbackContextRequest) GetRestartTimestamp() *TsVbuuid {
	if m != nil {
		return m.RestartTimestamp
	}
	return nil
}

func (m *UpdateRollbackContextRequest) GetKvTimestamp() *TsVbuuid {
	if m != nil {
		return m.KvTimestamp
	}
	return nil
}

func init() {
	proto.RegisterEnum("protobuf.RollbackState", RollbackState_name, RollbackState_value)
}
