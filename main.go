package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/invopop/jsonschema"
	"github.com/joho/godotenv"
	openai "github.com/sashabaranov/go-openai"
)

// main initializes and runs the OpenAI-powered chat agent
// It handles environment loading, client setup, and user input processing
func main() {
	// Load environment variables from .env file
	err := godotenv.Load()
	if err != nil {
		fmt.Printf("Error loading .env file: %v\n", err)
	}

	// Initialize OpenAI client with API key
	client := openai.NewClient(
		os.Getenv("OPENAI_API_KEY"),
	)

	// Configure input scanner and message handler
	scanner := bufio.NewScanner(os.Stdin)
	getUserMessage := func() (string, bool) {
		if !scanner.Scan() {
			return "", false
		}
		return scanner.Text(), true
	}

	// Define available tools and initialize agent
	tools := []ToolDefinition{ReadFileDefinition, ListFilesDefinition, EditFileDefinition}
	agent := NewAgent(client, getUserMessage, tools)
	
	// Start the agent's main loop
	err = agent.Run(context.TODO())
	if err != nil {
		fmt.Printf("Error: %s\n", err.Error())
	}
}

// NewAgent creates and initializes a new Agent instance with the given parameters
func NewAgent(
	client *openai.Client,
	getUserMessage func() (string, bool),
	tools []ToolDefinition,
) *Agent {
	return &Agent{
		client:         client,
		getUserMessage: getUserMessage,
		tools:          tools,
	}
}

// Agent represents a conversational agent powered by OpenAI
// It manages the conversation flow, user input, and tool executions
type Agent struct {
	client         *openai.Client              // OpenAI API client
	getUserMessage func() (string, bool)       // Function to retrieve user input
	tools          []ToolDefinition            // Available tools for the agent to use
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

		// Reset conversation context if it grows too large
		if len(conversation) > 20 {
			// Keep only the most recent messages to maintain context
			conversation = conversation[len(conversation)-10:]
		}

		userMessage := openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleUser,
			Content: userInput,
		}
		conversation = append(conversation, userMessage)

		// Continue conversation until no more tool calls
		for {
			resp, err := a.runInference(ctx, conversation)
			if err != nil {
				return err
			}

			// If no tool calls, just print the response and wait for next user input
			if len(resp.ToolCalls) == 0 {
				fmt.Printf("\u001b[93mAssistant\u001b[0m: %s\n", resp.Content)
				conversation = append(conversation, *resp)
				break
			}

			// Add assistant message with tool calls to conversation
			conversation = append(conversation, *resp)

			// Process all tool calls
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
					allToolsSuccessful = false
				}
			}

			// If any tool failed, break out of the tool execution loop
			// and wait for next user input, otherwise continue the conversation
			// with the model to get a final response
			if !allToolsSuccessful {
				break
			}
		}
	}

	return nil
}

// executeTool handles the execution of a specific tool by name with the given arguments
// It returns the result as a string or an error message if the tool fails
func (a *Agent) executeTool(id string, name string, input []byte) string {
	// Find the requested tool definition from available tools
	var toolDef ToolDefinition
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

// runInference sends the current conversation to the OpenAI API and processes the response
// It handles the conversion between internal tool definitions and OpenAI's tool format
func (a *Agent) runInference(ctx context.Context, conversation []openai.ChatCompletionMessage,
) (*openai.ChatCompletionMessage, error) {
	// Convert tool definitions to OpenAI's format
	openaiTools := []openai.Tool{}
	for _, tool := range a.tools {
		openaiTools = append(openaiTools, openai.Tool{
			Type: openai.ToolTypeFunction,
			Function: &openai.FunctionDefinition{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  tool.InputSchema,
			},
		})
	}

	// Send the chat completion request to OpenAI API
	resp, err := a.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:     openai.GPT3Dot5Turbo,
		MaxTokens: 1024,
		Messages:  conversation,
		Tools:     openaiTools,
	})
	if err != nil {
		return nil, err
	}

	// Extract the message from the API response
	message := resp.Choices[0].Message
	return &openai.ChatCompletionMessage{
		Role:      message.Role,
		Content:   message.Content,
		ToolCalls: message.ToolCalls,
	}, nil
}

// ToolDefinition represents a callable function that can be exposed to the AI model
// It includes metadata for OpenAI's function calling interface and the actual implementation
type ToolDefinition struct {
	Name        string                                  // Tool name used for function calling
	Description string                                  // Human-readable description of the tool's purpose
	InputSchema interface{}                             // JSON schema describing the expected input parameters
	Function    func(input json.RawMessage) (string, error) // The implementation of the tool
}

// ReadFileDefinition provides access to file contents
// This allows the AI to read and analyze files in the workspace
var ReadFileDefinition = ToolDefinition{
	Name:        "read_file",
	Description: "Read the contents of a given relative file path. Use this when you want to see what's inside a file. Do not use this with directory names.",
	InputSchema: GenerateSchema[ReadFileInput](),
	Function:    ReadFile,
}

// ReadFileInput defines the parameters needed to read a file
type ReadFileInput struct {
	Path string `json:"path" jsonschema_description:"The relative path of a file in the working directory."`
}

// ReadFile reads and returns the contents of the specified file
func ReadFile(input json.RawMessage) (string, error) {
	ReadFileInput := ReadFileInput{}
	err := json.Unmarshal(input, &ReadFileInput)
	if err != nil {
		return "", err
	}
	content, err := os.ReadFile(ReadFileInput.Path)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

// GenerateSchema converts a Go struct type into a JSON schema
// This is used to generate the schema for tool parameters
func GenerateSchema[T any]() map[string]interface{} {
	reflector := jsonschema.Reflector{
		AllowAdditionalProperties: false,
		DoNotReference:            true,
	}
	var v T

	schema := reflector.Reflect(v)

	return map[string]interface{}{
		"type":       "object",
		"properties": schema.Properties,
		"required":   schema.Required,
	}
}

// ListFilesDefinition provides a tool to explore the workspace directory structure
// This allows the AI to understand what files are available in the workspace
var ListFilesDefinition = ToolDefinition{
	Name:        "list_files",
	Description: "List files and directories at a given path. If no path is provided, lists files in the current directory.",
	InputSchema: ListFilesInputSchema,
	Function:    ListFiles,
}

// ListFilesInput defines the parameters for listing files in a directory
type ListFilesInput struct {
	Path string `json:"path" jsonschema_description:"The relative path of a directory in the working directory."`
}

var ListFilesInputSchema = GenerateSchema[ListFilesInput]()

// ListFiles recursively lists all files and directories at the specified path
// Directories are indicated with a trailing slash
func ListFiles(input json.RawMessage) (string, error) {
	listFilesInput := ListFilesInput{}
	err := json.Unmarshal(input, &listFilesInput)
	if (err != nil) {
		panic(err)
	}

	// Default to current directory if no path specified
	dir := "."
	if listFilesInput.Path != "" {
		dir = listFilesInput.Path
	}

	// Walk the directory tree to collect all files and directories
	var files []string
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Calculate relative path from the starting directory
		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		// Add all paths except the root directory itself
		if relPath != "." {
			if info.IsDir() {
				files = append(files, relPath+"/")
			} else {
				files = append(files, relPath)
			}
		}
		return nil
	})

	if err != nil {
		return "", err
	}

	// Return the file list as JSON
	result, err := json.Marshal(files)
	if err != nil {
		return "", err
	}

	return string(result), nil
}

var EditFileDefinition = ToolDefinition{
	Name: "edit_file",
	Description: `Make edits to a text file.

Replaces 'old_str' with 'new_str' in the given file. 'old_str' and 'new_str' MUST be different from each other.

If the file specified with path doesn't exist, it will be created.
`,
	InputSchema: EditFileInputSchema,
	Function:    EditFile,
}

type EditFileInput struct {
	Path   string `json:"path" jsonschema_description:"The relative path of a file in the working directory."`
	OldStr string `json:"old_str" jsonschema_description:"The string to be replaced."`
	NewStr string `json:"new_str" jsonschema_description:"The string to replace with."`
}

var EditFileInputSchema = GenerateSchema[EditFileInput]()

func EditFile(input json.RawMessage) (string, error) {
	editFileInput := EditFileInput{}
	err := json.Unmarshal(input, &editFileInput)
	if err != nil {
		return "", fmt.Errorf("failed to parse edit_file input: %w", err)
	}

	// More detailed validation
	if editFileInput.Path == "" {
		return "", fmt.Errorf("path cannot be empty")
	}
	if editFileInput.OldStr == editFileInput.NewStr {
		return "", fmt.Errorf("old_str and new_str cannot be identical")
	}

	content, err := os.ReadFile(editFileInput.Path)
	if err != nil {
		if os.IsNotExist(err) && editFileInput.OldStr == "" {
			result, createErr := createNewFile(editFileInput.Path, editFileInput.NewStr)
			if createErr != nil {
				return "", createErr
			}
			fmt.Printf("\u001b[92mEdit success\u001b[0m: Created new file %s\n", editFileInput.Path)
			return result, nil
		}
		return "", fmt.Errorf("failed to read file %s: %w", editFileInput.Path, err)
	}

	oldContent := string(content)
	newContent := strings.Replace(oldContent, editFileInput.OldStr, editFileInput.NewStr, -1)

	if oldContent == newContent && editFileInput.OldStr != "" {
		return "", fmt.Errorf("old_str '%s' not found in file %s", editFileInput.OldStr, editFileInput.Path)
	}

	err = os.WriteFile(editFileInput.Path, []byte(newContent), 0644)
	if err != nil {
		return "", fmt.Errorf("failed to write to file %s: %w", editFileInput.Path, err)
	}

	fmt.Printf("\u001b[92mEdit success\u001b[0m: Updated file %s\n", editFileInput.Path)
	return "File successfully edited", nil
}

// createNewFile creates a new file at the specified path with the given content.
// It automatically creates any necessary parent directories.
//
// Parameters:
//   - filePath: relative or absolute path where the file should be created
//   - content: string content to write to the new file
//
// Returns:
//   - a success message with the file path
//   - an error if directory creation or file writing fails
func createNewFile(filePath, content string) (string, error) {
	dir := path.Dir(filePath)
	if dir != "." {
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			return "", fmt.Errorf("failed to create directory: %w", err)
		}
	}

	err := os.WriteFile(filePath, []byte(content), 0644)
	if err != nil {
		return "", fmt.Errorf("failed to create file: %w", err)
	}

	return fmt.Sprintf("Successfully created file %s", filePath), nil
}
