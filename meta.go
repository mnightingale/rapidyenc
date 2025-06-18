package rapidyenc

type Meta struct {
	FileName   string
	FileSize   int64 // Total size of the file
	PartNumber int64
	TotalParts int64
	Offset     int64 // Offset of the part within the file relative to the start, like io.Seeker or io.WriterAt
	PartSize   int64 // Size of the unencoded data
}

// Begin is the "=ypart begin" value calculated from the Offset
func (m Meta) Begin() int64 {
	return m.Offset + 1
}

// End is the "=ypart end" value calculated from the Offset and PartSize
func (m Meta) End() int64 {
	return m.Offset + m.PartSize
}

type DecodedMeta struct {
	Meta
	Hash uint32 // CRC32 hash of the decoded data
}
