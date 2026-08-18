package cms

import (
	"encoding/binary"
	"io"
)

// WriteTo serializes the Count-Min Sketch to a writer.
func (s *Sketch) WriteTo(w io.Writer) (int64, error) {
	var hdr [8]byte
	binary.BigEndian.PutUint32(hdr[0:4], s.depth)
	binary.BigEndian.PutUint32(hdr[4:8], s.width)
	n, err := w.Write(hdr[:])
	if err != nil {
		return int64(n), err
	}
	total := int64(n)

	// Write seeds.
	buf := make([]byte, 8)
	for _, seed := range s.seeds {
		binary.BigEndian.PutUint64(buf, seed)
		nn, err := w.Write(buf)
		total += int64(nn)
		if err != nil {
			return total, err
		}
	}

	// Write rows.
	for _, row := range s.rows {
		for _, v := range row {
			binary.BigEndian.PutUint64(buf, v)
			nn, err := w.Write(buf)
			total += int64(nn)
			if err != nil {
				return total, err
			}
		}
	}
	return total, nil
}

// ReadFrom deserializes a Count-Min Sketch from a reader.
func (s *Sketch) ReadFrom(r io.Reader) (int64, error) {
	var hdr [8]byte
	n, err := io.ReadFull(r, hdr[:])
	if err != nil {
		return int64(n), err
	}
	total := int64(n)
	depth := binary.BigEndian.Uint32(hdr[0:4])
	width := binary.BigEndian.Uint32(hdr[4:8])

	seeds := make([]uint64, depth)
	buf := make([]byte, 8)
	for i := range seeds {
		nn, err := io.ReadFull(r, buf)
		total += int64(nn)
		if err != nil {
			return total, err
		}
		seeds[i] = binary.BigEndian.Uint64(buf)
	}

	rows := make([][]uint64, depth)
	for i := range rows {
		rows[i] = make([]uint64, width)
		for j := range rows[i] {
			nn, err := io.ReadFull(r, buf)
			total += int64(nn)
			if err != nil {
				return total, err
			}
			rows[i][j] = binary.BigEndian.Uint64(buf)
		}
	}

	s.depth = depth
	s.width = width
	s.seeds = seeds
	s.rows = rows
	return total, nil
}

// TotalCount returns the total increments across the sketch.
func (s *Sketch) TotalCount() uint64 { return s.total }
