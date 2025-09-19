### Design features

#### This Dify SDK follows the design pattern of OpenAI SDK, with a particular focus on the streaming processing part:

Generic Stream Reader: Abstracts stream processing logic using generics, making code more concise and reusable

OpenAI style API: The design of the CreateChatCompletionStream method is consistent with the OpenAI SDK

Server Sent Events Processing: Properly handle SSE format, including data: prefix and [DONE] tag

Complete error handling: including detailed error information and appropriate error types

Resource management: Ensure that HTTP response bodies are properly closed to avoid resource leakage

Context support: All API calls support context.Context, which facilitates timeout and uncontrolled operations

This implementation provides a development experience similar to OpenAI SDK, while being specifically optimized for the Dify API. You can further expand other API endpoints or add more advanced features as needed.


### Usage example

``` 
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/your-org/dify-sdk/dify"
)

func main() {
	// Init
	client, err := dify.NewClient(dify.ClientConfig{
		APIKey:  "your-api-key-here",
		BaseURL: "https://api.dify.ai",
		Timeout: 60 * time.Second,
	})
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}

	// Example 1: Complete regular chat
	ctx := context.Background()
	chatReq := dify.ChatCompletionRequest{
		Inputs: map[string]interface{}{
			"prompt": "holle world",
		},
		Query:        "Please explain the basic concepts of quantum computing",
		ResponseMode: "blocking",
		User:         "user-123",
	}

	chatResp, err := client.ChatCompletion(ctx, chatReq)
	if err != nil {
		log.Fatalf("Chat completion failed: %v", err)
	}

	fmt.Printf("Conversation ID: %s\n", chatResp.ConversationID)
	fmt.Printf("Answer: %s\n", chatResp.Answer)

	// Example 2: Streaming Chat Completion
	streamReq := dify.ChatCompletionRequest{
		Inputs: map[string]interface{}{
			"prompt": "holle world",
		},
		Query:  "Please write a short article about artificial intelligence",
		User:   "user-123",
	}

	stream, err := client.CreateChatCompletionStream(ctx, streamReq)
	if err != nil {
		log.Fatalf("Failed to create chat completion stream: %v", err)
	}
	defer stream.Close()

	fmt.Println("Stream response:")
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			fmt.Println("\nStream finished")
			break
		}

		if err != nil {
			log.Printf("Stream error: %v", err)
			break
		}

		fmt.Print(response.Answer)
	}

	// Example 3: Workflow Execution
	workflowReq := dify.WorkflowRunRequest{
		Inputs: map[string]interface{}{
			"requirement": "Go Development Engineer, Beijing, 15-20K, full-time",
		},
		ResponseMode: "blocking",
		User:         "user-123",
	}

	workflowResp, err := client.WorkflowRun(ctx, workflowReq)
	if err != nil {
		log.Fatalf("Workflow run failed: %v", err)
	}

	fmt.Printf("Workflow result: %+v\n", workflowResp)
}
```