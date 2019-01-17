package mpdisk

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"regexp"
	"strconv"
	"strings"

	mp "github.com/mackerelio/go-mackerel-plugin"
	"gopkg.in/alecthomas/kingpin.v2"
)

var deviceNamePattern = regexp.MustCompile(`[^[[:alnum:]]_-]`)

// DiskPlugin is for mackerel-agent-plugins
type DiskPlugin struct {
	IncludeVirtualDisk bool
	Prefix             string
}

// MetricKeyPrefix is metrics prefix
func (p *DiskPlugin) MetricKeyPrefix() string {
	if p.Prefix == "" {
		p.Prefix = "disk"
	}
	return p.Prefix
}

// GraphDefinition interface for mackerel plugin
func (p *DiskPlugin) GraphDefinition() map[string]mp.Graphs {
	labelPrefix := strings.Title(p.MetricKeyPrefix())
	return map[string]mp.Graphs{
		"throughput.#": {
			Label: fmt.Sprintf("%s Throughput", labelPrefix),
			Unit:  mp.UnitBytesPerSecond,
			Metrics: []mp.Metrics{
				{Name: "read", Label: "read", Diff: true, Scale: 1/60.0},
				{Name: "write", Label: "write", Diff: true, Scale: 1/60.0},
			},
		},
		"time.#": {
			Label: fmt.Sprintf("%s Time (ms)", labelPrefix),
			Unit:  mp.UnitFloat,
			Metrics: []mp.Metrics{
				{Name: "read", Label: "read", Diff: true},
				{Name: "write", Label: "write", Diff: true},
				{Name: "io", Label: "io", Diff: true},
				{Name: "ioWeighted", Label: "io weighted", Diff: true},
			},
		},
	}
}

// FetchMetrics interface for mackerel plugin
func (p *DiskPlugin) FetchMetrics() (map[string]float64, error) {
	blocks := make(map[string]bool)
	if !p.IncludeVirtualDisk {
		// fetch list of block devices.
		devices, err := ioutil.ReadDir("/sys/block")
		if err != nil {
			return nil, fmt.Errorf("cannot read from directory /sys/block/: %s", err)
		}

		for _, device := range devices {
			blocks[device.Name()] = false

			// check if it's not a symlink.
			if device.Mode()&os.ModeSymlink != os.ModeSymlink {
				continue
			}

			link, err := os.Readlink(fmt.Sprintf("/sys/block/%s", device.Name()))
			if err != nil {
				return nil, fmt.Errorf("cannot read from directory /sys/block/%s: %s", device.Name(), err)
			}
			// check if it's a virtual device.
			if strings.HasPrefix(link, "../devices/virtual/block/") {
				continue
			}
			blocks[device.Name()] = true
		}
	}

	file, err := os.Open("/proc/diskstats")
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return p.parseProcDiskstats(blocks, file)
}

func (p *DiskPlugin) parseProcDiskstats(blocks map[string]bool, out io.Reader) (map[string]float64, error) {
	stats := make(map[string]float64)
	scanner := bufio.NewScanner(out)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		key := fields[2]
		// check virtual device
		if v, ok := blocks[key]; ok && !v {
			continue
		}

		name := deviceNamePattern.ReplaceAllString(key, "")
		sectorRead, err := strconv.ParseFloat(fields[5], 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse sectors read: %s", name)
		}
		readTime, err := strconv.ParseFloat(fields[6], 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse time spent read: %s", name)
		}
		sectorWrite, err := strconv.ParseFloat(fields[9], 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse sectors write: %s", name)
		}
		writeTime, err := strconv.ParseFloat(fields[10], 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse time spent write: %s", name)
		}
		ioTime, err := strconv.ParseFloat(fields[12], 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse time spent doing IO/s: %s", name)
		}
		weightedTime, err := strconv.ParseFloat(fields[13], 64)
		if err != nil {
			return nil, fmt.Errorf("failed to parse weighted time spent doing I/Os: %s", name)
		}

		// throughput: 1 sector is fixed to 512 bytes in Linux system.
		// See https://github.com/torvalds/linux/blob/b219a1d2de0c025318475e3bbf8e3215cf49d083/Documentation/block/stat.txt#L50-L56 for details.
		stats[fmt.Sprintf("throughput.%s.read", name)] = sectorRead * 512
		stats[fmt.Sprintf("throughput.%s.write", name)] = sectorWrite * 512
		// io time
		stats[fmt.Sprintf("time.%s.read", name)] = readTime
		stats[fmt.Sprintf("time.%s.write", name)] = writeTime
		stats[fmt.Sprintf("time.%s.io", name)] = ioTime
		stats[fmt.Sprintf("time.%s.ioWeighted", name)] = weightedTime
	}
	return stats, nil
}

// Do the plugin
func Do() {
	optIncludeVirtualDisk := kingpin.Flag("include-virtual-disk", "Include virtual disk").Default("false").Bool()
	optPrefix := kingpin.Flag("metric-key-prefix", "Metric key prefix").String()
	optTempfile := kingpin.Flag("tempfile", "Temp file name").String()
	kingpin.Parse()

	p := mp.NewMackerelPlugin(&DiskPlugin{
		IncludeVirtualDisk: *optIncludeVirtualDisk,
		Prefix:             *optPrefix,
	})
	p.Tempfile = *optTempfile
	p.Run()
}
