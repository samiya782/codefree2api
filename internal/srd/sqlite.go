package srd

import (
	"encoding/binary"
	"os"
)

// readItem returns one ItemTable value from a VS Code state database.
// A missing file, a locked file, or a database this reader cannot parse
// yields ok=false so the caller can try another login.
func readItem(path, key string) (value []byte, ok bool, err error) {
	f, err := openState(path)
	if err != nil || f == nil {
		return nil, false, nil
	}
	return f.item("ItemTable", key)
}

type stateFile struct {
	pages    map[uint32][]byte
	pageSize int
	reserved int
}

func openState(path string) (*stateFile, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < 100 || string(b[:15]) != "SQLite format 3" {
		return nil, nil
	}
	ps := int(binary.BigEndian.Uint16(b[16:18]))
	if ps == 1 {
		ps = 65536
	}
	if ps < 512 || ps&(ps-1) != 0 {
		return nil, nil
	}
	f := &stateFile{
		pages:    map[uint32][]byte{},
		pageSize: ps,
		reserved: int(b[20]),
	}
	n := len(b) / ps
	for i := 1; i <= n; i++ {
		start := (i - 1) * ps
		page := make([]byte, ps)
		copy(page, b[start:start+ps])
		f.pages[uint32(i)] = page
	}
	f.applyWAL(path + "-wal")
	return f, nil
}

func (f *stateFile) usable() int { return f.pageSize - f.reserved }

func (f *stateFile) applyWAL(path string) {
	b, err := os.ReadFile(path)
	if err != nil || len(b) < 32 {
		return
	}
	magic := binary.BigEndian.Uint32(b[0:4])
	le := magic == 0x377f0683
	if magic != 0x377f0682 && !le {
		return
	}
	u32 := func(p []byte) uint32 {
		if le {
			return binary.LittleEndian.Uint32(p)
		}
		return binary.BigEndian.Uint32(p)
	}
	if int(u32(b[8:12])) != f.pageSize {
		return
	}
	salt1, salt2 := u32(b[16:20]), u32(b[20:24])
	frame := 24 + f.pageSize
	type pg struct {
		n    uint32
		data []byte
	}
	var frames []pg
	lastCommit := -1
	for off := 32; off+frame <= len(b); off += frame {
		if u32(b[off+8:off+12]) != salt1 || u32(b[off+12:off+16]) != salt2 {
			break
		}
		data := make([]byte, f.pageSize)
		copy(data, b[off+24:off+frame])
		frames = append(frames, pg{n: u32(b[off : off+4]), data: data})
		if u32(b[off+4:off+8]) != 0 {
			lastCommit = len(frames) - 1
		}
	}
	if lastCommit < 0 {
		return
	}
	for _, fr := range frames[:lastCommit+1] {
		if fr.n > 0 {
			f.pages[fr.n] = fr.data
		}
	}
}

type column struct {
	typ  uint64
	data []byte
}

func (f *stateFile) item(table, key string) ([]byte, bool, error) {
	root := f.tableRoot(table)
	if root == 0 {
		return nil, false, nil
	}
	var found []byte
	ok := false
	f.walk(root, map[uint32]bool{}, func(cols []column) {
		if ok || len(cols) < 2 {
			return
		}
		if string(cols[0].data) == key {
			found = append([]byte(nil), cols[1].data...)
			ok = true
		}
	})
	return found, ok, nil
}

func (f *stateFile) tableRoot(name string) uint32 {
	var root uint32
	f.walk(1, map[uint32]bool{}, func(cols []column) {
		if root != 0 || len(cols) < 4 {
			return
		}
		if string(cols[0].data) == "table" && string(cols[1].data) == name {
			root = uint32(colInt(cols[3]))
		}
	})
	return root
}

func (f *stateFile) walk(n uint32, seen map[uint32]bool, fn func([]column)) {
	if n == 0 || seen[n] {
		return
	}
	seen[n] = true
	p := f.pages[n]
	if len(p) < 8 {
		return
	}
	hdr := 0
	if n == 1 {
		hdr = 100
	}
	if hdr+8 > len(p) {
		return
	}
	typ := p[hdr]
	ncells := int(binary.BigEndian.Uint16(p[hdr+3 : hdr+5]))
	ptr := hdr + 8
	var right uint32
	switch typ {
	case 0x05:
		if hdr+12 > len(p) {
			return
		}
		right = binary.BigEndian.Uint32(p[hdr+8 : hdr+12])
		ptr = hdr + 12
	case 0x0d:
	default:
		return
	}
	for i := 0; i < ncells; i++ {
		if ptr+2 > len(p) {
			return
		}
		off := int(binary.BigEndian.Uint16(p[ptr : ptr+2]))
		ptr += 2
		if off < 0 || off >= len(p) {
			continue
		}
		if typ == 0x05 {
			if off+4 > len(p) {
				continue
			}
			f.walk(binary.BigEndian.Uint32(p[off:off+4]), seen, fn)
			continue
		}
		if cols, ok := f.leaf(p, off); ok {
			fn(cols)
		}
	}
	if right != 0 {
		f.walk(right, seen, fn)
	}
}

func (f *stateFile) leaf(page []byte, off int) ([]column, bool) {
	payloadLen, n1 := svarint(page[off:])
	if n1 == 0 || payloadLen == 0 || payloadLen > 8<<20 {
		return nil, false
	}
	_, n2 := svarint(page[off+n1:])
	if n2 == 0 {
		return nil, false
	}
	pos := off + n1 + n2
	local, overflow := localPayload(int(payloadLen), f.usable())
	if local < 0 || pos+local > len(page) {
		return nil, false
	}
	buf := append([]byte(nil), page[pos:pos+local]...)
	if overflow {
		if pos+local+4 > len(page) {
			return nil, false
		}
		next := binary.BigEndian.Uint32(page[pos+local : pos+local+4])
		chunk := f.usable() - 4
		if chunk <= 0 {
			return nil, false
		}
		for next != 0 && len(buf) < int(payloadLen) {
			pg := f.pages[next]
			if len(pg) < 4 {
				return nil, false
			}
			next = binary.BigEndian.Uint32(pg[:4])
			body := pg[4:]
			if len(body) > chunk {
				body = body[:chunk]
			}
			need := int(payloadLen) - len(buf)
			if len(body) > need {
				body = body[:need]
			}
			buf = append(buf, body...)
		}
	}
	if len(buf) < int(payloadLen) {
		return nil, false
	}
	return decodeRecord(buf[:payloadLen]), true
}

func localPayload(p, u int) (int, bool) {
	if u <= 35 {
		return 0, false
	}
	x := u - 35
	if p <= x {
		return p, false
	}
	m := ((u-12)*32)/255 - 23
	if m < 0 {
		m = 0
	}
	k := m + ((p - m) % (u - 4))
	if k <= x {
		return k, true
	}
	return m, true
}

func svarint(b []byte) (uint64, int) {
	var x uint64
	for i := 0; i < 8 && i < len(b); i++ {
		x = (x << 7) | uint64(b[i]&0x7f)
		if b[i] < 0x80 {
			return x, i + 1
		}
	}
	if len(b) < 9 {
		return 0, 0
	}
	return (x << 8) | uint64(b[8]), 9
}

func decodeRecord(b []byte) []column {
	hsz, n := svarint(b)
	if n == 0 || int(hsz) > len(b) || int(hsz) < n {
		return nil
	}
	var types []uint64
	for i := n; i < int(hsz); {
		t, m := svarint(b[i:])
		if m == 0 {
			return nil
		}
		types = append(types, t)
		i += m
	}
	rest := b[hsz:]
	out := make([]column, len(types))
	for i, t := range types {
		sz := serialSize(t)
		if sz > len(rest) {
			return nil
		}
		out[i] = column{typ: t, data: append([]byte(nil), rest[:sz]...)}
		rest = rest[sz:]
	}
	return out
}

func serialSize(t uint64) int {
	switch t {
	case 0, 8, 9:
		return 0
	case 1:
		return 1
	case 2:
		return 2
	case 3:
		return 3
	case 4:
		return 4
	case 5:
		return 6
	case 6, 7:
		return 8
	default:
		if t >= 12 && t%2 == 0 {
			return int((t - 12) / 2)
		}
		if t >= 13 && t%2 == 1 {
			return int((t - 13) / 2)
		}
		return 0
	}
}

func colInt(c column) int64 {
	switch c.typ {
	case 0, 8:
		return 0
	case 9:
		return 1
	}
	var x uint64
	for _, b := range c.data {
		x = (x << 8) | uint64(b)
	}
	bits := uint(len(c.data) * 8)
	if bits > 0 && bits < 64 && x&(1<<(bits-1)) != 0 {
		x |= ^uint64(0) << bits
	}
	return int64(x)
}
