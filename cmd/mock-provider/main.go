package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func main() {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.GET("/v1/models", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"object": "list",
			"data": []gin.H{
				{"id": "gpt-4", "object": "model", "created": 1687882411, "owned_by": "openai"},
				{"id": "gpt-4-turbo", "object": "model", "created": 1692901427, "owned_by": "openai"},
				{"id": "gpt-3.5-turbo", "object": "model", "created": 1677649963, "owned_by": "openai"},
				{"id": "text-embedding-ada-002", "object": "model", "created": 1671217299, "owned_by": "openai"},
			},
		})
	})

	r.POST("/v1/chat/completions", func(c *gin.Context) {
		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
			Stream *bool `json:"stream,omitempty"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"message": "invalid request body", "type": "invalid_request_error"},
			})
			return
		}

		now := time.Now().Unix()
		content := buildMockContent(req.Model)
		chunkID := "chatcmpl-mock-" + time.Now().Format("20060102150405")

		// 流式请求：返回 SSE 事件流
		if req.Stream != nil && *req.Stream {
			c.Header("Content-Type", "text/event-stream")
			c.Header("Cache-Control", "no-cache")
			c.Header("Connection", "keep-alive")
			c.Status(http.StatusOK)

			flusher, ok := c.Writer.(http.Flusher)
			if !ok {
				return
			}

			// 模拟逐 chunk 输出
			words := splitContent(content)
			for i, word := range words {
				chunk := gin.H{
					"id":      chunkID,
					"object":  "chat.completion.chunk",
					"created": now,
					"model":   req.Model,
					"choices": []gin.H{
						{
							"index": 0,
							"delta": gin.H{"content": word},
						},
					},
				}
				// 首个 chunk 带上 role
				if i == 0 {
					chunk["choices"].([]gin.H)[0]["delta"].(gin.H)["role"] = "assistant"
				}
				data, _ := json.Marshal(chunk)
				fmt.Fprintf(c.Writer, "data: %s\n\n", data)
				flusher.Flush()
				time.Sleep(50 * time.Millisecond) // 模拟延迟
			}

			// 最终 chunk 带上 usage 和 finish_reason
			final := gin.H{
				"id":      chunkID,
				"object":  "chat.completion.chunk",
				"created": now,
				"model":   req.Model,
				"choices": []gin.H{
					{
						"index":         0,
						"delta":         gin.H{},
						"finish_reason": "stop",
					},
				},
				"usage": gin.H{
					"prompt_tokens":     15,
					"completion_tokens": 20,
					"total_tokens":      35,
				},
			}
			finalData, _ := json.Marshal(final)
			fmt.Fprintf(c.Writer, "data: %s\n\n", finalData)
			fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"id":      "chatcmpl-mock-" + time.Now().Format("20060102150405"),
			"object":  "chat.completion",
			"created": now,
			"model":   req.Model,
			"choices": []gin.H{
				{
					"index":         0,
					"message":       gin.H{"role": "assistant", "content": content},
					"finish_reason": "stop",
				},
			},
			"usage": gin.H{
				"prompt_tokens":     15,
				"completion_tokens": 20,
				"total_tokens":      35,
			},
		})
	})

	r.POST("/v1/embeddings", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"object": "list",
			"data": []gin.H{
				{
					"object":    "embedding",
					"index":     0,
					"embedding": make([]float64, 1536),
				},
			},
			"model": "text-embedding-ada-002",
			"usage": gin.H{
				"prompt_tokens": 8,
				"total_tokens":  8,
			},
		})
	})

	addr := getEnv("LISTEN_ADDR", ":8080")
	slog.Info("mock-provider starting", "addr", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}

func buildMockContent(model string) string {
	return "Hello! This is a mock response from " + model + ". Your request has been received successfully."
}

// splitContent 将文本按单词拆分，用于模拟 SSE 流式输出。
func splitContent(content string) []string {
	words := strings.Split(content, " ")
	result := make([]string, 0, len(words))
	for i, w := range words {
		if i < len(words)-1 {
			result = append(result, w+" ")
		} else {
			result = append(result, w)
		}
	}
	return result
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
