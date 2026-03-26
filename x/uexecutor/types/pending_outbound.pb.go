// This file contains the Go types for the PendingOutbound protobuf messages.
// These types will be replaced by protoc-gen-gogo output when `make proto-gen`
// is run. They are provided here so that the rest of the codebase compiles
// before code generation is executed.

package types

import (
	fmt "fmt"
	io "io"
	math "math"
	math_bits "math/bits"

	query "github.com/cosmos/cosmos-sdk/types/query"
	proto "github.com/cosmos/gogoproto/proto"
)

// Ensure proto imports are used.
var _ = fmt.Errorf
var _ = math.Inf

// PendingOutboundEntry is a lightweight index entry for a pending outbound.
type PendingOutboundEntry struct {
	OutboundId     string `protobuf:"bytes,1,opt,name=outbound_id,json=outboundId,proto3" json:"outbound_id,omitempty"`
	UniversalTxId  string `protobuf:"bytes,2,opt,name=universal_tx_id,json=universalTxId,proto3" json:"universal_tx_id,omitempty"`
	CreatedAt      int64  `protobuf:"varint,3,opt,name=created_at,json=createdAt,proto3" json:"created_at,omitempty"`
}

func (m *PendingOutboundEntry) Reset()         { *m = PendingOutboundEntry{} }
func (m *PendingOutboundEntry) String() string { return proto.CompactTextString(m) }
func (*PendingOutboundEntry) ProtoMessage()    {}

func (m *PendingOutboundEntry) GetOutboundId() string {
	if m != nil {
		return m.OutboundId
	}
	return ""
}

func (m *PendingOutboundEntry) GetUniversalTxId() string {
	if m != nil {
		return m.UniversalTxId
	}
	return ""
}

func (m *PendingOutboundEntry) GetCreatedAt() int64 {
	if m != nil {
		return m.CreatedAt
	}
	return 0
}

func (m *PendingOutboundEntry) Marshal() (dAtA []byte, err error) {
	size := m.Size()
	dAtA = make([]byte, size)
	n, err := m.MarshalToSizedBuffer(dAtA[:size])
	if err != nil {
		return nil, err
	}
	return dAtA[:n], nil
}

func (m *PendingOutboundEntry) MarshalTo(dAtA []byte) (int, error) {
	size := m.Size()
	return m.MarshalToSizedBuffer(dAtA[:size])
}

func (m *PendingOutboundEntry) MarshalToSizedBuffer(dAtA []byte) (int, error) {
	i := len(dAtA)
	_ = i
	if m.CreatedAt != 0 {
		i = encodeVarintPendingOutbound(dAtA, i, uint64(m.CreatedAt))
		i--
		dAtA[i] = 0x18
	}
	if len(m.UniversalTxId) > 0 {
		i -= len(m.UniversalTxId)
		copy(dAtA[i:], m.UniversalTxId)
		i = encodeVarintPendingOutbound(dAtA, i, uint64(len(m.UniversalTxId)))
		i--
		dAtA[i] = 0x12
	}
	if len(m.OutboundId) > 0 {
		i -= len(m.OutboundId)
		copy(dAtA[i:], m.OutboundId)
		i = encodeVarintPendingOutbound(dAtA, i, uint64(len(m.OutboundId)))
		i--
		dAtA[i] = 0xa
	}
	return len(dAtA) - i, nil
}

func encodeVarintPendingOutbound(dAtA []byte, offset int, v uint64) int {
	offset--
	dAtA[offset] = uint8(v)
	for v >= 1<<7 {
		dAtA[offset] = uint8(v&0x7f | 0x80)
		v >>= 7
		offset--
	}
	dAtA[offset] = uint8(v)
	return offset
}

func (m *PendingOutboundEntry) Size() (n int) {
	if m == nil {
		return 0
	}
	var l int
	_ = l
	l = len(m.OutboundId)
	if l > 0 {
		n += 1 + l + sovPendingOutbound(uint64(l))
	}
	l = len(m.UniversalTxId)
	if l > 0 {
		n += 1 + l + sovPendingOutbound(uint64(l))
	}
	if m.CreatedAt != 0 {
		n += 1 + sovPendingOutbound(uint64(m.CreatedAt))
	}
	return n
}

func sovPendingOutbound(x uint64) (n int) {
	return (math_bits.Len64(x|1) + 6) / 7
}

func (m *PendingOutboundEntry) Unmarshal(dAtA []byte) error {
	l := len(dAtA)
	iNdEx := 0
	for iNdEx < l {
		preIndex := iNdEx
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return fmt.Errorf("proto: integer overflow")
			}
			if iNdEx >= l {
				return io.ErrUnexpectedEOF
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= uint64(b&0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		fieldNum := int32(wire >> 3)
		wireType := int(wire & 0x7)
		switch fieldNum {
		case 1: // outbound_id
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field OutboundId", wireType)
			}
			var stringLen uint64
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return fmt.Errorf("proto: integer overflow")
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				stringLen |= uint64(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			intStringLen := int(stringLen)
			if intStringLen < 0 {
				return fmt.Errorf("proto: negative length found during unmarshaling")
			}
			postIndex := iNdEx + intStringLen
			if postIndex < 0 {
				return fmt.Errorf("proto: negative length found during unmarshaling")
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.OutboundId = string(dAtA[iNdEx:postIndex])
			iNdEx = postIndex
		case 2: // universal_tx_id
			if wireType != 2 {
				return fmt.Errorf("proto: wrong wireType = %d for field UniversalTxId", wireType)
			}
			var stringLen uint64
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return fmt.Errorf("proto: integer overflow")
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				stringLen |= uint64(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			intStringLen := int(stringLen)
			if intStringLen < 0 {
				return fmt.Errorf("proto: negative length found during unmarshaling")
			}
			postIndex := iNdEx + intStringLen
			if postIndex < 0 {
				return fmt.Errorf("proto: negative length found during unmarshaling")
			}
			if postIndex > l {
				return io.ErrUnexpectedEOF
			}
			m.UniversalTxId = string(dAtA[iNdEx:postIndex])
			iNdEx = postIndex
		case 3: // created_at
			if wireType != 0 {
				return fmt.Errorf("proto: wrong wireType = %d for field CreatedAt", wireType)
			}
			m.CreatedAt = 0
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return fmt.Errorf("proto: integer overflow")
				}
				if iNdEx >= l {
					return io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				m.CreatedAt |= int64(b&0x7F) << shift
				if b < 0x80 {
					break
				}
			}
		default:
			iNdEx = preIndex
			skippy, err := skipPendingOutbound(dAtA[iNdEx:])
			if err != nil {
				return err
			}
			if (skippy < 0) || (iNdEx+skippy) < 0 {
				return fmt.Errorf("proto: negative length found during unmarshaling")
			}
			if iNdEx+skippy > l {
				return io.ErrUnexpectedEOF
			}
			iNdEx += skippy
		}
	}

	if iNdEx > l {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func skipPendingOutbound(dAtA []byte) (n int, err error) {
	l := len(dAtA)
	iNdEx := 0
	depth := 0
	for iNdEx < l {
		var wire uint64
		for shift := uint(0); ; shift += 7 {
			if shift >= 64 {
				return 0, fmt.Errorf("proto: integer overflow")
			}
			if iNdEx >= l {
				return 0, io.ErrUnexpectedEOF
			}
			b := dAtA[iNdEx]
			iNdEx++
			wire |= (uint64(b) & 0x7F) << shift
			if b < 0x80 {
				break
			}
		}
		wireType := int(wire & 0x7)
		switch wireType {
		case 0:
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return 0, fmt.Errorf("proto: integer overflow")
				}
				if iNdEx >= l {
					return 0, io.ErrUnexpectedEOF
				}
				iNdEx++
				if dAtA[iNdEx-1] < 0x80 {
					break
				}
			}
		case 1:
			iNdEx += 8
		case 2:
			var length int
			for shift := uint(0); ; shift += 7 {
				if shift >= 64 {
					return 0, fmt.Errorf("proto: integer overflow")
				}
				if iNdEx >= l {
					return 0, io.ErrUnexpectedEOF
				}
				b := dAtA[iNdEx]
				iNdEx++
				length |= (int(b) & 0x7F) << shift
				if b < 0x80 {
					break
				}
			}
			if length < 0 {
				return 0, fmt.Errorf("proto: negative length found during unmarshaling")
			}
			iNdEx += length
		case 3:
			depth++
		case 4:
			if depth == 0 {
				return 0, fmt.Errorf("proto: unexpected end of group")
			}
			depth--
		case 5:
			iNdEx += 4
		default:
			return 0, fmt.Errorf("proto: illegal wireType %d", wireType)
		}
		if iNdEx < 0 {
			return 0, fmt.Errorf("proto: negative length found during unmarshaling")
		}
		if depth == 0 {
			return iNdEx, nil
		}
	}
	return 0, io.ErrUnexpectedEOF
}

// Query request/response types for pending outbounds

type QueryGetPendingOutboundRequest struct {
	OutboundId string `protobuf:"bytes,1,opt,name=outbound_id,json=outboundId,proto3" json:"outbound_id,omitempty"`
}

func (m *QueryGetPendingOutboundRequest) Reset()         { *m = QueryGetPendingOutboundRequest{} }
func (m *QueryGetPendingOutboundRequest) String() string { return proto.CompactTextString(m) }
func (*QueryGetPendingOutboundRequest) ProtoMessage()    {}

func (m *QueryGetPendingOutboundRequest) GetOutboundId() string {
	if m != nil {
		return m.OutboundId
	}
	return ""
}

type QueryGetPendingOutboundResponse struct {
	Entry    *PendingOutboundEntry `protobuf:"bytes,1,opt,name=entry,proto3" json:"entry,omitempty"`
	Outbound *OutboundTx           `protobuf:"bytes,2,opt,name=outbound,proto3" json:"outbound,omitempty"`
}

func (m *QueryGetPendingOutboundResponse) Reset()         { *m = QueryGetPendingOutboundResponse{} }
func (m *QueryGetPendingOutboundResponse) String() string { return proto.CompactTextString(m) }
func (*QueryGetPendingOutboundResponse) ProtoMessage()    {}

func (m *QueryGetPendingOutboundResponse) GetEntry() *PendingOutboundEntry {
	if m != nil {
		return m.Entry
	}
	return nil
}

func (m *QueryGetPendingOutboundResponse) GetOutbound() *OutboundTx {
	if m != nil {
		return m.Outbound
	}
	return nil
}

type QueryAllPendingOutboundsRequest struct {
	Pagination *query.PageRequest `protobuf:"bytes,1,opt,name=pagination,proto3" json:"pagination,omitempty"`
}

func (m *QueryAllPendingOutboundsRequest) Reset()         { *m = QueryAllPendingOutboundsRequest{} }
func (m *QueryAllPendingOutboundsRequest) String() string { return proto.CompactTextString(m) }
func (*QueryAllPendingOutboundsRequest) ProtoMessage()    {}

func (m *QueryAllPendingOutboundsRequest) GetPagination() *query.PageRequest {
	if m != nil {
		return m.Pagination
	}
	return nil
}

type QueryAllPendingOutboundsResponse struct {
	Entries    []*PendingOutboundEntry `protobuf:"bytes,1,rep,name=entries,proto3" json:"entries,omitempty"`
	Outbounds  []*OutboundTx           `protobuf:"bytes,2,rep,name=outbounds,proto3" json:"outbounds,omitempty"`
	Pagination *query.PageResponse     `protobuf:"bytes,3,opt,name=pagination,proto3" json:"pagination,omitempty"`
}

func (m *QueryAllPendingOutboundsResponse) Reset()         { *m = QueryAllPendingOutboundsResponse{} }
func (m *QueryAllPendingOutboundsResponse) String() string { return proto.CompactTextString(m) }
func (*QueryAllPendingOutboundsResponse) ProtoMessage()    {}

func (m *QueryAllPendingOutboundsResponse) GetOutbounds() []*OutboundTx {
	if m != nil {
		return m.Outbounds
	}
	return nil
}

func (m *QueryAllPendingOutboundsResponse) GetEntries() []*PendingOutboundEntry {
	if m != nil {
		return m.Entries
	}
	return nil
}

func (m *QueryAllPendingOutboundsResponse) GetPagination() *query.PageResponse {
	if m != nil {
		return m.Pagination
	}
	return nil
}

func init() {
	proto.RegisterType((*PendingOutboundEntry)(nil), "uexecutor.v1.PendingOutboundEntry")
	proto.RegisterType((*QueryGetPendingOutboundRequest)(nil), "uexecutor.v1.QueryGetPendingOutboundRequest")
	proto.RegisterType((*QueryGetPendingOutboundResponse)(nil), "uexecutor.v1.QueryGetPendingOutboundResponse")
	proto.RegisterType((*QueryAllPendingOutboundsRequest)(nil), "uexecutor.v1.QueryAllPendingOutboundsRequest")
	proto.RegisterType((*QueryAllPendingOutboundsResponse)(nil), "uexecutor.v1.QueryAllPendingOutboundsResponse")
}
