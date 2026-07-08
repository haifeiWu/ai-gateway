package openai

// ChatCompletionRequest OpenAI 兼容的 chat completion 请求。
type ChatCompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   *int      `json:"max_tokens,omitempty"`
	Stream      *bool     `json:"stream,omitempty"`
}

// Message 聊天消息。
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatCompletionResponse OpenAI 兼容的 chat completion 响应。
type ChatCompletionResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice 聊天选择。
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage token 用量。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// EmbeddingRequest OpenAI 兼容的 embedding 请求。
type EmbeddingRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

// EmbeddingResponse OpenAI 兼容的 embedding 响应。
type EmbeddingResponse struct {
	Object string      `json:"object"`
	Data   []Embedding `json:"data"`
	Model  string      `json:"model"`
	Usage  Usage       `json:"usage"`
}

// Embedding 向量数据。
type Embedding struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

// ModelsResponse /v1/models 响应。
type ModelsResponse struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

// Model 模型信息。
type Model struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// ChatCompletionChunk SSE 流式响应的单个 chunk。
type ChatCompletionChunk struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []ChunkChoice `json:"choices"`
	Usage   *Usage       `json:"usage,omitempty"`
}

// ChunkChoice 流式 chunk 中的 choice（使用 delta 而非 message）。
type ChunkChoice struct {
	Index        int     `json:"index"`
	Delta        Delta   `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

// Delta 流式增量内容。
type Delta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// ErrorResponse OpenAI 兼容的错误响应。
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail 错误详情。
type ErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
}
