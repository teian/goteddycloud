// SPDX-License-Identifier: GPL-2.0-only
package firmware

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

var errFAT = errors.New("invalid or unsupported firmware FAT filesystem")

// FAT owns a private logical copy of the wear-levelled asset partition. Changes
// become part of the Image only after Commit, preserving the rotation and state.
type FAT struct {
	physical, logical                                                                                                  []byte
	mapping                                                                                                            []int
	sector, clusterBytes, fatOffset, fatSize, fatCopies, rootOffset, rootSize, dataOffset, clusters, bits, rootCluster int
}
type fileEntry struct {
	name                  string
	directory             bool
	cluster, size, offset int
}

func openFAT(partition []byte) (*FAT, error) {
	const sector = 4096
	if len(partition)%sector != 0 || len(partition) < 5*sector {
		return nil, errFAT
	}
	total := len(partition) / sector
	stateSectors := (64 + 16*total + sector - 1) / sector
	fatSectors := total - 1 - 2*stateSectors
	if fatSectors <= 1 {
		return nil, errFAT
	}
	stateOffset := len(partition) - (2*stateSectors+1)*sector
	if stateOffset < 0 || stateOffset+64 > len(partition) {
		return nil, errFAT
	}
	move := int(le.Uint32(partition[stateOffset+8:]))
	if move >= fatSectors {
		return nil, errFAT
	}
	// Match the pinned C reader: its packed state payload is 60 bytes, followed
	// by 16-byte records; the first read includes the state CRC's four bytes.
	records := 0
	empty := bytes.Repeat([]byte{255}, 16)
	for offset := stateOffset + 60; offset+16 <= stateOffset+stateSectors*sector; offset += 16 {
		if bytes.Equal(partition[offset:offset+16], empty) {
			break
		}
		records++
	}
	if records >= fatSectors {
		return nil, errFAT
	}
	f := &FAT{physical: partition, logical: make([]byte, fatSectors*sector), mapping: make([]int, fatSectors)}
	for i := range fatSectors {
		mapped := (i + move) % fatSectors
		if mapped >= records {
			mapped++
		}
		if mapped >= total {
			return nil, errFAT
		}
		f.mapping[i] = mapped
		copy(f.logical[i*sector:], partition[mapped*sector:(mapped+1)*sector])
	}
	b := f.logical
	if len(b) < 512 || b[510] != 0x55 || b[511] != 0xaa {
		return nil, errFAT
	}
	f.sector = int(le.Uint16(b[11:]))
	spc := int(b[13])
	reserved := int(le.Uint16(b[14:]))
	f.fatCopies = int(b[16])
	rootEntries := int(le.Uint16(b[17:]))
	sectors := int(le.Uint16(b[19:]))
	if sectors == 0 {
		sectors = int(le.Uint32(b[32:]))
	}
	fatSectorsBPB := int(le.Uint16(b[22:]))
	if fatSectorsBPB == 0 {
		fatSectorsBPB = int(le.Uint32(b[36:]))
	}
	if f.sector != 4096 || spc == 0 || spc&(spc-1) != 0 || reserved < 1 || f.fatCopies < 1 || f.fatCopies > 2 || fatSectorsBPB < 1 || sectors < 128 || sectors > len(b)/f.sector {
		return nil, errFAT
	}
	for _, mapped := range f.mapping[:sectors] {
		if mapped*sector >= stateOffset {
			return nil, errFAT
		}
	}
	f.clusterBytes = spc * f.sector
	f.fatOffset = reserved * f.sector
	f.fatSize = fatSectorsBPB * f.sector
	f.rootOffset = f.fatOffset + f.fatCopies*f.fatSize
	f.rootSize = (rootEntries*32 + f.sector - 1) / f.sector * f.sector
	f.dataOffset = f.rootOffset + f.rootSize
	if f.dataOffset >= sectors*f.sector {
		return nil, errFAT
	}
	f.clusters = (sectors*f.sector - f.dataOffset) / f.clusterBytes
	if f.clusters < 4085 {
		f.bits = 12
	} else if f.clusters < 65525 {
		f.bits = 16
	} else {
		f.bits = 32
		f.rootCluster = int(le.Uint32(b[44:]) & 0x0fffffff)
		if f.rootCluster < 2 || f.rootCluster >= f.clusters+2 || rootEntries != 0 {
			return nil, errFAT
		}
	}
	if (f.clusters+2)*f.bits > f.fatSize*8 {
		return nil, errFAT
	}
	return f, nil
}
func (f *FAT) next(cluster int) (int, error) {
	if cluster < 2 || cluster >= f.clusters+2 {
		return 0, errFAT
	}
	offset := f.fatOffset + cluster*f.bits/8
	var value int
	switch f.bits {
	case 12:
		value = int(le.Uint16(f.logical[offset:]))
		if cluster&1 == 1 {
			value >>= 4
		}
		value &= 0xfff
		if value >= 0xff8 {
			return 0, nil
		}
	case 16:
		value = int(le.Uint16(f.logical[offset:]))
		if value >= 0xfff8 {
			return 0, nil
		}
	case 32:
		value = int(le.Uint32(f.logical[offset:]) & 0x0fffffff)
		if value >= 0x0ffffff8 {
			return 0, nil
		}
	}
	if value < 2 || value >= f.clusters+2 {
		return 0, errFAT
	}
	return value, nil
}
func (f *FAT) chain(start int) ([]int, error) {
	if start == 0 {
		return []int{}, nil
	}
	chain := []int{}
	seen := map[int]bool{}
	for cluster := start; cluster != 0; {
		if seen[cluster] || len(chain) >= f.clusters {
			return nil, errFAT
		}
		seen[cluster] = true
		if cluster < 2 || cluster >= f.clusters+2 {
			return nil, errFAT
		}
		chain = append(chain, cluster)
		var err error
		cluster, err = f.next(cluster)
		if err != nil {
			return nil, err
		}
	}
	return chain, nil
}
func (f *FAT) offsets(cluster int) ([]int, error) {
	if cluster == 0 && f.bits != 32 {
		offsets := make([]int, 0, f.rootSize/32)
		for offset := f.rootOffset; offset < f.rootOffset+f.rootSize; offset += 32 {
			offsets = append(offsets, offset)
		}
		return offsets, nil
	}
	if cluster == 0 {
		cluster = f.rootCluster
	}
	chain, err := f.chain(cluster)
	if err != nil {
		return nil, err
	}
	offsets := []int{}
	for _, cluster := range chain {
		start := f.dataOffset + (cluster-2)*f.clusterBytes
		for pos := start; pos < start+f.clusterBytes; pos += 32 {
			offsets = append(offsets, pos)
		}
	}
	return offsets, nil
}
func (f *FAT) directory(cluster int) ([]fileEntry, error) {
	offsets, err := f.offsets(cluster)
	if err != nil {
		return nil, err
	}
	result := []fileEntry{}
	for _, offset := range offsets {
		b := f.logical[offset : offset+32]
		if b[0] == 0 {
			break
		}
		if b[0] == 0xe5 || b[11]&8 != 0 {
			continue
		}
		name := strings.TrimRight(string(b[:8]), " ")
		ext := strings.TrimRight(string(b[8:11]), " ")
		if ext != "" {
			name += "." + ext
		}
		if name == "." || name == ".." {
			continue
		}
		if strings.ContainsAny(name, "/\\:\x00") {
			return nil, errFAT
		}
		first := int(le.Uint16(b[26:]))
		if f.bits == 32 {
			first |= int(le.Uint16(b[20:])) << 16
		}
		result = append(result, fileEntry{name: name, directory: b[11]&16 != 0, cluster: first, size: int(le.Uint32(b[28:])), offset: offset})
	}
	return result, nil
}
func (f *FAT) folder(path string) (int, error) {
	cluster := 0
	for _, part := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == "." || part == ".." {
			return 0, errFAT
		}
		entries, err := f.directory(cluster)
		if err != nil {
			return 0, err
		}
		found := false
		for _, entry := range entries {
			if entry.directory && strings.EqualFold(entry.name, part) {
				cluster = entry.cluster
				found = true
				break
			}
		}
		if !found {
			return 0, fmt.Errorf("FAT directory %s missing", part)
		}
	}
	return cluster, nil
}
func (f *FAT) Extract(path string) (map[string][]byte, error) {
	cluster, err := f.folder(path)
	if err != nil {
		return nil, err
	}
	entries, err := f.directory(cluster)
	if err != nil {
		return nil, err
	}
	result := map[string][]byte{}
	total := 0
	for _, entry := range entries {
		if entry.directory {
			continue
		}
		if entry.size < 0 || entry.size > 4<<20 || total > 16<<20-entry.size {
			return nil, errFAT
		}
		chain, err := f.chain(entry.cluster)
		if err != nil {
			return nil, err
		}
		if len(chain)*f.clusterBytes < entry.size {
			return nil, errFAT
		}
		data := make([]byte, 0, entry.size)
		remaining := entry.size
		for _, cluster := range chain {
			count := min(remaining, f.clusterBytes)
			offset := f.dataOffset + (cluster-2)*f.clusterBytes
			data = append(data, f.logical[offset:offset+count]...)
			remaining -= count
			if remaining == 0 {
				break
			}
		}
		result[entry.name] = data
		total += entry.size
	}
	return result, nil
}
func (f *FAT) setNext(cluster, value int) {
	for copyIndex := 0; copyIndex < f.fatCopies; copyIndex++ {
		offset := f.fatOffset + copyIndex*f.fatSize + cluster*f.bits/8
		switch f.bits {
		case 12:
			old := le.Uint16(f.logical[offset:])
			if cluster&1 == 1 {
				old = old&0x000f | uint16(value&0xfff)<<4
			} else {
				old = old&0xf000 | uint16(value&0xfff)
			}
			le.PutUint16(f.logical[offset:], old)
		case 16:
			le.PutUint16(f.logical[offset:], uint16(value))
		case 32:
			old := le.Uint32(f.logical[offset:])
			le.PutUint32(f.logical[offset:], old&0xf0000000|uint32(value)&0x0fffffff)
		}
	}
}
func (f *FAT) free(cluster int) bool {
	offset := f.fatOffset + cluster*f.bits/8
	switch f.bits {
	case 12:
		value := le.Uint16(f.logical[offset:])
		if cluster&1 == 1 {
			value >>= 4
		}
		return value&0xfff == 0
	case 16:
		return le.Uint16(f.logical[offset:]) == 0
	default:
		return le.Uint32(f.logical[offset:])&0x0fffffff == 0
	}
}
func shortName(name string) ([]byte, error) {
	if name != strings.ToUpper(name) {
		name = strings.ToUpper(name)
	}
	parts := strings.Split(name, ".")
	if len(parts) > 2 || len(parts[0]) < 1 || len(parts[0]) > 8 || len(parts) == 2 && len(parts[1]) > 3 {
		return nil, errFAT
	}
	for _, r := range name {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-') {
			return nil, errFAT
		}
	}
	out := bytes.Repeat([]byte{' '}, 11)
	copy(out, parts[0])
	if len(parts) == 2 {
		copy(out[8:], parts[1])
	}
	return out, nil
}
func (f *FAT) Put(path, name string, data []byte) error {
	if len(data) > 4<<20 {
		return errFAT
	}
	encoded, err := shortName(name)
	if err != nil {
		return err
	}
	cluster, err := f.folder(path)
	if err != nil {
		return err
	}
	entries, err := f.directory(cluster)
	if err != nil {
		return err
	}
	entry := fileEntry{offset: -1}
	for _, candidate := range entries {
		if strings.EqualFold(candidate.name, name) {
			if candidate.directory {
				return errFAT
			}
			entry = candidate
			break
		}
	}
	if entry.offset < 0 {
		offsets, err := f.offsets(cluster)
		if err != nil {
			return err
		}
		for _, offset := range offsets {
			if f.logical[offset] == 0 || f.logical[offset] == 0xe5 {
				entry.offset = offset
				break
			}
		}
		if entry.offset < 0 {
			return errors.New("FAT directory full")
		}
	}
	oldChain, err := f.chain(entry.cluster)
	if err != nil {
		return err
	}
	needed := (len(data) + f.clusterBytes - 1) / f.clusterBytes
	chain := append([]int(nil), oldChain[:min(needed, len(oldChain))]...)
	for candidate := 2; len(chain) < needed && candidate < f.clusters+2; candidate++ {
		if f.free(candidate) {
			chain = append(chain, candidate)
		}
	}
	if len(chain) < needed {
		return errors.New("FAT volume full")
	}
	for _, cluster := range oldChain {
		f.setNext(cluster, 0)
	}
	for i, cluster := range chain {
		next := 0x0fffffff
		if i+1 < len(chain) {
			next = chain[i+1]
		}
		f.setNext(cluster, next)
		offset := f.dataOffset + (cluster-2)*f.clusterBytes
		clear(f.logical[offset : offset+f.clusterBytes])
		start := i * f.clusterBytes
		copy(f.logical[offset:], data[start:min(start+f.clusterBytes, len(data))])
	}
	b := f.logical[entry.offset : entry.offset+32]
	clear(b)
	copy(b, encoded)
	b[11] = 0x20
	if len(chain) > 0 {
		le.PutUint16(b[26:], uint16(chain[0]))
		if f.bits == 32 {
			le.PutUint16(b[20:], uint16(chain[0]>>16))
		}
	}
	le.PutUint32(b[28:], uint32(len(data)))
	return nil
}
func (f *FAT) Commit() {
	for i, mapped := range f.mapping {
		copy(f.physical[mapped*4096:(mapped+1)*4096], f.logical[i*4096:(i+1)*4096])
	}
}
