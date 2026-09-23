package worker

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"time"

	"askbase/be/internal/config"
)

const (
	minerUMaxInputBytes   = 200 << 20
	minerUMaxArchiveBytes = 512 << 20
	minerUMaxEntryBytes   = 64 << 20
)

// minerUClient 调用 MinerU 在线精准解析 API，并把结果包转换为解析原子。
type minerUClient struct {
	baseURL   string
	token     string
	model     string
	language  string
	ocr       bool
	formula   bool
	table     bool
	pollEvery time.Duration
	timeout   time.Duration
	http      *http.Client
}

// minerUEnvelope 是 MinerU API 的通用响应包装。
type minerUEnvelope[T any] struct {
	Code    minerUCode `json:"code"`
	Message string     `json:"msg"`
	TraceID string     `json:"trace_id"`
	Data    T          `json:"data"`
}

// minerUCode 表示 MinerU 响应中的数字或字符串状态码。
type minerUCode string

// UnmarshalJSON 解码 MinerU 数字或字符串形式的状态码。
func (c *minerUCode) UnmarshalJSON(data []byte) error {
	var value string
	if len(data) > 0 && data[0] == '"' {
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
	} else {
		value = strings.TrimSpace(string(data))
	}
	*c = minerUCode(value)
	return nil
}

func (c minerUCode) ok() bool {
	return c == "0"
}

// minerUSubmitData 保存批量上传任务的预签名地址和批次 ID。
type minerUSubmitData struct {
	BatchID  string   `json:"batch_id"`
	FileURLs []string `json:"file_urls"`
}

// minerUBatchData 保存批次查询返回的文件处理结果。
type minerUBatchData struct {
	BatchID       string                `json:"batch_id"`
	ExtractResult []minerUExtractResult `json:"extract_result"`
}

// minerUExtractResult 描述单个文件的解析状态和结果压缩包地址。
type minerUExtractResult struct {
	FileName   string `json:"file_name"`
	State      string `json:"state"`
	FullZipURL string `json:"full_zip_url"`
	Error      string `json:"err_msg"`
}

// minerUContentItem 对应 content_list.json 中的一个版面元素。
type minerUContentItem struct {
	Type          string    `json:"type"`
	Text          string    `json:"text"`
	TextLevel     int       `json:"text_level"`
	PageIndex     int       `json:"page_idx"`
	BBox          []float64 `json:"bbox"`
	ImagePath     string    `json:"img_path"`
	EquationPath  string    `json:"equation_img_path"`
	TableBody     string    `json:"table_body"`
	ImageCaption  []string  `json:"image_caption"`
	ImageFootnote []string  `json:"image_footnote"`
	TableCaption  []string  `json:"table_caption"`
	TableFootnote []string  `json:"table_footnote"`
	ChartCaption  []string  `json:"chart_caption"`
	ChartFootnote []string  `json:"chart_footnote"`
	CodeBody      string    `json:"code_body"`
	CodeCaption   []string  `json:"code_caption"`
	ListItems     []string  `json:"list_items"`
}

// newMinerUClient 根据配置创建客户端；未启用时返回 nil。
func newMinerUClient(cfg config.MinerUConfig) *minerUClient {
	if !cfg.Enabled {
		return nil
	}
	pollEvery := time.Duration(cfg.PollIntervalMs) * time.Millisecond
	if pollEvery <= 0 {
		pollEvery = 3 * time.Second
	}
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	return &minerUClient{
		baseURL:   strings.TrimRight(strings.TrimSpace(cfg.APIURL), "/"),
		token:     strings.TrimSpace(cfg.Token),
		model:     strings.TrimSpace(cfg.ModelVersion),
		language:  strings.TrimSpace(cfg.Language),
		ocr:       cfg.OCR,
		formula:   cfg.EnableFormula,
		table:     cfg.EnableTable,
		pollEvery: pollEvery,
		timeout:   timeout,
		http:      &http.Client{Timeout: 2 * time.Minute},
	}
}

// Parse 上传单个文件，轮询解析结果并读取结构化 ZIP。
func (c *minerUClient) Parse(ctx context.Context, filename string, data []byte) ([]parseAtom, error) {
	if c == nil {
		return nil, fmt.Errorf("MinerU 未启用")
	}
	if c.baseURL == "" || c.token == "" {
		return nil, fmt.Errorf("%w: MinerU API 地址或 Token 未配置", errPermanent)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: 待解析文件为空", errPermanent)
	}
	if len(data) > minerUMaxInputBytes {
		return nil, fmt.Errorf("%w: MinerU 在线 API 单文件不能超过 200MB", errPermanent)
	}
	// TODO: MinerU 单文件最多解析 200 页；PDF 超限时需分段提交，并按原始页序合并解析结果。
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	batchID, uploadURL, err := c.createUpload(ctx, filename)
	if err != nil {
		return nil, err
	}
	if err := c.upload(ctx, uploadURL, data); err != nil {
		return nil, err
	}
	zipURL, err := c.waitResult(ctx, batchID)
	if err != nil {
		return nil, err
	}
	archive, err := c.download(ctx, zipURL)
	if err != nil {
		return nil, err
	}
	atoms, err := parseMinerUArchive(archive, c.model)
	if err != nil {
		return nil, fmt.Errorf("%w: MinerU 结果解析失败: %v", errPermanent, err)
	}
	return atoms, nil
}

// createUpload 申请单文件上传地址；上传完成后 MinerU 会自动提交批次任务。
func (c *minerUClient) createUpload(ctx context.Context, filename string) (string, string, error) {
	payload := map[string]any{
		"files":          []map[string]any{{"name": filepath.Base(filename), "is_ocr": c.ocr}},
		"model_version":  c.model,
		"language":       c.language,
		"enable_formula": c.formula,
		"enable_table":   c.table,
	}
	var result minerUEnvelope[minerUSubmitData]
	if err := c.requestJSON(ctx, http.MethodPost, "/api/v4/file-urls/batch", payload, &result); err != nil {
		return "", "", err
	}
	if !result.Code.ok() {
		return "", "", fmt.Errorf("MinerU 申请上传地址失败[%s]: %s (trace_id=%s)", result.Code, result.Message, result.TraceID)
	}
	if result.Data.BatchID == "" || len(result.Data.FileURLs) != 1 {
		return "", "", fmt.Errorf("MinerU 返回的上传地址不完整")
	}
	return result.Data.BatchID, result.Data.FileURLs[0], nil
}

// upload 按官方协议 PUT 文件；预签名地址不附加 Content-Type 和 Authorization。
func (c *minerUClient) upload(ctx context.Context, url string, data []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("构造 MinerU 上传请求失败: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("上传文件到 MinerU 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("上传文件到 MinerU 返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// waitResult 轮询批次中的唯一文件，直到完成、失败或上下文超时。
func (c *minerUClient) waitResult(ctx context.Context, batchID string) (string, error) {
	for {
		var result minerUEnvelope[minerUBatchData]
		endpoint := "/api/v4/extract-results/batch/" + batchID
		if err := c.requestJSON(ctx, http.MethodGet, endpoint, nil, &result); err != nil {
			return "", err
		}
		if !result.Code.ok() {
			return "", fmt.Errorf("MinerU 查询任务失败[%s]: %s (trace_id=%s)", result.Code, result.Message, result.TraceID)
		}
		if len(result.Data.ExtractResult) > 0 {
			item := result.Data.ExtractResult[0]
			switch item.State {
			case "done":
				if item.FullZipURL == "" {
					return "", fmt.Errorf("MinerU 任务完成但未返回结果地址")
				}
				return item.FullZipURL, nil
			case "failed":
				return "", fmt.Errorf("%w: MinerU 解析失败: %s", errPermanent, item.Error)
			}
		}
		timer := time.NewTimer(c.pollEvery)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", fmt.Errorf("MinerU 解析等待超时: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

// download 下载解析结果包并限制最大体积。
func (c *minerUClient) download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("构造 MinerU 结果下载请求失败: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载 MinerU 结果失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载 MinerU 结果返回 %d", resp.StatusCode)
	}
	// TODO: 当前整个 ZIP 一次性读入内存（上限 512MB），单个解压文件限制 64MB，
	// 且 ZIP 内被引用的图片也会读入内存；后续应流式写入临时文件并增量解析，
	// 避免 200 页图片型 PDF 的压缩包、结构化内容和图片资源同时驻留内存。
	data, err := io.ReadAll(io.LimitReader(resp.Body, minerUMaxArchiveBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取 MinerU 结果失败: %w", err)
	}
	if len(data) > minerUMaxArchiveBytes {
		return nil, fmt.Errorf("MinerU 结果包超过 512MB 限制")
	}
	return data, nil
}

// requestJSON 发送带 Token 的 MinerU JSON 请求。
func (c *minerUClient) requestJSON(ctx context.Context, method, endpoint string, payload any, out any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("序列化 MinerU 请求失败: %w", err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+endpoint, body)
	if err != nil {
		return fmt.Errorf("构造 MinerU 请求失败: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("调用 MinerU 失败: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("读取 MinerU 响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("MinerU 返回 %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("解析 MinerU 响应失败: %w", err)
	}
	return nil
}

// parseMinerUArchive 优先读取 content_list，缺失时退化为 full.md。
func parseMinerUArchive(data []byte, parserVersion string) ([]parseAtom, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("无效 ZIP: %w", err)
	}
	files := make(map[string]*zip.File, len(zr.File))
	var contentList, markdown *zip.File
	for _, file := range zr.File {
		name, ok := safeMinerUPath(file.Name)
		if !ok || file.FileInfo().IsDir() {
			continue
		}
		files[name] = file
		base := strings.ToLower(path.Base(name))
		if strings.HasSuffix(base, "_content_list.json") || base == "content_list.json" {
			contentList = file
		}
		if base == "full.md" || base == "markdown.md" {
			markdown = file
		}
	}
	if contentList != nil {
		raw, err := readMinerUZipFile(contentList)
		if err != nil {
			return nil, err
		}
		var items []minerUContentItem
		if err := json.Unmarshal(raw, &items); err != nil {
			return nil, fmt.Errorf("解析 content_list 失败: %w", err)
		}
		atoms := minerUItemsToAtoms(items, files, "mineru-"+parserVersion)
		if len(atoms) > 0 {
			return atoms, nil
		}
	}
	if markdown != nil {
		raw, err := readMinerUZipFile(markdown)
		if err != nil {
			return nil, err
		}
		text := strings.TrimSpace(string(raw))
		if text != "" {
			return []parseAtom{{Text: text, ElementType: "text", Parser: "mineru-" + parserVersion}}, nil
		}
	}
	return nil, fmt.Errorf("结果包中没有可用的 content_list 或 Markdown")
}

// minerUItemsToAtoms 按源顺序把 MinerU 内容列表转换为正文、表格、公式和图片原子。
func minerUItemsToAtoms(items []minerUContentItem, files map[string]*zip.File, parser string) []parseAtom {
	var atoms []parseAtom
	for _, item := range items {
		typ := strings.ToLower(strings.TrimSpace(item.Type))
		base := parseAtom{ElementType: typ, Page: item.PageIndex + 1, BBox: item.BBox, Parser: parser}
		switch typ {
		case "text", "title", "paragraph", "section_header":
			base.Text = strings.TrimSpace(item.Text)
			if item.TextLevel > 0 && item.TextLevel <= 6 && base.Text != "" {
				base.ElementType = "title"
				base.Text = strings.Repeat("#", item.TextLevel) + " " + base.Text
			}
		case "table":
			base.Table = true
			base.Text = joinMinerUText(item.TableCaption, []string{item.TableBody}, item.TableFootnote)
		case "equation", "formula", "interline_equation":
			base.ElementType = "formula"
			base.Text = formatMinerUFormula(item.Text)
			imagePath := item.EquationPath
			if imagePath == "" {
				imagePath = item.ImagePath
			}
			if file := findMinerUAsset(files, imagePath); file != nil {
				base.Image, _ = readMinerUZipFile(file)
				base.Ext = strings.ToLower(path.Ext(file.Name))
			}
		case "image", "chart":
			captions, footnotes := item.ImageCaption, item.ImageFootnote
			if typ == "chart" {
				captions, footnotes = item.ChartCaption, item.ChartFootnote
			}
			base.Text = joinMinerUText(captions, footnotes)
			if file := findMinerUAsset(files, item.ImagePath); file != nil {
				base.Image, _ = readMinerUZipFile(file)
				base.Ext = strings.ToLower(path.Ext(file.Name))
			}
		case "code":
			base.Text = strings.TrimSpace(item.CodeBody)
			if base.Text != "" {
				base.Text = "```\n" + base.Text + "\n```"
			}
		case "list":
			base.Text = strings.Join(item.ListItems, "\n")
		case "header", "footer", "page_number", "discarded":
			continue
		default:
			base.Text = strings.TrimSpace(item.Text)
		}
		if base.Text != "" || len(base.Image) > 0 {
			atoms = append(atoms, base)
		}
	}
	return atoms
}

// findMinerUAsset 根据完整路径或文件名定位结果包中的图片资源。
func findMinerUAsset(files map[string]*zip.File, name string) *zip.File {
	clean, ok := safeMinerUPath(name)
	if !ok || clean == "" {
		return nil
	}
	if file := files[clean]; file != nil {
		return file
	}
	for pathName, file := range files {
		if strings.HasSuffix(pathName, "/"+clean) || path.Base(pathName) == path.Base(clean) {
			return file
		}
	}
	return nil
}

// readMinerUZipFile 限量读取 ZIP 单个条目，避免异常结果包占满内存。
func readMinerUZipFile(file *zip.File) ([]byte, error) {
	if file.UncompressedSize64 > minerUMaxEntryBytes {
		return nil, fmt.Errorf("结果文件 %s 超过 64MB 限制", file.Name)
	}
	rc, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("打开结果文件 %s 失败: %w", file.Name, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, minerUMaxEntryBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取结果文件 %s 失败: %w", file.Name, err)
	}
	if len(data) > minerUMaxEntryBytes {
		return nil, fmt.Errorf("结果文件 %s 超过 64MB 限制", file.Name)
	}
	return data, nil
}

// safeMinerUPath 规范化 ZIP 内路径并拒绝绝对路径和上级目录跳转。
func safeMinerUPath(name string) (string, bool) {
	name = strings.ReplaceAll(name, "\\", "/")
	if strings.HasPrefix(name, "/") {
		return "", false
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", false
		}
	}
	clean := strings.TrimPrefix(path.Clean("/"+name), "/")
	if clean == "" || clean == "." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

// joinMinerUText 按顺序拼接标题、正文和脚注，过滤空字符串。
func joinMinerUText(groups ...[]string) string {
	var parts []string
	for _, group := range groups {
		for _, value := range group {
			if value = strings.TrimSpace(value); value != "" {
				parts = append(parts, value)
			}
		}
	}
	return strings.Join(parts, "\n")
}

// formatMinerUFormula 把未带定界符的公式包装为 Markdown 块公式。
func formatMinerUFormula(text string) string {
	text = strings.TrimSpace(text)
	if text == "" || strings.HasPrefix(text, "$") {
		return text
	}
	return "$$\n" + text + "\n$$"
}
