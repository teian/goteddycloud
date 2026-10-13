// SPDX-License-Identifier: GPL-2.0-only
package cc3200cli

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"text/tabwriter"

	"goteddycloud/internal/cc3200"
)

// These field names follow the upstream Python filesystem JSON contract.
type filesystemFile struct {
	Index    int    `json:"index"`
	Start    int    `json:"start_block"`
	Blocks   int    `json:"size_blocks"`
	Mirrored bool   `json:"mirrored"`
	Flags    int    `json:"flags"`
	Name     string `json:"fname"`
	Total    int    `json:"total_blocks"`
	Header   []int  `json:"header"`
	Magic    []int  `json:"magic"`
	Size     int    `json:"size"`
}
type filesystemHole struct {
	Start  int `json:"start_block"`
	Blocks int `json:"size_blocks"`
}
type filesystem struct {
	Revision   uint16           `json:"fat_commit_revision"`
	BlockSize  int              `json:"block_size"`
	BlockCount int              `json:"block_count"`
	Used       int              `json:"used_blocks"`
	Files      []filesystemFile `json:"files"`
	Holes      []filesystemHole `json:"holes"`
}

func printFilesystem(flash []byte, options cc3200.ImageOptions, jsonOutput, extended bool, stdout io.Writer, t *terminal) error {
	manifest, e := cc3200.InspectImage(flash, options)
	if e != nil {
		return e
	}
	fs := filesystem{Revision: manifest.Revision, BlockSize: cc3200.BlockSize, BlockCount: len(flash) / cc3200.BlockSize, Used: 5, Files: []filesystemFile{}, Holes: []filesystemHole{}}
	selected := 0
	if binary.LittleEndian.Uint16(flash[cc3200.BlockSize:]) == manifest.Revision && binary.LittleEndian.Uint16(flash[cc3200.BlockSize+2:]) == 0x534c {
		selected = 1
	}
	for _, entry := range manifest.Entries {
		file := filesystemFile{Index: entry.ID, Start: entry.Start, Blocks: entry.Blocks, Mirrored: entry.Mirrored, Flags: int(flash[selected*cc3200.BlockSize+7+entry.ID*4] >> 4), Name: entry.Name, Total: entry.Blocks}
		if file.Mirrored {
			file.Total *= 2
		}
		if extended {
			start := entry.Start * cc3200.BlockSize
			header := flash[start : start+8]
			file.Size = int(header[0]) | int(header[1])<<8 | int(header[2])<<16
			if file.Size > entry.Blocks*cc3200.BlockSize-8 {
				return fmt.Errorf("file ID %d header exceeds FAT allocation", entry.ID)
			}
			file.Header = []int{}
			file.Magic = []int{}
			for i, b := range header {
				file.Header = append(file.Header, int(b))
				if i >= 3 {
					file.Magic = append(file.Magic, int(b))
				}
			}
		}
		fs.Files = append(fs.Files, file)
		fs.Used += file.Total
	}
	allocated := append([]filesystemFile{{Start: 0, Total: 5}}, fs.Files...)
	sort.Slice(allocated, func(i, j int) bool { return allocated[i].Start < allocated[j].Start })
	end := 0
	for _, f := range append(allocated, filesystemFile{Start: fs.BlockCount}) {
		if f.Start > end {
			fs.Holes = append(fs.Holes, filesystemHole{Start: end, Blocks: f.Start - end - 1})
		}
		end = f.Start + f.Total
	}
	if !t.quiet || !jsonOutput {
		fmt.Fprintf(t.out, "\n%s\n", t.paint("1", fmt.Sprintf("Filesystem FAT revision %d — %d blocks × %d bytes", fs.Revision, fs.BlockCount, fs.BlockSize)))
		w := tabwriter.NewWriter(t.out, 0, 4, 2, ' ', 0)
		if extended {
			fmt.Fprintln(w, "ID\tSTART\tBLOCKS\tBYTES\tMIRROR\tFLAGS\tTOTAL\tMAGIC\tFILENAME")
		} else {
			fmt.Fprintln(w, "ID\tSTART\tBLOCKS\tMIRROR\tFLAGS\tTOTAL\tFILENAME")
		}
		for _, f := range fs.Files {
			if extended {
				magic := make([]byte, len(f.Magic))
				for i, b := range f.Magic {
					magic[i] = byte(b)
				}
				fmt.Fprintf(w, "%d\t%d\t%d\t%d\t%t\t0x%x\t%d\t%s\t%s\n", f.Index, f.Start, f.Blocks, f.Size, f.Mirrored, f.Flags, f.Total, hex.EncodeToString(magic), f.Name)
			} else {
				fmt.Fprintf(w, "%d\t%d\t%d\t%t\t0x%x\t%d\t%s\n", f.Index, f.Start, f.Blocks, f.Mirrored, f.Flags, f.Total, f.Name)
			}
		}
		if e = w.Flush(); e != nil {
			return e
		}
		fmt.Fprintf(t.out, "Used: %d blocks; free: %d blocks; files: %d\n", fs.Used, fs.BlockCount-fs.Used, len(fs.Files))
		for _, hole := range fs.Holes {
			fmt.Fprintf(t.out, "Free blocks: %d..%d\n", hole.Start, hole.Start+hole.Blocks)
		}
	}
	if jsonOutput {
		return json.NewEncoder(stdout).Encode(fs)
	}
	return nil
}
