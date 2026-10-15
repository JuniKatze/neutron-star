package llm

import (
	"errors"
	"fmt"
	"mime"
	"net/url"
	"strings"
)

// SourceType 表示媒体或文件内容的寻址方式。
type SourceType string

const (
	// SourceData 表示直接携带原始字节。
	SourceData SourceType = "data"

	// SourceURL 表示通过绝对 HTTP(S) URL 获取内容。
	SourceURL SourceType = "url"

	// SourceAsset 表示应用资源库中的资源引用。
	SourceAsset SourceType = "asset"

	// SourceProvider 表示已经存储在供应商端的文件引用。
	SourceProvider SourceType = "provider"
)

// Source 描述媒体或文件内容的位置。
// Type 决定唯一有效的来源字段，其余来源字段必须未设置。
// Source 的零值无效。
//
// Source 不负责获取资源。调用 Validate 或 Clone 时，
// 调用放必须保证没有其他 goroutine 并发修改其字段或引用的数据。
type Source struct {
	// Type 指定来源分支。
	Type SourceType `json:"type"`

	// Data 保存 SourceData 分支的原始字节，且不得为空。
	// 其他分支的 Data 必须为 nil。
	Data []byte `json:"data,omitempty"`

	// URL 保存 SourceURL 分支的绝对 HTTP(S) URL，且不得包含用户信息。
	// 凭证不允许出现在 URL 里：它会被存档，也会被原样发给供应商。
	URL string `json:"url,omitempty"`

	// AssetID 保存 SourceAsset 分支的非空资源标识。
	// 它是应用资源库的标识，不应该直接作为本机文件路径使用。
	AssetID string `json:"assetId,omitempty"`

	// Provider 保存 SourceProvider 分支的供应商文件引用。
	Provider *ProviderFile `json:"provider,omitempty"`
}

// Validate 不修改 s，不获取资源，也不检查网络访问权限。
func (s Source) Validate() error {
	switch s.Type {
	case SourceData:
		if len(s.Data) == 0 {
			return errors.New("data: must not be empty")
		}

	case SourceURL:
		u, err := url.Parse(s.URL)
		if err != nil {
			return fmt.Errorf("url: invalid URL: %w", err)
		}

		schema := strings.ToLower(u.Scheme)
		if !u.IsAbs() ||
			u.Hostname() == "" ||
			u.Opaque != "" ||
			(schema != "http" && schema != "https") {
			return errors.New(
				"url: must be an absolute HTTP(S) URL with a host",
			)
		}

		if u.User != nil {
			return errors.New(
				"url: must not contain user information",
			)
		}

	case SourceAsset:
		if strings.TrimSpace(s.AssetID) == "" {
			return errors.New("assetId: required")
		}

	case SourceProvider:
		if err := s.Provider.Validate(); err != nil {
			return fmt.Errorf("provider: %w", err)
		}

	default:
		return fmt.Errorf("type: unknown source type %q", s.Type)
	}

	if s.Type != SourceData && s.Data != nil {
		return errors.New("data: incompatible with source type")
	}

	if s.Type != SourceURL && s.URL != "" {
		return errors.New("url: incompatible with source type")
	}

	if s.Type != SourceAsset && s.AssetID != "" {
		return errors.New("assetId: incompatible with source type")
	}

	if s.Type != SourceProvider && s.Provider != nil {
		return errors.New("provider: incompatible with source type")
	}

	return nil
}

// Clone 返回 s 的深拷贝。
// 返回值不与 s 共享 Data 的底层数组或 Provider 对象。
// Clone 保留 nil 与非 nil 空切片的区别，不校验或修复来源字段。
func (s Source) Clone() Source {
	out := s

	if s.Data != nil {
		out.Data = make([]byte, len(s.Data))
		copy(out.Data, s.Data)
	}

	out.Provider = s.Provider.Clone()

	return out
}

// ProviderFile 描述一个供应商端文件及其所属连接。
// 它不携带文件内容、凭证或模型标识。
// 文件是否存在、连接是否有权访问，由适配器或资源获取层检查。
// ProviderFile 的零值无效。
type ProviderFile struct {
	// Adapter 是供应商适配器的稳定标识。
	Adapter string `json:"adapter"`

	// Connection 是应用配置中连接的稳定标识，不是API key。
	Connection string `json:"connection"`

	// ID 是供应商返回的文件标识。
	ID string `json:"id"`
}

// Validate 检查 p 的适配器、连接和文件标识是否非空。
// p 为 nil 时返回错误。
//
// Validate 不修改标识，也不检查适配器是否注册或文件是否存在。
func (p *ProviderFile) Validate() error {
	if p == nil {
		return errors.New("provider file is nil")
	}

	if strings.TrimSpace(p.Adapter) == "" {
		return errors.New("adapter: required")
	}

	if strings.TrimSpace(p.Connection) == "" {
		return errors.New("connection: required")
	}

	if strings.TrimSpace(p.ID) == "" {
		return errors.New("id: required")
	}

	return nil
}

// Clone 返回 p 的独立副本。
// p 为 nil 时返回 nil。Clone 不检验字段。
func (p *ProviderFile) Clone() *ProviderFile {
	if p == nil {
		return nil
	}

	out := *p

	return &out
}

// Image 描述一份图片内容及其声明的媒体类型。
// Image 的零值无效。
type Image struct {
	// Source 指定图片内容的位置。
	Source Source `json:"source"`

	// MIMEType 必须是具体的 image/* 媒体类型。
	// 星号表示此处描述的类型类别，不允许作为字段中的通配符。
	MIMEType string `json:"mimeType"`
}

// Validate 检查 i 的来源及图片媒体类型。
//
// i 为 nil 时返回错误。它不修改 i，不读取图片字节，不探测其真实格式，
// 也不判断目标模型的图片能力或大小限制。
func (i *Image) Validate() error {
	if i == nil {
		return errors.New("image is nil")
	}

	if err := i.Source.Validate(); err != nil {
		return fmt.Errorf("source: %w", err)
	}

	return validateMIME(i.MIMEType, "image/")
}

// Clone 返回 i 的深拷贝。
// i 为 nil 时返回 nil。Clone 不校验图片内容或字段。
func (i *Image) Clone() *Image {
	if i == nil {
		return nil
	}

	out := *i
	out.Source = i.Source.Clone()
	return &out
}

// Audio 描述一份音频内容及其声明的媒体类型。
// Audio 的零值无效。
type Audio struct {
	// Source 指定音频内容的位置。
	Source Source `json:"source"`

	// MIMEType 必须是具体的 audio 类媒体类型，例如 audio/wav。
	MIMEType string `json:"mimeType"`
}

// Validate 检查 a 的来源及音频媒体类型。
// a 为 nil 时返回错误。Validate 不修改 a，也不解码音频目标。
func (a *Audio) Validate() error {
	if a == nil {
		return errors.New("audio is nil")
	}
	if err := a.Source.Validate(); err != nil {
		return fmt.Errorf("source: %w", err)
	}

	return validateMIME(a.MIMEType, "audio/")
}

// Clone 返回 a 的深拷贝。
// a 为 nil 时返回 nil。Clone 不校验音频内容或字段。
func (a *Audio) Clone() *Audio {
	if a == nil {
		return nil
	}

	out := *a
	out.Source = a.Source.Clone()
	return &out
}

// Video 描述一份视频内容及其声明的媒体类型。
// Video 的零值无效。
type Video struct {
	// Source 指定视频内容的位置。
	Source Source `json:"source"`

	// MIMEType 必须是具体的 video 类媒体类型，例如 video/mp4。
	MIMEType string `json:"mimeType"`
}

// Validate 检查 v 的来源及视频媒体类型。
// v 为 nil 时返回错误。Validate 不修改 v，也不解码视频内容。
func (v *Video) Validate() error {
	if v == nil {
		return errors.New("video is nil")
	}
	if err := v.Source.Validate(); err != nil {
		return fmt.Errorf("source: %w", err)
	}

	return validateMIME(v.MIMEType, "video/")
}

// Clone 返回 v 的深拷贝。
// v 为 nil 时返回 nil。Clone 不校验视频内容或字段。
func (v *Video) Clone() *Video {
	if v == nil {
		return nil
	}

	out := *v
	out.Source = v.Source.Clone()
	return &out
}

// Document 描述拟作为文档提供给模型的内容。
// 声明为 Document 不保证目标供应商能够原生读取它；
// 具体媒体类型支持由适配器检查。
// Document 的零值无效。
type Document struct {
	// Source 指定文档内容的位置。
	Source Source `json:"source"`

	// MIMEType 是具体的媒体类型，例如 application/pdf 或 text/plain。
	MIMEType string `json:"mimeType"`

	// Name 是可选的文件名提示，不用于定位本机文件。
	Name string `json:"name,omitempty"`

	// Title 是可选的文档标题。
	Title string `json:"title,omitempty"`

	// Context 是随文档提供的补充说明。
	Context string `json:"context,omitempty"`

	// Citations 控制是否请求文档引用功能。
	// nil 表示未指定，false 表示明确关闭，true 表示明确开启。
	// 目标供应商是否支持该选项，由适配器检查。
	Citations *bool `json:"citations,omitempty"`
}

// Validate 检查 d 的来源及媒体类型。
// d 为 nil 时返回错误。
//
// Validate 不修改 d，不读取文档，也不判断目标模型的文档支持能力。
func (d *Document) Validate() error {
	if d == nil {
		return errors.New("document is nil")
	}
	if err := d.Source.Validate(); err != nil {
		return fmt.Errorf("source: %w", err)
	}

	return validateMIME(d.MIMEType, "")
}

// Clone 返回 d 的深拷贝。
// 返回值不与 d 共享来源数据或 Citations 指针。
// d 为 nil 时返回 nil。Clone 不校验字段。
func (d *Document) Clone() *Document {
	if d == nil {
		return nil
	}

	out := *d
	out.Source = d.Source.Clone()

	if d.Citations != nil {
		value := *d.Citations
		out.Citations = &value
	}

	return &out
}

// File 描述供应用或工具操作的普通文件引用。
// File 不保证内容可被目标模型原生理解。
// File 的零值无效。
type File struct {
	// Source 指定文件内容的位置。
	Source Source `json:"source"`

	// MIMEType 是可选的具体媒体类型。
	// 空字符串表示未知或未指定。
	MIMEType string `json:"mimeType,omitempty"`

	// Name 是可选的文件名提示，不用于定位本机文件。
	Name string `json:"name,omitempty"`
}

// Validate 检查 f 的来源，以及已设置的媒体类型。
// f 为 nil 时返回错误。空 MIMEType 被视为未指定。
//
// Validate 不修改 f，也不读取或探测文件内容。
func (f *File) Validate() error {
	if f == nil {
		return errors.New("file is nil")
	}
	if err := f.Source.Validate(); err != nil {
		return fmt.Errorf("source: %w", err)
	}
	if f.MIMEType == "" {
		return nil
	}

	return validateMIME(f.MIMEType, "")
}

// Clone 返回 f 的深拷贝。
// f 为 nil 时返回 nil。Clone 不校验文件内容或字段。
func (f *File) Clone() *File {
	if f == nil {
		return nil
	}

	out := *f
	out.Source = f.Source.Clone()
	return &out
}

// validateMIME 检查 value 是否为具体的 type/subtype 媒体类型。
// 非空 prefix 进一步限制媒体类型类别。
// 合法参数被接受，但本函数不修改或规范化原始字段。
func validateMIME(value, prefix string) error {
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return fmt.Errorf("mimeType: invalid media type: %w", err)
	}

	typeName, subtype, ok := strings.Cut(mediaType, "/")
	if !ok || typeName == "" || subtype == "" ||
		strings.Contains(mediaType, "*") {
		return errors.New(
			"mimeType: expected a concrete type/subtype media type",
		)
	}

	if prefix != "" && !strings.HasPrefix(mediaType, prefix) {
		return fmt.Errorf(
			"mimeType: expected media type with prefix %q",
			prefix,
		)
	}

	return nil
}
