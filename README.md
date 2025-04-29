# Code Editing Agent

An AI-powered chat agent that can read, list, and edit files in your local workspace. This tool creates a conversational interface to your codebase, allowing you to interact with your files through natural language commands.

## Features

- Interactive chat interface with an OpenAI-powered assistant
- File operations through natural language:
  - Read file contents
  - List files in directories
  - Edit files with text replacement
  - Create new files
- Context-aware conversations that maintain history
- Clean, color-coded terminal output

## Requirements

- Go 1.18+ (for generics support)
- OpenAI API key
- The following Go packages:
  - github.com/invopop/jsonschema
  - github.com/joho/godotenv
  - github.com/sashabaranov/go-openai

## Installation

1. Clone this repository:
   ```bash
   git clone https://github.com/himanshuraimau/code-editing-agent.git
   cd code-editing-agent
   ```

2. Install the required dependencies:
   ```bash
   go mod tidy
   ```

3. Create a `.env` file in the project root with your OpenAI API key:
   ```
   OPENAI_API_KEY=your_api_key_here
   ```

## Usage

Run the application:

```bash
go run main.go
```

Once started, you'll see a chat interface where you can interact with the AI assistant. The agent supports the following operations:

### Reading Files

Ask to view the contents of a file:
```
You: Show me the contents of main.go
```

### Listing Files

Ask to see all files in a directory:
```
You: What files are in the current directory?
```

### Editing Files

Request changes to a file:
```
You: Replace "Hello World" with "Hello, AI!" in the file greeting.txt
```

### Creating Files

Create new files with content:
```
You: Create a new file called example.json with an empty JSON object
```

## How It Works

The agent uses OpenAI's function calling API to translate natural language requests into file operations. It provides the AI model with tools for reading, listing, and editing files, which the model can use to respond to your queries.

When you ask a question or make a request, the agent:

1. Sends your input to the OpenAI API
2. Interprets any tool calls requested by the model
3. Executes file operations locally
4. Returns the results back to the model
5. Provides you with the final response

## License

[MIT License](LICENSE)
