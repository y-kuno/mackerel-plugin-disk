package mpdisk

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	mp "github.com/mackerelio/go-mackerel-plugin"
	"github.com/mackerelio/golib/pluginutil"
	"gopkg.in/alecthomas/kingpin.v2"
)

var deviceNamePattern = regexp.MustCompile(`[^[[:alnum:]]_-]`)

// Metrics represents definition of a metric
type Metrics struct {
	Name      string  `json:"name"`
	Label     string  `json:"label"`
	Diff      bool    `json:"-"`
	Stacked   bool    `json:"stacked"`
	Scale     float64 `json:"-"`
	PerSecond bool    `json:"-"`
}

// Graphs represents definition of a graph
type Graphs struct {
	Label   string    `json:"label"`
	Unit    string    `json:"unit"`
	Metrics []Metrics `json:"metrics"`
}

// DiskPlugin is for mackerel-agent-plugins
type DiskPlugin struct {
	IncludeVirtualDisk bool
	Prefix             string
	Tempfile           string
	writer             io.Writer
}

// MetricKeyPrefix is metrics prefix
func (p *DiskPlugin) MetricKeyPrefix() string {
	if p.Prefix == "" {
		p.Prefix = "disk"
	}
	return p.Prefix
}

// GraphDefinition interface for mackerel plugin
func (p *DiskPlugin) GraphDefinition() map[string]Graphs {
	labelPrefix := strings.Title(p.MetricKeyPrefix())
	return map[string]Graphs{
		"throughput.#": {
			Label: fmt.Sprintf("%s Throughput", labelPrefix),
			Unit:  mp.UnitBytesPerSecond,
			Metrics: []Metrics{
				{Name: "read", Label: "read", Diff: true, PerSecond: true},
				{Name: "write", Label: "write", Diff: true, PerSecond: true},
			},
		},
		"time.#": {
			Label: fmt.Sprintf("%s Time (ms)", labelPrefix),
			Unit:  mp.UnitFloat,
			Metrics: []Metrics{
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
		stats[fmt.Sprintf("disk.throughput.%s.read", name)] = sectorRead * 512
		stats[fmt.Sprintf("disk.throughput.%s.write", name)] = sectorWrite * 512
		// io time
		stats[fmt.Sprintf("disk.time.%s.read", name)] = readTime
		stats[fmt.Sprintf("disk.time.%s.write", name)] = writeTime
		stats[fmt.Sprintf("disk.time.%s.io", name)] = ioTime
		stats[fmt.Sprintf("disk.time.%s.ioWeighted", name)] = weightedTime
	}
	return stats, nil
}

func (p *DiskPlugin) getWriter() io.Writer {
	if p.writer == nil {
		p.writer = os.Stdout
	}
	return p.writer
}

func (p *DiskPlugin) printValue(w io.Writer, key string, value float64, now time.Time) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		log.Printf("Invalid value: key = %s, value = %f\n", key, value)
		return
	}

	if value == float64(int(value)) {
		fmt.Fprintf(w, "%s\t%d\t%d\n", key, int(value), now.Unix())
	} else {
		fmt.Fprintf(w, "%s\t%f\t%d\n", key, value, now.Unix())
	}
}

func (p *DiskPlugin) fetchLastValues() (map[string]float64, time.Time, error) {
	lastTime := time.Now()

	f, err := os.Open(p.tempfileName())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, lastTime, nil
		}
		return nil, lastTime, err
	}
	defer f.Close()

	stats := make(map[string]float64)
	decoder := json.NewDecoder(f)
	err = decoder.Decode(&stats)
	lastTime = time.Unix(int64(stats["_lastTime"]), 0)
	if err != nil {
		return stats, lastTime, err
	}
	return stats, lastTime, nil
}

func (p *DiskPlugin) saveValues(values map[string]float64, now time.Time) error {
	f, err := os.Create(p.tempfileName())
	if err != nil {
		return err
	}
	defer f.Close()

	values["_lastTime"] = float64(now.Unix())
	encoder := json.NewEncoder(f)
	err = encoder.Encode(values)
	if err != nil {
		return err
	}
	return nil
}

func (p *DiskPlugin) calcDiff(value float64, lastValue float64, diffTime int64, perSecond bool) (float64, error) {
	if diffTime > 600 {
		return 0, fmt.Errorf("too long duration")
	}

	var diff float64
	if perSecond {
		diff = (value - lastValue) / float64(diffTime)
	} else {
		diff = (value - lastValue) * 60 / float64(diffTime)
	}

	if diff < 0 {
		return 0, fmt.Errorf("counter seems to be reset")
	}
	return diff, nil
}

func (p *DiskPlugin) tempfileName() string {
	if p.Tempfile != "" {
		return filepath.Join(pluginutil.PluginWorkDir(), p.Tempfile)
	}
	return filepath.Join(pluginutil.PluginWorkDir(), fmt.Sprintf("mackerel-plugin-%s", p.MetricKeyPrefix()))
}

// OutputValues output the metrics
func (p *DiskPlugin) OutputValues() {
	now := time.Now()
	stat, err := p.FetchMetrics()
	if err != nil {
		log.Fatalln("OutputValues: ", err)
	}

	lastStat, lastTime, err := p.fetchLastValues()
	if err != nil {
		log.Println("fetchLastValues (ignore):", err)
	}

	for key, graph := range p.GraphDefinition() {
		for _, metric := range graph.Metrics {
			if strings.ContainsAny(key+metric.Name, "*#") {
				p.formatValuesWithWildcard(key, metric, stat, lastStat, now, lastTime)
			} else {
				p.formatValues(key, metric, stat, lastStat, now, lastTime)
			}
		}
	}

	err = p.saveValues(stat, now)
	if err != nil {
		log.Fatalf("saveValues: %s", err)
	}
}

func (p *DiskPlugin) formatValuesWithWildcard(prefix string, metric Metrics, stat map[string]float64, lastStat map[string]float64, now time.Time, lastTime time.Time) {
	regexpStr := `\A` + prefix + "." + metric.Name
	regexpStr = strings.Replace(regexpStr, ".", `\.`, -1)
	regexpStr = strings.Replace(regexpStr, "*", `[-a-zA-Z0-9_]+`, -1)
	regexpStr = strings.Replace(regexpStr, "#", `[-a-zA-Z0-9_]+`, -1)
	re, err := regexp.Compile(regexpStr)
	if err != nil {
		log.Fatalln("Failed to compile regexp: ", err)
	}
	for k := range stat {
		if re.MatchString(k) {
			metricEach := metric
			metricEach.Name = k
			p.formatValues("", metricEach, stat, lastStat, now, lastTime)
		}
	}
}

func (p *DiskPlugin) formatValues(prefix string, metric Metrics, stat map[string]float64, lastStat map[string]float64, now time.Time, lastTime time.Time) {
	name := metric.Name
	value, ok := stat[name]
	if !ok {
		return
	}
	if metric.Diff {
		lastValue, ok := lastStat[name]
		if !ok {
			log.Printf("%s does not exist at last fetch\n", metric.Name)
			return
		}

		var err error
		diffTime := now.Unix() - lastTime.Unix()
		value, err = p.calcDiff(value, lastValue, diffTime, metric.PerSecond)
		if err != nil {
			log.Println("OutputValues: ", err)
		}
	}

	if metric.Scale != 0 {
		value *= metric.Scale
	}

	var metricNames []string
	metricNames = append(metricNames, p.MetricKeyPrefix())
	if prefix != "" {
		metricNames = append(metricNames, prefix)
	}
	metricNames = append(metricNames, metric.Name)
	p.printValue(p.getWriter(), strings.Join(metricNames, "."), value, now)
}

// GraphDef is graph definitions
type GraphDef struct {
	Graphs map[string]Graphs `json:"graphs"`
}

func title(s string) string {
	r := strings.NewReplacer(".", " ", "_", " ", "*", "", "#", "")
	return strings.TrimSpace(strings.Title(r.Replace(s)))
}

// OutputDefinitions outputs graph definitions
func (p *DiskPlugin) OutputDefinitions() {
	fmt.Fprintln(p.getWriter(), "# mackerel-agent-plugin")
	graphs := make(map[string]Graphs)
	for key, graph := range p.GraphDefinition() {
		g := graph
		k := key
		prefix := p.MetricKeyPrefix()
		if k == "" {
			k = prefix
		} else {
			k = prefix + "." + k
		}
		if g.Label == "" {
			g.Label = title(k)
		}
		var metrics []Metrics
		for _, v := range g.Metrics {
			if v.Label == "" {
				v.Label = title(v.Name)
			}
			metrics = append(metrics, v)
		}
		g.Metrics = metrics
		graphs[k] = g
	}
	var graphdef GraphDef
	graphdef.Graphs = graphs
	b, err := json.Marshal(graphdef)
	if err != nil {
		log.Fatalln("OutputDefinitions: ", err)
	}
	fmt.Fprintln(p.getWriter(), string(b))
}

// Do the plugin
func Do() {
	optIncludeVirtualDisk := kingpin.Flag("include-virtual-disk", "Include virtual disk").Default("false").Bool()
	optPrefix := kingpin.Flag("metric-key-prefix", "Metric key prefix").String()
	optTempfile := kingpin.Flag("tempfile", "Temp file name").String()
	kingpin.Parse()

	p := &DiskPlugin{
		IncludeVirtualDisk: *optIncludeVirtualDisk,
		Prefix:             *optPrefix,
		Tempfile:           *optTempfile,
	}

	if os.Getenv("MACKEREL_AGENT_PLUGIN_META") != "" {
		p.OutputDefinitions()
	} else {
		p.OutputValues()
	}
}
