package mpdisk

import (
	"github.com/stretchr/testify/assert"
	"strings"
	"testing"
)

func TestGraphDefinition(t *testing.T) {
	var p DiskPlugin

	graphdef := p.GraphDefinition()
	assert.EqualValues(t, len(graphdef), 2)
}

func TestParseProcDiskstats(t *testing.T) {
	str := `   7       0 loop0 12330 0 26704 960 0 0 0 0 0 68 720
   7       1 loop1 278 0 2590 48 0 0 0 0 0 8 28
 253       0 vda 568978 150 13872551 9214932 702789 28973 27693174 39969124 0 483933 49191357
 253       1 vda1 568892 150 13868407 9214909 702123 28973 27693166 39969007 0 483777 49187590`

	var p DiskPlugin
	devices := map[string]bool{
		"loop0": false,
		"loop1": false,
		"vda":   true,
		"vda1":  true,
	}

	stats, err := p.parseProcDiskstats(devices, strings.NewReader(str))
	if err != nil {
		t.Fatal(err)
	}

	if len(stats) == 0 {
		t.Fatalf("metrics is empty")
	}

	assert.EqualValues(t, len(stats), 12)
}

func TestParseProcDiskstatsIncludeVirtual(t *testing.T) {
	str := `   7       0 loop0 12330 0 26704 960 0 0 0 0 0 68 720
   7       1 loop1 278 0 2590 48 0 0 0 0 0 8 28
 253       0 vda 568978 150 13872551 9214932 702789 28973 27693174 39969124 0 483933 49191357
 253       1 vda1 568892 150 13868407 9214909 702123 28973 27693166 39969007 0 483777 49187590
 253      16 vdb 1357367 2080 34805026 10439061 1561480 21147 57531600 35716520 0 464104 46206065`

	var p DiskPlugin
	devices := make(map[string]bool)

	stats, err := p.parseProcDiskstats(devices, strings.NewReader(str))
	if err != nil {
		t.Fatal(err)
	}

	if len(stats) == 0 {
		t.Fatalf("metrics is empty")
	}

	assert.EqualValues(t, len(stats), 30)
	assert.EqualValues(t, stats["disk.throughput.vdb.read"], float64(34805026*512))
	assert.EqualValues(t, stats["disk.throughput.vdb.write"], float64(57531600*512))
	assert.EqualValues(t, stats["disk.time.vdb.read"], float64(10439061))
	assert.EqualValues(t, stats["disk.time.vdb.write"], float64(35716520))
	assert.EqualValues(t, stats["disk.time.vdb.io"], float64(464104))
	assert.EqualValues(t, stats["disk.time.vdb.ioWeighted"], float64(46206065))
}
