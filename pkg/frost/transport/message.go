package transport

import (
	"bytes"
	"encoding/binary"
	"errors"
	"google.golang.org/protobuf/encoding/protowire"
)

const maxEnvelope = 8448
const maxPacket = maxEnvelope + 512
const packetType = "frost/transport/v1"

var errPacket = errors.New("invalid FROST transport packet")

type packet struct {
	Version           uint64
	Domain, Attempt   []byte
	Sender, Recipient uint64
	Kind              string
	Body              []byte
}

func (*packet) Type() string { return packetType }

// A small explicit codec keeps the acceptance rule (no unknown fields or
// alternate encodings) visible. Field numbers match message.proto.
func (p *packet) Marshal() ([]byte, error) {
	var b []byte
	for i, v := range []uint64{p.Version} {
		b = protowire.AppendTag(b, protowire.Number(i+1), protowire.VarintType)
		b = protowire.AppendVarint(b, v)
	}
	b = protowire.AppendTag(b, 2, protowire.BytesType)
	b = protowire.AppendBytes(b, p.Domain)
	b = protowire.AppendTag(b, 3, protowire.BytesType)
	b = protowire.AppendBytes(b, p.Attempt)
	b = protowire.AppendTag(b, 4, protowire.VarintType)
	b = protowire.AppendVarint(b, p.Sender)
	b = protowire.AppendTag(b, 5, protowire.VarintType)
	b = protowire.AppendVarint(b, p.Recipient)
	b = protowire.AppendTag(b, 6, protowire.BytesType)
	b = protowire.AppendString(b, p.Kind)
	b = protowire.AppendTag(b, 7, protowire.BytesType)
	b = protowire.AppendBytes(b, p.Body)
	if len(b) > maxPacket {
		return nil, errPacket
	}
	return b, nil
}
func (p *packet) Unmarshal(raw []byte) error {
	if len(raw) > maxPacket {
		return errPacket
	}
	var q packet
	b := raw
	for field := protowire.Number(1); field <= 7; field++ {
		num, typ, n := protowire.ConsumeTag(b)
		if n < 0 || num != field {
			return errPacket
		}
		b = b[n:]
		if field == 1 || field == 4 || field == 5 {
			if typ != protowire.VarintType {
				return errPacket
			}
			v, n := protowire.ConsumeVarint(b)
			if n < 0 {
				return errPacket
			}
			b = b[n:]
			switch field {
			case 1:
				q.Version = v
			case 4:
				q.Sender = v
			case 5:
				q.Recipient = v
			}
		} else {
			if typ != protowire.BytesType {
				return errPacket
			}
			v, n := protowire.ConsumeBytes(b)
			if n < 0 {
				return errPacket
			}
			b = b[n:]
			v = append([]byte(nil), v...)
			switch field {
			case 2:
				q.Domain = v
			case 3:
				q.Attempt = v
			case 6:
				q.Kind = string(v)
			case 7:
				q.Body = v
			}
		}
	}
	if len(b) != 0 || q.Version != 1 || len(q.Domain) != 32 || len(q.Attempt) != 32 || q.Sender == 0 || q.Sender > 100 || q.Recipient > 100 || len(q.Kind) > 32 {
		return errPacket
	}
	canonical, e := q.Marshal()
	if e != nil || !bytes.Equal(raw, canonical) {
		return errPacket
	}
	*p = q
	return nil
}
func (p packet) header() []byte { p.Body = nil; b, _ := p.Marshal(); return b }

// inspect reads only the public routing envelope. Secret-bearing candidate and
// completion records never enter this codec; payload bytes are not interpreted.
func inspect(raw []byte) (kind string, attempt [32]byte, sender, recipient uint16, err error) {
	if len(raw) > maxEnvelope {
		err = errPacket
		return
	}
	var fields [6][]byte
	b := raw
	for i := range fields {
		if len(b) < 8 {
			err = errPacket
			return
		}
		n := binary.BigEndian.Uint64(b)
		b = b[8:]
		if n > uint64(len(b)) {
			err = errPacket
			return
		}
		fields[i] = b[:n]
		b = b[n:]
	}
	if len(b) != 0 || string(fields[0]) != "snowfall-message-v1" || len(fields[2]) != 32 || len(fields[3]) != 2 || len(fields[5]) > 8192 {
		err = errPacket
		return
	}
	kind = string(fields[1])
	copy(attempt[:], fields[2])
	sender = binary.BigEndian.Uint16(fields[3])
	if sender == 0 || sender > 100 {
		err = errPacket
		return
	}
	switch kind {
	case "dkg-round-one", "round-one-vote", "result-vote", "ready-attestation", "commitment", "share", "signature":
		if len(fields[4]) != 0 {
			err = errPacket
			return
		}
	case "dkg-round-two":
		if len(fields[4]) != 2 {
			err = errPacket
			return
		}
		recipient = binary.BigEndian.Uint16(fields[4])
		if recipient == 0 || recipient > 100 {
			err = errPacket
			return
		}
	default:
		err = errPacket
	}
	return
}
