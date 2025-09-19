package dify

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// 常量定义
const (
	DefaultBaseURL = "https://api.dify.ai"
	DefaultTimeout = 30 * time.Second
	UserAgent      = "Dify-Go-SDK/1.0"
)

// Client 表示Dify API客户端
type Client struct {
	mu          sync.RWMutex
	httpClient  *http.Client
	baseURL     *url.URL
	apiKey      string
	userAgent   string
	timeout     time.Duration
	retryPolicy RetryPolicy
}

// ClientConfig 客户端配置
type ClientConfig struct {
	APIKey      string
	BaseURL     string
	HTTPClient  *http.Client
	Timeout     time.Duration
	UserAgent   string
	RetryPolicy RetryPolicy
}

// RetryPolicy 重试策略
type RetryPolicy struct {
	MaxRetries int
	Delay      time.Duration
	MaxDelay   time.Duration
}

// DefaultRetryPolicy 默认重试策略
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		MaxRetries: 3,
		Delay:      100 * time.Millisecond,
		MaxDelay:   5 * time.Second,
	}
}

// APIError API错误
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

func (e *APIError) Error() string {
	if e.Details != "" {
		return fmt.Sprintf("Dify API error %d: %s (%s)", e.Code, e.Message, e.Details)
	}
	return fmt.Sprintf("Dify API error %d: %s", e.Code, e.Message)
}

// ChatCompletionRequest 聊天补全请求
type ChatCompletionRequest struct {
	Inputs         map[string]interface{} `json:"inputs"`
	Query          string                 `json:"query"`
	ResponseMode   string                 `json:"response_mode"`
	ConversationID string                 `json:"conversation_id,omitempty"`
	User           string                 `json:"user,omitempty"`
	Files          []File                 `json:"files,omitempty"`
}

// ChatCompletionResponse 聊天补全响应
type ChatCompletionResponse struct {
	ConversationID string `json:"conversation_id"`
	Answer         string `json:"answer"`
	ErrorCode      int    `json:"error_code,omitempty"`
	ErrorMessage   string `json:"message,omitempty"`
}

// ChatCompletionStreamResponse 流式聊天补全响应
type ChatCompletionStreamResponse struct {
	ConversationID string `json:"conversation_id"`
	Answer         string `json:"answer"`
	ErrorCode      int    `json:"error_code,omitempty"`
	ErrorMessage   string `json:"message,omitempty"`
}

// File 文件信息
type File struct {
	Type           string `json:"type"`
	TransferMethod string `json:"transfer_method"`
	UploadFileID   string `json:"upload_file_id,omitempty"`
}

// WorkflowRunRequest 工作流运行请求
type WorkflowRunRequest struct {
	Inputs       map[string]interface{} `json:"inputs"`
	ResponseMode string                 `json:"response_mode"`
	User         string                 `json:"user,omitempty"`
}

// streamReader 泛型流读取器
type streamReader[T any] struct {
	emptyMessagesLimit uint
	isFinished         bool
	reader             *bufio.Reader
	response           *http.Response
	err                error
	unmarshaler        func([]byte) (T, error)
}

// Recv 读取下一个流消息
func (stream *streamReader[T]) Recv() (T, error) {
	var empty T

	if stream.isFinished {
		return empty, io.EOF
	}

	if stream.err != nil {
		return empty, stream.err
	}

	// 读取并处理Server-Sent Events (SSE)
	for {
		var line []byte
		line, stream.err = stream.reader.ReadBytes('\n')
		if stream.err != nil {
			return empty, stream.err
		}

		// 移除末尾的换行符
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}

		// 检查是否为SSE数据行
		if bytes.HasPrefix(line, []byte("data: ")) {
			data := bytes.TrimPrefix(line, []byte("data: "))

			// 检查是否结束
			if string(data) == "[DONE]" {
				stream.isFinished = true
				return empty, io.EOF
			}

			// 解析JSON数据
			var response T
			response, stream.err = stream.unmarshaler(data)
			if stream.err != nil {
				return empty, fmt.Errorf("invalid stream data: %w", stream.err)
			}

			return response, nil
		}
	}
}

// Close 关闭流
func (stream *streamReader[T]) Close() {
	if stream.response != nil {
		stream.response.Body.Close()
	}
}

// ChatCompletionStream 聊天补全流
type ChatCompletionStream struct {
	*streamReader[ChatCompletionStreamResponse]
}

// NewClient 创建新的Dify客户端
func NewClient(config ClientConfig) (*Client, error) {
	if config.APIKey == "" {
		return nil, errors.New("API key is required")
	}

	baseURLStr := config.BaseURL
	if baseURLStr == "" {
		baseURLStr = DefaultBaseURL
	}

	parsedURL, err := url.Parse(baseURLStr)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: DefaultTimeout,
		}
	}

	timeout := config.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	retryPolicy := config.RetryPolicy
	if retryPolicy.MaxRetries == 0 {
		retryPolicy = DefaultRetryPolicy()
	}

	userAgent := config.UserAgent
	if userAgent == "" {
		userAgent = UserAgent
	}

	return &Client{
		httpClient:  httpClient,
		baseURL:     parsedURL,
		apiKey:      config.APIKey,
		userAgent:   userAgent,
		timeout:     timeout,
		retryPolicy: retryPolicy,
	}, nil
}

// SetHTTPClient 设置HTTP客户端
func (c *Client) SetHTTPClient(client *http.Client) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.httpClient = client
}

// SetTimeout 设置超时时间
func (c *Client) SetTimeout(timeout time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.timeout = timeout
}

// SetRetryPolicy 设置重试策略
func (c *Client) SetRetryPolicy(policy RetryPolicy) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.retryPolicy = policy
}

// ChatCompletion 执行聊天补全
func (c *Client) ChatCompletion(ctx context.Context, request ChatCompletionRequest) (*ChatCompletionResponse, error) {
	request.ResponseMode = "blocking"

	endpoint := c.baseURL.ResolveReference(&url.URL{Path: "/v1/chat-messages"})

	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), bytes.NewBuffer(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setRequestHeaders(httpReq)

	var resp *http.Response
	err = c.doWithRetry(func() error {
		var retryErr error
		resp, retryErr = c.httpClient.Do(httpReq)
		return retryErr
	})

	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleAPIError(resp)
	}

	var chatResp ChatCompletionResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if chatResp.ErrorCode != 0 {
		return nil, &APIError{
			Code:    chatResp.ErrorCode,
			Message: chatResp.ErrorMessage,
		}
	}

	return &chatResp, nil
}

// CreateChatCompletionStream 创建聊天补全流
func (c *Client) CreateChatCompletionStream(
	ctx context.Context,
	request ChatCompletionRequest,
) (stream *ChatCompletionStream, err error) {
	request.ResponseMode = "streaming"

	endpoint := c.baseURL.ResolveReference(&url.URL{Path: "/v1/chat-messages"})

	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), bytes.NewBuffer(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setRequestHeaders(httpReq)

	var resp *http.Response
	err = c.doWithRetry(func() error {
		var retryErr error
		resp, retryErr = c.httpClient.Do(httpReq)
		return retryErr
	})

	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, c.handleAPIError(resp)
	}

	stream = &ChatCompletionStream{
		streamReader: &streamReader[ChatCompletionStreamResponse]{
			emptyMessagesLimit: 100,
			reader:             bufio.NewReader(resp.Body),
			response:           resp,
			unmarshaler: func(data []byte) (ChatCompletionStreamResponse, error) {
				var response ChatCompletionStreamResponse
				err := json.Unmarshal(data, &response)
				return response, err
			},
		},
	}

	return stream, nil
}

// WorkflowRun 执行工作流
func (c *Client) WorkflowRun(ctx context.Context, request WorkflowRunRequest) (map[string]interface{}, error) {
	endpoint := c.baseURL.ResolveReference(&url.URL{Path: "/v1/workflows/run"})

	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint.String(), bytes.NewBuffer(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	c.setRequestHeaders(httpReq)

	var resp *http.Response
	err = c.doWithRetry(func() error {
		var retryErr error
		resp, retryErr = c.httpClient.Do(httpReq)
		return retryErr
	})

	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, c.handleAPIError(resp)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result, nil
}

// setRequestHeaders 设置请求头
func (c *Client) setRequestHeaders(req *http.Request) {
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.apiKey))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "text/event-stream")
}

// handleAPIError 处理API错误
func (c *Client) handleAPIError(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read error response: %w", err)
	}

	var apiErr APIError
	if err := json.Unmarshal(body, &apiErr); err != nil {
		return fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(body))
	}

	return &apiErr
}

// doWithRetry 带重试的执行
func (c *Client) doWithRetry(fn func() error) error {
	var err error
	delay := c.retryPolicy.Delay

	for i := 0; i <= c.retryPolicy.MaxRetries; i++ {
		err = fn()
		if err == nil {
			return nil
		}

		if !c.isRetryableError(err) {
			return err
		}

		if i == c.retryPolicy.MaxRetries {
			break
		}

		time.Sleep(delay)
		delay = min(delay*2, c.retryPolicy.MaxDelay)
	}

	return err
}

// isRetryableError 判断错误是否可重试
func (c *Client) isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	if strings.Contains(err.Error(), "timeout") ||
		strings.Contains(err.Error(), "network") ||
		strings.Contains(err.Error(), "5") {
		return true
	}

	return false
}

// min 返回较小的持续时间
func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
