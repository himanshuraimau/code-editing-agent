package agent

import (
	"context"
	"fmt"
	"strings"

	"code-editing-agent/internal/tools"

	openai "github.com/sashabaranov/go-openai"
)

type Agent struct {
	client         *openai.Client
	getUserMessage func() (string, bool)
	tools          []tools.ToolDefinition
}

func NewAgent(
	client *openai.Client,
	getUserMessage func() (string, bool),
	toolsList []tools.ToolDefinition,
) *Agent {
	return &Agent{
		client:         client,
		getUserMessage: getUserMessage,
		tools:          toolsList,
	}
}

func (a *Agent) Run(ctx context.Context) error {
	conversation := []openai.ChatCompletionMessage{}
	fmt.Println("Chat with OpenAI (use 'ctrl-c' to quit)")

	for {
		fmt.Print("\u001b[94mYou\u001b[0m: ")
		userInput, ok := a.getUserMessage()
		if !ok {
			break
		}

		if len(conversation) > 20 {
			conversation = conversation[len(conversation)-10:]
		}

		userMessage := openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: userInput,
		}
		conversation = append(conversation, userMessage)

		for {
			resp, err := a.runInference(ctx, conversation)
			if err != nil {
				return err
			}

			if len(resp.ToolCalls) == 0 {
				fmt.Printf("\u001b[93mAssistant\u001b[0m: %s\n", resp.Content)
				conversation = append(conversation, *resp)
				break
			}

			conversation = append(conversation, *resp)

			allToolsSuccessful := true
			for _, toolCall := range resp.ToolCalls {
				result := a.executeTool(toolCall.ID, toolCall.Function.Name, []byte(toolCall.Function.Arguments))
				toolMessage := openai.ChatCompletionMessage{
					Role:       openai.ChatMessageRoleTool,
					Content:    result,
					ToolCallID: toolCall.ID,
				}
				conversation = append(conversation, toolMessage)

				// Mark the entire tool execution as failed if any tool fails
				if strings.Contains(result, "error") || strings.Contains(result, "failed") {
					allToolsSuccessful = false
				}
			}

			if !allToolsSuccessful {
				break
			}
		}
	}
	return nil
}

func (a *Agent) executeTool(id string, name string, input []byte) string {
	var toolDef tools.ToolDefinition
	var found bool
	for _, tool := range a.tools {
		if tool.Name == name {
			toolDef = tool
			found = true
			break
		}
	}
	if !found {
		return "tool not found"
	}

	fmt.Printf("\u001b[92mtool\u001b[0m: %s(%s)\n", name, string(input))
	response, err := toolDef.Function(input)
	if err != nil {
		return err.Error()
	}
	return response
}

func (a *Agent) runInference(ctx context.Context, conversation []openai.ChatCompletionMessage,
) (*openai.ChatCompletionMessage, error) {
	openaiTools := []openai.Tool{}
	for _, tool := range a.tools {
		openaiTools = append(openaiTools, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: openai.FunctionDefinition{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		})
	}

	resp, err := a.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:     openai.GPT3Dot5Turbo,
		MaxTokens: 1024,
		Messages:  conversation,
		Tools:     openaiTools,
	})
	if err != nil {
		return nil, err
	}

	message := resp.Choices[0].Message
	return &openai.ChatCompletionMessage{
		Role:      message.Role,
		Content:   message.Content,
		ToolCalls: message.ToolCalls,
	}, nil
}
