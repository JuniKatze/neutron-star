package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"

	"neutron-star/internal/llm"
	"neutron-star/internal/llm/chatcompletions"
)

// 本文件把 internal/llm 的适配器接到前端。
//
// 一次生成的形状是：前端调 Send，Go 这边立刻返回，内容通过事件流出
// （chat:delta / chat:done / chat:error）。之所以不用"调用返回最终文本"的
// 形状，是因为那要等整段生成结束，界面只能干等；流式预览本来就是适配器
// 给的能力。
//
// 这一层只做搬运：拼一次请求、把增量转成事件、把结局转成事件。
// 工具执行、会话持久化、权限判断都不在这里。

// 事件名。前端按这些名字订阅。
const (
	chatEventDelta = "chat:delta"
	chatEventDone  = "chat:done"
	chatEventError = "chat:error"
)

// init 登记自定义事件的载荷类型。
//
// 两个作用：绑定生成器据此给前端生成类型；运行时发事件时会按登记的
// 类型检查载荷，发错形状会被立刻发现，而不是在前端静默变成 undefined。
func init() {
	application.RegisterEvent[ChatDelta](chatEventDelta)
	application.RegisterEvent[ChatOutcome](chatEventDone)
	application.RegisterEvent[ChatOutcome](chatEventError)
}

// 环境变量名。它们覆盖配置文件里的同名字段，方便临时换端点。
const (
	envEndpoint = "NEUTRON_ENDPOINT"
	envAPIKey   = "NEUTRON_API_KEY"
	envModel    = "NEUTRON_MODEL"
)

// modelConfig 是一个模型连接的配置。
//
// 它放在用户配置目录里，不放仓库：API key 不属于代码。
// 文件不存在时服务照常启动，只是报"未配置"并留一份模板。
type modelConfig struct {
	// Endpoint 是完整的 Chat Completions 地址，必须带上路径。
	Endpoint string `json:"endpoint"`

	// APIKey 为空时不发送 Authorization 头。
	APIKey string `json:"apiKey"`

	// Model 是目标模型名。
	Model string `json:"model"`

	// IncludeUsage 请求服务在流结束前返回 token 统计。
	// 不是所有兼容端点都认 stream_options，默认关闭。
	IncludeUsage bool `json:"includeUsage"`
}

// ChatDelta 是一项流式增量。
// Text 是文本、推理或拒绝内容的新增部分；工具调用没有文本，只有 ToolName。
type ChatDelta struct {
	// PartIndex 是这段内容在最终消息里的位置。
	PartIndex int `json:"partIndex"`

	// Kind 取 text、reasoning、refusal 或 tool_call。
	Kind string `json:"kind"`

	// Text 是本项新增的文本。
	Text string `json:"text,omitempty"`

	// ToolName 是工具调用声明的工具名。
	ToolName string `json:"toolName,omitempty"`
}

// ChatOutcome 报告一次生成的结局。
// Error 非空表示这次生成失败，此时 Text 只包含失败前收到的预览。
type ChatOutcome struct {
	// Text 是完整回答，只包含正文，不含推理与工具参数。
	Text string `json:"text,omitempty"`

	// StopReason 是归一化后的停止原因。
	StopReason string `json:"stopReason,omitempty"`

	// Error 是失败说明。
	Error string `json:"error,omitempty"`

	// Canceled 表示这次生成是被用户停止的。
	// 它和"失败"走同一条事件，但界面据此显示"已停止"而不是报错。
	Canceled bool `json:"canceled,omitempty"`
}

// ChatTurn 是历史对话里的一轮正文。
//
// 前端每次发送都重新带上完整历史，服务端不保存会话：界面清空、
// 应用重启、开第二个窗口，都不会出现两边历史对不上的情况。
type ChatTurn struct {
	// Role 取 user 或 assistant。
	Role string `json:"role"`

	// Text 是这一轮的正文；空文本会被忽略。
	Text string `json:"text"`
}

// ChatStatus 报告连接是否可用、是否正在生成。
type ChatStatus struct {
	// Configured 表示适配器已经建好，可以发请求。
	Configured bool `json:"configured"`

	// Running 表示当前有一次生成还没结束。
	Running bool `json:"running"`

	// Model 与 Endpoint 是当前生效的配置，便于界面显示。
	Model    string `json:"model,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`

	// ConfigPath 是配置文件的位置，未配置时界面把它显示给用户。
	ConfigPath string `json:"configPath,omitempty"`

	// Problem 说明为什么还不能用；为空表示配置正常。
	Problem string `json:"problem,omitempty"`
}

// ChatService 是绑定给前端的模型对话服务。
type ChatService struct {
	// app 只在生产环境用。
	app *application.App

	// emitFn 发送一个事件。它被单独抽出来，是为了让这层逻辑能在没有 GUI
	// 的测试里跑：测试塞一个收集器，生产环境塞 app.Event.Emit。
	emitFn func(name string, data any)

	// configPath 是配置文件位置，取出失败时为空。
	configPath string

	// mu 保护以下在调用之间变化的字段。
	mu       sync.Mutex
	client   llm.LLM
	model    string
	endpoint string
	problem  string
	cancel   context.CancelFunc
}

// newChatService 返回尚未加载配置的服务实例。
// main 用它注册服务；测试用它注入自己的 emit 与适配器。
func newChatService() *ChatService {
	return &ChatService{configPath: defaultConfigPath()}
}

// defaultConfigPath 返回配置文件的路径。
// 取不到系统配置目录时返回空字符串，此时只能靠环境变量。
func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}

	return filepath.Join(dir, "neutron-star", "model.json")
}

// ServiceStartup 在应用启动时读取配置。
//
// 配置有问题不让应用启动失败：界面需要能打开，好把问题显示给用户。
func (s *ChatService) ServiceStartup(
	ctx context.Context,
	options application.ServiceOptions,
) error {
	if s.app == nil {
		s.app = application.Get()
	}
	if s.emitFn == nil && s.app != nil {
		app := s.app
		s.emitFn = func(name string, data any) {
			app.Event.Emit(name, data)
		}
	}

	s.loadConfig()

	if s.currentProblem() != "" {
		slog.Warn(
			"chat: 模型未就绪",
			"problem", s.currentProblem(),
			"config", s.configPath,
		)
	}

	return nil
}

// ServiceShutdown 取消还在跑的生成。
func (s *ChatService) ServiceShutdown() error {
	s.Stop()

	return nil
}

// Status 报告连接是否可用、是否正在生成。
// 前端挂载时调一次，用来决定显示输入框还是配置提示。
func (s *ChatService) Status() ChatStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	return ChatStatus{
		Configured: s.client != nil,
		Running:    s.cancel != nil,
		Model:      s.model,
		Endpoint:   s.endpoint,
		ConfigPath: s.configPath,
		Problem:    s.problem,
	}
}

// Send 开始一次生成，内容通过事件流出。
//
// history 是界面里已有的对话，由前端每次发送时带上；本服务不保存会话，
// 因此"上一轮说了什么"完全取自前端，服务重启也不会丢上下文。
//
// 它在启动之前就完成请求构造与校验，所以"输入为空"这类问题会同步返回；
// 一旦返回 nil，后面的结果只通过事件报告。
func (s *ChatService) Send(prompt string, history []ChatTurn) error {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return errors.New("请输入内容")
	}

	messages, err := buildMessages(history, prompt)
	if err != nil {
		return err
	}

	s.mu.Lock()

	if s.client == nil {
		problem := s.problem
		s.mu.Unlock()

		if problem == "" {
			problem = "模型未配置"
		}

		return errors.New(problem)
	}
	if s.cancel != nil {
		s.mu.Unlock()

		return errors.New("上一次生成还没结束")
	}

	request := &llm.Request{
		Model:    s.model,
		Messages: messages,
	}
	if err := request.Validate(); err != nil {
		s.mu.Unlock()

		return err
	}

	client := s.client

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.mu.Unlock()

	go s.run(ctx, client, request)

	return nil
}

// Stop 取消正在跑的生成；没有在跑时什么也不做。
func (s *ChatService) Stop() {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// Reload 重新读取配置文件并返回新状态。
//
// 界面上的"重新加载"按钮用它，省得为了改一行配置重启应用；
// 正在跑的生成用的是取消前拿到的适配器，因此不受影响。
func (s *ChatService) Reload() ChatStatus {
	s.loadConfig()

	return s.Status()
}

// run 消费整条流，把增量与结局推给前端。
//
// ctx 取消时适配器会以取消错误结束，因此提前停止也走 chat:error，
// 界面据 Canceled 字段把"用户主动停止"和"真的失败"区分开。
func (s *ChatService) run(
	ctx context.Context,
	client llm.LLM,
	request *llm.Request,
) {
	defer func() {
		s.mu.Lock()
		s.cancel = nil
		s.mu.Unlock()
	}()

	var preview strings.Builder

	final, err := llm.Consume(
		client.Generate(ctx, request),
		func(delta *llm.PartDelta) error {
			// 预览单独累计一份：生成失败时界面至少能保住已经收到的内容。
			if delta.Kind == llm.PartText {
				preview.WriteString(delta.Text)
			}

			s.emitDelta(delta)

			return nil
		},
	)
	if err != nil {
		s.emit(chatEventError, ChatOutcome{
			Text:     preview.String(),
			Error:    err.Error(),
			Canceled: canceledByContext(ctx, err),
		})

		return
	}

	s.emit(chatEventDone, ChatOutcome{
		Text:       messageText(final.Message),
		StopReason: string(final.StopReason),
	})
}

// buildMessages 把前端历史拼成一次请求的上下文。
//
// 历史只承载正文：推理、工具调用与提示信息都不回放，因为它们要么不可
// 重放，要么对下一轮没有意义。空白轮次直接丢弃，避免把"空回复"塞进
// 上下文——模型对这种空 assistant 消息的反应通常比直接省略更差。
func buildMessages(history []ChatTurn, prompt string) ([]llm.Message, error) {
	messages := make([]llm.Message, 0, len(history)+1)

	for index := range history {
		turn := &history[index]

		text := strings.TrimSpace(turn.Text)
		if text == "" {
			continue
		}

		var role llm.Role

		switch strings.TrimSpace(turn.Role) {
		case string(llm.RoleUser):
			role = llm.RoleUser

		case string(llm.RoleAssistant):
			role = llm.RoleAssistant

		default:
			return nil, fmt.Errorf(
				"history[%d]: 未知角色 %q",
				index,
				turn.Role,
			)
		}

		messages = append(messages, llm.Message{
			Role:  role,
			Parts: []llm.Part{{Kind: llm.PartText, Text: text}},
		})
	}

	messages = append(messages, llm.Message{
		Role:  llm.RoleUser,
		Parts: []llm.Part{{Kind: llm.PartText, Text: prompt}},
	})

	return messages, nil
}

// canceledByContext 判断这次失败是不是用户主动停止造成的。
//
// 适配器会把取消归类为 llm.ErrorCanceled，但停止也可能落在两次事件
// 之间：此时错误可能来自传输层，而 ctx 已经取消，所以两条都看。
func canceledByContext(ctx context.Context, err error) bool {
	var llmErr *llm.Error
	if errors.As(err, &llmErr) && llmErr.Kind == llm.ErrorCanceled {
		return true
	}

	return errors.Is(ctx.Err(), context.Canceled)
}

// emitDelta 把一项增量翻译成事件载荷。
func (s *ChatService) emitDelta(delta *llm.PartDelta) {
	payload := ChatDelta{
		PartIndex: delta.PartIndex,
		Kind:      string(delta.Kind),
		Text:      delta.Text,
	}

	if delta.ToolCall != nil {
		payload.ToolName = delta.ToolCall.Name
	}

	s.emit(chatEventDelta, payload)
}

// emit 发送一个事件；没有发送通道时静默丢弃。
func (s *ChatService) emit(name string, data any) {
	if s.emitFn == nil {
		return
	}

	s.emitFn(name, data)
}

// loadConfig 读取配置并构造适配器。
//
// 它不返回错误：配置缺失不是启动失败，问题记在 problem 里由 Status
// 报给界面，用户改完文件重启即可。
func (s *ChatService) loadConfig() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.client = nil
	s.model = ""
	s.endpoint = ""
	s.problem = ""

	config := modelConfig{}
	missing := false

	if s.configPath == "" {
		s.problem = "找不到用户配置目录，只能靠环境变量"
	} else {
		data, err := os.ReadFile(s.configPath)

		switch {
		case err == nil:
			if err := json.Unmarshal(data, &config); err != nil {
				s.problem = fmt.Sprintf(
					"配置文件不是合法 JSON：%v",
					err,
				)

				return
			}

		case errors.Is(err, os.ErrNotExist):
			missing = true

		default:
			s.problem = fmt.Sprintf("读不了配置文件：%v", err)

			return
		}
	}

	applyEnv(&config)

	s.endpoint = strings.TrimSpace(config.Endpoint)
	s.model = strings.TrimSpace(config.Model)

	switch {
	case s.endpoint == "":
		s.problem = "配置里还没写 endpoint"

	case s.model == "":
		s.problem = "配置里还没写 model"
	}

	if s.problem != "" {
		// 第一次运行时留一份模板，省得用户猜路径和字段名。
		if missing {
			if err := writeConfigTemplate(s.configPath); err != nil {
				s.problem += fmt.Sprintf(
					"；配置文件不存在，模板也没写成：%v",
					err,
				)
			} else {
				s.problem += "；已生成配置文件模板"
			}
		}

		return
	}

	client, err := chatcompletions.New(chatcompletions.Options{
		Endpoint:     s.endpoint,
		APIKey:       strings.TrimSpace(config.APIKey),
		IncludeUsage: config.IncludeUsage,
		Logger:       slog.Default(),
	})
	if err != nil {
		s.problem = fmt.Sprintf("配置不能用：%v", err)

		return
	}

	s.client = client
}

// currentProblem 返回当前配置问题；为空表示配置正常。
// 它不导出：配置问题的对外出口只有 Status，绑到前端的方法越少越好。
func (s *ChatService) currentProblem() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.problem
}

// applyEnv 用环境变量覆盖文件里的配置，方便临时换端点。
func applyEnv(config *modelConfig) {
	if value := strings.TrimSpace(os.Getenv(envEndpoint)); value != "" {
		config.Endpoint = value
	}
	if value := strings.TrimSpace(os.Getenv(envAPIKey)); value != "" {
		config.APIKey = value
	}
	if value := strings.TrimSpace(os.Getenv(envModel)); value != "" {
		config.Model = value
	}
}

// writeConfigTemplate 写一份空配置。
// 目录与文件都只给当前用户读写：里面会放 API key。
func writeConfigTemplate(path string) error {
	if path == "" {
		return errors.New("没有配置路径")
	}

	data, err := json.MarshalIndent(modelConfig{}, "", "  ")
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	return os.WriteFile(path, append(data, '\n'), 0o600)
}

// messageText 拼接结果里的全部正文。
// 推理、拒绝与工具调用都不算回答正文，因此不进这里。
func messageText(message llm.Message) string {
	var text strings.Builder

	for index := range message.Parts {
		part := &message.Parts[index]

		if part.Kind == llm.PartText {
			text.WriteString(part.Text)
		}
	}

	return text.String()
}
