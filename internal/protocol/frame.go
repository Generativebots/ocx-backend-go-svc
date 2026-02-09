// Package protocol implements the AOCS protocol frame structure and session management.
// This defines the 110-byte AOCS header per the specification.
package protocol

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

// ============================================================================
// AOCS FRAME STRUCTURE (110-byte header)
// ============================================================================

// Magic bytes for AOCS protocol identification
const (
	MagicByte1 uint8 = 0x0C // 'O' in octal representation
	MagicByte2 uint8 = 0x58 // 'X' ASCII
)

// Protocol versions
const (
	VersionMajor uint8 = 1
	VersionMinor uint8 = 0
)

// Frame types
type FrameType uint8

const (
	FrameTypeHandshake     FrameType = 0x01
	FrameTypeMessage       FrameType = 0x02
	FrameTypeResponse      FrameType = 0x03
	FrameTypeHeartbeat     FrameType = 0x04
	FrameTypeEscrowHold    FrameType = 0x10
	FrameTypeEscrowRelease FrameType = 0x11
	FrameTypeEscrowReject  FrameType = 0x12
	FrameTypeFederation    FrameType = 0x20
	FrameTypeDisconnect    FrameType = 0xFE
	FrameTypeError         FrameType = 0xFF
)

func (ft FrameType) String() string {
	switch ft {
	case FrameTypeHandshake:
		return "HANDSHAKE"
	case FrameTypeMessage:
		return "MESSAGE"
	case FrameTypeResponse:
		return "RESPONSE"
	case FrameTypeHeartbeat:
		return "HEARTBEAT"
	case FrameTypeEscrowHold:
		return "ESCROW_HOLD"
	case FrameTypeEscrowRelease:
		return "ESCROW_RELEASE"
	case FrameTypeEscrowReject:
		return "ESCROW_REJECT"
	case FrameTypeFederation:
		return "FEDERATION"
	case FrameTypeDisconnect:
		return "DISCONNECT"
	case FrameTypeError:
		return "ERROR"
	default:
		return fmt.Sprintf("UNKNOWN(0x%02X)", uint8(ft))
	}
}

// ActionClass for escrow classification
type ActionClass uint8

const (
	ActionClassA ActionClass = 0 // Reversible - Ghost-Turn
	ActionClassB ActionClass = 1 // Irreversible - Atomic-Hold
)

// FrameFlags contains per-frame options
type FrameFlags uint16

const (
	FlagCompressed  FrameFlags = 1 << 0 // Payload is compressed
	FlagEncrypted   FrameFlags = 1 << 1 // Payload is encrypted
	FlagPriority    FrameFlags = 1 << 2 // High priority message
	FlagAckRequired FrameFlags = 1 << 3 // Acknowledgment required
	FlagHITL        FrameFlags = 1 << 4 // Human-in-the-loop required
	FlagMulticast   FrameFlags = 1 << 5 // Multicast message
	FlagFederated   FrameFlags = 1 << 6 // Cross-OCX message
	FlagReplay      FrameFlags = 1 << 7 // Replay of previous message
	FlagTrace       FrameFlags = 1 << 8 // Tracing enabled
	FlagDebug       FrameFlags = 1 << 9 // Debug mode
)

// ============================================================================
// AOCS FRAME HEADER (110 bytes)
// ============================================================================

// FrameHeader is the 110-byte AOCS protocol header
type FrameHeader struct {
	// Bytes 0-1: Magic bytes
	Magic [2]uint8

	// Byte 2: Major version
	VersionMajor uint8

	// Byte 3: Minor version
	VersionMinor uint8

	// Byte 4: Frame type
	FrameType FrameType

	// Byte 5: Action class (0=A, 1=B)
	ActionClass ActionClass

	// Bytes 6-7: Flags
	Flags FrameFlags

	// Bytes 8-23: Session ID (16 bytes / 128 bits)
	SessionID [16]byte

	// Bytes 24-55: Transaction ID (32 bytes / 256 bits)
	TransactionID [32]byte

	// Bytes 56-71: Source Virtual Address (16 bytes)
	SourceAddr [16]byte

	// Bytes 72-87: Destination Virtual Address (16 bytes)
	DestAddr [16]byte

	// Bytes 88-91: Tenant ID (4 bytes / 32 bits)
	TenantID uint32

	// Bytes 92-95: Agent ID (4 bytes / 32 bits)
	AgentID uint32

	// Bytes 96-99: Timestamp (4 bytes, Unix epoch seconds)
	Timestamp uint32

	// Bytes 100-101: Sequence number
	SequenceNum uint16

	// Bytes 102-103: Payload length
	PayloadLen uint16

	// Bytes 104-107: Governance Manifest Hash (first 4 bytes of SHA-256)
	ManifestHash uint32

	// Bytes 108-109: Checksum (CRC-16)
	Checksum uint16
}

// HeaderSize is the size of the AOCS frame header
const HeaderSize = 110

// NewFrameHeader creates a new frame header with defaults
func NewFrameHeader() *FrameHeader {
	return &FrameHeader{
		Magic:        [2]uint8{MagicByte1, MagicByte2},
		VersionMajor: VersionMajor,
		VersionMinor: VersionMinor,
		Timestamp:    uint32(time.Now().Unix()),
	}
}

// Validate checks if the header is valid
func (h *FrameHeader) Validate() error {
	if h.Magic[0] != MagicByte1 || h.Magic[1] != MagicByte2 {
		return fmt.Errorf("invalid magic bytes: %02X %02X", h.Magic[0], h.Magic[1])
	}

	if h.VersionMajor != VersionMajor {
		return fmt.Errorf("unsupported major version: %d (expected %d)", h.VersionMajor, VersionMajor)
	}

	return nil
}

// Marshal serializes the header to bytes
func (h *FrameHeader) Marshal() ([]byte, error) {
	buf := new(bytes.Buffer)

	// Write each field in order
	if err := binary.Write(buf, binary.BigEndian, h.Magic); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, h.VersionMajor); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, h.VersionMinor); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, h.FrameType); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, h.ActionClass); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, h.Flags); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, h.SessionID); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, h.TransactionID); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, h.SourceAddr); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, h.DestAddr); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, h.TenantID); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, h.AgentID); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, h.Timestamp); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, h.SequenceNum); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, h.PayloadLen); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, h.ManifestHash); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, h.Checksum); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// Unmarshal deserializes the header from bytes
func (h *FrameHeader) Unmarshal(data []byte) error {
	if len(data) < HeaderSize {
		return fmt.Errorf("data too short: %d bytes (need %d)", len(data), HeaderSize)
	}

	buf := bytes.NewReader(data)

	if err := binary.Read(buf, binary.BigEndian, &h.Magic); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &h.VersionMajor); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &h.VersionMinor); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &h.FrameType); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &h.ActionClass); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &h.Flags); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &h.SessionID); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &h.TransactionID); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &h.SourceAddr); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &h.DestAddr); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &h.TenantID); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &h.AgentID); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &h.Timestamp); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &h.SequenceNum); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &h.PayloadLen); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &h.ManifestHash); err != nil {
		return err
	}
	if err := binary.Read(buf, binary.BigEndian, &h.Checksum); err != nil {
		return err
	}

	return nil
}

// ============================================================================
// AOCS FRAME (Header + Payload)
// ============================================================================

// Frame represents a complete AOCS protocol frame
type Frame struct {
	Header  *FrameHeader
	Payload []byte
}

// NewFrame creates a new frame with the given payload
func NewFrame(frameType FrameType, payload []byte) *Frame {
	header := NewFrameHeader()
	header.FrameType = frameType
	header.PayloadLen = uint16(len(payload))

	return &Frame{
		Header:  header,
		Payload: payload,
	}
}

// Marshal serializes the complete frame
func (f *Frame) Marshal() ([]byte, error) {
	headerBytes, err := f.Header.Marshal()
	if err != nil {
		return nil, err
	}

	result := make([]byte, len(headerBytes)+len(f.Payload))
	copy(result, headerBytes)
	copy(result[len(headerBytes):], f.Payload)

	return result, nil
}

// Unmarshal deserializes a complete frame
func (f *Frame) Unmarshal(data []byte) error {
	if f.Header == nil {
		f.Header = &FrameHeader{}
	}

	if err := f.Header.Unmarshal(data); err != nil {
		return err
	}

	if len(data) < HeaderSize+int(f.Header.PayloadLen) {
		return fmt.Errorf("payload too short: have %d bytes, need %d",
			len(data)-HeaderSize, f.Header.PayloadLen)
	}

	f.Payload = make([]byte, f.Header.PayloadLen)
	copy(f.Payload, data[HeaderSize:HeaderSize+int(f.Header.PayloadLen)])

	return nil
}

// ReadFrame reads a frame from an io.Reader
func ReadFrame(r io.Reader) (*Frame, error) {
	// Read header
	headerBuf := make([]byte, HeaderSize)
	if _, err := io.ReadFull(r, headerBuf); err != nil {
		return nil, err
	}

	header := &FrameHeader{}
	if err := header.Unmarshal(headerBuf); err != nil {
		return nil, err
	}

	if err := header.Validate(); err != nil {
		return nil, err
	}

	// Read payload
	payload := make([]byte, header.PayloadLen)
	if header.PayloadLen > 0 {
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, err
		}
	}

	return &Frame{
		Header:  header,
		Payload: payload,
	}, nil
}

// WriteFrame writes a frame to an io.Writer
func WriteFrame(w io.Writer, f *Frame) error {
	data, err := f.Marshal()
	if err != nil {
		return err
	}

	_, err = w.Write(data)
	return err
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

// CalculateCRC16 calculates CRC-16 checksum for the header (excluding checksum field)
func CalculateCRC16(data []byte) uint16 {
	var crc uint16 = 0xFFFF

	for _, b := range data {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0xA001
			} else {
				crc >>= 1
			}
		}
	}

	return crc
}

// SetSessionID sets the session ID in the header
func (h *FrameHeader) SetSessionID(id []byte) {
	copy(h.SessionID[:], id)
}

// SetTransactionID sets the transaction ID in the header
func (h *FrameHeader) SetTransactionID(id []byte) {
	copy(h.TransactionID[:], id)
}

// SetAddresses sets source and destination addresses
func (h *FrameHeader) SetAddresses(src, dst []byte) {
	copy(h.SourceAddr[:], src)
	copy(h.DestAddr[:], dst)
}

// SetFlag sets a specific flag
func (h *FrameHeader) SetFlag(flag FrameFlags) {
	h.Flags |= flag
}

// ClearFlag clears a specific flag
func (h *FrameHeader) ClearFlag(flag FrameFlags) {
	h.Flags &^= flag
}

// HasFlag checks if a flag is set
func (h *FrameHeader) HasFlag(flag FrameFlags) bool {
	return h.Flags&flag != 0
}
