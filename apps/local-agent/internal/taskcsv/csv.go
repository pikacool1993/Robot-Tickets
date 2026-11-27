package taskcsv

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"robot/apps/local-agent/internal/tasks"
)

const MaxBytes = 10 << 20

type Document struct {
	Header   []string
	Records  [][]string
	IDColumn int
	// ClientNumber scopes qualified task IDs to the current client.
	ClientNumber string
}
type Row struct {
	Mode          string     `json:"mode"`
	AllowCross    bool       `json:"allowCross"`
	RelaxSeats    bool       `json:"relaxSeats"`
	RelaxAfterSec int64      `json:"relaxAfterSec"`
	Index         int        `json:"index"`
	Line          int        `json:"line"`
	Names         string     `json:"names"`
	Seats         string     `json:"seats"`
	Status        string     `json:"status"`
	Message       string     `json:"message"`
	Warning       string     `json:"warning"`
	TaskID        int64      `json:"taskId"`
	Config        tasks.Task `json:"-"`
}

var fields = []string{"任务ID", "观演人", "购票张数", "座位要求类型", "指定区域", "指定票类", "按层选择", "允许跨区凑单", "按价格放宽区域", "开票后自动扩区秒数"}

func Parse(raw []byte) (*Document, error) {
	if len(raw) > MaxBytes {
		return nil, errors.New("CSV 文件不能超过 10 MB")
	}
	raw = bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	if !utf8.Valid(raw) {
		return nil, errors.New("请导入 UTF-8 编码的 CSV")
	}
	reader := csv.NewReader(bytes.NewReader(raw))
	header, err := reader.Read()
	if err != nil {
		return nil, errors.New("CSV 表头无法读取")
	}
	columns := map[string]int{}
	for i, name := range header {
		name = strings.TrimSpace(name)
		if _, ok := columns[name]; ok {
			return nil, fmt.Errorf("CSV 表头重复：%s", name)
		}
		columns[name] = i
	}
	for _, name := range fields {
		if _, ok := columns[name]; !ok {
			return nil, fmt.Errorf("缺少 CSV 列：%s", name)
		}
	}
	doc := &Document{Header: header, IDColumn: columns["任务ID"]}
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("CSV 格式错误：%w", err)
		}
		doc.Records = append(doc.Records, record)
	}
	if len(doc.Records) == 0 {
		return nil, errors.New("CSV 中没有记录")
	}
	return doc, nil
}
func (d *Document) Export() ([]byte, error) {
	var buffer bytes.Buffer
	buffer.Write([]byte{0xef, 0xbb, 0xbf})
	writer := csv.NewWriter(&buffer)
	writer.UseCRLF = true
	if err := writer.Write(d.Header); err != nil {
		return nil, err
	}
	if err := writer.WriteAll(d.Records); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}
func (d *Document) Preview(view tasks.View) []Row {
	columns := map[string]int{}
	for i, name := range d.Header {
		columns[strings.TrimSpace(name)] = i
	}
	existingIDs := map[int64]bool{}
	duplicates := map[string]bool{}
	for _, task := range view.Tasks {
		existingIDs[task.ID] = true
		names := task.ImportNames
		if len(names) == 0 {
			names = task.SpectatorNames
		}
		if len(names) > 0 {
			duplicates[signature(task, names)] = true
		}
	}
	rows := make([]Row, 0, len(d.Records))
	for index, record := range d.Records {
		value := func(name string) string { return strings.TrimSpace(record[columns[name]]) }
		row := Row{Index: index, Line: index + 2, Names: value("观演人"), Status: "ready"}
		id := value("任务ID")
		if id != "" {
			if separator := strings.LastIndexByte(id, '-'); separator >= 0 {
				if separator == 0 || id[:separator] != d.ClientNumber {
					row.Status = "error"
					row.Message = "任务 ID 的客户端编号与当前客户端不一致，请核对"
					rows = append(rows, row)
					continue
				}
				id = id[separator+1:]
			}
			parsed, err := strconv.ParseInt(id, 10, 64)
			row.TaskID = parsed
			if err != nil || parsed <= 0 || !existingIDs[parsed] {
				row.Status = "error"
				row.Message = "任务 ID 在本机不存在，请核对"
			} else {
				row.Status = "linked"
				row.Message = "已关联，跳过"
			}
			rows = append(rows, row)
			continue
		}
		config, warnings, err := parseConfig(value, view.Catalog)
		row.Config = config
		row.Mode, row.AllowCross, row.RelaxSeats, row.RelaxAfterSec = config.Mode, config.AllowCross, config.RelaxSeats, config.RelaxAfterSec
		row.Seats = strings.Join(config.Selected, " ")
		row.Warning = strings.Join(warnings, "；")
		if view.ConfigError != "" {
			err = errors.New(view.ConfigError)
		}
		if err != nil {
			row.Status = "error"
			row.Message = err.Error()
		} else {
			key := signature(config, config.ImportNames)
			if duplicates[key] {
				row.Status = "duplicate"
				row.Message = "疑似重复，请确认是否仍创建"
			}
			duplicates[key] = true
		}
		rows = append(rows, row)
	}
	return rows
}
func signature(t tasks.Task, names []string) string {
	n := append([]string{}, names...)
	s := append([]string{}, t.Selected...)
	sort.Strings(n)
	sort.Strings(s)
	delay := t.RelaxAfterSec
	if !t.RelaxSeats {
		delay = 0
	}
	return fmt.Sprintf("%q|%s|%q|%t|%t|%d", n, t.Mode, s, t.AllowCross, t.RelaxSeats, delay)
}

var pricePattern = regexp.MustCompile(`[￥¥]\s*([0-9]+(?:\.[0-9]+)?)`)

func parseConfig(value func(string) string, catalog tasks.Catalog) (tasks.Task, []string, error) {
	var config tasks.Task
	config.ImportNames = strings.Fields(value("观演人"))
	count, err := strconv.Atoi(value("购票张数"))
	if err != nil || count < 1 || count > 2 || len(config.ImportNames) != count {
		return config, nil, errors.New("购票张数须为 1 或 2，并与观演人人数一致")
	}
	config.ImportCount = count
	if config.AllowCross, err = parseBool(value("允许跨区凑单")); err != nil {
		return config, nil, fmt.Errorf("允许跨区凑单：%w", err)
	}
	if config.RelaxSeats, err = parseBool(value("按价格放宽区域")); err != nil {
		return config, nil, fmt.Errorf("按价格放宽区域：%w", err)
	}
	if config.RelaxSeats {
		config.RelaxAfterSec, err = strconv.ParseInt(value("开票后自动扩区秒数"), 10, 64)
		if err != nil || config.RelaxAfterSec < 0 || config.RelaxAfterSec > 86400 {
			return config, nil, errors.New("价格放宽秒数须为 0 至 86400 的整数")
		}
	}
	source := ""
	switch value("座位要求类型") {
	case "固定区域":
		config.Mode = "area"
		source = "指定区域"
	case "指定票类":
		config.Mode = "tier"
		source = "指定票类"
	case "按层选择":
		config.Mode = "area"
		source = "按层选择"
	default:
		return config, nil, errors.New("座位要求类型无效")
	}
	if value(source) == "" {
		return config, nil, fmt.Errorf("%s不能为空", source)
	}
	var warnings []string
	for _, field := range []string{"指定区域", "指定票类", "按层选择"} {
		if field != source && value(field) != "" {
			warnings = append(warnings, "已忽略非当前类型字段："+field)
		}
	}
	selected := map[string]bool{}
	for _, raw := range strings.Split(value(source), ",") {
		option := strings.TrimSpace(raw)
		tokens := strings.Fields(option)
		if len(tokens) == 0 {
			return config, warnings, errors.New("座位要求含空选项")
		}
		matched := false
		for _, tier := range catalog.Tiers {
			if source == "指定票类" {
				if strings.ToUpper(strings.TrimSuffix(tokens[0], "类")) == strings.ToUpper(tier.Tier) {
					matched = true
					selected[tier.Tier] = true
					if priceDiffers(option, tier.Price) {
						warnings = append(warnings, option+"价格与目录不同，使用本机目录")
					}
				}
				continue
			}
			for _, area := range tier.Areas {
				isMatch := source == "指定区域" && strings.EqualFold(tokens[0], area.Code) || source == "按层选择" && option == area.Level
				if !isMatch {
					continue
				}
				matched = true
				if !area.IsSaleable {
					if source == "指定区域" {
						return config, warnings, fmt.Errorf("区域 %s 在本机目录不可售", area.Code)
					}
					continue
				}
				selected[area.Code] = true
				if source == "指定区域" {
					if priceDiffers(option, tier.Price) || strings.Contains(option, "未开售") || strings.Contains(option, "看台") && !strings.Contains(option, area.Level) {
						warnings = append(warnings, option+"描述与目录有差异，使用本机目录")
					}
				}
			}
		}
		if !matched {
			return config, warnings, fmt.Errorf("本机座位目录不存在选项：%s", option)
		}
	}
	for name := range selected {
		config.Selected = append(config.Selected, name)
	}
	sort.Strings(config.Selected)
	if len(config.Selected) == 0 {
		return config, warnings, errors.New("座位要求没有可售区域")
	}
	return config, warnings, nil
}
func parseBool(raw string) (bool, error) {
	switch raw {
	case "1":
		return true, nil
	case "0", "":
		return false, nil
	}
	return false, errors.New("仅支持 1、0 或空白")
}
func priceDiffers(option string, price float64) bool {
	match := pricePattern.FindStringSubmatch(option)
	if len(match) == 0 {
		return false
	}
	number, err := strconv.ParseFloat(match[1], 64)
	return err != nil || number != price
}
